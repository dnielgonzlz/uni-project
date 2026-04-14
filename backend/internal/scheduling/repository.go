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
		          status, notes, created_at, updated_at`

	row := r.db.QueryRow(ctx, q,
		s.CoachID, s.ClientID, s.ScheduleRunID, s.StartsAt, s.EndsAt, s.Status, s.Notes,
	)
	return scanSession(row)
}

// GetSessionByID returns a session by primary key.
func (r *Repository) GetSessionByID(ctx context.Context, id uuid.UUID) (*Session, error) {
	const q = `
		SELECT id, coach_id, client_id, schedule_run_id, starts_at, ends_at,
		       status, notes, created_at, updated_at
		FROM sessions WHERE id = $1 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, id)
	s, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// ListSessionsByCoach returns active sessions for a coach, optionally filtered by status.
func (r *Repository) ListSessionsByCoach(ctx context.Context, coachID uuid.UUID, status string) ([]Session, error) {
	q := `
		SELECT id, coach_id, client_id, schedule_run_id, starts_at, ends_at,
		       status, notes, created_at, updated_at
		FROM sessions
		WHERE coach_id = $1 AND deleted_at IS NULL`

	args := []any{coachID}
	if status != "" {
		q += ` AND status = $2`
		args = append(args, status)
	}
	q += ` ORDER BY starts_at`

	return r.querySessionList(ctx, q, args...)
}

// ListSessionsByClient returns active sessions for a client, optionally filtered by status.
func (r *Repository) ListSessionsByClient(ctx context.Context, clientID uuid.UUID, status string) ([]Session, error) {
	q := `
		SELECT id, coach_id, client_id, schedule_run_id, starts_at, ends_at,
		       status, notes, created_at, updated_at
		FROM sessions
		WHERE client_id = $1 AND deleted_at IS NULL`

	args := []any{clientID}
	if status != "" {
		q += ` AND status = $2`
		args = append(args, status)
	}
	q += ` ORDER BY starts_at`

	return r.querySessionList(ctx, q, args...)
}

// ListActiveSessionsByRunID returns all proposed/confirmed sessions belonging to a run.
func (r *Repository) ListActiveSessionsByRunID(ctx context.Context, runID uuid.UUID) ([]Session, error) {
	const q = `
		SELECT id, coach_id, client_id, schedule_run_id, starts_at, ends_at,
		       status, notes, created_at, updated_at
		FROM sessions
		WHERE schedule_run_id = $1 AND deleted_at IS NULL
		ORDER BY starts_at`

	return r.querySessionList(ctx, q, runID)
}

// ListConfirmedSessionsForCoachInRange returns confirmed sessions in a date range (for solver input).
func (r *Repository) ListConfirmedSessionsForCoachInRange(ctx context.Context, coachID uuid.UUID, from, to time.Time) ([]Session, error) {
	const q = `
		SELECT id, coach_id, client_id, schedule_run_id, starts_at, ends_at,
		       status, notes, created_at, updated_at
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
		          status, notes, created_at, updated_at`

	row := r.db.QueryRow(ctx, q, id, status)
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
		row := rows
		var s Session
		if err := row.Scan(
			&s.ID, &s.CoachID, &s.ClientID, &s.ScheduleRunID,
			&s.StartsAt, &s.EndsAt, &s.Status, &s.Notes,
			&s.CreatedAt, &s.UpdatedAt,
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
