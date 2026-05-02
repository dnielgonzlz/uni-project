package scheduling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a session or schedule run does not exist.
var ErrNotFound = errors.New("not found")

// Repository handles all DB operations for sessions, schedule runs, and credits.
type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// --- Sessions ---

// CreateSession inserts a proposed session. The DB exclusion constraint will reject
// any overlap for the same coach or client.
func (r *Repository) CreateSession(ctx context.Context, s *Session) (*Session, error) {
	const q = `
		INSERT INTO sessions (coach_id, client_id, schedule_run_id, starts_at, ends_at, status, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, coach_id, client_id, schedule_run_id, starts_at, ends_at,
		          status, notes, cancellation_reason, cancellation_requested_at,
		          created_at, updated_at`

	row := r.db.QueryRow(ctx, q,
		s.CoachID, s.ClientID, s.ScheduleRunID, s.StartsAt, s.EndsAt, s.Status, s.Notes,
	)
	return scanSession(row)
}

// GetSessionByID returns a session by primary key.
func (r *Repository) GetSessionByID(ctx context.Context, id uuid.UUID) (*Session, error) {
	const q = `
		SELECT id, coach_id, client_id, schedule_run_id, starts_at, ends_at,
		       status, notes, cancellation_reason, cancellation_requested_at,
		       created_at, updated_at
		FROM sessions WHERE id = $1 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, id)
	s, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// ListSessionsByCoach returns active sessions for a coach with resolved client names.
func (r *Repository) ListSessionsByCoach(ctx context.Context, coachID uuid.UUID, status string) ([]Session, error) {
	q := `
		SELECT s.id, s.coach_id, s.client_id, s.schedule_run_id, s.starts_at, s.ends_at,
		       s.status, s.notes, s.cancellation_reason, s.cancellation_requested_at,
		       s.created_at, s.updated_at, u.full_name AS client_name
		FROM sessions s
		JOIN clients c ON c.id = s.client_id
		JOIN users u ON u.id = c.user_id
		WHERE s.coach_id = $1 AND s.deleted_at IS NULL`

	args := []any{coachID}
	if status != "" {
		q += ` AND s.status = $2`
		args = append(args, status)
	}
	q += ` ORDER BY s.starts_at`

	return r.querySessionListWithClientName(ctx, q, args...)
}

// ListSessionsByClient returns active sessions for a client with resolved coach names.
func (r *Repository) ListSessionsByClient(ctx context.Context, clientID uuid.UUID, status string) ([]Session, error) {
	q := `
		SELECT s.id, s.coach_id, s.client_id, s.schedule_run_id, s.starts_at, s.ends_at,
		       s.status, s.notes, s.cancellation_reason, s.cancellation_requested_at,
		       s.created_at, s.updated_at, u.full_name AS coach_name
		FROM sessions s
		JOIN coaches co ON co.id = s.coach_id
		JOIN users u ON u.id = co.user_id
		WHERE s.client_id = $1 AND s.deleted_at IS NULL`

	args := []any{clientID}
	if status != "" {
		q += ` AND s.status = $2`
		args = append(args, status)
	}
	q += ` ORDER BY s.starts_at`

	return r.querySessionListWithCoachName(ctx, q, args...)
}

// ListActiveSessionsByRunID returns all proposed/confirmed sessions belonging to a run, with client names.
func (r *Repository) ListActiveSessionsByRunID(ctx context.Context, runID uuid.UUID) ([]Session, error) {
	const q = `
		SELECT s.id, s.coach_id, s.client_id, s.schedule_run_id, s.starts_at, s.ends_at,
		       s.status, s.notes, s.cancellation_reason, s.cancellation_requested_at,
		       s.created_at, s.updated_at, u.full_name AS client_name
		FROM sessions s
		JOIN clients c ON c.id = s.client_id
		JOIN users u ON u.id = c.user_id
		WHERE s.schedule_run_id = $1 AND s.deleted_at IS NULL
		ORDER BY s.starts_at`

	return r.querySessionListWithClientName(ctx, q, runID)
}

// ListConfirmedSessionsForCoachInRange returns confirmed sessions in a date range (for solver input).
func (r *Repository) ListConfirmedSessionsForCoachInRange(ctx context.Context, coachID uuid.UUID, from, to time.Time) ([]Session, error) {
	const q = `
		SELECT id, coach_id, client_id, schedule_run_id, starts_at, ends_at,
		       status, notes, cancellation_reason, cancellation_requested_at,
		       created_at, updated_at
		FROM sessions
		WHERE coach_id = $1
		  AND status = 'confirmed'
		  AND starts_at >= $2 AND starts_at < $3
		  AND deleted_at IS NULL
		ORDER BY starts_at`

	return r.querySessionList(ctx, q, coachID, from, to)
}

// UpdateSessionStatus sets a session's status.
func (r *Repository) UpdateSessionStatus(ctx context.Context, id uuid.UUID, status string) (*Session, error) {
	const q = `
		UPDATE sessions SET status = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, coach_id, client_id, schedule_run_id, starts_at, ends_at,
		          status, notes, cancellation_reason, cancellation_requested_at,
		          created_at, updated_at`

	row := r.db.QueryRow(ctx, q, id, status)
	s, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// RequestCancellation sets a session to pending_cancellation and records the reason and timestamp.
func (r *Repository) RequestCancellation(ctx context.Context, id uuid.UUID, reason string, requestedAt time.Time) (*Session, error) {
	const q = `
		UPDATE sessions
		SET status                    = 'pending_cancellation',
		    cancellation_reason       = $2,
		    cancellation_requested_at = $3,
		    updated_at                = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, coach_id, client_id, schedule_run_id, starts_at, ends_at,
		          status, notes, cancellation_reason, cancellation_requested_at,
		          created_at, updated_at`

	row := r.db.QueryRow(ctx, q, id, reason, requestedAt)
	s, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// CancelSessionsByRunID soft-cancels all proposed sessions belonging to a schedule run.
func (r *Repository) CancelSessionsByRunID(ctx context.Context, runID uuid.UUID) error {
	const q = `
		UPDATE sessions
		SET status = 'cancelled', updated_at = NOW()
		WHERE schedule_run_id = $1 AND status = 'proposed' AND deleted_at IS NULL`
	_, err := r.db.Exec(ctx, q, runID)
	return err
}

// ConfirmSessionsByRunID confirms all proposed sessions in a run in a single update.
func (r *Repository) ConfirmSessionsByRunID(ctx context.Context, runID uuid.UUID) error {
	const q = `
		UPDATE sessions
		SET status = 'confirmed', updated_at = NOW()
		WHERE schedule_run_id = $1 AND status = 'proposed' AND deleted_at IS NULL`
	_, err := r.db.Exec(ctx, q, runID)
	return err
}

// CancelSessionsByIDs cancels specific sessions by primary key (used for partial run confirmation).
func (r *Repository) CancelSessionsByIDs(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	const q = `
		UPDATE sessions SET status = 'cancelled', updated_at = NOW()
		WHERE id = ANY($1) AND deleted_at IS NULL`
	_, err := r.db.Exec(ctx, q, ids)
	return err
}

// UpdateSessionTimes reschedules a session to new start/end times.
func (r *Repository) UpdateSessionTimes(ctx context.Context, id uuid.UUID, startsAt, endsAt time.Time) (*Session, error) {
	const q = `
		UPDATE sessions SET starts_at = $2, ends_at = $3, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, coach_id, client_id, schedule_run_id, starts_at, ends_at,
		          status, notes, cancellation_reason, cancellation_requested_at,
		          created_at, updated_at`
	row := r.db.QueryRow(ctx, q, id, startsAt, endsAt)
	s, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// --- Schedule runs ---

// CreateScheduleRun inserts a new schedule run record.
func (r *Repository) CreateScheduleRun(ctx context.Context, coachID uuid.UUID, weekStart time.Time, input json.RawMessage) (*ScheduleRun, error) {
	const q = `
		INSERT INTO schedule_runs (coach_id, week_start, status, solver_input)
		VALUES ($1, $2, 'pending_confirmation', $3)
		RETURNING id, coach_id, week_start, status, solver_input, solver_output, expires_at, created_at, updated_at`

	row := r.db.QueryRow(ctx, q, coachID, weekStart, input)
	return scanScheduleRun(row)
}

// GetScheduleRunByID returns a schedule run by primary key.
func (r *Repository) GetScheduleRunByID(ctx context.Context, id uuid.UUID) (*ScheduleRun, error) {
	const q = `
		SELECT id, coach_id, week_start, status, solver_input, solver_output, expires_at, created_at, updated_at
		FROM schedule_runs WHERE id = $1`

	row := r.db.QueryRow(ctx, q, id)
	run, err := scanScheduleRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return run, err
}

// UpdateScheduleRunStatus updates the run status and optionally stores solver output.
func (r *Repository) UpdateScheduleRunStatus(ctx context.Context, id uuid.UUID, status string, output json.RawMessage) (*ScheduleRun, error) {
	const q = `
		UPDATE schedule_runs
		SET status = $2, solver_output = $3, updated_at = NOW()
		WHERE id = $1
		RETURNING id, coach_id, week_start, status, solver_input, solver_output, expires_at, created_at, updated_at`

	row := r.db.QueryRow(ctx, q, id, status, output)
	run, err := scanScheduleRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return run, err
}

// ExpireOldRuns marks pending runs older than their expires_at as expired.
// Called on startup and periodically.
func (r *Repository) ExpireOldRuns(ctx context.Context) (int64, error) {
	const q = `
		UPDATE schedule_runs
		SET status = 'expired', updated_at = NOW()
		WHERE status = 'pending_confirmation' AND expires_at < NOW()`

	tag, err := r.db.Exec(ctx, q)
	return tag.RowsAffected(), err
}

// --- Session credits ---

// CreateSessionCredit records a credit earned from a cancelled session.
func (r *Repository) CreateSessionCredit(ctx context.Context, clientID, sourceSessionID uuid.UUID, reason string, expiresAt time.Time) (*SessionCredit, error) {
	const q = `
		INSERT INTO session_credits (client_id, reason, source_session_id, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, client_id, reason, source_session_id, used_session_id, expires_at, created_at`

	var sc SessionCredit
	err := r.db.QueryRow(ctx, q, clientID, reason, sourceSessionID, expiresAt).Scan(
		&sc.ID, &sc.ClientID, &sc.Reason, &sc.SourceSessionID, &sc.UsedSessionID, &sc.ExpiresAt, &sc.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scheduling: create credit: %w", err)
	}
	return &sc, nil
}

// --- helpers ---

func (r *Repository) querySessionList(ctx context.Context, q string, args ...any) ([]Session, error) {
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("scheduling: query sessions: %w", err)
	}
	defer rows.Close()

	var result []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(
			&s.ID, &s.CoachID, &s.ClientID, &s.ScheduleRunID,
			&s.StartsAt, &s.EndsAt, &s.Status, &s.Notes,
			&s.CancellationReason, &s.CancellationRequestedAt,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scheduling: scan session: %w", err)
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (r *Repository) querySessionListWithClientName(ctx context.Context, q string, args ...any) ([]Session, error) {
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("scheduling: query sessions: %w", err)
	}
	defer rows.Close()

	var result []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(
			&s.ID, &s.CoachID, &s.ClientID, &s.ScheduleRunID,
			&s.StartsAt, &s.EndsAt, &s.Status, &s.Notes,
			&s.CancellationReason, &s.CancellationRequestedAt,
			&s.CreatedAt, &s.UpdatedAt, &s.ClientName,
		); err != nil {
			return nil, fmt.Errorf("scheduling: scan session: %w", err)
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (r *Repository) querySessionListWithCoachName(ctx context.Context, q string, args ...any) ([]Session, error) {
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("scheduling: query sessions: %w", err)
	}
	defer rows.Close()

	var result []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(
			&s.ID, &s.CoachID, &s.ClientID, &s.ScheduleRunID,
			&s.StartsAt, &s.EndsAt, &s.Status, &s.Notes,
			&s.CancellationReason, &s.CancellationRequestedAt,
			&s.CreatedAt, &s.UpdatedAt, &s.CoachName,
		); err != nil {
			return nil, fmt.Errorf("scheduling: scan session: %w", err)
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func scanSession(row pgx.Row) (*Session, error) {
	var s Session
	err := row.Scan(
		&s.ID, &s.CoachID, &s.ClientID, &s.ScheduleRunID,
		&s.StartsAt, &s.EndsAt, &s.Status, &s.Notes,
		&s.CancellationReason, &s.CancellationRequestedAt,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scheduling: scan session: %w", err)
	}
	return &s, nil
}

func scanScheduleRun(row pgx.Row) (*ScheduleRun, error) {
	var run ScheduleRun
	err := row.Scan(
		&run.ID, &run.CoachID, &run.WeekStart, &run.Status,
		&run.SolverInput, &run.SolverOutput, &run.ExpiresAt,
		&run.CreatedAt, &run.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scheduling: scan run: %w", err)
	}
	return &run, nil
}
