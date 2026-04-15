package billing

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/validator"
	"github.com/google/uuid"
)

// Handler exposes the billing service over HTTP.
type Handler struct {
	svc    *Service
	logger *slog.Logger
}

func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// setupIntentBody is the combined request body for POST /api/v1/payments/setup-intent.
type setupIntentBody struct {
	ClientID string `json:"client_id" validate:"required,uuid4"`
	Email    string `json:"email"     validate:"required,email"`
	FullName string `json:"full_name" validate:"required"`
}

// CreateSetupIntent handles POST /api/v1/payments/setup-intent
// Creates a Stripe SetupIntent so the client can save their card without an immediate charge.
// FRONTEND: use the returned client_secret to initialise Stripe Elements.
func (h *Handler) CreateSetupIntent(w http.ResponseWriter, r *http.Request) {
	var req setupIntentBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validator.Validate.Struct(req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":  "validation failed",
			"fields": validator.ValidationErrors(err),
		})
		return
	}

	clientID, _ := uuid.Parse(req.ClientID) // safe — validated above

	resp, err := h.svc.CreateSetupIntent(r.Context(), clientID, req.Email, req.FullName)
	if err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, resp)
}

// CreateMandateFlow handles POST /api/v1/payments/mandate
// Starts a GoCardless redirect flow.
// FRONTEND: redirect the client to the returned redirect_url.
func (h *Handler) CreateMandateFlow(w http.ResponseWriter, r *http.Request) {
	var req MandateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validator.Validate.Struct(req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":  "validation failed",
			"fields": validator.ValidationErrors(err),
		})
		return
	}

	clientID, _ := uuid.Parse(req.ClientID) // safe — validated above

	resp, err := h.svc.CreateMandateFlow(r.Context(), clientID, req.RedirectURI)
	if err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, resp)
}

// CompleteMandateFlow handles POST /api/v1/payments/mandate/complete
// Finalises a GoCardless redirect flow after the client has authorised the mandate.
// Expects ?redirect_flow_id=RF123 on the query string.
func (h *Handler) CompleteMandateFlow(w http.ResponseWriter, r *http.Request) {
	flowID := r.URL.Query().Get("redirect_flow_id")
	if flowID == "" {
		httpx.Error(w, http.StatusBadRequest, "redirect_flow_id query parameter is required")
		return
	}

	var req struct {
		ClientID string `json:"client_id" validate:"required,uuid4"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validator.Validate.Struct(req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":  "validation failed",
			"fields": validator.ValidationErrors(err),
		})
		return
	}

	clientID, _ := uuid.Parse(req.ClientID) // safe — validated above

	mandate, err := h.svc.CompleteMandateFlow(r.Context(), clientID, flowID)
	if err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	httpx.JSON(w, http.StatusOK, mandate)
}

// chargeBody is the combined request body for POST /api/v1/billing/charge.
type chargeBody struct {
	ClientID     string `json:"client_id"     validate:"required,uuid4"`
	Provider     string `json:"provider"      validate:"required,oneof=stripe gocardless"`
	AmountPence  int    `json:"amount_pence"  validate:"required,min=100"`
	BillingYear  int    `json:"billing_year"  validate:"required,min=2024"`
	BillingMonth int    `json:"billing_month" validate:"required,min=1,max=12"`
}

// Charge handles POST /api/v1/billing/charge
// Allows a coach to manually trigger the monthly charge for a client.
// Idempotent — safe to call multiple times for the same billing period.
func (h *Handler) Charge(w http.ResponseWriter, r *http.Request) {
	var req chargeBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validator.Validate.Struct(req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":  "validation failed",
			"fields": validator.ValidationErrors(err),
		})
		return
	}

	clientID, _ := uuid.Parse(req.ClientID) // safe — validated above

	payment, err := h.svc.ChargeMonthly(
		r.Context(),
		clientID,
		req.Provider,
		req.AmountPence,
		req.BillingYear,
		req.BillingMonth,
	)
	if err != nil {
		if err == ErrAlreadyPaid {
			// FRONTEND: inform the coach that this billing period was already charged
			httpx.Error(w, http.StatusConflict, "payment already exists for this billing period")
			return
		}
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, payment)
}

// StripeWebhook handles POST /api/v1/webhooks/stripe
// Receives and verifies Stripe webhook events.
// IMPORTANT: this handler reads the raw body itself — do NOT put RequireJSON
// middleware on this route, as it would interfere with raw body access needed
// for signature verification.
func (h *Handler) StripeWebhook(w http.ResponseWriter, r *http.Request) {
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	sigHeader := r.Header.Get("Stripe-Signature")
	if sigHeader == "" {
		httpx.Error(w, http.StatusBadRequest, "missing Stripe-Signature header")
		return
	}

	if err := h.svc.HandleStripeWebhook(r.Context(), rawBody, sigHeader); err != nil {
		// Return 400 so Stripe retries the event.
		httpx.Error(w, http.StatusBadRequest, "webhook processing failed")
		return
	}

	// Stripe expects 2xx to stop retrying.
	w.WriteHeader(http.StatusOK)
}

// GoCardlessWebhook handles POST /api/v1/webhooks/gocardless
// Receives and verifies GoCardless webhook events.
// IMPORTANT: this handler reads the raw body itself — do NOT put RequireJSON
// middleware on this route, as it would interfere with HMAC verification.
func (h *Handler) GoCardlessWebhook(w http.ResponseWriter, r *http.Request) {
	rawBody, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer r.Body.Close()

	sigHeader := r.Header.Get("Webhook-Signature")
	if sigHeader == "" {
		httpx.Error(w, http.StatusBadRequest, "missing Webhook-Signature header")
		return
	}

	if err := h.svc.HandleGoCardlessWebhook(r.Context(), rawBody, sigHeader); err != nil {
		httpx.Error(w, http.StatusBadRequest, "webhook processing failed")
		return
	}

	w.WriteHeader(http.StatusOK)
}
