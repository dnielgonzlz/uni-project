package scheduling

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/auth"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/validator"
	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

// Handler holds HTTP handlers for scheduling endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// TriggerScheduleRun handles POST /api/v1/schedule-runs
func (h *Handler) TriggerScheduleRun(w http.ResponseWriter, r *http.Request) {
	coachID, ok := coachIDFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	var req TriggerRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validator.Validate.Struct(req); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, "validation failed")
		return
	}

	run, err := h.svc.TriggerScheduleRun(r.Context(), coachID, req)
	if err != nil {
		if errors.Is(err, ErrInfeasible) {
			// FRONTEND: show "no valid schedule could be found — check working hours and client count"
			httpx.Error(w, http.StatusUnprocessableEntity, "no feasible schedule found — check working hours and client count")
			return
		}
		var ce *ConstraintError
		if errors.As(err, &ce) {
			httpx.Error(w, http.StatusUnprocessableEntity, ce.Message)
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to trigger schedule run")
		return
	}

	httpx.JSON(w, http.StatusCreated, run)
}

// GetScheduleRun handles GET /api/v1/schedule-runs/{runID}
func (h *Handler) GetScheduleRun(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(chi.URLParam(r, "runID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid run ID")
		return
	}

	run, err := h.svc.GetScheduleRun(r.Context(), runID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "schedule run not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to get schedule run")
		return
	}

	httpx.JSON(w, http.StatusOK, run)
}

// ConfirmScheduleRun handles POST /api/v1/schedule-runs/{runID}/confirm
func (h *Handler) ConfirmScheduleRun(w http.ResponseWriter, r *http.Request) {
	coachID, ok := coachIDFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	runID, err := uuid.Parse(chi.URLParam(r, "runID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid run ID")
		return
	}

	run, err := h.svc.ConfirmScheduleRun(r.Context(), coachID, runID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "schedule run not found")
			return
		}
		if errors.Is(err, ErrForbidden) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, ErrRunNotPending) {
			// FRONTEND: show "this schedule has already been confirmed, rejected, or expired"
			httpx.Error(w, http.StatusConflict, "schedule run is not pending confirmation")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to confirm schedule run")
		return
	}

	httpx.JSON(w, http.StatusOK, run)
}

// RejectScheduleRun handles POST /api/v1/schedule-runs/{runID}/reject
func (h *Handler) RejectScheduleRun(w http.ResponseWriter, r *http.Request) {
	coachID, ok := coachIDFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	runID, err := uuid.Parse(chi.URLParam(r, "runID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid run ID")
		return
	}

	run, err := h.svc.RejectScheduleRun(r.Context(), coachID, runID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "schedule run not found")
			return
		}
		if errors.Is(err, ErrForbidden) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, ErrRunNotPending) {
			httpx.Error(w, http.StatusConflict, "schedule run is not pending confirmation")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to reject schedule run")
		return
	}

	httpx.JSON(w, http.StatusOK, run)
}

// ListSessions handles GET /api/v1/sessions
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}
	role, _ := auth.RoleFromContext(r.Context())
	status := r.URL.Query().Get("status")

	var sessions []Session
	var err error

	switch role {
	case users.RoleCoach:
		// Coach sees all sessions across their clients
		coachID := userID // for coaches, userID maps to coach profile — resolved in service
		sessions, err = h.svc.ListSessions(r.Context(), &coachID, nil, status)
	case users.RoleClient:
		clientID := userID
		sessions, err = h.svc.ListSessions(r.Context(), nil, &clientID, status)
	default:
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	httpx.JSON(w, http.StatusOK, sessions)
}

// CancelSession handles POST /api/v1/sessions/{sessionID}/cancel
func (h *Handler) CancelSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid session ID")
		return
	}

	var req CancelSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validator.Validate.Struct(req); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, "validation failed")
		return
	}

	session, credit, err := h.svc.CancelSession(r.Context(), sessionID, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "session not found")
			return
		}
		var ce *ConstraintError
		if errors.As(err, &ce) {
			httpx.Error(w, http.StatusUnprocessableEntity, ce.Message)
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to cancel session")
		return
	}

	resp := map[string]any{"session": session}
	if credit != nil {
		// FRONTEND: show "session cancelled — a credit has been added to your account" banner
		resp["credit"] = credit
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// coachIDFromContext extracts the coach's user UUID from the request context.
// Returns false if the caller is not a coach.
func coachIDFromContext(r *http.Request) (uuid.UUID, bool) {
	role, _ := auth.RoleFromContext(r.Context())
	if role != users.RoleCoach && role != users.RoleAdmin {
		return uuid.UUID{}, false
	}
	id, ok := auth.UserIDFromContext(r.Context())
	return id, ok
}
