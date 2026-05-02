package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ApprovalChecker interface {
	CheckTemplateStatus(ctx context.Context, contentSID string) (*TemplateStatusResponse, error)
}

type TwilioContentClient struct {
	accountSID string
	authToken  string
	client     *http.Client
}

func NewTwilioContentClient(accountSID, authToken string) *TwilioContentClient {
	return &TwilioContentClient{
		accountSID: accountSID,
		authToken:  authToken,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *TwilioContentClient) CheckTemplateStatus(ctx context.Context, contentSID string) (*TemplateStatusResponse, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("https://content.twilio.com/v1/Content/%s/ApprovalRequests", contentSID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("agent: build Twilio approval request: %w", err)
	}
	req.SetBasicAuth(c.accountSID, c.authToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agent: call Twilio approval API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("agent: Twilio approval API returned %d", resp.StatusCode)
	}

	var body struct {
		WhatsApp *struct {
			Status          string  `json:"status"`
			RejectionReason *string `json:"rejection_reason"`
		} `json:"whatsapp"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("agent: decode Twilio approval response: %w", err)
	}

	if body.WhatsApp == nil {
		return &TemplateStatusResponse{TemplateStatus: TemplateStatusPending}, nil
	}

	return &TemplateStatusResponse{
		TemplateStatus:  normaliseTemplateStatus(body.WhatsApp.Status),
		RejectionReason: body.WhatsApp.RejectionReason,
	}, nil
}

func normaliseTemplateStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "approved":
		return TemplateStatusApproved
	case "rejected", "failed":
		return TemplateStatusRejected
	case "pending", "received", "in_review":
		return TemplateStatusPending
	default:
		return TemplateStatusPending
	}
}
