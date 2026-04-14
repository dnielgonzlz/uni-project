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

	"github.com/danielgonzalez/pt-scheduler/internal/auth"
	"github.com/danielgonzalez/pt-scheduler/internal/availability"
	"github.com/danielgonzalez/pt-scheduler/internal/availability_intake"
	"github.com/danielgonzalez/pt-scheduler/internal/messaging"
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

	usersRepo := users.NewRepository(db)
	usersSvc := users.NewService(usersRepo)
	usersHandler := users.NewHandler(usersSvc)

	emailSvc := messaging.NewEmailService(cfg.ResendAPIKey, cfg.ResendFromAddress)

	authRepo := auth.NewRepository(db)
	authSvc := auth.NewService(
		authRepo, usersRepo, clk,
		cfg.JWTSecret,
		cfg.JWTAccessExpiryMin,
		cfg.JWTRefreshExpiryDays,
		cfg.PasswordResetExpiryMin,
		emailSvc,
		fmt.Sprintf("http://localhost:%s", cfg.Port), // FRONTEND: replace with real app URL in production
	)
	authHandler := auth.NewHandler(authSvc)

	availRepo := availability.NewRepository(db)
	availSvc := availability.NewService(availRepo)
	availHandler := availability.NewHandler(availSvc)

	solver := scheduling.NewHTTPSolver(cfg.SolverURL, cfg.SolverTimeoutSeconds)
	schedRepo := scheduling.NewRepository(db)
	schedSvc := scheduling.NewService(schedRepo, usersRepo, availRepo, solver, clk, db)
	schedHandler := scheduling.NewHandler(schedSvc)

	intakeRepo := availability_intake.NewRepository(db)
	intakeSvc := availability_intake.NewService(intakeRepo, availRepo, log)
	intakeHandler := availability_intake.NewHandler(intakeSvc, log)

	// --- Router ---
	r := chi.NewRouter()

	// Middleware stack (in order per architecture plan)
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(httpx.RequestLogger(log))
	r.Use(middleware.Timeout(30 * time.Second))
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
	r.Use(httpx.RequireJSON)

	// --- Health routes (no auth) ---
	r.Get("/healthz", healthzHandler())
	r.Get("/readyz", readyzHandler(db))

	// --- API v1 ---
	r.Route("/api/v1", func(r chi.Router) {

		// Auth routes — stricter rate limit: 10 req/min per IP
		r.Group(func(r chi.Router) {
			r.Use(httprate.LimitByIP(10, time.Minute))
			r.Post("/auth/register", authHandler.Register)
			r.Post("/auth/login", authHandler.Login)
			r.Post("/auth/forgot-password", authHandler.ForgotPassword)
			r.Post("/auth/reset-password", authHandler.ResetPassword)
		})

		// Auth routes that require a valid (but not necessarily fresh) token
		r.Group(func(r chi.Router) {
			r.Use(httprate.LimitByIP(30, time.Minute))
			r.Post("/auth/logout", authHandler.Logout)
			r.Post("/auth/refresh", authHandler.Refresh)
		})

		// Protected routes — JWT required
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(cfg.JWTSecret))

			// Coach-only routes
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireRole(users.RoleCoach, users.RoleAdmin))
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
				r.Use(auth.RequireRole(users.RoleClient, users.RoleAdmin))
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

			// Phase 4–5 routes registered here as implemented
		})

		// --- Webhooks (no JWT — verified by provider signature in Phase 6) ---
		r.Post("/webhooks/twilio", intakeHandler.InboundSMS)
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

	go func() {
		log.Info("server starting", "port", cfg.Port, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-stop
	log.Info("shutting down server")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	log.Info("server stopped")
}
