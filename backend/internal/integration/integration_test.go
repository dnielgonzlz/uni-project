//go:build integration

// Package integration contains end-to-end tests that run against a real
// PostgreSQL instance spun up via testcontainers.
//
// Run with:
//
//	go test -tags integration -race ./internal/integration/
package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/danielgonzalez/pt-scheduler/internal/auth"
	"github.com/danielgonzalez/pt-scheduler/internal/billing"
	"github.com/danielgonzalez/pt-scheduler/internal/platform/clock"
	"github.com/danielgonzalez/pt-scheduler/internal/scheduling"
	"github.com/danielgonzalez/pt-scheduler/internal/users"
)

// testDB holds the shared pool for the test suite.
var testDB *pgxpool.Pool

// TestMain starts Postgres, runs all migrations, then executes the suite.
func TestMain(m *testing.M) {
	ctx := context.Background()

	pgc, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("ptscheduler_test"),
		tcpostgres.WithUsername("pttest"),
		tcpostgres.WithPassword("pttest"),
		tcpostgres.WithSQLDriver("pgx"),
		tcpostgres.WithInitScripts(
			"../../migrations/000001_initial_schema.up.sql",
			"../../migrations/000002_outbox.up.sql",
			"../../migrations/000003_pending_cancellation.up.sql",
			"../../migrations/000004_calendar_token.up.sql",
			"../../migrations/000005_agent_settings.up.sql",
			"../../migrations/000006_agent_campaigns.up.sql",
			"../../migrations/000007_parse_status.up.sql",
			"../../migrations/000008_coach_max_sessions.up.sql",
			"../../migrations/000009_email_verification.up.sql",
			"../../migrations/000010_subscriptions.up.sql",
		),
		testcontainers.WithAdditionalWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start postgres container: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = pgc.Terminate(ctx) }()

	connStr, err := pgc.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get connection string: %v\n", err)
		os.Exit(1)
	}

	testDB, err = pgxpool.New(ctx, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create pool: %v\n", err)
		os.Exit(1)
	}
	defer testDB.Close()

	os.Exit(m.Run())
}

// --- Auth: register + login ---

func TestAuthRegisterAndLogin(t *testing.T) {
	ctx := context.Background()
	clk := clock.Real{}

	usersRepo := users.NewRepository(testDB)
	authRepo := auth.NewRepository(testDB)

	authSvc := auth.NewService(
		authRepo, usersRepo, clk,
		"test-jwt-secret-32-chars-padding!",
		15,    // access expiry minutes
		7,     // refresh expiry days
		60,    // password reset expiry minutes
		&noopMailer{},
		"http://localhost:3000",
	)

	email := fmt.Sprintf("coach+%s@test.com", uuid.New().String()[:8])

	t.Run("register new coach", func(t *testing.T) {
		busName := "Test Gym"
		tokens, err := authSvc.Register(ctx, auth.RegisterRequest{
			Email:        email,
			Password:     "securepass123",
			FullName:     "Test Coach",
			Role:         users.RoleCoach,
			Timezone:     "Europe/London",
			BusinessName: &busName,
		})
		require.NoError(t, err)
		require.NotEmpty(t, tokens.AccessToken)
		require.NotEmpty(t, tokens.RefreshToken)
		require.NotEqual(t, uuid.Nil, tokens.UserID)
	})

	t.Run("duplicate email rejected", func(t *testing.T) {
		_, err := authSvc.Register(ctx, auth.RegisterRequest{
			Email:    email,
			Password: "anotherpass123",
			FullName: "Duplicate Coach",
			Role:     users.RoleCoach,
			Timezone: "Europe/London",
		})
		require.ErrorIs(t, err, users.ErrEmailTaken)
	})

	t.Run("login with correct credentials", func(t *testing.T) {
		tokens, err := authSvc.Login(ctx, auth.LoginRequest{
			Email:    email,
			Password: "securepass123",
		})
		require.NoError(t, err)
		require.NotEmpty(t, tokens.AccessToken)
	})

	t.Run("login with wrong password rejected", func(t *testing.T) {
		_, err := authSvc.Login(ctx, auth.LoginRequest{
			Email:    email,
			Password: "wrongpassword",
		})
		require.ErrorIs(t, err, auth.ErrInvalidPassword)
	})
}

// --- Billing: payment idempotency ---

func TestPaymentIdempotency(t *testing.T) {
	ctx := context.Background()

	usersRepo := users.NewRepository(testDB)

	coachUser, err := usersRepo.CreateUser(ctx, &users.User{
		Email:        fmt.Sprintf("coach-billing+%s@test.com", uuid.New().String()[:8]),
		PasswordHash: "hash",
		Role:         users.RoleCoach,
		FullName:     "Billing Coach",
		Timezone:     "Europe/London",
	})
	require.NoError(t, err)

	businessName := "Billing Gym"
	coach, err := usersRepo.CreateCoach(ctx, coachUser.ID, &businessName)
	require.NoError(t, err)

	clientUser, err := usersRepo.CreateUser(ctx, &users.User{
		Email:        fmt.Sprintf("client-billing+%s@test.com", uuid.New().String()[:8]),
		PasswordHash: "hash",
		Role:         users.RoleClient,
		FullName:     "Billing Client",
		Timezone:     "Europe/London",
	})
	require.NoError(t, err)

	client, err := usersRepo.CreateClient(ctx, clientUser.ID, coach.ID, 4)
	require.NoError(t, err)

	billingRepo := billing.NewRepository(testDB)

	year, month := 2026, 1
	idempKey := billing.IdempotencyKey(billing.ProviderStripe, client.ID, year, month)
	providerRef := "pi_test_001"

	t.Run("first payment insert succeeds", func(t *testing.T) {
		payment := &billing.Payment{
			ClientID:       client.ID,
			Provider:       billing.ProviderStripe,
			ProviderRef:    &providerRef,
			AmountPence:    5000,
			Currency:       "GBP",
			BillingYear:    year,
			BillingMonth:   month,
			Status:         billing.PaymentStatusPending,
			IdempotencyKey: idempKey,
		}
		created, err := billingRepo.CreatePayment(ctx, payment)
		require.NoError(t, err)
		require.NotEqual(t, uuid.Nil, created.ID)
	})

	t.Run("second insert with same idempotency key is a no-op", func(t *testing.T) {
		// Different provider ref simulates a retry with a new PI id.
		providerRef2 := "pi_test_002"
		payment := &billing.Payment{
			ClientID:       client.ID,
			Provider:       billing.ProviderStripe,
			ProviderRef:    &providerRef2,
			AmountPence:    5000,
			Currency:       "GBP",
			BillingYear:    year,
			BillingMonth:   month,
			Status:         billing.PaymentStatusPending,
			IdempotencyKey: idempKey,
		}
		created, err := billingRepo.CreatePayment(ctx, payment)
		// The repository returns the existing row (or nil) on conflict — no error.
		require.NoError(t, err)
		// created may be nil when ON CONFLICT DO NOTHING returns no rows.
		if created != nil {
			require.Equal(t, "pi_test_001", *created.ProviderRef, "original provider ref preserved")
		}
	})
}

// --- Scheduling: double-booking exclusion constraint ---

func TestSessionExclusionConstraint(t *testing.T) {
	ctx := context.Background()

	usersRepo := users.NewRepository(testDB)

	coachUser, err := usersRepo.CreateUser(ctx, &users.User{
		Email:        fmt.Sprintf("coach-sched+%s@test.com", uuid.New().String()[:8]),
		PasswordHash: "hash",
		Role:         users.RoleCoach,
		FullName:     "Sched Coach",
		Timezone:     "Europe/London",
	})
	require.NoError(t, err)
	coach, err := usersRepo.CreateCoach(ctx, coachUser.ID, nil)
	require.NoError(t, err)

	clientUser, err := usersRepo.CreateUser(ctx, &users.User{
		Email:        fmt.Sprintf("client-sched+%s@test.com", uuid.New().String()[:8]),
		PasswordHash: "hash",
		Role:         users.RoleClient,
		FullName:     "Sched Client",
		Timezone:     "Europe/London",
	})
	require.NoError(t, err)
	client, err := usersRepo.CreateClient(ctx, clientUser.ID, coach.ID, 4)
	require.NoError(t, err)

	schedRepo := scheduling.NewRepository(testDB)

	startsAt := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)  // Monday 09:00
	endsAt := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)   // Monday 10:00

	t.Run("first session inserts OK", func(t *testing.T) {
		_, err := schedRepo.CreateSession(ctx, &scheduling.Session{
			CoachID:  coach.ID,
			ClientID: client.ID,
			StartsAt: startsAt,
			EndsAt:   endsAt,
			Status:   scheduling.StatusConfirmed,
		})
		require.NoError(t, err)
	})

	t.Run("overlapping session for same coach is rejected by DB exclusion constraint", func(t *testing.T) {
		// Overlaps by 30 minutes with the first session.
		_, err := schedRepo.CreateSession(ctx, &scheduling.Session{
			CoachID:  coach.ID,
			ClientID: client.ID,
			StartsAt: startsAt.Add(30 * time.Minute),
			EndsAt:   endsAt.Add(30 * time.Minute),
			Status:   scheduling.StatusConfirmed,
		})
		require.Error(t, err, "DB exclusion constraint should reject overlapping session")
	})
}

// --- helpers ---

// noopMailer satisfies auth.Mailer without sending real emails.
type noopMailer struct{}

func (n *noopMailer) SendPasswordReset(_ context.Context, _, _, _ string) error     { return nil }
func (n *noopMailer) SendVerificationEmail(_ context.Context, _, _, _ string) error { return nil }
func (n *noopMailer) SendEmail(_ context.Context, _, _, _, _ string) error           { return nil }
