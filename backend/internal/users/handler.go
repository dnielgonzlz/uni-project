package users

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/audit"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/ctxkeys"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/validator"
)

// Handler holds the HTTP handlers for user profile endpoints.
type Handler struct {
	svc    *Service
	audit  *audit.Logger
	logger *slog.Logger
}

func NewHandler(svc *Service, auditLog *audit.Logger, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, audit: auditLog, logger: logger}
}

// ListCoachClients handles GET /api/v1/coaches/me/clients
//
//	@Summary      List coach clients
//	@Description  Returns all clients belonging to the currently authenticated coach, including confirmed session counts.
//	@Tags         coaches
//	@Produce      json
//	@Success      200  {array}   CoachClientSummary
//	@Failure      401  {object}  httpx.ErrorResponse
//	@Failure      403  {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /coaches/me/clients [get]
func (h *Handler) ListCoachClients(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	clients, err := h.svc.ListCoachClients(r.Context(), coachUserID)
	if err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	httpx.JSON(w, http.StatusOK, clients)
}

// DeleteCoachClient handles DELETE /api/v1/coaches/me/clients/{clientID}
//
//	@Summary      Delete coach client
//	@Description  Soft-deletes a client owned by the authenticated coach and removes future active sessions from active views.
//	@Tags         coaches
//	@Param        clientID  path      string  true  "Client UUID"
//	@Success      204
//	@Failure      400  {object}  httpx.ErrorResponse
//	@Failure      401  {object}  httpx.ErrorResponse
//	@Failure      403  {object}  httpx.ErrorResponse
//	@Failure      404  {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /coaches/me/clients/{clientID} [delete]
func (h *Handler) DeleteCoachClient(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	clientID, err := uuid.Parse(chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid client ID")
		return
	}

	if err := h.svc.DeleteCoachClient(r.Context(), coachUserID, clientID); err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "client not found")
			return
		}
		if errors.Is(err, ErrForbidden) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	ip := audit.IPFromRequest(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	h.audit.Log(r.Context(), &coachUserID, audit.ActionClientDeleted, "client", &clientID, nil, ip)

	w.WriteHeader(http.StatusNoContent)
}

// GetCoachProfile handles GET /api/v1/coaches/{coachID}/profile
//
//	@Summary      Get coach profile
//	@Description  Returns the public profile of a coach, including business name and user details.
//	@Tags         coaches
//	@Produce      json
//	@Param        coachID  path      string  true  "Coach UUID"
//	@Success      200      {object}  CoachProfile
//	@Failure      400      {object}  httpx.ErrorResponse
//	@Failure      404      {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /coaches/{coachID}/profile [get]
func (h *Handler) GetCoachProfile(w http.ResponseWriter, r *http.Request) {
	coachID, err := uuid.Parse(chi.URLParam(r, "coachID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid coach ID")
		return
	}

	profile, err := h.svc.GetCoachProfile(r.Context(), coachID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "coach not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to get profile")
		return
	}

	httpx.JSON(w, http.StatusOK, profile)
}

// UpdateCoachProfile handles PUT /api/v1/coaches/{coachID}/profile
//
//	@Summary      Update coach profile
//	@Description  Updates the coach's business name, full name, phone, and timezone.
//	@Tags         coaches
//	@Accept       json
//	@Produce      json
//	@Param        coachID  path      string             true  "Coach UUID"
//	@Param        body     body      UpdateCoachRequest true  "Updated fields"
//	@Success      200      {object}  CoachProfile
//	@Failure      400      {object}  httpx.ErrorResponse
//	@Failure      404      {object}  httpx.ErrorResponse
//	@Failure      422      {object}  httpx.ValidationErrorResponse
//	@Security     BearerAuth
//	@Router       /coaches/{coachID}/profile [put]
func (h *Handler) UpdateCoachProfile(w http.ResponseWriter, r *http.Request) {
	coachID, err := uuid.Parse(chi.URLParam(r, "coachID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid coach ID")
		return
	}

	var req UpdateCoachRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validator.Validate.Struct(req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":  "validation failed",
			"fields": validator.ValidationErrors(err),
		})
		return
	}

	profile, err := h.svc.UpdateCoachProfile(r.Context(), coachID, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "coach not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to update profile")
		return
	}

	httpx.JSON(w, http.StatusOK, profile)
}

// GetClientProfile handles GET /api/v1/clients/{clientID}/profile
//
//	@Summary      Get client profile
//	@Description  Returns a client's profile including their sessions-per-month quota and priority score.
//	@Tags         clients
//	@Produce      json
//	@Param        clientID  path      string  true  "Client UUID"
//	@Success      200       {object}  ClientProfile
//	@Failure      400       {object}  httpx.ErrorResponse
//	@Failure      404       {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /clients/{clientID}/profile [get]
func (h *Handler) GetClientProfile(w http.ResponseWriter, r *http.Request) {
	clientID, err := uuid.Parse(chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid client ID")
		return
	}

	profile, err := h.svc.GetClientProfile(r.Context(), clientID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "client not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to get profile")
		return
	}

	httpx.JSON(w, http.StatusOK, profile)
}

// UpdateClientProfile handles PUT /api/v1/clients/{clientID}/profile
//
//	@Summary      Update client profile
//	@Description  Updates a client's full name, phone, and timezone.
//	@Tags         clients
//	@Accept       json
//	@Produce      json
//	@Param        clientID  path      string              true  "Client UUID"
//	@Param        body      body      UpdateClientRequest true  "Updated fields"
//	@Success      200       {object}  ClientProfile
//	@Failure      400       {object}  httpx.ErrorResponse
//	@Failure      404       {object}  httpx.ErrorResponse
//	@Failure      422       {object}  httpx.ValidationErrorResponse
//	@Security     BearerAuth
//	@Router       /clients/{clientID}/profile [put]
func (h *Handler) UpdateClientProfile(w http.ResponseWriter, r *http.Request) {
	clientID, err := uuid.Parse(chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid client ID")
		return
	}

	var req UpdateClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validator.Validate.Struct(req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":  "validation failed",
			"fields": validator.ValidationErrors(err),
		})
		return
	}

	profile, err := h.svc.UpdateClientProfile(r.Context(), clientID, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "client not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to update profile")
		return
	}

	httpx.JSON(w, http.StatusOK, profile)
}

// ExportMyData handles GET /api/v1/me/export
//
//	@Summary      Export my data (GDPR)
//	@Description  Returns all data held for the currently authenticated user as a JSON document (GDPR right of access).
//	@Tags         gdpr
//	@Produce      json
//	@Success      200  {object}  DataExport
//	@Failure      401  {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /me/export [get]
func (h *Handler) ExportMyData(w http.ResponseWriter, r *http.Request) {
	userID := ctxkeys.UserIDFromContext(r.Context())
	if userID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	export, err := h.svc.ExportUserData(r.Context(), userID)
	if err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	ip := audit.IPFromRequest(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	h.audit.Log(r.Context(), &userID, audit.ActionDataExport, "user", &userID, nil, ip)

	httpx.JSON(w, http.StatusOK, export)
}
