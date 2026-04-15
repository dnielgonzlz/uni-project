package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GoCardless API base URLs
const (
	gcLiveURL    = "https://api.gocardless.com"
	gcSandboxURL = "https://api.gocardless.com" // same domain, env set via header
)

// GoCardlessClient wraps the GoCardless REST API using plain HTTP.
// We use HTTP directly rather than the SDK to keep dependencies minimal
// and give explicit control over idempotency and error handling.
type GoCardlessClient struct {
	accessToken    string
	webhookSecret  string
	env            string // "sandbox" | "live"
	httpClient     *http.Client
}

func NewGoCardlessClient(accessToken, webhookSecret, env string) *GoCardlessClient {
	return &GoCardlessClient{
		accessToken:   accessToken,
		webhookSecret: webhookSecret,
		env:           env,
		httpClient:    &http.Client{Timeout: 15 * time.Second},
	}
}

// CreateRedirectFlow starts a GoCardless billing request flow.
// The client is redirected to `redirectURI?redirect_flow_id=…` after authorising.
// The mandate is created once CompleteRedirectFlow is called.
func (gc *GoCardlessClient) CreateRedirectFlow(ctx context.Context, description, redirectURI, sessionToken string) (string, string, error) {
	body := map[string]any{
		"redirect_flows": map[string]any{
			"description":    description,
			"session_token":  sessionToken,
			"success_redirect_url": redirectURI,
			"scheme":         "bacs",
		},
	}

	var resp struct {
		RedirectFlows struct {
			ID          string `json:"id"`
			RedirectURL string `json:"redirect_url"`
		} `json:"redirect_flows"`
	}

	if err := gc.post(ctx, "/redirect_flows", body, &resp); err != nil {
		return "", "", fmt.Errorf("gocardless: create redirect flow: %w", err)
	}

	return resp.RedirectFlows.RedirectURL, resp.RedirectFlows.ID, nil
}

// CompleteRedirectFlow completes the redirect flow and returns the mandate ID.
// Must be called after the client returns to the redirect URI.
func (gc *GoCardlessClient) CompleteRedirectFlow(ctx context.Context, flowID, sessionToken string) (string, error) {
	body := map[string]any{
		"data": map[string]any{
			"session_token": sessionToken,
		},
	}

	var resp struct {
		RedirectFlows struct {
			Links struct {
				Mandate string `json:"mandate"`
			} `json:"links"`
		} `json:"redirect_flows"`
	}

	path := fmt.Sprintf("/redirect_flows/%s/actions/complete", flowID)
	if err := gc.post(ctx, path, body, &resp); err != nil {
		return "", fmt.Errorf("gocardless: complete redirect flow: %w", err)
	}

	return resp.RedirectFlows.Links.Mandate, nil
}

// CreatePayment submits a Bacs Direct Debit payment against an existing mandate.
// Returns the GoCardless payment ID.
//
// IMPORTANT: Bacs payments require advance notice (3 working days for first payment,
// 2 for subsequent). The caller must verify this before calling CreatePayment.
func (gc *GoCardlessClient) CreatePayment(ctx context.Context, mandateID, idempotencyKey string, amountPence int, description string) (string, error) {
	chargeDate := BacsEarliestChargeDate(time.Now().UTC(), false)

	body := map[string]any{
		"payments": map[string]any{
			"amount":       amountPence,
			"currency":     "GBP",
			"charge_date":  chargeDate.Format("2006-01-02"),
			"description":  description,
			"links": map[string]string{
				"mandate": mandateID,
			},
		},
	}

	var resp struct {
		Payments struct {
			ID string `json:"id"`
		} `json:"payments"`
	}

	if err := gc.postWithIdempotency(ctx, "/payments", body, &resp, idempotencyKey); err != nil {
		return "", fmt.Errorf("gocardless: create payment: %w", err)
	}

	return resp.Payments.ID, nil
}

// VerifyWebhookSignature validates the GoCardless Webhook-Signature header.
// rawBody must be the unmodified request body bytes.
func (gc *GoCardlessClient) VerifyWebhookSignature(rawBody []byte, sigHeader string) error {
	mac := hmac.New(sha256.New, []byte(gc.webhookSecret))
	mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(strings.ToLower(sigHeader))) {
		return fmt.Errorf("gocardless: webhook signature mismatch")
	}
	return nil
}

// --- HTTP helpers ---

func (gc *GoCardlessClient) post(ctx context.Context, path string, body, dest any) error {
	return gc.postWithIdempotency(ctx, path, body, dest, "")
}

func (gc *GoCardlessClient) postWithIdempotency(ctx context.Context, path string, body, dest any, idempotencyKey string) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("gocardless: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gcLiveURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+gc.accessToken)
	req.Header.Set("GoCardless-Version", "2015-07-06")
	if gc.env == "sandbox" {
		req.Header.Set("GoCardless-Environment", "sandbox")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	resp, err := gc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("gocardless: http: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("gocardless: API error %d: %s", resp.StatusCode, string(respBody))
	}

	if dest != nil {
		if err := json.Unmarshal(respBody, dest); err != nil {
			return fmt.Errorf("gocardless: decode response: %w", err)
		}
	}
	return nil
}
