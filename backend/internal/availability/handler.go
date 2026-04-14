package availability

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/validator"
)

// Handler holds HTTP handlers for availability endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// GetWorkingHours handles GET /api/v1/coaches/{coachID}/availability
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
func (h *Handler) GetPreferredWindows(w http.ResponseWriter, r *http.Request) {
	clientID, err := uuid.Parse(chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid client ID")
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
func (h *Handler) SetPreferredWindows(w http.ResponseWriter, r *http.Request) {
	clientID, err := uuid.Parse(chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid client ID")
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
