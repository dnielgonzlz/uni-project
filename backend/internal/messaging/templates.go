package messaging

import (
	"fmt"
	"time"
)

// UK date/time formatting helpers.
const ukDateFormat = "02/01/2006"
const ukTimeFormat = "15:04"

func formatUKDateTime(t time.Time) string {
	// Convert UTC → Europe/London for display.
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		loc = time.UTC
	}
	local := t.In(loc)
	return fmt.Sprintf("%s at %s", local.Format(ukDateFormat), local.Format(ukTimeFormat))
}

// --- Email templates ---

// BookingConfirmationEmail returns subject + HTML body for a new session confirmation.
func BookingConfirmationEmail(clientName, coachName string, startsAt time.Time) (subject, html string) {
	subject = "Your PT session is confirmed"
	html = fmt.Sprintf(`
<p>Hi %s,</p>
<p>Your personal training session with <strong>%s</strong> has been confirmed for <strong>%s</strong>.</p>
<p>The session is 60 minutes. Please arrive a few minutes early.</p>
<p>If you need to cancel, please do so at least 24 hours in advance to receive a session credit.</p>
<p>— PT Scheduler</p>
`, clientName, coachName, formatUKDateTime(startsAt))
	return
}

// SessionReminderEmail returns subject + HTML body for a 24-hour reminder.
func SessionReminderEmail(clientName, coachName string, startsAt time.Time) (subject, html string) {
	subject = "Reminder: PT session tomorrow"
	html = fmt.Sprintf(`
<p>Hi %s,</p>
<p>This is a reminder that your personal training session with <strong>%s</strong> is scheduled for <strong>%s</strong> — that's tomorrow!</p>
<p>See you there.</p>
<p>— PT Scheduler</p>
`, clientName, coachName, formatUKDateTime(startsAt))
	return
}

// SessionCancelledEmail returns subject + HTML body for a cancellation notice.
// creditIssued indicates whether a session credit was granted (cancellation within notice window).
func SessionCancelledEmail(clientName string, startsAt time.Time, creditIssued bool) (subject, html string) {
	subject = "Your PT session has been cancelled"
	credit := ""
	if creditIssued {
		credit = "<p>Because you cancelled with sufficient notice, a <strong>session credit</strong> has been added to your account. You can use it to book a replacement session.</p>"
	}
	html = fmt.Sprintf(`
<p>Hi %s,</p>
<p>Your personal training session scheduled for <strong>%s</strong> has been cancelled.</p>
%s
<p>— PT Scheduler</p>
`, clientName, formatUKDateTime(startsAt), credit)
	return
}

// PaymentFailedEmail returns subject + HTML body alerting a coach of a failed payment.
func PaymentFailedEmail(coachName, clientName string, year, month int, provider string) (subject, html string) {
	subject = fmt.Sprintf("Payment failed for %s — %02d/%d", clientName, month, year)
	html = fmt.Sprintf(`
<p>Hi %s,</p>
<p>The <strong>%s</strong> payment for <strong>%s</strong> (billing period %02d/%d) has <strong>failed</strong>.</p>
<p>Please log in to PT Scheduler to review and retry the charge, or contact your client to update their payment details.</p>
<p>— PT Scheduler</p>
`, coachName, provider, clientName, month, year)
	return
}

// --- SMS templates ---

// BookingConfirmationSMS returns a short SMS confirming a session.
func BookingConfirmationSMS(coachName string, startsAt time.Time) string {
	return fmt.Sprintf("PT Scheduler: Your session with %s is confirmed for %s. Cancel 24h+ in advance to keep your credit.",
		coachName, formatUKDateTime(startsAt))
}

// SessionReminderSMS returns a short SMS reminder sent ~24h before the session.
func SessionReminderSMS(coachName string, startsAt time.Time) string {
	return fmt.Sprintf("PT Scheduler: Reminder — your session with %s is tomorrow at %s. See you there!",
		coachName, startsAt.In(londonLoc()).Format(ukTimeFormat))
}

// SessionCancelledSMS returns a short SMS confirming cancellation.
func SessionCancelledSMS(startsAt time.Time, creditIssued bool) string {
	credit := ""
	if creditIssued {
		credit = " A session credit has been added to your account."
	}
	return fmt.Sprintf("PT Scheduler: Your session on %s has been cancelled.%s",
		formatUKDateTime(startsAt), credit)
}

// PaymentFailedSMS returns a short SMS alerting the coach of a failed charge.
func PaymentFailedSMS(clientName string, year, month int) string {
	return fmt.Sprintf("PT Scheduler: Payment failed for %s (%02d/%d). Please log in to retry.", clientName, month, year)
}

func londonLoc() *time.Location {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		return time.UTC
	}
	return loc
}
