package scheduling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danielgonzalez/pt-scheduler/internal/availability"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/clock"
	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

// sessionStore is the subset of Repository methods used by Service.
// Defining it as an interface allows the service to be tested with fakes.
type sessionStore interface {
	CreateScheduleRun(ctx context.Context, coachID uuid.UUID, weekStart time.Time, input json.RawMessage) (*ScheduleRun, error)
	GetScheduleRunByID(ctx context.Context, id uuid.UUID) (*ScheduleRun, error)
	UpdateScheduleRunStatus(ctx context.Context, id uuid.UUID, status string, output json.RawMessage) (*ScheduleRun, error)
	CreateSession(ctx context.Context, s *Session) (*Session, error)
	ListActiveSessionsByRunID(ctx context.Context, runID uuid.UUID) ([]Session, error)
	ListConfirmedSessionsForCoachInRange(ctx context.Context, coachID uuid.UUID, from, to time.Time) ([]Session, error)
	ConfirmSessionsByRunID(ctx context.Context, runID uuid.UUID) error
	CancelSessionsByRunID(ctx context.Context, runID uuid.UUID) error
	CancelSessionsByIDs(ctx context.Context, ids []uuid.UUID) error
	UpdateSessionTimes(ctx context.Context, id uuid.UUID, startsAt, endsAt time.Time) (*Session, error)
	ListSessionsByCoach(ctx context.Context, coachID uuid.UUID, status string) ([]Session, error)
	ListSessionsByClient(ctx context.Context, clientID uuid.UUID, status string) ([]Session, error)
	GetSessionByID(ctx context.Context, id uuid.UUID) (*Session, error)
	UpdateSessionStatus(ctx context.Context, id uuid.UUID, status string) (*Session, error)
	RequestCancellation(ctx context.Context, id uuid.UUID, reason string, requestedAt time.Time) (*Session, error)
	CreateSessionCredit(ctx context.Context, clientID, sourceSessionID uuid.UUID, reason string, expiresAt time.Time) (*SessionCredit, error)
}

// userLookup is the subset of users.Repository methods used by Service.
type userLookup interface {
	GetClientsByCoachID(ctx context.Context, coachID uuid.UUID) ([]users.Client, error)
	GetCoachByID(ctx context.Context, coachID uuid.UUID) (*users.Coach, error)
	GetCoachByUserID(ctx context.Context, userID uuid.UUID) (*users.Coach, error)
	GetClientByID(ctx context.Context, clientID uuid.UUID) (*users.Client, error)
	GetClientByUserID(ctx context.Context, userID uuid.UUID) (*users.Client, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (*users.User, error)
}

// availLookup is the subset of availability.Repository methods used by Service.
type availLookup interface {
	GetWorkingHours(ctx context.Context, coachID uuid.UUID) ([]availability.WorkingHours, error)
	GetPreferredWindows(ctx context.Context, clientID uuid.UUID) ([]availability.PreferredWindow, error)
}

// ErrInfeasible is returned when the solver cannot find any valid schedule.
var ErrInfeasible = errors.New("no feasible schedule found for the given constraints")

// ErrRunNotPending is returned when trying to confirm/reject a run that is not pending.
var ErrRunNotPending = errors.New("schedule run is not pending confirmation")

// ErrForbidden is returned when a user tries to act on a resource they don't own.
var ErrForbidden = errors.New("forbidden")

// Notifier is implemented by the messaging package and called after key events.
// Using an interface keeps the scheduling package free of messaging imports.
type Notifier interface {
	NotifySessionsConfirmed(ctx context.Context, sessions []SessionNotifPayload) error
	NotifySessionCancelled(ctx context.Context, p CancelNotifPayload) error
	NotifyCancellationPending(ctx context.Context, p PendingCancellationNotifPayload) error
}

// SessionNotifPayload carries the data the notification service needs to
// send booking confirmation + reminder messages to a client.
type SessionNotifPayload struct {
	SessionID   uuid.UUID
	ClientID    uuid.UUID
	CoachID     uuid.UUID
	StartsAt    time.Time
	ClientName  string
	ClientEmail string
	ClientPhone string
	CoachName   string
	CoachEmail  string
}

// CancelNotifPayload carries data for a session cancellation notification.
type CancelNotifPayload struct {
	SessionID    uuid.UUID
	ClientID     uuid.UUID
	StartsAt     time.Time
	ClientName   string
	ClientEmail  string
	ClientPhone  string
	CreditIssued bool
}

// PendingCancellationNotifPayload carries data sent to the coach when a client
// requests cancellation inside the 24h window and the coach must decide.
type PendingCancellationNotifPayload struct {
	SessionID          uuid.UUID
	ClientID           uuid.UUID
	CoachID            uuid.UUID
	StartsAt           time.Time
	ClientName         string
	CancellationReason string
	CoachName          string
	CoachEmail         string
	CoachPhone         string
}

// Service handles session booking, schedule runs, and session credits.
type Service struct {
	repo      sessionStore
	usersRepo userLookup
	availRepo availLookup
	solver    Solver
	clock     clock.Clock
	db        *pgxpool.Pool
	notifier  Notifier // optional; nil disables notifications
}

func NewService(
	repo sessionStore,
	usersRepo userLookup,
	availRepo availLookup,
	solver Solver,
	clk clock.Clock,
	db *pgxpool.Pool,
) *Service {
	return &Service{
		repo:      repo,
		usersRepo: usersRepo,
		availRepo: availRepo,
		solver:    solver,
		clock:     clk,
		db:        db,
	}
}

// WithNotifier attaches a notification dispatcher to the service.
// Call this after NewService to enable post-confirmation notifications.
func (s *Service) WithNotifier(n Notifier) {
	s.notifier = n
}

// TriggerScheduleRun loads coach/client data, calls the solver, and persists proposed sessions.
func (s *Service) TriggerScheduleRun(ctx context.Context, coachUserID uuid.UUID, req TriggerRunRequest) (*ScheduleRun, error) {
	// Resolve the JWT user UUID to the coach profile UUID used in all FK relations.
	coachRec, err := s.usersRepo.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, fmt.Errorf("scheduling: resolve coach: %w", err)
	}
	coachID := coachRec.ID

	weekStart, err := time.Parse("2006-01-02", req.WeekStart)
	if err != nil {
		return nil, fmt.Errorf("scheduling: parse week_start: %w", err)
	}
	weekStart = weekStart.UTC()
	weekEnd := weekStart.AddDate(0, 0, 7)

	// Load coach working hours
	workingHours, err := s.availRepo.GetWorkingHours(ctx, coachID)
	if err != nil {
		return nil, fmt.Errorf("scheduling: load working hours: %w", err)
	}
	if len(workingHours) == 0 {
		return nil, &ConstraintError{Code: "no_working_hours", Message: "coach has no working hours configured"}
	}

	// Load active clients for this coach
	clientRows, err := s.usersRepo.GetClientsByCoachID(ctx, coachID)
	if err != nil {
		return nil, fmt.Errorf("scheduling: load clients: %w", err)
	}
	if len(clientRows) == 0 {
		return nil, &ConstraintError{Code: "no_clients", Message: "coach has no active clients"}
	}

	// Load existing confirmed sessions for the week (belt-and-suspenders alongside DB constraints)
	existingSessions, err := s.repo.ListConfirmedSessionsForCoachInRange(ctx, coachID, weekStart, weekEnd)
	if err != nil {
		return nil, fmt.Errorf("scheduling: load existing sessions: %w", err)
	}

	// Build solver request
	solverReq := SolverRequest{
		WeekStart: req.WeekStart,
		Coach: SolverCoach{
			ID:                coachID.String(),
			WorkingHours:      toSolverTimeSlots(workingHours),
			MaxSessionsPerDay: coachRec.MaxSessionsPerDay,
		},
		ExistingSessions: toSolverSessions(existingSessions),
	}

	for _, client := range clientRows {
		windows, _ := s.availRepo.GetPreferredWindows(ctx, client.ID)

		// Derive weekly session count: sessions_per_month / 4, minimum 1
		weeklyCount := client.SessionsPerMonth / 4
		if weeklyCount < 1 {
			weeklyCount = 1
		}

		solverReq.Clients = append(solverReq.Clients, SolverClient{
			ID:               client.ID.String(),
			SessionCount:     weeklyCount,
			PriorityScore:    client.PriorityScore,
			PreferredWindows: toSolverWindowSlots(windows),
		})
	}

	// Store solver input for audit purposes
	inputJSON, _ := json.Marshal(solverReq)

	run, err := s.repo.CreateScheduleRun(ctx, coachID, weekStart, json.RawMessage(inputJSON))
	if err != nil {
		return nil, fmt.Errorf("scheduling: create run: %w", err)
	}

	// Call solver (30s timeout already enforced by HTTPSolver)
	solverResp, err := s.solver.Solve(ctx, solverReq)
	if err != nil {
		// Mark run as rejected so it doesn't hang as pending
		_, _ = s.repo.UpdateScheduleRunStatus(ctx, run.ID, RunRejected, nil)
		return nil, fmt.Errorf("scheduling: solver error: %w", err)
	}

	if solverResp.Status == "infeasible" {
		_, _ = s.repo.UpdateScheduleRunStatus(ctx, run.ID, RunRejected, nil)
		return nil, ErrInfeasible
	}

	// Persist proposed sessions
	runID := run.ID
	for _, ss := range solverResp.Sessions {
		clientID, err := uuid.Parse(ss.ClientID)
		if err != nil {
			continue
		}
		startsAt, err := time.Parse(time.RFC3339, ss.StartsAt)
		if err != nil {
			continue
		}
		endsAt := startsAt.Add(SessionDuration)

		if _, err := s.repo.CreateSession(ctx, &Session{
			CoachID:       coachID,
			ClientID:      clientID,
			ScheduleRunID: &runID,
			StartsAt:      startsAt,
			EndsAt:        endsAt,
			Status:        StatusProposed,
		}); err != nil {
			// DB exclusion constraint caught a conflict — solver bug
			return nil, fmt.Errorf("scheduling: persist session (solver produced overlapping sessions): %w", err)
		}
	}

	// Store solver output
	outputJSON, _ := json.Marshal(solverResp)
	run, err = s.repo.UpdateScheduleRunStatus(ctx, run.ID, RunPendingConfirmation, json.RawMessage(outputJSON))
	if err != nil {
		return nil, fmt.Errorf("scheduling: update run status: %w", err)
	}

	// Attach proposed sessions to the response
	run.Sessions, _ = s.repo.ListActiveSessionsByRunID(ctx, run.ID)
	return run, nil
}

// GetScheduleRun returns a run with its proposed sessions.
// Marks expired runs on read.
func (s *Service) GetScheduleRun(ctx context.Context, runID uuid.UUID) (*ScheduleRun, error) {
	run, err := s.repo.GetScheduleRunByID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("scheduling: get run: %w", err)
	}

	// Lazy expiry check
	if run.Status == RunPendingConfirmation && s.clock.Now().After(run.ExpiresAt) {
		run, _ = s.repo.UpdateScheduleRunStatus(ctx, runID, RunExpired, nil)
	}

	run.Sessions, _ = s.repo.ListActiveSessionsByRunID(ctx, runID)
	return run, nil
}

// ConfirmScheduleRun confirms proposed sessions in a run. Sessions whose IDs appear in
// excludedIDs are cancelled instead of confirmed, allowing partial confirmation.
func (s *Service) ConfirmScheduleRun(ctx context.Context, coachUserID, runID uuid.UUID, excludedIDs []uuid.UUID) (*ScheduleRun, error) {
	coachRec, err := s.usersRepo.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, ErrForbidden
	}
	coachID := coachRec.ID

	run, err := s.repo.GetScheduleRunByID(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.CoachID != coachID {
		return nil, ErrForbidden
	}
	if run.Status != RunPendingConfirmation {
		return nil, ErrRunNotPending
	}

	// Cancel excluded sessions before confirming the rest.
	if len(excludedIDs) > 0 {
		if err := s.repo.CancelSessionsByIDs(ctx, excludedIDs); err != nil {
			return nil, fmt.Errorf("scheduling: cancel excluded sessions: %w", err)
		}
	}

	if err := s.repo.ConfirmSessionsByRunID(ctx, runID); err != nil {
		return nil, fmt.Errorf("scheduling: confirm sessions: %w", err)
	}

	run, err = s.repo.UpdateScheduleRunStatus(ctx, runID, RunConfirmed, nil)
	if err != nil {
		return nil, fmt.Errorf("scheduling: confirm run: %w", err)
	}

	run.Sessions, _ = s.repo.ListActiveSessionsByRunID(ctx, runID)

	// Enqueue notifications for confirmed sessions only (non-fatal if it fails).
	if s.notifier != nil && len(run.Sessions) > 0 {
		confirmed := make([]Session, 0, len(run.Sessions))
		for _, sess := range run.Sessions {
			if sess.Status == StatusConfirmed {
				confirmed = append(confirmed, sess)
			}
		}
		if payloads, err := s.buildSessionNotifPayloads(ctx, coachID, confirmed); err == nil {
			if err := s.notifier.NotifySessionsConfirmed(ctx, payloads); err != nil {
				_ = err
			}
		}
	}

	return run, nil
}

// UpdateSession reschedules a confirmed session to new start/end times.
// Only the owning coach can call this.
func (s *Service) UpdateSession(ctx context.Context, coachUserID, sessionID uuid.UUID, req UpdateSessionRequest) (*Session, error) {
	session, err := s.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if err := s.assertIsCoachForSession(ctx, coachUserID, session); err != nil {
		return nil, err
	}

	if session.Status != StatusConfirmed {
		return nil, &ConstraintError{Code: "invalid_status", Message: "only confirmed sessions can be rescheduled"}
	}

	startsAt, err := time.Parse(time.RFC3339, req.StartsAt)
	if err != nil {
		return nil, &ConstraintError{Code: "invalid_starts_at", Message: "starts_at must be a valid RFC3339 timestamp"}
	}
	endsAt, err := time.Parse(time.RFC3339, req.EndsAt)
	if err != nil {
		return nil, &ConstraintError{Code: "invalid_ends_at", Message: "ends_at must be a valid RFC3339 timestamp"}
	}

	if !endsAt.After(startsAt) {
		return nil, &ConstraintError{Code: "invalid_times", Message: "ends_at must be after starts_at"}
	}

	return s.repo.UpdateSessionTimes(ctx, sessionID, startsAt, endsAt)
}

// RejectScheduleRun cancels all proposed sessions and marks the run as rejected.
func (s *Service) RejectScheduleRun(ctx context.Context, coachUserID, runID uuid.UUID) (*ScheduleRun, error) {
	coachRec, err := s.usersRepo.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return nil, ErrForbidden
	}
	coachID := coachRec.ID

	run, err := s.repo.GetScheduleRunByID(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.CoachID != coachID {
		return nil, ErrForbidden
	}
	if run.Status != RunPendingConfirmation {
		return nil, ErrRunNotPending
	}

	if err := s.repo.CancelSessionsByRunID(ctx, runID); err != nil {
		return nil, fmt.Errorf("scheduling: cancel sessions: %w", err)
	}

	run, err = s.repo.UpdateScheduleRunStatus(ctx, runID, RunRejected, nil)
	if err != nil {
		return nil, fmt.Errorf("scheduling: reject run: %w", err)
	}

	return run, nil
}

// ListSessions returns sessions for a coach (all clients) or a specific client.
// coachUserID and clientUserID are users.id values from the JWT; this function
// resolves them to the correct FK UUIDs before querying.
func (s *Service) ListSessions(ctx context.Context, coachUserID *uuid.UUID, clientUserID *uuid.UUID, status string) ([]Session, error) {
	if coachUserID != nil {
		coach, err := s.usersRepo.GetCoachByUserID(ctx, *coachUserID)
		if err != nil {
			return nil, fmt.Errorf("scheduling: resolve coach: %w", err)
		}
		return s.repo.ListSessionsByCoach(ctx, coach.ID, status)
	}
	if clientUserID != nil {
		client, err := s.usersRepo.GetClientByUserID(ctx, *clientUserID)
		if err != nil {
			return nil, fmt.Errorf("scheduling: resolve client: %w", err)
		}
		return s.repo.ListSessionsByClient(ctx, client.ID, status)
	}
	return nil, fmt.Errorf("scheduling: list sessions requires coachID or clientID")
}

// CancelSession handles a cancellation request from a client.
//
// Outside the 24h window: immediately cancels and issues a session credit.
// Inside the 24h window:  moves to "pending_cancellation" and notifies the coach,
// who must approve (session lost) or waive (credit issued) via separate endpoints.
func (s *Service) CancelSession(ctx context.Context, sessionID uuid.UUID, req CancelSessionRequest) (*CancelSessionResponse, error) {
	session, err := s.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if session.Status != StatusProposed && session.Status != StatusConfirmed {
		return nil, &ConstraintError{
			Code:    "invalid_status",
			Message: "only proposed or confirmed sessions can be cancelled",
		}
	}

	now := s.clock.Now()
	withinWindow := !CancellationEarnsCredit(session.StartsAt, now)

	// --- Inside 24h window: request pending coach review ---
	if withinWindow {
		pending, err := s.repo.RequestCancellation(ctx, sessionID, req.Reason, now)
		if err != nil {
			return nil, fmt.Errorf("scheduling: request cancellation: %w", err)
		}

		// Notify the coach (non-fatal).
		if s.notifier != nil {
			s.notifyCoachPendingCancellation(ctx, pending, req.Reason)
		}

		return &CancelSessionResponse{
			Session:      pending,
			WithinWindow: true,
			Message:      "Your cancellation request has been sent to your coach. Because it is within 24 hours of the session, they will decide whether the session is waived or lost.",
		}, nil
	}

	// --- Outside 24h window: cancel immediately with credit ---
	cancelled, err := s.repo.UpdateSessionStatus(ctx, sessionID, StatusCancelled)
	if err != nil {
		return nil, fmt.Errorf("scheduling: cancel session: %w", err)
	}

	expiresAt := now.AddDate(0, 1, 0)
	credit, err := s.repo.CreateSessionCredit(ctx, session.ClientID, sessionID, req.Reason, expiresAt)
	if err != nil {
		credit = nil
	}

	if s.notifier != nil {
		s.notifyClientCancelled(ctx, cancelled, credit != nil)
	}

	return &CancelSessionResponse{
		Session:      cancelled,
		Credit:       credit,
		WithinWindow: false,
		Message:      "Session cancelled. A credit has been added to your account.",
	}, nil
}

// ApproveCancellation is called by the coach to confirm the client loses the session.
// The session is cancelled and no credit is issued.
func (s *Service) ApproveCancellation(ctx context.Context, coachUserID, sessionID uuid.UUID) (*Session, error) {
	session, err := s.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if session.Status != StatusPendingCancellation {
		return nil, &ConstraintError{
			Code:    "invalid_status",
			Message: "session is not awaiting cancellation review",
		}
	}

	if err := s.assertIsCoachForSession(ctx, coachUserID, session); err != nil {
		return nil, err
	}

	cancelled, err := s.repo.UpdateSessionStatus(ctx, sessionID, StatusCancelled)
	if err != nil {
		return nil, fmt.Errorf("scheduling: approve cancellation: %w", err)
	}

	// Notify client: session lost, no credit.
	if s.notifier != nil {
		s.notifyClientCancelled(ctx, cancelled, false)
	}

	return cancelled, nil
}

// WaiveCancellation is called by the coach to waive the 24h policy.
// The session is cancelled and a session credit is issued to the client.
func (s *Service) WaiveCancellation(ctx context.Context, coachUserID, sessionID uuid.UUID) (*Session, *SessionCredit, error) {
	session, err := s.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}

	if session.Status != StatusPendingCancellation {
		return nil, nil, &ConstraintError{
			Code:    "invalid_status",
			Message: "session is not awaiting cancellation review",
		}
	}

	if err := s.assertIsCoachForSession(ctx, coachUserID, session); err != nil {
		return nil, nil, err
	}

	cancelled, err := s.repo.UpdateSessionStatus(ctx, sessionID, StatusCancelled)
	if err != nil {
		return nil, nil, fmt.Errorf("scheduling: waive cancellation: %w", err)
	}

	reason := ""
	if session.CancellationReason != nil {
		reason = *session.CancellationReason
	}
	expiresAt := s.clock.Now().AddDate(0, 1, 0)
	credit, err := s.repo.CreateSessionCredit(ctx, session.ClientID, sessionID, reason, expiresAt)
	if err != nil {
		credit = nil
	}

	if s.notifier != nil {
		s.notifyClientCancelled(ctx, cancelled, credit != nil)
	}

	return cancelled, credit, nil
}

// --- private notification helpers ---

func (s *Service) notifyCoachPendingCancellation(ctx context.Context, session *Session, reason string) {
	coach, err := s.usersRepo.GetCoachByID(ctx, session.CoachID)
	if err != nil {
		return
	}
	coachUser, err := s.usersRepo.GetUserByID(ctx, coach.UserID)
	if err != nil {
		return
	}
	client, err := s.usersRepo.GetClientByID(ctx, session.ClientID)
	if err != nil {
		return
	}
	clientUser, err := s.usersRepo.GetUserByID(ctx, client.UserID)
	if err != nil {
		return
	}
	coachPhone := ""
	if coachUser.PhoneE164 != nil {
		coachPhone = *coachUser.PhoneE164
	}
	_ = s.notifier.NotifyCancellationPending(ctx, PendingCancellationNotifPayload{
		SessionID:          session.ID,
		ClientID:           session.ClientID,
		CoachID:            session.CoachID,
		StartsAt:           session.StartsAt,
		ClientName:         clientUser.FullName,
		CancellationReason: reason,
		CoachName:          coachUser.FullName,
		CoachEmail:         coachUser.Email,
		CoachPhone:         coachPhone,
	})
}

func (s *Service) notifyClientCancelled(ctx context.Context, session *Session, creditIssued bool) {
	client, err := s.usersRepo.GetClientByID(ctx, session.ClientID)
	if err != nil {
		return
	}
	clientUser, err := s.usersRepo.GetUserByID(ctx, client.UserID)
	if err != nil {
		return
	}
	phone := ""
	if clientUser.PhoneE164 != nil {
		phone = *clientUser.PhoneE164
	}
	_ = s.notifier.NotifySessionCancelled(ctx, CancelNotifPayload{
		SessionID:    session.ID,
		ClientID:     session.ClientID,
		StartsAt:     session.StartsAt,
		ClientName:   clientUser.FullName,
		ClientEmail:  clientUser.Email,
		ClientPhone:  phone,
		CreditIssued: creditIssued,
	})
}

// assertIsCoachForSession checks that the given user ID maps to the coach on this session.
func (s *Service) assertIsCoachForSession(ctx context.Context, coachUserID uuid.UUID, session *Session) error {
	coach, err := s.usersRepo.GetCoachByUserID(ctx, coachUserID)
	if err != nil {
		return ErrForbidden
	}
	if coach.ID != session.CoachID {
		return ErrForbidden
	}
	return nil
}

// --- helpers ---

// buildSessionNotifPayloads fetches user contact info for each session so the
// notification service can send emails/SMS without its own DB access.
func (s *Service) buildSessionNotifPayloads(ctx context.Context, coachID uuid.UUID, sessions []Session) ([]SessionNotifPayload, error) {
	coach, err := s.usersRepo.GetCoachByID(ctx, coachID)
	if err != nil {
		return nil, err
	}
	coachUser, err := s.usersRepo.GetUserByID(ctx, coach.UserID)
	if err != nil {
		return nil, err
	}

	payloads := make([]SessionNotifPayload, 0, len(sessions))
	for _, sess := range sessions {
		client, err := s.usersRepo.GetClientByID(ctx, sess.ClientID)
		if err != nil {
			continue
		}
		clientUser, err := s.usersRepo.GetUserByID(ctx, client.UserID)
		if err != nil {
			continue
		}
		phone := ""
		if clientUser.PhoneE164 != nil {
			phone = *clientUser.PhoneE164
		}
		payloads = append(payloads, SessionNotifPayload{
			SessionID:   sess.ID,
			ClientID:    sess.ClientID,
			CoachID:     coachID,
			StartsAt:    sess.StartsAt,
			ClientName:  clientUser.FullName,
			ClientEmail: clientUser.Email,
			ClientPhone: phone,
			CoachName:   coachUser.FullName,
			CoachEmail:  coachUser.Email,
		})
	}
	return payloads, nil
}

func toSolverTimeSlots(hours []availability.WorkingHours) []SolverTimeSlot {
	slots := make([]SolverTimeSlot, len(hours))
	for i, h := range hours {
		slots[i] = SolverTimeSlot{
			DayOfWeek: h.DayOfWeek,
			StartTime: h.StartTime,
			EndTime:   h.EndTime,
		}
	}
	return slots
}

func toSolverWindowSlots(windows []availability.PreferredWindow) []SolverTimeSlot {
	slots := make([]SolverTimeSlot, len(windows))
	for i, w := range windows {
		slots[i] = SolverTimeSlot{
			DayOfWeek: w.DayOfWeek,
			StartTime: w.StartTime,
			EndTime:   w.EndTime,
		}
	}
	return slots
}

func toSolverSessions(sessions []Session) []SolverSession {
	ss := make([]SolverSession, len(sessions))
	for i, s := range sessions {
		ss[i] = SolverSession{
			ClientID: s.ClientID.String(),
			StartsAt: s.StartsAt.Format(time.RFC3339),
			EndsAt:   s.EndsAt.Format(time.RFC3339),
		}
	}
	return ss
}

