package availability_intake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"time"
)

// AvailabilityParser converts a raw WhatsApp message into structured availability windows.
type AvailabilityParser interface {
	Parse(ctx context.Context, req ParseRequest) (*ParseResult, error)
}

// ParseRequest is the input to the parser.
type ParseRequest struct {
	MessageText string
	WeekStart   time.Time // Monday of the campaign week, Europe/London
	Timezone    string    // e.g. "Europe/London"
}

// ParseStatus indicates how well the message was understood.
type ParseStatus string

const (
	ParseStatusParsed     ParseStatus = "parsed"
	ParseStatusAmbiguous  ParseStatus = "ambiguous"
	ParseStatusIrrelevant ParseStatus = "irrelevant"
)

// ParsedWindow is one extracted availability slot.
type ParsedWindow struct {
	DayOfWeek  int     `json:"day_of_week"` // 0=Mon ISO
	StartTime  string  `json:"start_time"`  // "HH:MM"
	EndTime    string  `json:"end_time"`    // "HH:MM"
	Confidence float64 `json:"confidence"`  // 0.0–1.0
	Source     string  `json:"source"`      // originating text snippet
}

// ParseResult is what the parser returns.
type ParseResult struct {
	Status   ParseStatus    `json:"status"`
	Windows  []ParsedWindow `json:"windows"`
	FollowUp string         `json:"follow_up,omitempty"` // non-empty when Status==ambiguous
}

// ─── OpenRouter implementation ────────────────────────────────────────────────

const openRouterURL = "https://openrouter.ai/api/v1/chat/completions"

// maxOpenRouterAttempts covers 429s, short outages, and occasional slow reads (body still arriving).
// Keep modest: this runs inside the synchronous Twilio webhook handler.
const maxOpenRouterAttempts = 3

// openRouterPerCallTimeout bounds each HTTP attempt (request + read full body). Not configurable
// via env so Twilio webhook + chi middleware timeouts stay predictable.
const openRouterPerCallTimeout = 35 * time.Second

// openRouterParser calls the OpenRouter API (OpenAI-compatible endpoint).
// The model is configurable via OPENROUTER_MODEL — swap it without any code change.
type openRouterParser struct {
	httpClient     *http.Client
	apiKey         string
	model          string
	logger         *slog.Logger
	perCallTimeout time.Duration // always openRouterPerCallTimeout; stored for Parse()
}

// NewOpenRouterParser creates a production parser backed by OpenRouter.
func NewOpenRouterParser(apiKey, model string, logger *slog.Logger) AvailabilityParser {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	return &openRouterParser{
		httpClient:     &http.Client{Transport: tr},
		apiKey:         apiKey,
		model:          model,
		logger:         logger,
		perCallTimeout: openRouterPerCallTimeout,
	}
}

// orRequest is the OpenAI-compatible chat completions request body.
type orRequest struct {
	Model          string            `json:"model"`
	Messages       []orMessage       `json:"messages"`
	ResponseFormat orFormat          `json:"response_format"`
	Provider       *orProviderSelect `json:"provider,omitempty"` // avoids routing via disallowed backends for restricted keys
}

// orProviderSelect pins inference to providers the account allows (see OpenRouter provider routing docs).
type orProviderSelect struct {
	Only           []string `json:"only"`
	AllowFallbacks bool     `json:"allow_fallbacks"`
}

type orMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type orFormat struct {
	Type string `json:"type"` // "json_object"
}

// orResponse holds only the fields we need from the chat completions response.
type orResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (p *openRouterParser) Parse(ctx context.Context, req ParseRequest) (*ParseResult, error) {
	systemPrompt := buildSystemPrompt(req.WeekStart, req.Timezone)

	body := orRequest{
		Model: p.model,
		Messages: []orMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: req.MessageText},
		},
		ResponseFormat: orFormat{Type: "json_object"},
		// Many OpenRouter keys only allow a subset of providers; without this, routing can pick
		// DeepSeek/Anthropic/Google and return 404 "No allowed providers are available".
		Provider: &orProviderSelect{
			Only:           []string{"openai", "azure"},
			AllowFallbacks: false,
		},
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openrouter parse: marshal request: %w", err)
	}

	var lastNonOKBody []byte

	for attempt := 0; attempt < maxOpenRouterAttempts; attempt++ {
		if attempt > 0 {
			base := time.Duration(math.Pow(2, float64(attempt-1))) * 400 * time.Millisecond
			if base > 5*time.Second {
				base = 5 * time.Second
			}
			jitter := time.Duration(rand.IntN(150)) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("openrouter parse: %w", ctx.Err())
			case <-time.After(base + jitter):
			}
		}

		// Per-attempt deadline: slow models can take >10s to finish streaming the JSON body.
		attemptCtx, cancelAttempt := context.WithTimeout(context.Background(), p.perCallTimeout)
		httpReq, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, openRouterURL, bytes.NewReader(bodyJSON))
		if err != nil {
			cancelAttempt()
			return nil, fmt.Errorf("openrouter parse: create request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := p.httpClient.Do(httpReq)
		if err != nil {
			cancelAttempt()
			if attempt < maxOpenRouterAttempts-1 && isTransientOpenRouterErr(err) {
				p.logger.WarnContext(ctx, "openrouter request error, retrying",
					"attempt", attempt+1, "max", maxOpenRouterAttempts, "error", err)
				continue
			}
			return nil, fmt.Errorf("openrouter parse: http: %w", err)
		}

		bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		cancelAttempt()

		if resp.StatusCode != http.StatusOK {
			lastNonOKBody = bodyBytes
			if attempt < maxOpenRouterAttempts-1 && isOpenRouterRetryableStatus(resp.StatusCode) {
				p.logger.WarnContext(ctx, "openrouter transient response, retrying",
					"attempt", attempt+1, "max", maxOpenRouterAttempts, "status", resp.StatusCode)
				continue
			}
			return nil, fmt.Errorf("openrouter parse: status %d: %s", resp.StatusCode, lastNonOKBody)
		}

		if readErr != nil {
			if attempt < maxOpenRouterAttempts-1 && isTransientOpenRouterErr(readErr) {
				p.logger.WarnContext(ctx, "openrouter read response failed, retrying",
					"attempt", attempt+1, "max", maxOpenRouterAttempts, "error", readErr)
				continue
			}
			return nil, fmt.Errorf("openrouter parse: read response: %w", readErr)
		}

		var orResp orResponse
		if err := json.Unmarshal(bodyBytes, &orResp); err != nil {
			return nil, fmt.Errorf("openrouter parse: decode response: %w", err)
		}
		if len(orResp.Choices) == 0 {
			return nil, fmt.Errorf("openrouter parse: no choices in response")
		}

		content := orResp.Choices[0].Message.Content
		var result ParseResult
		if err := json.Unmarshal([]byte(content), &result); err != nil {
			p.logger.Warn("openrouter parse: malformed JSON in LLM response; treating as ambiguous",
				"content", content, "err", err)
			return &ParseResult{
				Status:   ParseStatusAmbiguous,
				FollowUp: "Could you list which days and times work for you next week?",
			}, nil
		}

		p.logger.Debug("openrouter parse result",
			"status", result.Status,
			"window_count", len(result.Windows),
			"follow_up", result.FollowUp,
		)

		return &result, nil
	}

	return nil, fmt.Errorf("openrouter parse: retry loop exited without result")
}

// isTransientOpenRouterErr is true for timeouts while waiting on OpenRouter (slow model or network).
func isTransientOpenRouterErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// isOpenRouterRetryableStatus is true for shared-provider rate limits and transient gateway errors.
func isOpenRouterRetryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func buildSystemPrompt(weekStart time.Time, timezone string) string {
	if timezone == "" {
		timezone = "Europe/London"
	}
	weekStartStr := weekStart.Format("2006-01-02")
	return fmt.Sprintf(`You are an availability parser for a UK personal trainer scheduling app.
Extract the client's available training windows from their WhatsApp message.
Campaign week: Monday %s (%s timezone).

Time defaults (UK personal training context):
  morning       = 06:00-12:00
  afternoon     = 12:00-17:00
  evening       = 17:00-21:00
  after work    = 17:00-21:00
  before work   = 06:00-09:00
  lunchtime     = 12:00-13:00
  <day only>    = 06:00-22:00

Day resolution: weekday names and "next [day]" refer to the campaign week above.
"except Thursday" means all days except Thursday.
Be inclusive - if a window could reasonably be interpreted, include it.

Return ONLY valid JSON:
{
  "status": "parsed" | "ambiguous" | "irrelevant",
  "windows": [
    {"day_of_week":0,"start_time":"HH:MM","end_time":"HH:MM","confidence":0.95,"source":"..."}
  ],
  "follow_up": ""
}

Rules:
- status "parsed"     -> windows is non-empty, follow_up is ""
- status "ambiguous"  -> windows may be partial, follow_up is ONE concise question (max 160 chars)
- status "irrelevant" -> message is not about availability at all (e.g. "ok thanks")`, weekStartStr, timezone)
}

// ─── NoopParser ───────────────────────────────────────────────────────────────

// NoopParser always returns irrelevant, triggering the rule-based fallback.
// Used when OPENROUTER_API_KEY is not set.
type NoopParser struct{}

func (NoopParser) Parse(_ context.Context, _ ParseRequest) (*ParseResult, error) {
	return &ParseResult{Status: ParseStatusIrrelevant}, nil
}
