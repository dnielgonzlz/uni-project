package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrAlreadyPaid is returned when a monthly payment already exists for a client.
var ErrAlreadyPaid = errors.New("payment already exists for this billing period")

// ErrDuplicateWebhook is returned when an event has already been processed.
var ErrDuplicateWebhook = errors.New("webhook event already processed")

// Repository handles DB operations for billing.
type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

// --- Payments ---

// CreatePayment inserts a new payment record. Returns ErrAlreadyPaid if the
// idempotency key already exists (safe to call concurrently).
func (r *Repository) CreatePayment(ctx context.Context, p *Payment) (*Payment, error) {
	const q = `
		INSERT INTO payments
		  (client_id, provider, amount_pence, currency, billing_year, billing_month, status, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id, client_id, provider, provider_ref, amount_pence, currency,
		          billing_year, billing_month, status, idempotency_key, created_at, updated_at`

	row := r.db.QueryRow(ctx, q,
		p.ClientID, p.Provider, p.AmountPence, p.Currency,
		p.BillingYear, p.BillingMonth, p.Status, p.IdempotencyKey,
	)
	result, err := scanPayment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAlreadyPaid
	}
	return result, err
}

// GetPaymentByProviderRef finds a payment by its external provider reference.
func (r *Repository) GetPaymentByProviderRef(ctx context.Context, provider, ref string) (*Payment, error) {
	const q = `
		SELECT id, client_id, provider, provider_ref, amount_pence, currency,
		       billing_year, billing_month, status, idempotency_key, created_at, updated_at
		FROM payments WHERE provider = $1 AND provider_ref = $2`

	row := r.db.QueryRow(ctx, q, provider, ref)
	p, err := scanPayment(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("billing: payment not found: %s/%s", provider, ref)
	}
	return p, err
}

// UpdatePaymentStatus sets the status and provider reference on a payment.
func (r *Repository) UpdatePaymentStatus(ctx context.Context, id uuid.UUID, status string, providerRef *string) error {
	const q = `
		UPDATE payments
		SET status = $2, provider_ref = COALESCE($3, provider_ref), updated_at = NOW()
		WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id, status, providerRef)
	return err
}

// ListPaymentsByClient returns all payments for a client ordered by billing period descending.
func (r *Repository) ListPaymentsByClient(ctx context.Context, clientID uuid.UUID) ([]Payment, error) {
	const q = `
		SELECT id, client_id, provider, provider_ref, amount_pence, currency,
		       billing_year, billing_month, status, idempotency_key, created_at, updated_at
		FROM payments WHERE client_id = $1
		ORDER BY billing_year DESC, billing_month DESC`

	rows, err := r.db.Query(ctx, q, clientID)
	if err != nil {
		return nil, fmt.Errorf("billing: list payments: %w", err)
	}
	defer rows.Close()

	var result []Payment
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *p)
	}
	return result, rows.Err()
}

// --- GoCardless mandates ---

// UpsertMandate inserts or updates a GoCardless mandate for a client.
func (r *Repository) UpsertMandate(ctx context.Context, clientID uuid.UUID, mandateID, status string) (*Mandate, error) {
	const q = `
		INSERT INTO gocardless_mandates (client_id, mandate_id, status)
		VALUES ($1, $2, $3)
		ON CONFLICT (client_id) DO UPDATE
		  SET mandate_id = EXCLUDED.mandate_id, status = EXCLUDED.status, updated_at = NOW()
		RETURNING id, client_id, mandate_id, status, created_at, updated_at`

	var m Mandate
	err := r.db.QueryRow(ctx, q, clientID, mandateID, status).Scan(
		&m.ID, &m.ClientID, &m.MandateID, &m.Status, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("billing: upsert mandate: %w", err)
	}
	return &m, nil
}

// GetMandateByClientID returns the active GoCardless mandate for a client.
func (r *Repository) GetMandateByClientID(ctx context.Context, clientID uuid.UUID) (*Mandate, error) {
	const q = `
		SELECT id, client_id, mandate_id, status, created_at, updated_at
		FROM gocardless_mandates WHERE client_id = $1
		ORDER BY created_at DESC LIMIT 1`

	var m Mandate
	err := r.db.QueryRow(ctx, q, clientID).Scan(
		&m.ID, &m.ClientID, &m.MandateID, &m.Status, &m.CreatedAt, &m.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("billing: no mandate for client %s", clientID)
	}
	if err != nil {
		return nil, fmt.Errorf("billing: get mandate: %w", err)
	}
	return &m, nil
}

// --- Webhook idempotency ---

// RecordWebhookEvent inserts a webhook event record. Returns ErrDuplicateWebhook
// if the (provider, event_id) pair has already been seen.
func (r *Repository) RecordWebhookEvent(ctx context.Context, provider, eventID string, payload []byte) error {
	const q = `
		INSERT INTO webhook_events (provider, event_id, payload)
		VALUES ($1, $2, $3)
		ON CONFLICT (provider, event_id) DO NOTHING`

	tag, err := r.db.Exec(ctx, q, provider, eventID, payload)
	if err != nil {
		return fmt.Errorf("billing: record webhook: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrDuplicateWebhook
	}
	return nil
}

// --- Bacs advance notice ---

// BacsEarliestChargeDate returns the earliest date a new Bacs payment can be
// submitted, accounting for the 3 working day advance notice requirement.
// Subsequent payments on an active mandate only need 2 working days.
func BacsEarliestChargeDate(from time.Time, isFirstPayment bool) time.Time {
	days := 3
	if !isFirstPayment {
		days = 2
	}
	d := from
	added := 0
	for added < days {
		d = d.AddDate(0, 0, 1)
		// Skip weekends (Bacs is Mon–Fri only). Bank holidays not modelled for MVP.
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			added++
		}
	}
	return d
}

// --- scan helpers ---

func scanPayment(row interface {
	Scan(...any) error
}) (*Payment, error) {
	var p Payment
	err := row.Scan(
		&p.ID, &p.ClientID, &p.Provider, &p.ProviderRef, &p.AmountPence, &p.Currency,
		&p.BillingYear, &p.BillingMonth, &p.Status, &p.IdempotencyKey,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("billing: scan payment: %w", err)
	}
	return &p, nil
}
