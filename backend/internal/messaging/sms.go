package messaging

import (
	"context"
	"fmt"

	"github.com/twilio/twilio-go"
	openapi "github.com/twilio/twilio-go/rest/api/v2010"
)

// SMSService sends SMS messages via Twilio.
// The same interface supports WhatsApp when MESSAGING_CHANNEL=whatsapp:
// the fromNumber is prefixed with "whatsapp:" and toNumber likewise.
type SMSService struct {
	client     *twilio.RestClient
	fromNumber string // E.164 for SMS; "whatsapp:+44..." for WhatsApp
	channel    string // "sms" | "whatsapp"
}

func NewSMSService(accountSID, authToken, fromNumber, channel string) *SMSService {
	client := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: accountSID,
		Password: authToken,
	})

	// Prefix numbers for WhatsApp channel.
	if channel == "whatsapp" {
		fromNumber = "whatsapp:" + fromNumber
	}

	return &SMSService{
		client:     client,
		fromNumber: fromNumber,
		channel:    channel,
	}
}

// Send sends a text message to the given E.164 phone number.
func (s *SMSService) Send(ctx context.Context, toE164, body string) error {
	to := toE164
	if s.channel == "whatsapp" {
		to = "whatsapp:" + toE164
	}

	params := &openapi.CreateMessageParams{}
	params.SetTo(to)
	params.SetFrom(s.fromNumber)
	params.SetBody(body)

	_, err := s.client.Api.CreateMessage(params)
	if err != nil {
		return fmt.Errorf("sms: send to %s: %w", toE164, err)
	}
	return nil
}
