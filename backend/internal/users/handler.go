package users

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/validator"
)

// Handler holds the HTTP handlers for user profile endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// GetCoachProfile handles GET /api/v1/coaches/{coachID}/profile
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
