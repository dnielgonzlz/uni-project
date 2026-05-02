package availability

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository handles DB operations for availability data.
type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// --- Working hours ---

// GetWorkingHours returns all working hour rows for a coach.
func (r *Repository) GetWorkingHours(ctx context.Context, coachID uuid.UUID) ([]WorkingHours, error) {
	const q = `
		SELECT id, coach_id, day_of_week, start_time::text, end_time::text, created_at, updated_at
		FROM trainer_working_hours
		WHERE coach_id = $1
		ORDER BY day_of_week, start_time`

	rows, err := r.db.Query(ctx, q, coachID)
	if err != nil {
		return nil, fmt.Errorf("availability: get working hours: %w", err)
	}
	defer rows.Close()

	var result []WorkingHours
	for rows.Next() {
		var wh WorkingHours
		if err := rows.Scan(&wh.ID, &wh.CoachID, &wh.DayOfWeek, &wh.StartTime, &wh.EndTime, &wh.CreatedAt, &wh.UpdatedAt); err != nil {
			return nil, fmt.Errorf("availability: scan working hours: %w", err)
		}
		result = append(result, wh)
	}
	return result, rows.Err()
}

// ReplaceWorkingHours deletes all existing rows for a coach and inserts the new set atomically.
func (r *Repository) ReplaceWorkingHours(ctx context.Context, coachID uuid.UUID, entries []WorkingHoursEntry) ([]WorkingHours, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("availability: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM trainer_working_hours WHERE coach_id = $1`, coachID); err != nil {
		return nil, fmt.Errorf("availability: delete working hours: %w", err)
	}

	const ins = `
		INSERT INTO trainer_working_hours (coach_id, day_of_week, start_time, end_time)
		VALUES ($1, $2, $3::time, $4::time)
		RETURNING id, coach_id, day_of_week, start_time::text, end_time::text, created_at, updated_at`

	var result []WorkingHours
	for _, e := range entries {
		var wh WorkingHours
		err := tx.QueryRow(ctx, ins, coachID, e.DayOfWeek, e.StartTime, e.EndTime).Scan(
			&wh.ID, &wh.CoachID, &wh.DayOfWeek, &wh.StartTime, &wh.EndTime, &wh.CreatedAt, &wh.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("availability: insert working hours: %w", err)
		}
		result = append(result, wh)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("availability: commit working hours: %w", err)
	}
	return result, nil
}

// --- Client preferred windows ---

// GetPreferredWindows returns all preferred windows for a client.
func (r *Repository) GetPreferredWindows(ctx context.Context, clientID uuid.UUID) ([]PreferredWindow, error) {
	const q = `
		SELECT id, client_id, day_of_week, start_time::text, end_time::text,
		       source, collected_at, created_at
		FROM client_preferred_windows
		WHERE client_id = $1
		ORDER BY day_of_week, start_time`

	rows, err := r.db.Query(ctx, q, clientID)
	if err != nil {
		return nil, fmt.Errorf("availability: get preferred windows: %w", err)
	}
	defer rows.Close()

	var result []PreferredWindow
	for rows.Next() {
		var pw PreferredWindow
		if err := rows.Scan(
			&pw.ID, &pw.ClientID, &pw.DayOfWeek, &pw.StartTime, &pw.EndTime,
			&pw.Source, &pw.CollectedAt, &pw.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("availability: scan preferred window: %w", err)
		}
		result = append(result, pw)
	}
	return result, rows.Err()
}

// UpsertSMSWindows inserts or replaces SMS-sourced preferred windows for a client.
// Manual windows are preserved.
func (r *Repository) UpsertSMSWindows(ctx context.Context, clientID interface{ String() string }, entries []PreferredWindowEntry) ([]PreferredWindow, error) {
	return r.UpsertTwilioWindows(ctx, clientID, "sms", entries)
}

// UpsertTwilioWindows inserts or replaces Twilio-sourced preferred windows for a client.
// Manual windows and other Twilio channels are preserved.
func (r *Repository) UpsertTwilioWindows(ctx context.Context, clientID interface{ String() string }, source string, entries []PreferredWindowEntry) ([]PreferredWindow, error) {
	if source != "sms" && source != "whatsapp" {
		return nil, fmt.Errorf("availability: invalid Twilio source %q", source)
	}

	id, err := uuid.Parse(clientID.String())
	if err != nil {
		return nil, fmt.Errorf("availability: invalid client id: %w", err)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("availability: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM client_preferred_windows WHERE client_id = $1 AND source = $2`,
		id, source,
	); err != nil {
		return nil, fmt.Errorf("availability: delete %s windows: %w", source, err)
	}

	const ins = `
		INSERT INTO client_preferred_windows (client_id, day_of_week, start_time, end_time, source)
		VALUES ($1, $2, $3::time, $4::time, $5)
		RETURNING id, client_id, day_of_week, start_time::text, end_time::text,
		          source, collected_at, created_at`

	var result []PreferredWindow
	for _, e := range entries {
		var pw PreferredWindow
		err := tx.QueryRow(ctx, ins, id, e.DayOfWeek, e.StartTime, e.EndTime, source).Scan(
			&pw.ID, &pw.ClientID, &pw.DayOfWeek, &pw.StartTime, &pw.EndTime,
			&pw.Source, &pw.CollectedAt, &pw.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("availability: insert %s window: %w", source, err)
		}
		result = append(result, pw)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("availability: commit %s windows: %w", source, err)
	}
	return result, nil
}

// ReplacePreferredWindows atomically replaces all manual-source windows for a client.
func (r *Repository) ReplacePreferredWindows(ctx context.Context, clientID uuid.UUID, entries []PreferredWindowEntry) ([]PreferredWindow, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("availability: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Only replace manually-set windows; preserve SMS-sourced ones.
	if _, err := tx.Exec(ctx,
		`DELETE FROM client_preferred_windows WHERE client_id = $1 AND source = 'manual'`,
		clientID,
	); err != nil {
		return nil, fmt.Errorf("availability: delete preferred windows: %w", err)
	}

	const ins = `
		INSERT INTO client_preferred_windows (client_id, day_of_week, start_time, end_time, source)
		VALUES ($1, $2, $3::time, $4::time, 'manual')
		RETURNING id, client_id, day_of_week, start_time::text, end_time::text,
		          source, collected_at, created_at`

	var result []PreferredWindow
	for _, e := range entries {
		var pw PreferredWindow
		err := tx.QueryRow(ctx, ins, clientID, e.DayOfWeek, e.StartTime, e.EndTime).Scan(
			&pw.ID, &pw.ClientID, &pw.DayOfWeek, &pw.StartTime, &pw.EndTime,
			&pw.Source, &pw.CollectedAt, &pw.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("availability: insert preferred window: %w", err)
		}
		result = append(result, pw)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("availability: commit preferred windows: %w", err)
	}
	return result, nil
}
