package availability_intake

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
)

const twilioSignatureHeader = "X-Twilio-Signature"

// intakeWebhookProcessTimeout bounds Handle after Twilio may have closed the TCP connection (~15s).
// Without this, r.Context() is cancelled while the LLM runs and pgx.Begin fails with "context canceled".
const intakeWebhookProcessTimeout = 90 * time.Second

// Handler processes inbound Twilio webhooks for availability intake.
type Handler struct {
	svc             *Service
	logger          *slog.Logger
	twilioAuthToken string
}

func NewHandler(svc *Service, logger *slog.Logger, twilioAuthToken string) *Handler {
	return &Handler{svc: svc, logger: logger, twilioAuthToken: twilioAuthToken}
}

// InboundSMS handles POST /api/v1/webhooks/twilio.
//
//	@Summary      Twilio inbound messaging webhook
//	@Description  Receives inbound SMS or WhatsApp messages from clients via Twilio. Parses availability and updates the client's preferred windows. Replies with TwiML. Authenticated by Twilio HMAC (X-Twilio-Signature), not JWT.
//	@Tags         webhooks
//	@Accept       application/x-www-form-urlencoded
//	@Produce      xml
//	@Param        X-Twilio-Signature  header    string  true  "Twilio request signature"
//	@Param        MessageSid           formData  string  true  "Twilio message identifier"
//	@Param        From                 formData  string  true  "Sender phone number (E.164 or whatsapp:+E.164)"
//	@Param        Body                 formData  string  true  "Message body"
//	@Success      200
//	@Router       /webhooks/twilio [post]
func (h *Handler) InboundSMS(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid form body")
		return
	}

	if !h.hasValidTwilioSignature(r) {
		h.logger.WarnContext(r.Context(), "invalid Twilio webhook signature", "host", r.Host)
		httpx.Error(w, http.StatusForbidden, "invalid Twilio signature")
		return
	}

	messageSID := r.PostFormValue("MessageSid")
	if messageSID == "" {
		httpx.Error(w, http.StatusBadRequest, "missing MessageSid")
		return
	}

	if err := h.svc.repo.RecordWebhookEvent(r.Context(), messageSID, copyFormValues(r.PostForm)); err != nil {
		if errors.Is(err, ErrDuplicateWebhook) {
			// Twilio retries webhooks; acknowledge duplicates without sending the client a second reply.
			writeTwiML(w, "")
			return
		}
		h.logger.ErrorContext(r.Context(), "failed to record Twilio webhook", "message_sid", messageSID, "error", err)
		httpx.Error(w, http.StatusBadRequest, "webhook processing failed")
		return
	}

	from, channel := normaliseTwilioAddress(r.FormValue("From"))
	body := r.FormValue("Body")

	if from == "" || body == "" {
		httpx.Error(w, http.StatusBadRequest, "missing From or Body")
		return
	}

	// Twilio often drops the client before we finish (slow LLM). Do not tie DB work to r.Context().
	processCtx, cancelProcess := context.WithTimeout(context.WithoutCancel(r.Context()), intakeWebhookProcessTimeout)
	defer cancelProcess()

	reply, err := h.svc.Handle(processCtx, InboundMessage{
		MessageSID: messageSID,
		From:       from,
		Body:       body,
		Channel:    channel,
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "intake handler error", "from", from, "error", err)
		// Still reply to avoid Twilio retrying
		reply = "Something went wrong. Please contact your trainer directly."
	}

	writeTwiML(w, reply)
}

func (h *Handler) hasValidTwilioSignature(r *http.Request) bool {
	signature := r.Header.Get(twilioSignatureHeader)
	if signature == "" || h.twilioAuthToken == "" {
		return false
	}

	expected := twilioSignature(h.publicURL(r), r.PostForm, h.twilioAuthToken)
	return hmac.Equal([]byte(signature), []byte(expected))
}

func (h *Handler) publicURL(r *http.Request) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}

	return scheme + "://" + host + r.URL.RequestURI()
}

func twilioSignature(requestURL string, params map[string][]string, authToken string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	payload := requestURL
	for _, key := range keys {
		values := append([]string(nil), params[key]...)
		sort.Strings(values)
		for _, value := range values {
			payload += key + value
		}
	}

	mac := hmac.New(sha1.New, []byte(authToken))
	_, _ = mac.Write([]byte(payload))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func normaliseTwilioAddress(address string) (string, string) {
	address = strings.TrimSpace(address)
	if strings.HasPrefix(address, "whatsapp:") {
		return strings.TrimPrefix(address, "whatsapp:"), ChannelWhatsApp
	}
	return address, ChannelSMS
}

func copyFormValues(values map[string][]string) map[string][]string {
	copied := make(map[string][]string, len(values))
	for key, item := range values {
		copied[key] = append([]string(nil), item...)
	}
	return copied
}

func writeTwiML(w http.ResponseWriter, reply string) {
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	if strings.TrimSpace(reply) == "" {
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response></Response>`))
		return
	}
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Response><Message>` + xmlEscape(reply) + `</Message></Response>`))
}

// xmlEscape replaces characters that would break TwiML XML.
func xmlEscape(s string) string {
	return html.EscapeString(s)
}
