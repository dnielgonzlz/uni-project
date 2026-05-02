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
//
//	@Summary      Trigger a schedule run
//	@Description  Runs the OR-Tools solver for the given week and returns proposed sessions for the coach to review.
//	@Tags         scheduling
//	@Accept       json
//	@Produce      json
//	@Param        body  body      TriggerRunRequest  true  "Week start date (Monday, YYYY-MM-DD)"
//	@Success      201   {object}  ScheduleRun
//	@Failure      400   {object}  httpx.ErrorResponse
//	@Failure      403   {object}  httpx.ErrorResponse
//	@Failure      422   {object}  httpx.ErrorResponse  "Infeasible schedule or constraint violation"
//	@Security     BearerAuth
//	@Router       /schedule-runs [post]
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
//
//	@Summary      Get a schedule run
//	@Description  Returns a schedule run and its proposed or confirmed sessions.
//	@Tags         scheduling
//	@Produce      json
//	@Param        runID  path      string  true  "Schedule run UUID"
//	@Success      200    {object}  ScheduleRun
//	@Failure      400    {object}  httpx.ErrorResponse
//	@Failure      404    {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /schedule-runs/{runID} [get]
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
//
//	@Summary      Confirm a schedule run
//	@Description  Coach approves the proposed schedule. All proposed sessions become confirmed. Notifications are enqueued.
//	@Tags         scheduling
//	@Produce      json
//	@Param        runID  path      string  true  "Schedule run UUID"
//	@Success      200    {object}  ScheduleRun
//	@Failure      400    {object}  httpx.ErrorResponse
//	@Failure      403    {object}  httpx.ErrorResponse
//	@Failure      404    {object}  httpx.ErrorResponse
//	@Failure      409    {object}  httpx.ErrorResponse  "Run is not pending confirmation"
//	@Security     BearerAuth
//	@Router       /schedule-runs/{runID}/confirm [post]
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

	var req ConfirmRunRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body is optional

	excludedIDs := make([]uuid.UUID, 0, len(req.ExcludedSessionIDs))
	for _, raw := range req.ExcludedSessionIDs {
		if id, err := uuid.Parse(raw); err == nil {
			excludedIDs = append(excludedIDs, id)
		}
	}

	run, err := h.svc.ConfirmScheduleRun(r.Context(), coachID, runID, excludedIDs)
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
//
//	@Summary      Reject a schedule run
//	@Description  Coach rejects the proposed schedule. All proposed sessions are soft-deleted.
//	@Tags         scheduling
//	@Produce      json
//	@Param        runID  path      string  true  "Schedule run UUID"
//	@Success      200    {object}  ScheduleRun
//	@Failure      400    {object}  httpx.ErrorResponse
//	@Failure      403    {object}  httpx.ErrorResponse
//	@Failure      404    {object}  httpx.ErrorResponse
//	@Failure      409    {object}  httpx.ErrorResponse  "Run is not pending confirmation"
//	@Security     BearerAuth
//	@Router       /schedule-runs/{runID}/reject [post]
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
//
//	@Summary      List sessions
//	@Description  Coaches see all sessions across their clients. Clients see their own sessions. Filter by status with ?status=proposed|confirmed|cancelled|completed.
//	@Tags         scheduling
//	@Produce      json
//	@Param        status  query     string  false  "Filter by session status"  Enums(proposed, confirmed, cancelled, completed)
//	@Success      200     {array}   Session
//	@Failure      401     {object}  httpx.ErrorResponse
//	@Failure      403     {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /sessions [get]
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
//
//	@Summary      Cancel a session
//	@Description  Outside 24h window: cancels immediately and issues a credit. Inside 24h window: moves to pending_cancellation and notifies the coach to decide. Check within_24h_window in the response to know which path was taken.
//	@Tags         scheduling
//	@Accept       json
//	@Produce      json
//	@Param        sessionID  path      string               true  "Session UUID"
//	@Param        body       body      CancelSessionRequest true  "Cancellation reason"
//	@Success      200        {object}  CancelSessionResponse
//	@Failure      400        {object}  httpx.ErrorResponse
//	@Failure      404        {object}  httpx.ErrorResponse
//	@Failure      422        {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /sessions/{sessionID}/cancel [post]
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

	resp, err := h.svc.CancelSession(r.Context(), sessionID, req)
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

	// FRONTEND: if resp.within_24h_window is true, show:
	//   "Your request has been sent to your coach. They will decide whether the session is waived or lost."
	// If false and resp.credit is present, show:
	//   "Session cancelled. A credit has been added to your account."
	httpx.JSON(w, http.StatusOK, resp)
}

// ApproveCancellation handles POST /api/v1/sessions/{sessionID}/cancel/approve
//
//	@Summary      Approve cancellation (session lost)
//	@Description  Coach confirms the client loses the session. No credit is issued. Only valid when session is in pending_cancellation status.
//	@Tags         scheduling
//	@Produce      json
//	@Param        sessionID  path      string  true  "Session UUID"
//	@Success      200        {object}  Session
//	@Failure      400        {object}  httpx.ErrorResponse
//	@Failure      403        {object}  httpx.ErrorResponse
//	@Failure      404        {object}  httpx.ErrorResponse
//	@Failure      422        {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /sessions/{sessionID}/cancel/approve [post]
func (h *Handler) ApproveCancellation(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid session ID")
		return
	}

	coachID, ok := coachIDFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	session, err := h.svc.ApproveCancellation(r.Context(), coachID, sessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "session not found")
			return
		}
		if errors.Is(err, ErrForbidden) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		var ce *ConstraintError
		if errors.As(err, &ce) {
			httpx.Error(w, http.StatusUnprocessableEntity, ce.Message)
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to approve cancellation")
		return
	}

	httpx.JSON(w, http.StatusOK, session)
}

// WaiveCancellation handles POST /api/v1/sessions/{sessionID}/cancel/waive
//
//	@Summary      Waive cancellation policy (credit issued)
//	@Description  Coach waives the 24h policy. Session is cancelled and a session credit is issued to the client. Only valid when session is in pending_cancellation status.
//	@Tags         scheduling
//	@Produce      json
//	@Param        sessionID  path      string  true  "Session UUID"
//	@Success      200        {object}  CancelSessionResponse
//	@Failure      400        {object}  httpx.ErrorResponse
//	@Failure      403        {object}  httpx.ErrorResponse
//	@Failure      404        {object}  httpx.ErrorResponse
//	@Failure      422        {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /sessions/{sessionID}/cancel/waive [post]
func (h *Handler) WaiveCancellation(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid session ID")
		return
	}

	coachID, ok := coachIDFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	session, credit, err := h.svc.WaiveCancellation(r.Context(), coachID, sessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "session not found")
			return
		}
		if errors.Is(err, ErrForbidden) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		var ce *ConstraintError
		if errors.As(err, &ce) {
			httpx.Error(w, http.StatusUnprocessableEntity, ce.Message)
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to waive cancellation")
		return
	}

	// FRONTEND: show "Policy waived. The client has been notified and their credit has been issued."
	httpx.JSON(w, http.StatusOK, CancelSessionResponse{Session: session, Credit: credit, WithinWindow: true})
}

// UpdateSession handles PUT /api/v1/sessions/{sessionID}
//
//	@Summary      Reschedule a session
//	@Description  Coach updates the start and end time of a confirmed session.
//	@Tags         scheduling
//	@Accept       json
//	@Produce      json
//	@Param        sessionID  path      string               true  "Session UUID"
//	@Param        body       body      UpdateSessionRequest true  "New times"
//	@Success      200        {object}  Session
//	@Failure      400        {object}  httpx.ErrorResponse
//	@Failure      403        {object}  httpx.ErrorResponse
//	@Failure      404        {object}  httpx.ErrorResponse
//	@Failure      422        {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /sessions/{sessionID} [put]
func (h *Handler) UpdateSession(w http.ResponseWriter, r *http.Request) {
	coachID, ok := coachIDFromContext(r)
	if !ok {
		httpx.Error(w, http.StatusForbidden, "forbidden")
		return
	}

	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid session ID")
		return
	}

	var req UpdateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validator.Validate.Struct(req); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, "validation failed")
		return
	}

	session, err := h.svc.UpdateSession(r.Context(), coachID, sessionID, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "session not found")
			return
		}
		if errors.Is(err, ErrForbidden) {
			httpx.Error(w, http.StatusForbidden, "forbidden")
			return
		}
		var ce *ConstraintError
		if errors.As(err, &ce) {
			httpx.Error(w, http.StatusUnprocessableEntity, ce.Message)
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to update session")
		return
	}

	httpx.JSON(w, http.StatusOK, session)
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
