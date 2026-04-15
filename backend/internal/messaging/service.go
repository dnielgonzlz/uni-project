package messaging

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/danielgonzalez/pt-scheduler/internal/scheduling"
	"github.com/google/uuid"
)

// NotificationService enqueues outbox events and delivers them.
// It is the single entry point for all notification logic; callers
// (scheduling service, billing service) only call Enqueue* methods.
// The worker calls Deliver to actually send.
type NotificationService struct {
	outbox *OutboxRepository
	email  *EmailService
	sms    *SMSService
	logger *slog.Logger
}

func NewNotificationService(
	outbox *OutboxRepository,
	email *EmailService,
	sms *SMSService,
	logger *slog.Logger,
) *NotificationService {
	return &NotificationService{outbox: outbox, email: email, sms: sms, logger: logger}
}

// --- scheduling.Notifier interface implementation ---

// NotifySessionsConfirmed implements scheduling.Notifier.
// Called by the scheduling service after a schedule run is confirmed.
func (n *NotificationService) NotifySessionsConfirmed(ctx context.Context, sessions []scheduling.SessionNotifPayload) error {
	for _, s := range sessions {
		if err := n.EnqueueSessionConfirmed(ctx, SessionConfirmedPayload{
			SessionID:   s.SessionID,
			ClientID:    s.ClientID,
			CoachID:     s.CoachID,
			StartsAt:    s.StartsAt,
			ClientName:  s.ClientName,
			ClientPhone: s.ClientPhone,
			ClientEmail: s.ClientEmail,
			CoachName:   s.CoachName,
			CoachEmail:  s.CoachEmail,
		}); err != nil {
			return err
		}
	}
	return nil
}

// NotifySessionCancelled implements scheduling.Notifier.
// Called by the scheduling service when a session is cancelled.
func (n *NotificationService) NotifySessionCancelled(ctx context.Context, p scheduling.CancelNotifPayload) error {
	return n.EnqueueSessionCancelled(ctx, SessionCancelledPayload{
		SessionID:    p.SessionID,
		ClientID:     p.ClientID,
		StartsAt:     p.StartsAt,
		ClientName:   p.ClientName,
		ClientPhone:  p.ClientPhone,
		ClientEmail:  p.ClientEmail,
		CreditIssued: p.CreditIssued,
	})
}

// --- Enqueue helpers (called transactionally alongside the triggering DB write) ---

// EnqueueSessionConfirmed enqueues an immediate booking confirmation notification
// and a 24-hour-before reminder for the client.
func (n *NotificationService) EnqueueSessionConfirmed(ctx context.Context, p SessionConfirmedPayload) error {
	// Immediate confirmation.
	if err := n.outbox.Enqueue(ctx, EventSessionConfirmed, p, time.Now()); err != nil {
		return fmt.Errorf("messaging: enqueue session confirmed: %w", err)
	}

	// 24h reminder — scheduled to fire 24h before the session starts.
	reminderAt := p.StartsAt.Add(-24 * time.Hour)
	if reminderAt.After(time.Now()) {
		if err := n.outbox.Enqueue(ctx, EventSessionReminder, p, reminderAt); err != nil {
			return fmt.Errorf("messaging: enqueue session reminder: %w", err)
		}
	}
	return nil
}

// EnqueueSessionCancelled enqueues a cancellation notification for the client.
func (n *NotificationService) EnqueueSessionCancelled(ctx context.Context, p SessionCancelledPayload) error {
	if err := n.outbox.Enqueue(ctx, EventSessionCancelled, p, time.Now()); err != nil {
		return fmt.Errorf("messaging: enqueue session cancelled: %w", err)
	}
	return nil
}

// EnqueuePaymentFailed enqueues a failed payment alert for the coach.
func (n *NotificationService) EnqueuePaymentFailed(ctx context.Context, p PaymentFailedPayload) error {
	if err := n.outbox.Enqueue(ctx, EventPaymentFailed, p, time.Now()); err != nil {
		return fmt.Errorf("messaging: enqueue payment failed: %w", err)
	}
	return nil
}

// --- Delivery (called by the worker) ---

// Deliver dispatches a single outbox entry. Returns an error if delivery fails.
func (n *NotificationService) Deliver(ctx context.Context, entry OutboxEntry) error {
	switch entry.EventType {
	case EventSessionConfirmed:
		return n.deliverSessionConfirmed(ctx, entry)
	case EventSessionReminder:
		return n.deliverSessionReminder(ctx, entry)
	case EventSessionCancelled:
		return n.deliverSessionCancelled(ctx, entry)
	case EventPaymentFailed:
		return n.deliverPaymentFailed(ctx, entry)
	default:
		return fmt.Errorf("messaging: unknown event type: %s", entry.EventType)
	}
}

func (n *NotificationService) deliverSessionConfirmed(ctx context.Context, entry OutboxEntry) error {
	var p SessionConfirmedPayload
	if err := unmarshalPayload(entry.Payload, &p); err != nil {
		return err
	}

	var errs []error

	// Email the client.
	subject, html := BookingConfirmationEmail(p.ClientName, p.CoachName, p.StartsAt)
	if err := n.email.SendEmail(ctx, p.ClientEmail, p.ClientName, subject, html); err != nil {
		n.logger.WarnContext(ctx, "session confirmation email failed", "client_id", p.ClientID, "error", err)
		errs = append(errs, err)
	}

	// SMS the client (if phone number available).
	if p.ClientPhone != "" {
		msg := BookingConfirmationSMS(p.CoachName, p.StartsAt)
		if err := n.sms.Send(ctx, p.ClientPhone, msg); err != nil {
			n.logger.WarnContext(ctx, "session confirmation sms failed", "client_id", p.ClientID, "error", err)
			errs = append(errs, err)
		}
	}

	return firstErr(errs)
}

func (n *NotificationService) deliverSessionReminder(ctx context.Context, entry OutboxEntry) error {
	var p SessionConfirmedPayload
	if err := unmarshalPayload(entry.Payload, &p); err != nil {
		return err
	}

	var errs []error

	subject, html := SessionReminderEmail(p.ClientName, p.CoachName, p.StartsAt)
	if err := n.email.SendEmail(ctx, p.ClientEmail, p.ClientName, subject, html); err != nil {
		n.logger.WarnContext(ctx, "session reminder email failed", "client_id", p.ClientID, "error", err)
		errs = append(errs, err)
	}

	if p.ClientPhone != "" {
		msg := SessionReminderSMS(p.CoachName, p.StartsAt)
		if err := n.sms.Send(ctx, p.ClientPhone, msg); err != nil {
			n.logger.WarnContext(ctx, "session reminder sms failed", "client_id", p.ClientID, "error", err)
			errs = append(errs, err)
		}
	}

	return firstErr(errs)
}

func (n *NotificationService) deliverSessionCancelled(ctx context.Context, entry OutboxEntry) error {
	var p SessionCancelledPayload
	if err := unmarshalPayload(entry.Payload, &p); err != nil {
		return err
	}

	var errs []error

	subject, html := SessionCancelledEmail(p.ClientName, p.StartsAt, p.CreditIssued)
	if err := n.email.SendEmail(ctx, p.ClientEmail, p.ClientName, subject, html); err != nil {
		n.logger.WarnContext(ctx, "session cancelled email failed", "client_id", p.ClientID, "error", err)
		errs = append(errs, err)
	}

	if p.ClientPhone != "" {
		msg := SessionCancelledSMS(p.StartsAt, p.CreditIssued)
		if err := n.sms.Send(ctx, p.ClientPhone, msg); err != nil {
			n.logger.WarnContext(ctx, "session cancelled sms failed", "client_id", p.ClientID, "error", err)
			errs = append(errs, err)
		}
	}

	return firstErr(errs)
}

func (n *NotificationService) deliverPaymentFailed(ctx context.Context, entry OutboxEntry) error {
	var p PaymentFailedPayload
	if err := unmarshalPayload(entry.Payload, &p); err != nil {
		return err
	}

	var errs []error

	// Alert the coach by email and SMS.
	subject, html := PaymentFailedEmail(p.CoachName, p.ClientName, p.BillingYear, p.BillingMonth, p.Provider)
	if err := n.email.SendEmail(ctx, p.CoachEmail, p.CoachName, subject, html); err != nil {
		n.logger.WarnContext(ctx, "payment failed email failed", "coach_id", p.CoachID, "error", err)
		errs = append(errs, err)
	}

	if p.CoachPhone != "" {
		msg := PaymentFailedSMS(p.ClientName, p.BillingYear, p.BillingMonth)
		if err := n.sms.Send(ctx, p.CoachPhone, msg); err != nil {
			n.logger.WarnContext(ctx, "payment failed sms failed", "coach_id", p.CoachID, "error", err)
			errs = append(errs, err)
		}
	}

	return firstErr(errs)
}

// --- helpers ---

func unmarshalPayload(raw []byte, dest any) error {
	if err := jsonUnmarshal(raw, dest); err != nil {
		return fmt.Errorf("messaging: unmarshal payload: %w", err)
	}
	return nil
}

// firstErr returns the first non-nil error in the slice, or nil if all succeeded.
// We attempt both email and SMS even if one fails; the entry is retried if either fails.
func firstErr(errs []error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// EnqueueSessionConfirmedForRun is a convenience helper called after a schedule run
// is confirmed. It enqueues confirmations for all sessions in the run.
func (n *NotificationService) EnqueueSessionConfirmedForRun(ctx context.Context, sessions []SessionConfirmedPayload) error {
	for _, s := range sessions {
		if err := n.EnqueueSessionConfirmed(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// EnqueuePaymentFailedAlert is called by the billing webhook handler when a payment fails.
func (n *NotificationService) EnqueuePaymentFailedAlert(ctx context.Context, coachID, clientID uuid.UUID,
	coachName, coachEmail, coachPhone, clientName string,
	year, month int, provider string) error {

	return n.EnqueuePaymentFailed(ctx, PaymentFailedPayload{
		ClientID:     clientID,
		CoachID:      coachID,
		ClientName:   clientName,
		CoachName:    coachName,
		CoachEmail:   coachEmail,
		CoachPhone:   coachPhone,
		BillingYear:  year,
		BillingMonth: month,
		Provider:     provider,
	})
}
