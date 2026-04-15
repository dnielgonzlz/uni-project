# Understanding This Go Backend (for TypeScript/React Developers)

This document explains how the entire backend works, written for someone who knows TypeScript and React well but has never written Go. Every concept is explained by comparing it to something you already know.

---

## Table of Contents

1. [Go vs TypeScript: The Key Differences](#1-go-vs-typescript-the-key-differences)
2. [Project Structure](#2-project-structure)
3. [How the HTTP Server Works](#3-how-the-http-server-works)
4. [How the Database Works](#4-how-the-database-works)
5. [Authentication & JWT](#5-authentication--jwt)
6. [The Domain Packages](#6-the-domain-packages)
7. [The Billing System](#7-the-billing-system)
8. [The Scheduling Engine](#8-the-scheduling-engine)
9. [The Notification System](#9-the-notification-system)
10. [The Availability Intake (SMS Bot)](#10-the-availability-intake-sms-bot)
11. [Configuration & Environment Variables](#11-configuration--environment-variables)
12. [How to Run the Project](#12-how-to-run-the-project)

---

## 1. Go vs TypeScript: The Key Differences

### Compiled, not interpreted

TypeScript gets compiled to JavaScript, which runs in Node.js (a runtime). Go compiles directly to a native binary — no runtime needed. `go build` produces a single executable file you can just run.

### Strongly typed with no `any`

Go has types like TypeScript, but you can't opt out. There's no `any`. Every variable has a concrete type known at compile time.

### Structs instead of classes

Go doesn't have classes. It has **structs** (like TypeScript interfaces, but they can also have methods):

```typescript
// TypeScript
interface Payment {
  id: string;
  amountPence: number;
  status: 'pending' | 'paid' | 'failed';
}
```

```go
// Go equivalent
type Payment struct {
    ID          uuid.UUID `json:"id"`
    AmountPence int       `json:"amount_pence"`
    Status      string    `json:"status"`
}
```

The backtick annotations (`` `json:"id"` ``) are called **struct tags**. They tell the JSON encoder what name to use — exactly like `@JsonProperty` in Java or the key in `JSON.stringify`.

### Interfaces work differently

In TypeScript you explicitly declare that a class implements an interface:
```typescript
class MyService implements Notifier { ... }
```

In Go, interfaces are **implicit** — if your type has the right methods, it automatically satisfies the interface. No `implements` keyword needed:
```go
// The interface says: any type with a Send method is a Sender
type Sender interface {
    Send(ctx context.Context, to, body string) error
}

// SMSService automatically satisfies Sender because it has a Send method
// with the right signature — no declaration needed
type SMSService struct { ... }
func (s *SMSService) Send(ctx context.Context, to, body string) error { ... }
```

### Error handling

There are no `try/catch` blocks in Go. Functions that can fail return two values: the result and an error:

```typescript
// TypeScript
try {
  const user = await getUser(id);
} catch (e) {
  console.error(e);
}
```

```go
// Go
user, err := getUser(ctx, id)
if err != nil {
    return nil, fmt.Errorf("failed to get user: %w", err)
}
```

This looks verbose but it forces you to think about failures at every step. The `%w` in the format string "wraps" the error (like chaining error causes).

### `context.Context`

Almost every function in this codebase takes a `ctx context.Context` as the first argument. Think of it like a request-scoped object that carries a deadline, cancellation signal, and request metadata. When a client disconnects or a request times out, the context is cancelled and database queries and HTTP calls abort cleanly.

### Goroutines (concurrency)

Go has lightweight threads called **goroutines**. You start one with `go`:
```go
go worker.Run(ctx)  // runs concurrently, doesn't block
```
This is how the notification worker runs in the background without blocking the HTTP server.

### Pointers

Go has pointers (`*string`, `*int`). You'll see these used for optional fields:
```go
type User struct {
    PhoneE164 *string  // pointer = can be nil (like TypeScript's string | null)
    FullName  string   // value = always present (like TypeScript's string)
}
```

---

## 2. Project Structure

```
backend/
├── cmd/
│   ├── api/
│   │   ├── main.go        ← Entry point (like index.ts / server.ts)
│   │   └── handlers.go    ← /healthz and /readyz endpoints
│   └── migrate/
│       └── main.go        ← CLI tool for database migrations
│
├── internal/              ← All application code (private to this module)
│   ├── auth/              ← Login, logout, JWT, password reset
│   ├── users/             ← Coach and client profiles
│   ├── availability/      ← Working hours, preferred time windows
│   ├── scheduling/        ← Session booking, OR-Tools integration, constraints
│   ├── billing/           ← Stripe, GoCardless, webhooks
│   ├── messaging/         ← Email (Resend), SMS (Twilio), notification worker
│   ├── availability_intake/ ← SMS bot for collecting client availability
│   └── platform/          ← Shared infrastructure (no business logic)
│       ├── config/        ← Environment variable loading
│       ├── database/      ← PostgreSQL connection pool
│       ├── httpx/         ← HTTP response helpers, middleware
│       ├── validator/     ← Input validation
│       ├── logger/        ← Structured logging setup
│       └── clock/         ← Injectable clock (for testable time logic)
│
├── migrations/            ← SQL files for database schema changes
├── solver/                ← Python FastAPI + OR-Tools microservice
└── docs/                  ← This file lives here
```

**The pattern you'll see repeated in every domain package** (`auth/`, `billing/`, etc.):

| File | What it does | TypeScript equivalent |
|---|---|---|
| `model.go` | Type definitions | `types.ts` |
| `repository.go` | Database queries | `db/queries.ts` or a DAL layer |
| `service.go` | Business logic | `services/myService.ts` |
| `handler.go` | HTTP request/response | `routes/myRoute.ts` (Express/Next.js API route) |

This is called **layered architecture**: HTTP → Service → Repository → Database. Each layer only knows about the layer below it.

---

## 3. How the HTTP Server Works

The router is **Chi** — think of it like Express.js for Go.

### Route registration (in `cmd/api/main.go`)

```go
// Like Express:
// app.get('/healthz', healthzHandler)
// app.use('/api/v1', apiRouter)

r.Get("/healthz", healthzHandler())
r.Route("/api/v1", func(r chi.Router) {
    r.Post("/auth/login", authHandler.Login)
})
```

### Middleware

Middleware in Chi works identically to Express middleware — it wraps each request/response cycle:

```go
// Applied to all routes
r.Use(middleware.RequestID)     // add a unique request ID to each request
r.Use(middleware.Recoverer)     // catch panics, return 500 instead of crashing
r.Use(httpx.RequestLogger(log)) // log every request
r.Use(middleware.Timeout(30 * time.Second)) // auto-cancel slow requests
```

`r.Group(func(r chi.Router) { ... })` creates a sub-router with additional middleware, like nesting routers in Express:

```go
// All routes in this group require a valid JWT
r.Group(func(r chi.Router) {
    r.Use(auth.Middleware(cfg.JWTSecret)) // verify JWT on every request
    r.Use(auth.RequireRole("coach"))      // only coaches can access these

    r.Post("/schedule-runs", schedHandler.TriggerScheduleRun)
})
```

### A handler function

A handler is just a function with the signature `func(http.ResponseWriter, *http.Request)`. Exactly like an Express route handler but with `(req, res)` flipped and typed differently:

```typescript
// Express
app.post('/auth/login', async (req, res) => {
  const { email, password } = req.body;
  // ...
  res.json({ token });
});
```

```go
// Go/Chi
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
    var req LoginRequest
    json.NewDecoder(r.Body).Decode(&req)  // parse JSON body
    // ...
    httpx.JSON(w, http.StatusOK, tokens) // send JSON response
}
```

### Response helpers (`internal/platform/httpx/response.go`)

Instead of `res.json()` and `res.status(400).json(...)`, we have:

```go
httpx.JSON(w, http.StatusOK, data)           // 200 with data envelope
httpx.Error(w, http.StatusBadRequest, "msg") // 4xx error
httpx.InternalError(w, r, logger, err)       // 500 (logs error, hides detail from client)
```

All responses are wrapped in an envelope:
```json
{ "data": { ... } }       // success
{ "error": "not found" }  // failure
```

---

## 4. How the Database Works

The database layer uses **pgx** — a low-level PostgreSQL driver. There is **no ORM**. You write raw SQL.

Think of it like writing `pg.query()` in Node.js directly, but with type-safe scanning.

### Repository pattern

Every domain has a `Repository` struct that owns all DB queries for that domain:

```go
type Repository struct {
    db *pgxpool.Pool  // connection pool (like a pg.Pool in Node)
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
    const q = `SELECT id, email, full_name FROM users WHERE email = $1`
    //                                                              ↑ positional params (not :email or ?)
    row := r.db.QueryRow(ctx, q, email)

    var u User
    err := row.Scan(&u.ID, &u.Email, &u.FullName) // like destructuring the row
    if err != nil {
        return nil, err
    }
    return &u, nil
}
```

`$1`, `$2`, etc. are positional parameters (PostgreSQL style). They prevent SQL injection — the values are never string-concatenated into the query.

### Migrations

Database schema changes are tracked in numbered SQL files:

```
migrations/
├── 000001_initial_schema.up.sql    ← creates all tables
├── 000001_initial_schema.down.sql  ← drops all tables (rollback)
├── 000002_outbox.up.sql            ← adds notification_outbox table
└── 000002_outbox.down.sql
```

Run with: `go run ./cmd/migrate up` / `go run ./cmd/migrate down`

This is equivalent to running Prisma migrations, Sequelize migrations, or Flyway.

### Transactions

When multiple DB writes must succeed or fail together (e.g. confirming sessions and updating the run status atomically):

```go
tx, err := db.Begin(ctx)
defer tx.Rollback(ctx) // rolls back if we return early with an error

// ... do multiple writes using tx instead of db ...

tx.Commit(ctx) // only commits if we reach this line
```

Like `await db.transaction(async (t) => { ... })` in Sequelize.

---

## 5. Authentication & JWT

### Registration flow

1. Client sends `POST /api/v1/auth/register` with `{ email, password, role, full_name }`
2. Password is hashed with **argon2id** (more secure than bcrypt, the Go recommendation)
3. A `users` row is created, plus a `coaches` or `clients` profile row
4. Two tokens are returned:
   - **Access token**: short-lived (15 min), a JWT sent in the `Authorization: Bearer` header
   - **Refresh token**: long-lived (7 days), stored in the DB hashed, used to get new access tokens

### How JWT middleware works

```go
// In auth/middleware.go:
// This runs before every protected handler
func Middleware(jwtSecret string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            token := r.Header.Get("Authorization") // "Bearer eyJhbG..."
            claims, err := ParseAccessToken(token, jwtSecret)
            if err != nil {
                httpx.Error(w, 401, "unauthorised")
                return
            }
            // Attach user ID and role to the request context
            ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

This is exactly like a `passport.authenticate('jwt')` middleware in Express, or `getServerSideProps` auth checking in Next.js.

Handlers then read the user from context:
```go
userID := auth.UserIDFromContext(r.Context())
```

### Password reset flow

1. `POST /api/v1/auth/forgot-password` — generates a random 64-char token, stores its SHA-256 hash in DB (never the raw token), sends an email with a link containing the raw token
2. `POST /api/v1/auth/reset-password` — hashes the incoming token, looks it up in DB, verifies it hasn't expired or been used, updates the password

---

## 6. The Domain Packages

### `internal/users/`

Manages the two profile types:
- **Coach** — a personal trainer; has a `coaches` row linked to their `users` row
- **Client** — a gym member; has a `clients` row linked to their `users` row and their coach

A user account (`users` table) is always created first. Then a `coaches` or `clients` row is added. This is a **1:1 relationship** enforced by `UNIQUE` constraints.

### `internal/availability/`

- **Working hours** (`trainer_working_hours`): The coach sets which days and times they're available. Stored as `day_of_week` (0=Sunday, 6=Saturday) + `start_time` + `end_time`.
- **Client preferred windows** (`client_preferred_windows`): When a client would prefer to train. Can be set manually via the API or collected via SMS (see below).

When working hours are updated, the whole set is replaced atomically (delete all for that coach, insert new ones in a single transaction).

---

## 7. The Billing System

### Two payment providers

| Provider | Use case | How it works |
|---|---|---|
| **Stripe** | Card payments (credit/debit) | Client saves their card via Stripe Elements on the frontend; backend charges it monthly |
| **GoCardless** | Direct Debit (Bacs) | Client authorises a mandate via a GoCardless redirect; backend submits payment against it |

### Stripe card flow

```
1. Coach triggers setup for a client
   → POST /api/v1/payments/setup-intent
   → Backend calls Stripe API → creates a "SetupIntent"
   → Returns { client_secret: "..." } to the frontend

2. FRONTEND: uses client_secret to render Stripe Elements (card form)
   → Stripe handles SCA/3DS2 directly in the browser
   → Card is saved to the Stripe customer

3. When billing runs:
   → Backend calls Stripe API with the saved payment method
   → Stripe charges the card "off_session" (client not present)
   → Payment webhook arrives at /api/v1/webhooks/stripe
   → Backend updates payment status to "paid" or "failed"
```

### GoCardless Direct Debit flow

```
1. Coach triggers mandate setup
   → POST /api/v1/payments/mandate
   → Backend calls GoCardless API → creates a "redirect flow"
   → Returns { redirect_url: "..." }

2. FRONTEND: redirects client to redirect_url
   → Client fills in bank details on GoCardless-hosted page
   → GoCardless redirects client back to your app with ?redirect_flow_id=...

3. FRONTEND calls:
   → POST /api/v1/payments/mandate/complete?redirect_flow_id=RF123
   → Backend completes the flow, GoCardless creates a mandate
   → Mandate ID stored in gocardless_mandates table

4. When billing runs:
   → Backend creates a payment against the mandate
   → Bacs advance notice: payment must be ≥ 3 working days in the future
   → GoCardless processes the Direct Debit
   → Webhook arrives at /api/v1/webhooks/gocardless
   → Backend updates payment status
```

### Idempotency (safe to call twice)

Every payment has an **idempotency key** — a unique string per client per billing period:
```
stripe-{client-uuid}-2025-01
gocardless-{client-uuid}-2025-01
```

If a charge is attempted twice for the same client/month, the DB `ON CONFLICT DO NOTHING` clause silently ignores the duplicate. This prevents double-charging if the coach clicks "charge" twice or if a network retry occurs.

### Webhooks

Stripe and GoCardless send HTTP POST requests to your server when payment status changes (paid, failed, refunded). The backend:

1. Reads the raw request body (never re-parsed — needed for signature verification)
2. Verifies the cryptographic signature (Stripe uses HMAC-SHA256; GoCardless also uses HMAC-SHA256)
3. Records the event in `webhook_events` to prevent processing the same event twice
4. Updates the payment status in the DB

These webhook routes **bypass** the `RequireJSON` middleware because:
- Stripe sends `application/json` but we need the exact raw bytes for signature verification
- GoCardless similarly needs raw bytes

---

## 8. The Scheduling Engine

### Why a separate Python service?

Google OR-Tools (the scheduling solver) is a C++ library with a first-class Python API. There's no maintained Go binding. Rather than wrestling with CGo (Go's C interop), we run a Python FastAPI service alongside the Go server. The Go server calls it over HTTP.

```
Go API Server  →  POST /solve  →  Python OR-Tools Service
               ←  proposed schedule (JSON)  ←
```

### How a schedule run works

```
1. Coach triggers a run:
   POST /api/v1/schedule-runs { week_start: "2025-01-06" }

2. Go service loads:
   - Coach's working hours
   - All active clients for that coach
   - Each client's sessions_per_month, preferred windows, priority score
   - Already-booked sessions (to avoid conflicts)

3. Pre-checks (before calling the solver):
   - Are there enough working hours for all the sessions needed?
   - Does any client have impossible constraints?
   If not feasible → return 422 with explanation

4. Call Python solver (30 second timeout):
   Sends all constraints as JSON, receives proposed session times

5. Save proposed sessions to DB with status = 'proposed'
   The DB exclusion constraint double-checks for overlaps
   (catches any solver bugs)

6. Return the proposed schedule to the coach (frontend shows it)

7. Coach reviews and confirms:
   POST /api/v1/schedule-runs/{id}/confirm
   → All 'proposed' sessions → 'confirmed' in a single transaction
   → Notification service enqueues booking confirmations for each client
```

### Hard constraints (non-negotiable)

These are enforced at multiple levels:

| Constraint | Enforced by |
|---|---|
| No trainer double-booking | PostgreSQL exclusion constraint (`btree_gist`) + solver |
| No client double-booking | PostgreSQL exclusion constraint + solver |
| Sessions within working hours | Pre-solver check in `constraints.go` + solver |
| 24h recovery between sessions | Pre-solver check + solver |
| Max 4 sessions/day | Pre-solver check + solver |

The DB constraints are the final safety net. Even if the solver has a bug and proposes two overlapping sessions, the DB insert will fail.

### Session cancellations and credits

When a session is cancelled with ≥24 hours notice:
1. The session is marked `cancelled`
2. A `session_credits` row is created for the client
3. The credit expires in 1 month
4. The credit can be used to book a replacement session

No refund is issued — billing is monthly, not per session.

---

## 9. The Notification System

### The outbox pattern

Instead of sending emails/SMS immediately when something happens (which could fail if Twilio is down), all notifications are stored in a `notification_outbox` database table first. A background worker processes them.

This guarantees that if the process crashes between "booking confirmed" and "SMS sent", the SMS will still be sent when the process restarts — because the outbox row was written in the same DB transaction as the booking.

```
1. Coach confirms schedule run
   ↓ (inside the same DB write)
2. notification_outbox rows inserted for each session:
   - event_type: "session_confirmed" → send now
   - event_type: "session_reminder"  → send 24h before the session

3. Background worker polls every 15 seconds:
   SELECT FOR UPDATE SKIP LOCKED ← atomically claims rows
   ↓
4. Worker calls NotificationService.Deliver()
   ↓
5. Send email (Resend) + SMS (Twilio)
   ↓
6. Mark outbox row as "done"
   (or "pending" again to retry if delivery fails; permanently "failed" after 5 attempts)
```

### `SELECT FOR UPDATE SKIP LOCKED`

This is a Postgres feature that lets multiple worker processes safely claim rows without conflicts:
- `FOR UPDATE` locks the selected rows
- `SKIP LOCKED` skips any rows already locked by another worker

This means you could run 3 copies of the server and they'd each process different outbox entries without stepping on each other.

### The Notifier interface

The scheduling service doesn't directly import the messaging package (that would create tight coupling). Instead, it defines a minimal interface:

```go
// In scheduling/service.go:
type Notifier interface {
    NotifySessionsConfirmed(ctx, []SessionNotifPayload) error
    NotifySessionCancelled(ctx, CancelNotifPayload) error
}
```

The messaging `NotificationService` automatically satisfies this interface (Go's implicit interface implementation). In `main.go`:
```go
schedSvc.WithNotifier(notifSvc) // wire them together at startup
```

This pattern (dependency inversion via interfaces) is very common in Go — it's how you make code testable and loosely coupled.

---

## 10. The Availability Intake (SMS Bot)

Clients can tell the system their availability via SMS. The flow:

```
Coach sends a message to the client's phone: "Text your availability to +44..."

Client texts:  "Monday Tuesday Thursday"
Bot replies:   "Got it! What times work for you on those days?"
Client texts:  "9am-12pm"
Bot replies:   "Perfect! I've saved Mon/Tue/Thu 09:00-12:00 for you."

→ Stored in client_preferred_windows with source = 'sms'
→ Used by the solver as soft constraints
```

The state machine lives in `internal/availability_intake/service.go`:
- `idle` → receives days → `awaiting_times`
- `awaiting_times` → receives time range → `complete`
- Goes back to `idle` if something unexpected arrives

Twilio sends inbound SMS to `POST /api/v1/webhooks/twilio`. The handler parses the form-encoded body (Twilio sends `application/x-www-form-urlencoded`, not JSON — another reason webhooks bypass `RequireJSON`).

---

## 11. Configuration & Environment Variables

All configuration is loaded from environment variables at startup using the **envconfig** library. This is defined in `internal/platform/config/config.go`:

```go
type Config struct {
    Port        string `envconfig:"PORT" default:"8080"`
    DatabaseURL string `envconfig:"DATABASE_URL" required:"true"`
    // ...
}
```

The `required:"true"` tag means the server refuses to start if that variable is missing. The `default:"8080"` provides a fallback.

This is like using `dotenv` + `zod.parse(process.env)` in TypeScript — it validates your config at startup so you get a clear error immediately rather than a cryptic runtime failure later.

---

## 12. How to Run the Project

### Prerequisites

- Go 1.22+ (`brew install go` on Mac)
- PostgreSQL 16 (`brew install postgresql@16`)
- Python 3.11+ (for the solver)

### First-time setup

```bash
# 1. Create the database
createdb pt_scheduler

# 2. Copy and fill in the env file
cp .env.example .env
# Edit .env — at minimum set DATABASE_URL and JWT_SECRET

# 3. Run migrations (creates all tables)
make migrate-up

# 4. Start the Python solver (in a separate terminal)
cd solver
python -m venv venv
source venv/bin/activate
pip install -r requirements.txt
uvicorn main:app --port 8000

# 5. Start the Go server
make run
# or: go run ./cmd/api
```

### Useful commands

```bash
make run          # start the API server
make migrate-up   # apply pending migrations
make migrate-down # roll back the last migration
make test         # run all unit tests
make build        # compile to ./bin/api binary
make lint         # run golangci-lint
```

### Testing a request

Once running, test it:
```bash
# Health check
curl http://localhost:8080/healthz

# Register a coach
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"coach@example.com","password":"password123","role":"coach","full_name":"John Smith"}'
```

---

## Appendix: Key Go Concepts Glossary

| Go term | What it means | TypeScript equivalent |
|---|---|---|
| `struct` | Data type with named fields | `interface` or `type` |
| `func (s *Service) Method()` | Method on a struct | `class Service { method() {} }` |
| `*string` | Pointer to a string (can be nil) | `string \| null` |
| `error` | Built-in error type | `Error` or thrown exception |
| `context.Context` | Request-scoped cancellation + metadata | React's Context, or a request object |
| `goroutine` | Lightweight concurrent thread | `Promise` / async worker |
| `channel` | Communication between goroutines | `EventEmitter` / message queue |
| `interface` | Set of methods a type must have (implicit) | TypeScript `interface` (but implicit) |
| `go build` | Compiles to binary | `tsc` + bundler |
| `go mod tidy` | Sync dependencies | `npm install` |
| `go test ./...` | Run all tests | `jest --all` |
| `defer` | Run at end of function (like `finally`) | `finally {}` block |
| `make([]T, 0)` | Create an empty slice (dynamic array) | `new Array<T>()` or `[]` |
| `map[string]any` | Dynamic key-value map | `Record<string, any>` |
