package availability

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/ctxkeys"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/validator"
	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

// preferencesAccess authorises GET/PUT on client preferred windows (coach read vs client write).
type preferencesAccess interface {
	AssertClientPreferencesAccess(ctx context.Context, actorUserID uuid.UUID, role string, clientID uuid.UUID, write bool) error
}

// Handler holds HTTP handlers for availability endpoints.
type Handler struct {
	svc      *Service
	prefAuth preferencesAccess
}

func NewHandler(svc *Service, prefAuth preferencesAccess) *Handler {
	return &Handler{svc: svc, prefAuth: prefAuth}
}

// GetWorkingHours handles GET /api/v1/coaches/{coachID}/availability
//
//	@Summary      Get coach working hours
//	@Description  Returns the coach's declared working hours for each day of the week.
//	@Tags         availability
//	@Produce      json
//	@Param        coachID  path      string  true  "Coach UUID"
//	@Success      200      {array}   WorkingHours
//	@Failure      400      {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /coaches/{coachID}/availability [get]
func (h *Handler) GetWorkingHours(w http.ResponseWriter, r *http.Request) {
	coachID, err := uuid.Parse(chi.URLParam(r, "coachID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid coach ID")
		return
	}

	hours, err := h.svc.GetWorkingHours(r.Context(), coachID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to get working hours")
		return
	}

	httpx.JSON(w, http.StatusOK, hours)
}

// SetWorkingHours handles PUT /api/v1/coaches/{coachID}/availability
//
//	@Summary      Set coach working hours
//	@Description  Replaces all working-hour slots for the coach. Provide the full weekly schedule on every call.
//	@Tags         availability
//	@Accept       json
//	@Produce      json
//	@Param        coachID  path      string                true  "Coach UUID"
//	@Param        body     body      SetWorkingHoursRequest true  "Weekly schedule"
//	@Success      200      {array}   WorkingHours
//	@Failure      400      {object}  httpx.ErrorResponse
//	@Failure      422      {object}  httpx.ValidationErrorResponse
//	@Security     BearerAuth
//	@Router       /coaches/{coachID}/availability [put]
func (h *Handler) SetWorkingHours(w http.ResponseWriter, r *http.Request) {
	coachID, err := uuid.Parse(chi.URLParam(r, "coachID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid coach ID")
		return
	}

	var req SetWorkingHoursRequest
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

	hours, err := h.svc.SetWorkingHours(r.Context(), coachID, req)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to set working hours")
		return
	}

	httpx.JSON(w, http.StatusOK, hours)
}

// GetPreferredWindows handles GET /api/v1/clients/{clientID}/preferences
//
//	@Summary      Get client preferred windows
//	@Description  Returns the time windows the client prefers for their sessions.
//	@Tags         availability
//	@Produce      json
//	@Param        clientID  path      string  true  "Client UUID"
//	@Success      200       {array}   PreferredWindow
//	@Failure      400       {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /clients/{clientID}/preferences [get]
func (h *Handler) GetPreferredWindows(w http.ResponseWriter, r *http.Request) {
	clientID, err := uuid.Parse(chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid client ID")
		return
	}

	actorID := ctxkeys.UserIDFromContext(r.Context())
	role := ctxkeys.RoleFromContext(r.Context())
	if err := h.prefAuth.AssertClientPreferencesAccess(r.Context(), actorID, role, clientID, false); err != nil {
		if errors.Is(err, users.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "client not found")
			return
		}
		if errors.Is(err, users.ErrForbidden) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to verify access")
		return
	}

	windows, err := h.svc.GetPreferredWindows(r.Context(), clientID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to get preferences")
		return
	}

	httpx.JSON(w, http.StatusOK, windows)
}

// SetPreferredWindows handles PUT /api/v1/clients/{clientID}/preferences
//
//	@Summary      Set client preferred windows
//	@Description  Replaces all preferred time windows for the client. Provide the full set on every call.
//	@Tags         availability
//	@Accept       json
//	@Produce      json
//	@Param        clientID  path      string                 true  "Client UUID"
//	@Param        body      body      SetPreferencesRequest  true  "Preferred windows"
//	@Success      200       {array}   PreferredWindow
//	@Failure      400       {object}  httpx.ErrorResponse
//	@Failure      422       {object}  httpx.ValidationErrorResponse
//	@Security     BearerAuth
//	@Router       /clients/{clientID}/preferences [put]
func (h *Handler) SetPreferredWindows(w http.ResponseWriter, r *http.Request) {
	clientID, err := uuid.Parse(chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid client ID")
		return
	}

	actorID := ctxkeys.UserIDFromContext(r.Context())
	role := ctxkeys.RoleFromContext(r.Context())
	if err := h.prefAuth.AssertClientPreferencesAccess(r.Context(), actorID, role, clientID, true); err != nil {
		if errors.Is(err, users.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "client not found")
			return
		}
		if errors.Is(err, users.ErrForbidden) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to verify access")
		return
	}

	var req SetPreferencesRequest
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

	windows, err := h.svc.SetPreferredWindows(r.Context(), clientID, req)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to set preferences")
		return
	}

	httpx.JSON(w, http.StatusOK, windows)
}
