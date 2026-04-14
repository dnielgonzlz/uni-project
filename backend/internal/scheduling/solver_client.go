package scheduling

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Solver is the interface used by the service layer to call the scheduling solver.
// The real implementation calls the Python FastAPI microservice; tests use a fake.
type Solver interface {
	Solve(ctx context.Context, req SolverRequest) (*SolverResponse, error)
}

// HTTPSolver calls the Python OR-Tools FastAPI microservice over HTTP.
type HTTPSolver struct {
	baseURL string
	timeout time.Duration
	client  *http.Client
}

// NewHTTPSolver creates a solver client pointing at the given base URL.
func NewHTTPSolver(baseURL string, timeoutSeconds int) *HTTPSolver {
	timeout := time.Duration(timeoutSeconds) * time.Second
	return &HTTPSolver{
		baseURL: baseURL,
		timeout: timeout,
		client:  &http.Client{Timeout: timeout},
	}
}

// Solve sends the scheduling problem to the Python solver and returns the proposed schedule.
// A 30s timeout is enforced; the solver must never be retried (calls are expensive).
func (s *HTTPSolver) Solve(ctx context.Context, req SolverRequest) (*SolverResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("solver: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/solve", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("solver: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("solver: call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("solver: unexpected status %d", resp.StatusCode)
	}

	var result SolverResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("solver: decode response: %w", err)
	}
	return &result, nil
}
