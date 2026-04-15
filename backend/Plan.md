# PT Scheduler — Backend Implementation Plan

> Last updated: Phase 5 complete

---

## Stack

| Concern | Choice |
|---|---|
| Language | Go |
| Router | chi v5 |
| Database | PostgreSQL (pgx/v5, no ORM) |
| Migrations | golang-migrate |
| Auth | JWT (golang-jwt/v5) + argon2id passwords |
| Validation | go-playground/validator v10 |
| Config | envconfig |
| Logging | stdlib slog (text dev / JSON prod) |
| Email | Resend |
| SMS | Twilio (WhatsApp upgrade path via `MESSAGING_CHANNEL` env var) |
| Payments | Stripe (cards) + GoCardless (Direct Debit) |
| Scheduling | Python FastAPI + OR-Tools CP-SAT (HTTP microservice → AWS Lambda) |
| Docs | swaggo/swag |
| Testing | stdlib testing + testify + testcontainers |
| Deploy | AWS Elastic Beanstalk (Go API) + AWS RDS (Postgres) + AWS Lambda (solver) |

---

## Key Decisions

- **Standalone coaches** — no gym/studio multi-tenancy
- **Fixed 60-min sessions**
- **Monthly billing** — one charge per client per calendar month, not per session
- **Session credits** — cancelled sessions (within allowed window) generate a credit for rebooking, no refunds
- **Trainer-controlled monthly assignments** — coach sets session count per client per month (supports mid-month onboarding)
- **Twilio SMS for MVP** — WhatsApp Business API takes too long to approve; toggle later with `MESSAGING_CHANNEL=whatsapp`
- **No Docker** — local dev uses native Postgres; prod uses AWS managed services

---

## Architecture

```
backend/
├── cmd/
│   ├── api/            # HTTP server entrypoint
│   └── migrate/        # Migration CLI
├── internal/
│   ├── auth/           # JWT, login/logout, password reset
│   ├── users/          # Coach + client profiles
│   ├── availability/   # Working hours, client preferences
│   ├── scheduling/     # Booking engine, OR-Tools client, constraints
│   ├── billing/        # Stripe + GoCardless
│   ├── messaging/      # Twilio SMS + Resend email
│   ├── availability_intake/  # SMS availability collection flow
│   └── platform/       # Config, DB, HTTP helpers, logger, validator, clock
├── migrations/         # SQL up/down pairs
├── solver/             # Python FastAPI + OR-Tools (separate service)
└── .github/workflows/  # CI/CD
```

---

## Phases

---

### Phase 1 — Foundation ✅ COMPLETE

**Goal**: Compilable project with a running HTTP server, database connection, and full schema.

- [x] `go.mod` initialised (`github.com/danielgonzalez/pt-scheduler`)
- [x] All package directories created
- [x] `internal/platform/config` — envconfig struct for all env vars
- [x] `internal/platform/logger` — slog (text / JSON based on `ENV`)
- [x] `internal/platform/database` — pgx pool with ping validation
- [x] `internal/platform/httpx` — JSON/Error response helpers + request logger middleware
- [x] `internal/platform/validator` — go-playground/validator wrapper
- [x] `internal/platform/clock` — injectable clock for testable time logic
- [x] `cmd/api/main.go` — chi router, full middleware stack, graceful shutdown
- [x] `cmd/api/handlers.go` — `/healthz` and `/readyz` endpoints
- [x] `cmd/migrate/main.go` — migration CLI (`up` / `down N`)
- [x] `migrations/000001_initial_schema` — all 15 tables, indexes, exclusion constraints for no-double-booking
- [x] `.env.example` — all variables documented
- [x] `Makefile` — `run`, `migrate-up`, `migrate-down`, `test`, `lint`, `build`
- [x] `.gitignore`
- [x] All dependencies installed and `go build ./...` passes

**To run locally:**
```bash
cp .env.example .env   # fill in DATABASE_URL and JWT_SECRET at minimum
make migrate-up        # creates all tables
make run               # starts server on :8080
```

---

### Phase 2 — Auth & Users ✅ COMPLETE

**Goal**: Registration, login, logout, token refresh, password reset, RBAC middleware.

- [x] `internal/users/model.go` — User, Coach, Client structs
- [x] `internal/users/repository.go` — DB queries for users, coaches, clients
- [x] `internal/users/service.go` — create coach/client, fetch profile
- [x] `internal/users/handler.go` — GET/PUT profile endpoints
- [x] `internal/auth/model.go` — token claims structs
- [x] `internal/auth/repository.go` — refresh token + password reset token DB queries
- [x] `internal/auth/password.go` — argon2id hashing + SHA-256 token hashing
- [x] `internal/auth/jwt.go` — JWT generation and parsing
- [x] `internal/auth/service.go` — register, login, logout, refresh, forgot/reset password
- [x] `internal/auth/handler.go` — HTTP handlers for all auth endpoints
- [x] `internal/auth/middleware.go` — JWT verify middleware + role-based authz
- [x] Rate limiting tightened on `/api/v1/auth/*` routes (10 req/min)
- [x] `internal/messaging/email.go` — Resend wrapper for password reset email
- [x] Unit tests: password hashing, token generation, JWT sign/verify/expiry, clock
- [x] All routes wired in `cmd/api/main.go`

**Endpoints added:**
```
POST /api/v1/auth/register
POST /api/v1/auth/login
POST /api/v1/auth/logout
POST /api/v1/auth/refresh
POST /api/v1/auth/forgot-password
POST /api/v1/auth/reset-password
GET  /api/v1/coaches/{id}/profile
PUT  /api/v1/coaches/{id}/profile
GET  /api/v1/clients/{id}/profile
PUT  /api/v1/clients/{id}/profile
```

---

### Phase 3 — Availability & Core Scheduling ✅ COMPLETE

**Goal**: Trainer working hours, client preferences, session booking with constraint enforcement, OR-Tools integration, schedule confirmation flow.

- [x] `internal/availability/` — model, repository (replace working hours + preferred windows), service, handler
- [x] `internal/scheduling/model.go` — Session, ScheduleRun, SessionCredit, SolverRequest/Response
- [x] `internal/scheduling/constraints.go` — CheckRecoveryPeriod, CheckDailyLimit, CheckWithinWorkingHours, CancellationEarnsCredit
- [x] `internal/scheduling/repository.go` — session + schedule run + credit DB queries; ExpireOldRuns
- [x] `internal/scheduling/solver_client.go` — Solver interface + HTTPSolver (30s timeout, no retries)
- [x] `internal/scheduling/service.go` — TriggerScheduleRun, GetScheduleRun, Confirm, Reject, ListSessions, CancelSession
- [x] `internal/scheduling/handler.go` — 6 HTTP handlers with full error mapping
- [x] `solver/solver.py` — OR-Tools CP-SAT model (all hard + soft constraints, 30-min slots, 25s time limit)
- [x] `solver/main.py` — FastAPI wrapper (POST /solve, GET /healthz)
- [x] `solver/requirements.txt`
- [x] `internal/availability_intake/` — SMS state machine (idle → awaiting_days → awaiting_times → complete)
- [x] `internal/availability_intake/handler.go` — Twilio TwiML webhook handler
- [x] Unit tests: 14 constraint tests (recovery period, daily limit, working hours, cancellation credit)
- [x] All routes wired in `cmd/api/main.go`

**Endpoints added:**
```
GET  /api/v1/coaches/{id}/availability
PUT  /api/v1/coaches/{id}/availability
GET  /api/v1/clients/{id}/preferences
PUT  /api/v1/clients/{id}/preferences
GET  /api/v1/sessions
POST /api/v1/sessions/{id}/cancel
POST /api/v1/schedule-runs
GET  /api/v1/schedule-runs/{id}
POST /api/v1/schedule-runs/{id}/confirm
POST /api/v1/schedule-runs/{id}/reject
```

---

### Phase 4 — Billing ✅ COMPLETE

**Goal**: Monthly Stripe charges, GoCardless Direct Debit mandates, webhook idempotency.

- [x] `internal/billing/model.go` — Payment, Mandate, WebhookEvent; request/response types; IdempotencyKey helper
- [x] `internal/billing/repository.go` — CreatePayment (idempotent), GetPaymentByProviderRef, UpdatePaymentStatus, ListPaymentsByClient, UpsertMandate, GetMandateByClientID, RecordWebhookEvent; BacsEarliestChargeDate (skips weekends, 3-day/2-day advance notice)
- [x] `internal/billing/stripe.go` — StripeClient: CreateOrGetCustomer, CreateSetupIntent, ChargeMonthly (off_session SCA/3DS2), VerifyWebhookSignature
- [x] `internal/billing/gocardless.go` — GoCardlessClient (raw HTTP, no SDK): CreateRedirectFlow, CompleteRedirectFlow, CreatePayment (with Bacs charge_date), VerifyWebhookSignature (HMAC-SHA256)
- [x] `internal/billing/service.go` — CreateSetupIntent, CreateMandateFlow, CompleteMandateFlow, ChargeMonthly (idempotent, Stripe + GoCardless), HandleStripeWebhook, HandleGoCardlessWebhook
- [x] `internal/billing/handler.go` — HTTP handlers for setup-intent, mandate, mandate/complete, charge, Stripe webhook, GoCardless webhook
- [x] `internal/billing/utils.go` — parseJSON helper
- [x] Webhook idempotency via `webhook_events` table (ON CONFLICT DO NOTHING)
- [x] `RequireJSON` moved off global middleware — now scoped to JSON API routes only; webhook routes bypass it for raw body + HMAC verification
- [x] All billing routes wired in `cmd/api/main.go`
- [x] Unit tests: 14 tests covering IdempotencyKey format/uniqueness, BacsEarliestChargeDate weekend-skipping, pad helpers

**Endpoints added:**
```
POST /api/v1/payments/setup-intent
POST /api/v1/payments/mandate
POST /api/v1/payments/mandate/complete
POST /api/v1/billing/charge
POST /api/v1/webhooks/stripe
POST /api/v1/webhooks/gocardless
```

---

### Phase 5 — Notifications ✅ COMPLETE

**Goal**: Email and SMS notifications for bookings, reminders, and cancellations via transactional outbox.

- [x] `migrations/000002_outbox.up.sql` — `notification_outbox` table with `pending/processing/done/failed` status, `process_after` for scheduled delivery, `SELECT FOR UPDATE SKIP LOCKED` for safe concurrent workers
- [x] `internal/messaging/sms.go` — Twilio SMS wrapper; WhatsApp channel toggle via `MESSAGING_CHANNEL=whatsapp`
- [x] `internal/messaging/templates.go` — UK-localised email (HTML) and SMS templates for: booking confirmation, 24h reminder, cancellation (with/without credit), payment failed coach alert
- [x] `internal/messaging/outbox.go` — OutboxRepository: Enqueue, ClaimBatch (SKIP LOCKED), MarkDone, MarkFailed (retry up to 5 attempts)
- [x] `internal/messaging/service.go` — NotificationService: enqueue helpers + Deliver dispatcher; implements `scheduling.Notifier` interface (NotifySessionsConfirmed, NotifySessionCancelled)
- [x] `internal/messaging/worker.go` — background poll loop (15s interval, batch size 10, graceful shutdown via context)
- [x] `internal/messaging/email.go` — added generic SendEmail method
- [x] `scheduling.Notifier` interface added to scheduling package — thin interface keeping scheduling ↔ messaging decoupled
- [x] `scheduling.Service.WithNotifier()` — attaches notifier post-construction; nil-safe (notifications are best-effort, never fail a booking)
- [x] Scheduling service enqueues confirmations after `ConfirmScheduleRun` and cancellation notice after `CancelSession`
- [x] Worker goroutine started in `main.go` with proper lifecycle (cancelled before DB pool closes)
- [x] `POST /api/v1/webhooks/twilio` — already wired (Phase 3 SMS intake)

---

### Phase 6 — Production Readiness 🔲

**Goal**: Swagger docs, monitoring, CI/CD pipeline, GDPR endpoints, integration tests.

- [ ] Swagger annotations on all handlers (`swaggo/swag`)
- [ ] Sentry integration (error tracking + spike alerts)
- [ ] CloudWatch log shipping (automatic via Elastic Beanstalk)
- [ ] CloudWatch alarms: 5xx rate > 1%, p95 latency > 5s, payment failure > 5%
- [ ] GitHub Actions CI: lint → vet → test → build → deploy to Elastic Beanstalk
- [ ] Integration tests with testcontainers (real Postgres)
- [ ] `GET /me/export` — GDPR data export endpoint
- [ ] Audit log populated by key actions (login, booking, payment)
- [ ] DB index review pass
- [ ] Load test the `/schedule-runs` endpoint

---

## Database Schema (summary)

| Table | Purpose |
|---|---|
| `users` | All accounts (coach / client / admin) |
| `coaches` | Coach profile (1:1 with users) |
| `clients` | Client profile, linked to coach |
| `client_monthly_assignments` | Sessions per client per calendar month |
| `trainer_working_hours` | Days + times a coach is available |
| `client_preferred_windows` | Client's preferred session times |
| `schedule_runs` | Each OR-Tools solver invocation |
| `sessions` | Individual booked sessions (exclusion constraints prevent double-booking) |
| `payments` | Monthly charge per client |
| `session_credits` | Replacement session credits from cancellations |
| `gocardless_mandates` | DD mandate per client |
| `refresh_tokens` | JWT refresh token store |
| `password_reset_tokens` | Single-use password reset tokens |
| `webhook_events` | Idempotency store for inbound webhooks |
| `availability_intake_conversations` | SMS state machine per client |
| `audit_log` | Append-only action log |

---

## AWS Deployment

| Component | Service |
|---|---|
| Go API | Elastic Beanstalk (Go platform, binary ZIP) |
| OR-Tools solver | Lambda (Python 3.12 + OR-Tools layer) |
| PostgreSQL | RDS PostgreSQL 16 (db.t3.micro) |
| Secrets | AWS Secrets Manager |
| Logs | CloudWatch Logs |
| Uptime | UptimeRobot → `/healthz` |

**Deploy flow (GitHub Actions):**
```
merge to main → build Linux binary → zip → EB deploy → Lambda update
tag v* → manual approval gate → run migrations → deploy prod
```

---

## Hard Constraints (enforced in DB + service layer)

| Constraint | Enforcement |
|---|---|
| No trainer double-booking | DB exclusion constraint on `sessions` |
| No client double-booking | DB exclusion constraint on `sessions` |
| Sessions within working hours | Pre-solver check in `constraints.go` |
| 24h recovery between sessions | Pre-solver check in `constraints.go` |
| Max 4 sessions/day (5 exception) | Pre-solver check in `constraints.go` |
| Trainer confirmation required | `schedule_runs.status` state machine |
| Monthly session count | `client_monthly_assignments` + solver input |

---

## Soft Constraints (OR-Tools CP-SAT objective)

- Minimise deviation from client preferred time windows
- Cluster clients on the same day by preferred times
- Higher-priority clients (more sessions + longer tenure) scheduled first
