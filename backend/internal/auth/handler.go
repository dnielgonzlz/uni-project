package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/audit"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/ctxkeys"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/validator"
	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

// Handler holds the HTTP handlers for auth endpoints.
type Handler struct {
	svc   *Service
	audit *audit.Logger
}

func NewHandler(svc *Service, auditLog *audit.Logger) *Handler {
	return &Handler{svc: svc, audit: auditLog}
}

// Register handles POST /api/v1/auth/register
//
//	@Summary      Register a new user
//	@Description  Creates a coach or client account and returns access + refresh tokens.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      RegisterRequest  true  "Registration payload"
//	@Success      201   {object}  TokenResponse
//	@Failure      400   {object}  httpx.ErrorResponse
//	@Failure      409   {object}  httpx.ErrorResponse  "Email already registered"
//	@Failure      422   {object}  httpx.ValidationErrorResponse
//	@Router       /auth/register [post]
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

	ip := audit.IPFromRequest(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	h.audit.Log(r.Context(), &tokens.UserID, audit.ActionRegister, "user", &tokens.UserID,
		map[string]string{"role": req.Role}, ip)

	httpx.JSON(w, http.StatusCreated, tokens)
}

// CreateClientForCoach handles POST /api/v1/coaches/me/clients
//
//	@Summary      Create a client for the current coach
//	@Description  Creates a client account under the authenticated coach and emails the client a password-setup link.
//	@Tags         coaches
//	@Accept       json
//	@Produce      json
//	@Param        body  body      CreateCoachClientRequest  true  "Client details"
//	@Success      201   {object}  users.CoachClientSummary
//	@Failure      400   {object}  httpx.ErrorResponse
//	@Failure      401   {object}  httpx.ErrorResponse
//	@Failure      409   {object}  httpx.ErrorResponse
//	@Failure      422   {object}  httpx.ValidationErrorResponse
//	@Security     BearerAuth
//	@Router       /coaches/me/clients [post]
func (h *Handler) CreateClientForCoach(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	var req CreateCoachClientRequest
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

	client, err := h.svc.CreateClientForCoach(r.Context(), coachUserID, req)
	if err != nil {
		if errors.Is(err, users.ErrEmailTaken) {
			httpx.Error(w, http.StatusConflict, "email already registered")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to create client")
		return
	}

	httpx.JSON(w, http.StatusCreated, client)
}

// Login handles POST /api/v1/auth/login
//
//	@Summary      Login
//	@Description  Authenticates a user and returns access + refresh tokens.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      LoginRequest  true  "Login credentials"
//	@Success      200   {object}  TokenResponse
//	@Failure      400   {object}  httpx.ErrorResponse
//	@Failure      401   {object}  httpx.ErrorResponse  "Incorrect email or password"
//	@Router       /auth/login [post]
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

	ip := audit.IPFromRequest(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
	h.audit.Log(r.Context(), &tokens.UserID, audit.ActionLogin, "user", &tokens.UserID, nil, ip)

	httpx.JSON(w, http.StatusOK, tokens)
}

// Logout handles POST /api/v1/auth/logout
//
//	@Summary      Logout
//	@Description  Revokes the provided refresh token.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      LogoutRequest  true  "Refresh token to revoke"
//	@Success      200   {object}  map[string]string
//	@Failure      400   {object}  httpx.ErrorResponse
//	@Router       /auth/logout [post]
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
//
//	@Summary      Refresh tokens
//	@Description  Issues a new access token from a valid refresh token.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      RefreshRequest  true  "Refresh token"
//	@Success      200   {object}  TokenResponse
//	@Failure      400   {object}  httpx.ErrorResponse
//	@Failure      401   {object}  httpx.ErrorResponse  "Invalid or expired refresh token"
//	@Router       /auth/refresh [post]
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
//
//	@Summary      Request password reset
//	@Description  Sends a password reset email if the account exists. Always returns 200 to prevent email enumeration.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      ForgotPasswordRequest  true  "Email address"
//	@Success      200   {object}  map[string]string
//	@Failure      400   {object}  httpx.ErrorResponse
//	@Router       /auth/forgot-password [post]
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

// VerifyEmail handles POST /api/v1/auth/verify-email
//
//	@Summary      Verify email address
//	@Description  Marks the coach's email as verified using a single-use token sent on registration.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      VerifyEmailRequest  true  "Verification token"
//	@Success      200   {object}  map[string]string
//	@Failure      400   {object}  httpx.ErrorResponse
//	@Failure      422   {object}  httpx.ErrorResponse  "Token invalid or expired"
//	@Router       /auth/verify-email [post]
func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req VerifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validator.Validate.Struct(req); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, "validation failed")
		return
	}

	if err := h.svc.VerifyEmail(r.Context(), req); err != nil {
		if errors.Is(err, ErrInvalidToken) {
			// FRONTEND: show "this verification link is invalid or has expired" message
			httpx.Error(w, http.StatusUnprocessableEntity, "verification link is invalid or has expired")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "email verification failed")
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"message": "email verified successfully"})
}

// ResendVerification handles POST /api/v1/auth/resend-verification
//
//	@Summary      Resend verification email
//	@Description  Sends a fresh email verification link to the authenticated coach.
//	@Tags         auth
//	@Produce      json
//	@Success      200   {object}  map[string]string
//	@Failure      400   {object}  httpx.ErrorResponse  "Already verified"
//	@Failure      401   {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /auth/resend-verification [post]
func (h *Handler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	userID := ctxkeys.UserIDFromContext(r.Context())
	if userID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	if err := h.svc.ResendVerification(r.Context(), userID); err != nil {
		if errors.Is(err, ErrAlreadyVerified) {
			httpx.Error(w, http.StatusBadRequest, "email already verified")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "failed to resend verification email")
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"message": "verification email sent"})
}

// ResetPassword handles POST /api/v1/auth/reset-password
//
//	@Summary      Reset password
//	@Description  Sets a new password using a single-use reset token (valid 1 hour).
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body      ResetPasswordRequest  true  "Reset token and new password"
//	@Success      200   {object}  map[string]string
//	@Failure      400   {object}  httpx.ErrorResponse
//	@Failure      422   {object}  httpx.ErrorResponse  "Token invalid or expired"
//	@Router       /auth/reset-password [post]
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
