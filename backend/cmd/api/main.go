// @title           PT Scheduler API
// @version         1.0
// @description     Intelligent scheduling system for UK personal trainers.
// @termsOfService  http://swagger.io/terms/

// @contact.name   PT Scheduler Support
// @contact.email  support@pt-scheduler.io

// @license.name  MIT

// @host      localhost:8080
// @BasePath  /api/v1

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Enter "Bearer " followed by your access token

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	_ "github.com/danielgonzalez/pt-scheduler/docs" // generated swagger docs
	"github.com/danielgonzalez/pt-scheduler/internal/agent"
	"github.com/danielgonzalez/pt-scheduler/internal/auth"
	"github.com/danielgonzalez/pt-scheduler/internal/availability"
	"github.com/danielgonzalez/pt-scheduler/internal/availability_intake"
	"github.com/danielgonzalez/pt-scheduler/internal/billing"
	"github.com/danielgonzalez/pt-scheduler/internal/calendar"
	"github.com/danielgonzalez/pt-scheduler/internal/messaging"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/audit"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/clock"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/config"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/database"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/httpx"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/logger"
	"github.com/danielgonzalez/pt-scheduler/internal/scheduling"
	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	log := logger.New(cfg.Env)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	log.Info("database connected")

	// --- Dependency wiring ---
	clk := clock.Real{}

	auditLog := audit.NewLogger(db, log)

	usersRepo := users.NewRepository(db)
	usersSvc := users.NewService(usersRepo)
	usersHandler := users.NewHandler(usersSvc, auditLog, log)

	emailSvc := messaging.NewEmailService(cfg.ResendAPIKey, cfg.ResendFromAddress)

	authRepo := auth.NewRepository(db)
	authSvc := auth.NewService(
		authRepo, usersRepo, clk,
		cfg.JWTSecret,
		cfg.JWTAccessExpiryMin,
		cfg.JWTRefreshExpiryDays,
		cfg.PasswordResetExpiryMin,
		emailSvc,
		cfg.AppBaseURL,
	)
	authHandler := auth.NewHandler(authSvc, auditLog)

	availRepo := availability.NewRepository(db)
	availSvc := availability.NewService(availRepo)
	availHandler := availability.NewHandler(availSvc, usersSvc)

	solver := scheduling.NewHTTPSolver(cfg.SolverURL, cfg.SolverTimeoutSeconds)
	schedRepo := scheduling.NewRepository(db)
	schedSvc := scheduling.NewService(schedRepo, usersRepo, availRepo, solver, clk, db)
	schedHandler := scheduling.NewHandler(schedSvc)

	intakeRepo := availability_intake.NewRepository(db)
	var availParser availability_intake.AvailabilityParser = availability_intake.NoopParser{}
	if cfg.OpenRouterAPIKey != "" {
		availParser = availability_intake.NewOpenRouterParser(
			cfg.OpenRouterAPIKey,
			cfg.OpenRouterModel,
			log,
		)
	} else {
		log.Warn("OPENROUTER_API_KEY not set; using rule-based availability intake fallback")
	}
	intakeSvc := availability_intake.NewService(intakeRepo, availRepo, availParser, log)
	intakeHandler := availability_intake.NewHandler(intakeSvc, log, cfg.TwilioAuthToken)

	smsSvc := messaging.NewSMSService(cfg.TwilioAccountSID, cfg.TwilioAuthToken, cfg.TwilioFromNumber, cfg.MessagingChannel)

	agentRepo := agent.NewRepository(db)
	agentTwilio := agent.NewTwilioContentClient(cfg.TwilioAccountSID, cfg.TwilioAuthToken)
	agentSvc := agent.NewService(agentRepo, usersRepo, agentTwilio, smsSvc, log)
	agentHandler := agent.NewHandler(agentSvc, log)

	billingRepo := billing.NewRepository(db)
	stripeClient := billing.NewStripeClient(cfg.StripeSecretKey, cfg.StripeWebhookSecret)
	gcClient := billing.NewGoCardlessClient(cfg.GoCardlessAccessToken, cfg.GoCardlessWebhookSecret, cfg.GoCardlessEnv)
	subRepo := billing.NewSubscriptionRepository(db)
	subSvc := billing.NewSubscriptionService(subRepo, stripeClient, usersRepo, log)
	billingSvc := billing.NewService(billingRepo, stripeClient, gcClient, log).
		WithUserLookup(usersRepo).
		WithSubscriptionService(subRepo, subSvc)
	billingHandler := billing.NewHandler(billingSvc, log)
	subHandler := billing.NewSubscriptionHandler(subSvc, log)

	calendarHandler := calendar.NewHandler(usersRepo, schedRepo, cfg)

	outboxRepo := messaging.NewOutboxRepository(db)
	notifSvc := messaging.NewNotificationService(outboxRepo, emailSvc, smsSvc, log)

	// Wire the notification service into the scheduling service so it can enqueue
	// booking confirmations and cancellation messages after schedule runs are confirmed.
	schedSvc.WithNotifier(notifSvc)

	// Wire the notification service into the billing service so it can alert the coach
	// when a Stripe or GoCardless payment fails.
	billingSvc.WithNotifier(notifSvc)

	// --- Router ---
	r := chi.NewRouter()

	// Global middleware stack (in order per architecture plan)
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(httpx.RequestLogger(log))
	// Long enough for Twilio webhooks that call OpenRouter (multi-attempt, ~35s each) plus DB work.
	r.Use(middleware.Timeout(120 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	// Global: 100 req/min per IP
	r.Use(httprate.LimitByIP(100, time.Minute))
	r.Use(middleware.CleanPath)
	// NOTE: RequireJSON is applied per-route-group below, NOT globally, so that
	// webhook routes (Stripe, GoCardless, Twilio) can receive their native content
	// types and read raw bodies for signature verification.

	// --- Swagger UI (non-production only) ---
	r.Get("/swagger/*", httpSwagger.WrapHandler)

	// --- Health routes (no auth, no content-type enforcement) ---
	r.Get("/healthz", healthzHandler())
	r.Get("/readyz", readyzHandler(db))

	// --- Calendar ICS feed (public, token-authenticated via URL path) ---
	// No JWT — calendar apps poll this URL without user interaction.
	r.Get("/calendar/{token}.ics", calendarHandler.ServeICS)

	// --- API v1 ---
	r.Route("/api/v1", func(r chi.Router) {

		// Webhook routes: raw body, provider-native content types, no RequireJSON.
		// These are authenticated by provider HMAC/signature, not JWT.
		r.Group(func(r chi.Router) {
			r.Post("/webhooks/stripe", billingHandler.StripeWebhook)
			r.Post("/webhooks/gocardless", billingHandler.GoCardlessWebhook)
			// Twilio sends application/x-www-form-urlencoded — also excluded from RequireJSON.
			r.Post("/webhooks/twilio", intakeHandler.InboundSMS)
		})

		// All remaining API routes enforce Content-Type: application/json.
		r.Group(func(r chi.Router) {
			r.Use(httpx.RequireJSON)

			// Auth routes — stricter rate limit: 10 req/min per IP
			r.Group(func(r chi.Router) {
				r.Use(httprate.LimitByIP(10, time.Minute))
				r.Post("/auth/register", authHandler.Register)
				r.Post("/auth/login", authHandler.Login)
				r.Post("/auth/forgot-password", authHandler.ForgotPassword)
				r.Post("/auth/reset-password", authHandler.ResetPassword)
				r.Post("/auth/verify-email", authHandler.VerifyEmail)
			})

			// Auth routes that require a valid (but not necessarily fresh) token
			r.Group(func(r chi.Router) {
				r.Use(httprate.LimitByIP(30, time.Minute))
				r.Post("/auth/logout", authHandler.Logout)
				r.Post("/auth/refresh", authHandler.Refresh)
				r.Post("/auth/resend-verification", authHandler.ResendVerification)
			})

			// Protected routes — JWT required
			r.Group(func(r chi.Router) {
				r.Use(auth.Middleware(cfg.JWTSecret))

				// Coach-only routes
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(users.RoleCoach, users.RoleAdmin))
					r.Get("/coaches/me/clients", usersHandler.ListCoachClients)
					r.Post("/coaches/me/clients", authHandler.CreateClientForCoach)
					r.Delete("/coaches/me/clients/{clientID}", usersHandler.DeleteCoachClient)
					r.Get("/coaches/me/agent-settings", agentHandler.GetSettings)
					r.Put("/coaches/me/agent-settings", agentHandler.UpdateSettings)
					r.Post("/coaches/me/agent-settings/check-template", agentHandler.CheckTemplateStatus)
					r.Get("/coaches/me/agent-clients", agentHandler.ListClients)
					r.Put("/coaches/me/agent-clients/{clientID}", agentHandler.UpdateClientEnabled)
					r.Get("/coaches/me/agent-overview", agentHandler.GetOverview)
					r.Post("/coaches/me/agent-campaigns/send-now", agentHandler.SendCampaignNow)
					r.Get("/coaches/{coachID}/profile", usersHandler.GetCoachProfile)
					r.Put("/coaches/{coachID}/profile", usersHandler.UpdateCoachProfile)
				})

				// Client-only routes
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(users.RoleClient, users.RoleAdmin))
					r.Get("/clients/{clientID}/profile", usersHandler.GetClientProfile)
					r.Put("/clients/{clientID}/profile", usersHandler.UpdateClientProfile)
				})

				// --- Availability ---
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(users.RoleCoach, users.RoleAdmin))
					r.Get("/coaches/{coachID}/availability", availHandler.GetWorkingHours)
					r.Put("/coaches/{coachID}/availability", availHandler.SetWorkingHours)
				})
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(users.RoleCoach, users.RoleClient, users.RoleAdmin))
					r.Get("/clients/{clientID}/preferences", availHandler.GetPreferredWindows)
					r.Put("/clients/{clientID}/preferences", availHandler.SetPreferredWindows)
				})

				// --- Scheduling ---
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(users.RoleCoach, users.RoleAdmin))
					r.Post("/schedule-runs", schedHandler.TriggerScheduleRun)
					r.Post("/schedule-runs/{runID}/confirm", schedHandler.ConfirmScheduleRun)
					r.Post("/schedule-runs/{runID}/reject", schedHandler.RejectScheduleRun)
				})
				r.Get("/schedule-runs/{runID}", schedHandler.GetScheduleRun)
				r.Get("/sessions", schedHandler.ListSessions)
				r.Post("/sessions/{sessionID}/cancel", schedHandler.CancelSession)

				// Reschedule — coach only
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(users.RoleCoach, users.RoleAdmin))
					r.Put("/sessions/{sessionID}", schedHandler.UpdateSession)
				})

				// Cancellation review — coach only
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(users.RoleCoach, users.RoleAdmin))
					r.Post("/sessions/{sessionID}/cancel/approve", schedHandler.ApproveCancellation)
					r.Post("/sessions/{sessionID}/cancel/waive", schedHandler.WaiveCancellation)
				})

				// --- GDPR ---
				r.Get("/me/export", usersHandler.ExportMyData)

				// --- Calendar ---
				r.Get("/me/calendar-url", calendarHandler.GetCalendarURL)
				r.Post("/me/calendar-url/regenerate", calendarHandler.RegenerateCalendarURL)

				// --- Billing (coach only) ---
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(users.RoleCoach, users.RoleAdmin))
					r.Post("/payments/setup-intent", billingHandler.CreateSetupIntent)
					r.Post("/payments/mandate", billingHandler.CreateMandateFlow)
					r.Post("/payments/mandate/complete", billingHandler.CompleteMandateFlow)
					r.Post("/billing/charge", billingHandler.Charge)
					// Subscription plans
					r.Post("/subscription-plans", subHandler.CreatePlan)
					r.Get("/subscription-plans", subHandler.ListPlans)
					r.Put("/subscription-plans/{planID}", subHandler.UpdatePlan)
					r.Delete("/subscription-plans/{planID}", subHandler.ArchivePlan)
					// Client subscriptions
					r.Post("/clients/{clientID}/subscription", subHandler.AssignPlan)
					r.Get("/clients/{clientID}/subscription", subHandler.GetClientSubscription)
					r.Delete("/clients/{clientID}/subscription", subHandler.CancelSubscription)
					r.Post("/clients/{clientID}/subscription/plan-change", subHandler.RequestPlanChange)
					// Plan change approvals
					r.Get("/subscription-plan-changes", subHandler.ListPendingChanges)
					r.Post("/subscription-plan-changes/{changeID}/approve", subHandler.ApprovePlanChange)
					r.Post("/subscription-plan-changes/{changeID}/reject", subHandler.RejectPlanChange)
				})

				// Client subscription view (client-safe — no price)
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(users.RoleClient, users.RoleAdmin))
					r.Get("/me/subscription", subHandler.GetMySubscription)
				})
			})
		})
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// Start the notification outbox worker. It shares the server's lifecycle:
	// when the stop signal fires we cancel workerCtx before closing the DB pool.
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	notifWorker := messaging.NewWorker(outboxRepo, notifSvc, log)
	go notifWorker.Run(workerCtx)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		_ = agentSvc.RunDueCampaigns(workerCtx, time.Now())
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				_ = agentSvc.RunDueCampaigns(workerCtx, time.Now())
			}
		}
	}()

	go func() {
		log.Info("server starting", "port", cfg.Port, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-stop
	log.Info("shutting down server")

	// Stop the notification worker before closing the DB pool.
	workerCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("server stopped")
}
