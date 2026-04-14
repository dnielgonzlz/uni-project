package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// envelope is the standard JSON wrapper for all API responses.
type envelope struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// JSON writes a JSON response with the given status code and data payload.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Data: data})
}

// Error writes a JSON error response.
func Error(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Error: message})
}

// InternalError logs an unexpected error and returns a generic 500 response.
// The internal error detail is never sent to the client.
func InternalError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	logger.ErrorContext(r.Context(), "internal server error", "error", err, "path", r.URL.Path)
	Error(w, http.StatusInternalServerError, "an unexpected error occurred")
}
