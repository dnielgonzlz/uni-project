package billing

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/danielgonzalez/pt-scheduler/internal/platform/ctxkeys"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/validator"
)

// SubscriptionHandler exposes subscription plan and billing endpoints.
type SubscriptionHandler struct {
	svc    *SubscriptionService
	logger *slog.Logger
}

func NewSubscriptionHandler(svc *SubscriptionService, logger *slog.Logger) *SubscriptionHandler {
	return &SubscriptionHandler{svc: svc, logger: logger}
}

func (h *SubscriptionHandler) decodeAndValidate(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if err := validator.Validate.Struct(dst); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":  "validation failed",
			"fields": validator.ValidationErrors(err),
		})
		return false
	}
	return true
}

// CreatePlan handles POST /api/v1/subscription-plans
//
//	@Summary      Create subscription plan
//	@Description  Creates a subscription plan with an associated Stripe Product and Price.
//	@Tags         billing
//	@Accept       json
//	@Produce      json
//	@Param        body  body      CreatePlanRequest  true  "Plan details"
//	@Success      201   {object}  SubscriptionPlan
//	@Failure      400   {object}  httpx.ErrorResponse
//	@Failure      422   {object}  httpx.ValidationErrorResponse
//	@Security     BearerAuth
//	@Router       /subscription-plans [post]
func (h *SubscriptionHandler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	var req CreatePlanRequest
	if !h.decodeAndValidate(w, r, &req) {
		return
	}

	plan, err := h.svc.CreatePlan(r.Context(), coachUserID, req)
	if err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, plan)
}

// ListPlans handles GET /api/v1/subscription-plans
//
//	@Summary      List subscription plans
//	@Description  Returns all subscription plans created by the authenticated coach.
//	@Tags         billing
//	@Produce      json
//	@Success      200  {array}   SubscriptionPlan
//	@Security     BearerAuth
//	@Router       /subscription-plans [get]
func (h *SubscriptionHandler) ListPlans(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	plans, err := h.svc.ListPlans(r.Context(), coachUserID)
	if err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	if plans == nil {
		plans = []SubscriptionPlan{}
	}
	httpx.JSON(w, http.StatusOK, plans)
}

// UpdatePlan handles PUT /api/v1/subscription-plans/{planID}
//
//	@Summary      Update subscription plan
//	@Description  Updates plan name, description, and sessions_included. Price changes require creating a new plan.
//	@Tags         billing
//	@Accept       json
//	@Produce      json
//	@Param        planID  path      string            true  "Plan UUID"
//	@Param        body    body      UpdatePlanRequest  true  "Updated fields"
//	@Success      200     {object}  SubscriptionPlan
//	@Failure      400     {object}  httpx.ErrorResponse
//	@Failure      404     {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /subscription-plans/{planID} [put]
func (h *SubscriptionHandler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	planID, err := uuid.Parse(chi.URLParam(r, "planID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid plan ID")
		return
	}

	var req UpdatePlanRequest
	if !h.decodeAndValidate(w, r, &req) {
		return
	}

	plan, err := h.svc.UpdatePlan(r.Context(), coachUserID, planID, req)
	if err != nil {
		if errors.Is(err, ErrPlanNotFound) {
			httpx.Error(w, http.StatusNotFound, "plan not found")
			return
		}
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	httpx.JSON(w, http.StatusOK, plan)
}

// ArchivePlan handles DELETE /api/v1/subscription-plans/{planID}
//
//	@Summary      Archive subscription plan
//	@Description  Archives a plan so it can no longer be assigned to new clients.
//	@Tags         billing
//	@Param        planID  path  string  true  "Plan UUID"
//	@Success      204
//	@Failure      404  {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /subscription-plans/{planID} [delete]
func (h *SubscriptionHandler) ArchivePlan(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	planID, err := uuid.Parse(chi.URLParam(r, "planID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid plan ID")
		return
	}

	if err := h.svc.ArchivePlan(r.Context(), coachUserID, planID); err != nil {
		if errors.Is(err, ErrPlanNotFound) {
			httpx.Error(w, http.StatusNotFound, "plan not found")
			return
		}
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AssignPlan handles POST /api/v1/clients/{clientID}/subscription
//
//	@Summary      Assign subscription plan to client
//	@Description  Creates a Stripe Subscription for the client and grants their first session credits.
//	@Tags         billing
//	@Accept       json
//	@Produce      json
//	@Param        clientID  path      string            true  "Client UUID"
//	@Param        body      body      AssignPlanRequest  true  "Plan to assign"
//	@Success      201       {object}  ClientSubscription
//	@Failure      400       {object}  httpx.ErrorResponse
//	@Failure      409       {object}  httpx.ErrorResponse  "Client already has a subscription"
//	@Security     BearerAuth
//	@Router       /clients/{clientID}/subscription [post]
func (h *SubscriptionHandler) AssignPlan(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	clientID, err := uuid.Parse(chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid client ID")
		return
	}

	var req AssignPlanRequest
	if !h.decodeAndValidate(w, r, &req) {
		return
	}

	planID, _ := uuid.Parse(req.PlanID)

	result, err := h.svc.AssignPlan(r.Context(), coachUserID, clientID, planID)
	if err != nil {
		if errors.Is(err, ErrSubscriptionAlreadyExists) {
			httpx.Error(w, http.StatusConflict, "client already has an active subscription")
			return
		}
		if errors.Is(err, ErrNoCardOnFile) {
			httpx.Error(w, http.StatusBadRequest, "client has no payment card on file — set up card first")
			return
		}
		if errors.Is(err, ErrPlanNotFound) {
			httpx.Error(w, http.StatusNotFound, "plan not found")
			return
		}
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, result)
}

// GetClientSubscription handles GET /api/v1/clients/{clientID}/subscription
//
//	@Summary      Get client subscription (coach view)
//	@Description  Returns the client's subscription detail including sessions balance and plan info.
//	@Tags         billing
//	@Produce      json
//	@Param        clientID  path      string  true  "Client UUID"
//	@Success      200       {object}  ClientSubscriptionDetail
//	@Failure      404       {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /clients/{clientID}/subscription [get]
func (h *SubscriptionHandler) GetClientSubscription(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	clientID, err := uuid.Parse(chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid client ID")
		return
	}

	detail, err := h.svc.GetClientSubscriptionDetail(r.Context(), coachUserID, clientID)
	if err != nil {
		if errors.Is(err, ErrSubscriptionNotFound) {
			httpx.Error(w, http.StatusNotFound, "no subscription found for this client")
			return
		}
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	httpx.JSON(w, http.StatusOK, detail)
}

// CancelSubscription handles DELETE /api/v1/clients/{clientID}/subscription
//
//	@Summary      Cancel client subscription
//	@Description  Cancels the Stripe subscription immediately and marks it cancelled in the database.
//	@Tags         billing
//	@Param        clientID  path  string  true  "Client UUID"
//	@Success      204
//	@Failure      404  {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /clients/{clientID}/subscription [delete]
func (h *SubscriptionHandler) CancelSubscription(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	clientID, err := uuid.Parse(chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid client ID")
		return
	}

	if err := h.svc.CancelClientSubscription(r.Context(), coachUserID, clientID); err != nil {
		if errors.Is(err, ErrSubscriptionNotFound) {
			httpx.Error(w, http.StatusNotFound, "no subscription found for this client")
			return
		}
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RequestPlanChange handles POST /api/v1/clients/{clientID}/subscription/plan-change
//
//	@Summary      Request plan change
//	@Description  Stages a plan change for coach approval. The change is not applied until approved.
//	@Tags         billing
//	@Accept       json
//	@Produce      json
//	@Param        clientID  path      string                   true  "Client UUID"
//	@Param        body      body      RequestPlanChangeRequest  true  "New plan ID"
//	@Success      201       {object}  PlanChange
//	@Failure      400       {object}  httpx.ErrorResponse
//	@Failure      404       {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /clients/{clientID}/subscription/plan-change [post]
func (h *SubscriptionHandler) RequestPlanChange(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	clientID, err := uuid.Parse(chi.URLParam(r, "clientID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid client ID")
		return
	}

	var req RequestPlanChangeRequest
	if !h.decodeAndValidate(w, r, &req) {
		return
	}

	newPlanID, _ := uuid.Parse(req.NewPlanID)

	change, err := h.svc.RequestPlanChange(r.Context(), coachUserID, clientID, newPlanID)
	if err != nil {
		if errors.Is(err, ErrSubscriptionNotFound) || errors.Is(err, ErrPlanNotFound) {
			httpx.Error(w, http.StatusNotFound, "subscription or plan not found")
			return
		}
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	httpx.JSON(w, http.StatusCreated, change)
}

// ListPendingChanges handles GET /api/v1/subscription-plan-changes
//
//	@Summary      List pending plan changes
//	@Description  Returns all pending plan change requests for the coach's clients.
//	@Tags         billing
//	@Produce      json
//	@Success      200  {array}   PlanChange
//	@Security     BearerAuth
//	@Router       /subscription-plan-changes [get]
func (h *SubscriptionHandler) ListPendingChanges(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	changes, err := h.svc.ListPendingPlanChanges(r.Context(), coachUserID)
	if err != nil {
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	if changes == nil {
		changes = []PlanChange{}
	}
	httpx.JSON(w, http.StatusOK, changes)
}

// ApprovePlanChange handles POST /api/v1/subscription-plan-changes/{changeID}/approve
//
//	@Summary      Approve plan change
//	@Description  Applies the staged plan change immediately in Stripe and adjusts the session balance.
//	@Tags         billing
//	@Param        changeID  path      string  true  "Plan change UUID"
//	@Success      200       {object}  PlanChange
//	@Failure      404       {object}  httpx.ErrorResponse
//	@Failure      409       {object}  httpx.ErrorResponse  "Change is not pending"
//	@Security     BearerAuth
//	@Router       /subscription-plan-changes/{changeID}/approve [post]
func (h *SubscriptionHandler) ApprovePlanChange(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	changeID, err := uuid.Parse(chi.URLParam(r, "changeID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid change ID")
		return
	}

	change, err := h.svc.ApprovePlanChange(r.Context(), coachUserID, changeID)
	if err != nil {
		if errors.Is(err, ErrPlanChangeNotPending) {
			httpx.Error(w, http.StatusConflict, "plan change is not pending")
			return
		}
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	httpx.JSON(w, http.StatusOK, change)
}

// RejectPlanChange handles POST /api/v1/subscription-plan-changes/{changeID}/reject
//
//	@Summary      Reject plan change
//	@Description  Rejects a pending plan change. The client's current plan is unchanged.
//	@Tags         billing
//	@Param        changeID  path  string  true  "Plan change UUID"
//	@Success      204
//	@Failure      404  {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /subscription-plan-changes/{changeID}/reject [post]
func (h *SubscriptionHandler) RejectPlanChange(w http.ResponseWriter, r *http.Request) {
	coachUserID := ctxkeys.UserIDFromContext(r.Context())
	if coachUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	changeID, err := uuid.Parse(chi.URLParam(r, "changeID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid change ID")
		return
	}

	if err := h.svc.RejectPlanChange(r.Context(), coachUserID, changeID); err != nil {
		if errors.Is(err, ErrPlanChangeNotPending) {
			httpx.Error(w, http.StatusConflict, "plan change is not pending")
			return
		}
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetMySubscription handles GET /api/v1/me/subscription
//
//	@Summary      Get my subscription (client view)
//	@Description  Returns the client's plan name and session balance. Price is intentionally excluded.
//	@Tags         billing
//	@Produce      json
//	@Success      200  {object}  ClientSubscriptionView
//	@Failure      404  {object}  httpx.ErrorResponse
//	@Security     BearerAuth
//	@Router       /me/subscription [get]
func (h *SubscriptionHandler) GetMySubscription(w http.ResponseWriter, r *http.Request) {
	clientUserID := ctxkeys.UserIDFromContext(r.Context())
	if clientUserID == uuid.Nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorised")
		return
	}

	view, err := h.svc.GetClientSubscriptionView(r.Context(), clientUserID)
	if err != nil {
		if errors.Is(err, ErrSubscriptionNotFound) {
			httpx.Error(w, http.StatusNotFound, "no active subscription")
			return
		}
		httpx.InternalError(w, r, h.logger, err)
		return
	}

	httpx.JSON(w, http.StatusOK, view)
}
