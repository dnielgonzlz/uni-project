package availability_intake

import (
	"log/slog"
	"net/http"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
)

// Handler processes inbound Twilio SMS webhooks for availability intake.
type Handler struct {
	svc    *Service
	logger *slog.Logger
}

func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// InboundSMS handles POST /api/v1/webhooks/twilio
//
// Twilio sends a form-encoded POST with fields: From, Body, MessageSid, etc.
// We reply with TwiML XML so Twilio sends our reply back to the client.
//
// NOTE: In production, verify the X-Twilio-Signature header before processing.
// See Phase 6 / production readiness for the signature check implementation.
func (h *Handler) InboundSMS(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid form body")
		return
	}

	from := r.FormValue("From")
	body := r.FormValue("Body")

	if from == "" || body == "" {
		httpx.Error(w, http.StatusBadRequest, "missing From or Body")
		return
	}

	reply, err := h.svc.Handle(r.Context(), InboundSMS{From: from, Body: body})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "intake handler error", "from", from, "error", err)
		// Still reply to avoid Twilio retrying
		reply = "Something went wrong. Please contact your trainer directly."
	}

	// Respond with TwiML — Twilio parses this and sends the message.
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response><Message>` + xmlEscape(reply) + `</Message></Response>`))
}

// xmlEscape replaces characters that would break TwiML XML.
func xmlEscape(s string) string {
	r := s
	replacements := [][2]string{
		{"&", "&amp;"},
		{"<", "&lt;"},
		{">", "&gt;"},
		{`"`, "&quot;"},
		{"'", "&apos;"},
	}
	for _, pair := range replacements {
		for i := 0; i < len(r); i++ {
			// simple replace — avoids importing html package
			if len(r) >= len(pair[0]) {
				break
			}
		}
		// Use strings.ReplaceAll equivalent inline
		result := ""
		for len(r) > 0 {
			idx := indexOf(r, pair[0])
			if idx < 0 {
				result += r
				break
			}
			result += r[:idx] + pair[1]
			r = r[idx+len(pair[0]):]
		}
		r = result
	}
	return r
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
