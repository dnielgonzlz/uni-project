package messaging

import (
	"context"
	"fmt"

	"github.com/resend/resend-go/v2"
)

// EmailService sends transactional emails via Resend.
type EmailService struct {
	client      *resend.Client
	fromAddress string
}

func NewEmailService(apiKey, fromAddress string) *EmailService {
	return &EmailService{
		client:      resend.NewClient(apiKey),
		fromAddress: fromAddress,
	}
}

// SendPasswordReset sends a password reset email with a single-use link.
// The link expires after the configured reset expiry window (default 60 min).
func (e *EmailService) SendPasswordReset(ctx context.Context, toEmail, toName, resetLink string) error {
	body := fmt.Sprintf(`
<p>Hi %s,</p>
<p>We received a request to reset your PT Scheduler password.</p>
<p><a href="%s">Click here to reset your password</a></p>
<p>This link expires in 60 minutes. If you didn't request a reset, you can ignore this email.</p>
<p>— PT Scheduler Team</p>
`, toName, resetLink)

	params := &resend.SendEmailRequest{
		From:    e.fromAddress,
		To:      []string{toEmail},
		Subject: "Reset your PT Scheduler password",
		Html:    body,
	}

	if _, err := e.client.Emails.SendWithContext(ctx, params); err != nil {
		return fmt.Errorf("email: send password reset to %s: %w", toEmail, err)
	}
	return nil
}
