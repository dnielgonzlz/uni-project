package agent

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/ctxkeys"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/validator"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	svc    *Service
	logger *slog.Logger
}

func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	settings, err := h.svc.GetSettings(r.Context(), coachUserID)
	if err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}
	httpx.JSON(w, http.StatusOK, settings)
}

func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	var req UpdateSettingsRequest
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

	settings, err := h.svc.UpdateSettings(r.Context(), coachUserID, req)
	if err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}
	httpx.JSON(w, http.StatusOK, settings)
}

func (h *Handler) CheckTemplateStatus(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	result, err := h.svc.CheckTemplateStatus(r.Context(), coachUserID)
	if err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}

func (h *Handler) ListClients(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	clients, err := h.svc.ListClients(r.Context(), coachUserID)
	if err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}
	httpx.JSON(w, http.StatusOK, clients)
}

func (h *Handler) GetOverview(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	overview, err := h.svc.GetOverview(r.Context(), coachUserID)
	if err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}
	httpx.JSON(w, http.StatusOK, overview)
}

func (h *Handler) RunDueCampaigns(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RunDueCampaigns(r.Context(), time.Now()); err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) SendCampaignNow(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	if err := h.svc.SendCampaignNow(r.Context(), coachUserID, time.Now()); err != nil {
		if errors.Is(err, ErrAgentNotReady) {
			httpx.Error(w, http.StatusConflict, "agent must be enabled with an approved WhatsApp template before sending")
			return
		}
		httpx.InternalError(w, r, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UpdateClientEnabled(w http.ResponseWriter, r *http.Request) {
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

	var req UpdateAgentClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	client, err := h.svc.UpdateClientEnabled(r.Context(), coachUserID, clientID, req.Enabled)
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		httpx.InternalError(w, r, h.logger, err)
		return
	}
	httpx.JSON(w, http.StatusOK, client)
}
