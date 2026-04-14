package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/validator"
	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

// Handler holds the HTTP handlers for auth endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register handles POST /api/v1/auth/register
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
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

	tokens, err := h.svc.Register(r.Context(), req)
	if err != nil {
		if errors.Is(err, users.ErrEmailTaken) {
			// FRONTEND: show "an account with this email already exists" message
			httpx.Error(w, http.StatusConflict, "email already registered")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "registration failed")
		return
	}

	httpx.JSON(w, http.StatusCreated, tokens)
}

// Login handles POST /api/v1/auth/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validator.Validate.Struct(req); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, "validation failed")
		return
	}

	tokens, err := h.svc.Login(r.Context(), req)
	if err != nil {
		if errors.Is(err, ErrInvalidPassword) {
			// FRONTEND: show "incorrect email or password" — never reveal which is wrong
			httpx.Error(w, http.StatusUnauthorized, "incorrect email or password")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "login failed")
		return
	}

	httpx.JSON(w, http.StatusOK, tokens)
}

// Logout handles POST /api/v1/auth/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req LogoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validator.Validate.Struct(req); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, "validation failed")
		return
	}

	if err := h.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "logout failed")
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

// Refresh handles POST /api/v1/auth/refresh
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validator.Validate.Struct(req); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, "validation failed")
		return
	}

	tokens, err := h.svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrInvalidToken) {
			// FRONTEND: clear stored tokens and redirect to login
			httpx.Error(w, http.StatusUnauthorized, "invalid or expired refresh token")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "token refresh failed")
		return
	}

	httpx.JSON(w, http.StatusOK, tokens)
}

// ForgotPassword handles POST /api/v1/auth/forgot-password
func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validator.Validate.Struct(req); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, "validation failed")
		return
	}

	// Always return 200 to avoid email enumeration — the service handles this internally.
	_ = h.svc.ForgotPassword(r.Context(), req.Email)
	httpx.JSON(w, http.StatusOK, map[string]string{
		"message": "if an account exists for that email, a reset link has been sent",
	})
}

// ResetPassword handles POST /api/v1/auth/reset-password
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
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

	if err := h.svc.ResetPassword(r.Context(), req); err != nil {
		if errors.Is(err, ErrInvalidToken) {
			// FRONTEND: show "this reset link is invalid or has expired" message
			httpx.Error(w, http.StatusUnprocessableEntity, "reset link is invalid or has expired")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "password reset failed")
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"message": "password updated successfully"})
}
