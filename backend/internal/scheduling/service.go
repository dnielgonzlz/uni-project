package scheduling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/danielgonzalez/pt-scheduler/internal/availability"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/clock"
	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

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

// Service handles session booking, schedule runs, and session credits.
type Service struct {
	repo      *Repository
	usersRepo *users.Repository
	availRepo *availability.Repository
	solver    Solver
	clock     clock.Clock
	db        *pgxpool.Pool
	notifier  Notifier // optional; nil disables notifications
}

func NewService(
	repo *Repository,
	usersRepo *users.Repository,
	availRepo *availability.Repository,
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
func (s *Service) TriggerScheduleRun(ctx context.Context, coachID uuid.UUID, req TriggerRunRequest) (*ScheduleRun, error) {
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
			ID:           coachID.String(),
			WorkingHours: toSolverTimeSlots(workingHours),
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

// ConfirmScheduleRun confirms all proposed sessions in a run in a single transaction.
func (s *Service) ConfirmScheduleRun(ctx context.Context, coachID, runID uuid.UUID) (*ScheduleRun, error) {
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

	if err := s.repo.ConfirmSessionsByRunID(ctx, runID); err != nil {
		return nil, fmt.Errorf("scheduling: confirm sessions: %w", err)
	}

	run, err = s.repo.UpdateScheduleRunStatus(ctx, runID, RunConfirmed, nil)
	if err != nil {
		return nil, fmt.Errorf("scheduling: confirm run: %w", err)
	}

	run.Sessions, _ = s.repo.ListActiveSessionsByRunID(ctx, runID)

	// Enqueue notifications for every confirmed session (non-fatal if it fails).
	if s.notifier != nil && len(run.Sessions) > 0 {
		if payloads, err := s.buildSessionNotifPayloads(ctx, coachID, run.Sessions); err == nil {
			if err := s.notifier.NotifySessionsConfirmed(ctx, payloads); err != nil {
				// Notifications are best-effort — don't fail the confirmation.
				_ = err
			}
		}
	}

	return run, nil
}

// RejectScheduleRun cancels all proposed sessions and marks the run as rejected.
func (s *Service) RejectScheduleRun(ctx context.Context, coachID, runID uuid.UUID) (*ScheduleRun, error) {
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
func (s *Service) ListSessions(ctx context.Context, coachID *uuid.UUID, clientID *uuid.UUID, status string) ([]Session, error) {
	if coachID != nil {
		return s.repo.ListSessionsByCoach(ctx, *coachID, status)
	}
	if clientID != nil {
		return s.repo.ListSessionsByClient(ctx, *clientID, status)
	}
	return nil, fmt.Errorf("scheduling: list sessions requires coachID or clientID")
}

// CancelSession cancels a session and issues a credit if enough notice was given.
func (s *Service) CancelSession(ctx context.Context, sessionID uuid.UUID, req CancelSessionRequest) (*Session, *SessionCredit, error) {
	session, err := s.repo.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}

	if session.Status != StatusProposed && session.Status != StatusConfirmed {
		return nil, nil, &ConstraintError{
			Code:    "invalid_status",
			Message: "only proposed or confirmed sessions can be cancelled",
		}
	}

	now := s.clock.Now()
	cancelled, err := s.repo.UpdateSessionStatus(ctx, sessionID, StatusCancelled)
	if err != nil {
		return nil, nil, fmt.Errorf("scheduling: cancel session: %w", err)
	}

	// Issue credit if sufficient notice was given
	var credit *SessionCredit
	if CancellationEarnsCredit(session.StartsAt, now) {
		expiresAt := now.AddDate(0, 1, 0) // credit expires in 1 month
		credit, err = s.repo.CreateSessionCredit(ctx, session.ClientID, sessionID, req.Reason, expiresAt)
		if err != nil {
			// Non-fatal: session is cancelled, credit issuance failure is logged but not returned
			_ = err
		}
	}

	// Enqueue cancellation notification (non-fatal).
	if s.notifier != nil {
		if client, err := s.usersRepo.GetClientByID(ctx, session.ClientID); err == nil {
			if clientUser, err := s.usersRepo.GetUserByID(ctx, client.UserID); err == nil {
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
					CreditIssued: credit != nil,
				})
			}
		}
	}

	return cancelled, credit, nil
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

// weeklySessionCount divides monthly sessions evenly across 4 weeks.
func weeklySessionCount(sessionsPerMonth int) int {
	_ = strconv.Itoa // silence import if unused
	wc := sessionsPerMonth / 4
	if wc < 1 {
		return 1
	}
	return wc
}
