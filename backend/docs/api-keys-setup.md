# API Keys & Tokens Setup Guide

This document lists every external service credential the backend needs, where to get each one, and which source files use it.

---

## Quick Reference

| Environment Variable | Service | Required |
|---|---|---|
| `DATABASE_URL` | PostgreSQL | Yes |
| `JWT_SECRET` | Internal (you generate this) | Yes |
| `STRIPE_SECRET_KEY` | Stripe | Yes |
| `STRIPE_WEBHOOK_SECRET` | Stripe | Yes |
| `GOCARDLESS_ACCESS_TOKEN` | GoCardless | Yes |
| `GOCARDLESS_WEBHOOK_SECRET` | GoCardless | Yes |
| `GOCARDLESS_ENV` | GoCardless | Yes (sandbox/live) |
| `TWILIO_ACCOUNT_SID` | Twilio | Yes |
| `TWILIO_AUTH_TOKEN` | Twilio | Yes |
| `TWILIO_FROM_NUMBER` | Twilio | Yes |
| `MESSAGING_CHANNEL` | Internal toggle | No (default: sms) |
| `RESEND_API_KEY` | Resend | Yes |
| `RESEND_FROM_ADDRESS` | Resend | Yes |
| `SOLVER_URL` | Internal (your Python service) | No (default: http://localhost:8000) |
| `SENTRY_DSN` | Sentry | No (disables error tracking if empty) |
| `CORS_ALLOWED_ORIGINS` | Internal | Yes |
| `PORT` | Internal | No (default: 8080) |
| `ENV` | Internal | No (default: development) |

All of these go in your `.env` file (copy `.env.example` and fill in the values).

---

## 1. PostgreSQL — `DATABASE_URL`

**What it is**: The connection string to your Postgres database.

**Where to get it**:
- **Local dev**: Install Postgres (`brew install postgresql@16` on Mac), create a database: `createdb pt_scheduler`, then the URL is `postgres://your_mac_username@localhost:5432/pt_scheduler`
- **Production (AWS RDS)**: Found in the RDS console under "Connectivity & security" → "Endpoint". Format: `postgres://username:password@your-rds-endpoint.rds.amazonaws.com:5432/pt_scheduler`

**Format**: `postgres://USER:PASSWORD@HOST:PORT/DBNAME?sslmode=disable`

**Files that use it**:
- `internal/platform/database/database.go` — opens the connection pool
- `cmd/api/main.go` — passes it to the database package
- `cmd/migrate/main.go` — migration CLI reads it to run up/down migrations

---

## 2. JWT Secret — `JWT_SECRET`

**What it is**: A secret string you invent. It's used to sign and verify JSON Web Tokens (login sessions). Never share it.

**Where to get it**: Generate it yourself. Run this in your terminal:
```bash
openssl rand -hex 32
```
Copy the output (it will look like `a3f8c2...`) and use it as the value.

**Files that use it**:
- `internal/auth/jwt.go` — signs access tokens on login, verifies them on every protected request
- `internal/auth/middleware.go` — reads the secret to verify incoming tokens
- `cmd/api/main.go` — passes it to the auth middleware and service

---

## 3. Stripe

Stripe handles card payments (SCA/3DS2 compliant for UK). You need **two** keys.

### How to set up your Stripe account
1. Go to [stripe.com](https://stripe.com) and create an account
2. In the dashboard, go to **Developers → API keys**
3. You'll see a **Publishable key** (starts with `pk_`) and a **Secret key** (starts with `sk_`). You only need the **secret key** for the backend.
4. For testing, use the **Test mode** keys (starts with `sk_test_`). Switch to **Live mode** when going to production.

### `STRIPE_SECRET_KEY`

**What it is**: Authenticates your backend calls to Stripe's API (creating customers, PaymentIntents, etc.)

**Where to get it**: Stripe Dashboard → Developers → API keys → Secret key

**Files that use it**:
- `internal/billing/stripe.go` — set as `stripe.Key` globally; used for `customer.New`, `paymentintent.New`, `setupintent.New`

### `STRIPE_WEBHOOK_SECRET`

**What it is**: Used to verify that incoming webhook calls actually came from Stripe (not an impersonator). Each webhook endpoint has its own secret.

**Where to get it**:
1. Stripe Dashboard → Developers → Webhooks → Add endpoint
2. Set the endpoint URL to `https://your-domain.com/api/v1/webhooks/stripe`
3. Select these events to listen for:
   - `payment_intent.succeeded`
   - `payment_intent.payment_failed`
4. After creating the endpoint, click on it and find **Signing secret** (starts with `whsec_`)

**Files that use it**:
- `internal/billing/stripe.go` — `VerifyWebhookSignature` uses it via `webhook.ConstructEvent`
- `internal/billing/handler.go` — `StripeWebhook` handler reads the `Stripe-Signature` header and passes it to the verifier

> **Local testing**: Use the Stripe CLI (`stripe listen --forward-to localhost:8080/api/v1/webhooks/stripe`) to forward webhook events to your local machine. The CLI gives you a temporary webhook secret.

---

## 4. GoCardless (Direct Debit)

GoCardless handles UK Bacs Direct Debit mandates. You need **three** values.

### How to set up your GoCardless account
1. Go to [gocardless.com](https://gocardless.com) and sign up for a developer account
2. In the dashboard, go to **Developers → API keys**
3. GoCardless uses a **single access token** (not a public/private key pair like Stripe)
4. There is a **Sandbox** environment for testing (no real money moves)

### `GOCARDLESS_ACCESS_TOKEN`

**What it is**: Authenticates all API calls to GoCardless (creating redirect flows, completing mandates, creating payments).

**Where to get it**: GoCardless Dashboard → Developers → API keys → Create token with read/write access

**Files that use it**:
- `internal/billing/gocardless.go` — sent as `Authorization: Bearer <token>` on every HTTP request to `api.gocardless.com`

### `GOCARDLESS_WEBHOOK_SECRET`

**What it is**: GoCardless signs webhook payloads with HMAC-SHA256. This secret lets you verify the signature.

**Where to get it**:
1. GoCardless Dashboard → Developers → Webhooks → Create endpoint
2. Set the URL to `https://your-domain.com/api/v1/webhooks/gocardless`
3. Copy the **Secret** shown after creation

**Files that use it**:
- `internal/billing/gocardless.go` — `VerifyWebhookSignature` computes HMAC-SHA256 and compares it to the `Webhook-Signature` header

### `GOCARDLESS_ENV`

**What it is**: Tells the client which GoCardless environment to use.

**Values**: `sandbox` (testing) or `live` (production)

**Files that use it**:
- `internal/billing/gocardless.go` — when `sandbox`, adds the `GoCardless-Environment: sandbox` header to all requests

---

## 5. Twilio (SMS / WhatsApp)

Twilio sends SMS messages to clients (booking confirmations, reminders, etc.) and receives inbound SMS from clients to collect their availability. You need **three** values.

### How to set up your Twilio account
1. Go to [twilio.com](https://twilio.com) and create an account
2. In the Console, go to **Account → Keys & Credentials**
3. You'll see your **Account SID** and **Auth Token** on the main console page
4. To get a phone number: Console → Phone Numbers → Manage → Buy a number (choose a UK number with SMS capability)

### `TWILIO_ACCOUNT_SID`

**What it is**: Your Twilio account identifier.

**Where to get it**: Twilio Console home page (top-left box)

**Files that use it**:
- `internal/messaging/sms.go` — passed to `twilio.NewRestClientWithParams` as `Username`

### `TWILIO_AUTH_TOKEN`

**What it is**: Your Twilio authentication token (like a password for the API).

**Where to get it**: Twilio Console home page (next to Account SID — click the eye icon to reveal it)

**Files that use it**:
- `internal/messaging/sms.go` — passed to `twilio.NewRestClientWithParams` as `Password`

### `TWILIO_FROM_NUMBER`

**What it is**: The Twilio phone number that messages are sent from. Must be in E.164 format (e.g. `+441234567890`).

**Where to get it**: Twilio Console → Phone Numbers → Manage → Active numbers

**Files that use it**:
- `internal/messaging/sms.go` — set as the `From` field on outgoing messages; prefixed with `whatsapp:` if `MESSAGING_CHANNEL=whatsapp`

### `MESSAGING_CHANNEL` (optional)

**What it is**: A feature toggle. Set to `sms` (default) to send via SMS. Set to `whatsapp` to use WhatsApp Business API via Twilio (requires separate WhatsApp sender approval from Meta, which can take weeks).

**Values**: `sms` | `whatsapp`

**Files that use it**:
- `internal/messaging/sms.go` — determines whether to prefix numbers with `whatsapp:`
- `internal/platform/config/config.go` — loaded from env

**Twilio webhook (inbound SMS)**:
- Configure in Twilio Console → Phone Numbers → your number → Messaging → "A message comes in" → Webhook → set URL to `https://your-domain.com/api/v1/webhooks/twilio`
- No secret needed — Twilio sends a `X-Twilio-Signature` header but for MVP we accept all messages and use the `From` number to identify the client

---

## 6. Resend (Email)

Resend sends transactional emails (password reset, booking confirmations, reminders, payment failure alerts).

### How to set up your Resend account
1. Go to [resend.com](https://resend.com) and create an account
2. Add and verify your sending domain (e.g. `notifications@yourptapp.com`). Resend gives you DNS records to add.
3. Create an API key: Resend Dashboard → API Keys → Create API Key

### `RESEND_API_KEY`

**What it is**: Authenticates API calls to Resend.

**Where to get it**: Resend Dashboard → API Keys (starts with `re_`)

**Files that use it**:
- `internal/messaging/email.go` — passed to `resend.NewClient`

### `RESEND_FROM_ADDRESS`

**What it is**: The email address emails are sent from. Must be on a domain you've verified in Resend.

**Example**: `PT Scheduler <notifications@yourptapp.com>`

**Files that use it**:
- `internal/messaging/email.go` — used as the `From` field on every email

---

## 7. Solver URL — `SOLVER_URL` (internal)

**What it is**: The URL of the Python OR-Tools scheduling microservice. Not an external API key.

**Value**:
- Local dev: `http://localhost:8000` (the Python FastAPI server running locally)
- AWS: The Lambda function URL or an internal API Gateway URL

**Files that use it**:
- `internal/scheduling/solver_client.go` — `HTTPSolver.Solve` sends `POST` requests here

---

## 8. Sentry — `SENTRY_DSN` (optional)

**What it is**: Error tracking. Sentry captures unhandled errors and notifies you.

**Where to get it**:
1. Go to [sentry.io](https://sentry.io) and create a project (choose Go platform)
2. Sentry gives you a DSN (looks like `https://abc123@o123456.ingest.sentry.io/789`)

**Files that use it**:
- `internal/platform/config/config.go` — loaded but not yet wired (Phase 6 adds Sentry SDK calls)

> **Note**: Leaving this empty disables Sentry with no side effects. Safe to skip for local development.

---

## 9. CORS — `CORS_ALLOWED_ORIGINS`

**What it is**: A comma-separated list of frontend URLs that are allowed to call the API from a browser.

**Value**:
- Local dev: `http://localhost:3000` (or whatever port your React frontend runs on)
- Production: `https://yourptapp.com`

**Files that use it**:
- `cmd/api/main.go` — passed to `cors.Handler` in the middleware stack

---

## Environment File Setup

Copy `.env.example` to `.env` and fill in your values:

```bash
cp .env.example .env
```

Your `.env` should look like this when filled in:

```env
# Server
PORT=8080
ENV=development

# Database
DATABASE_URL=postgres://your_username@localhost:5432/pt_scheduler?sslmode=disable

# JWT (generate with: openssl rand -hex 32)
JWT_SECRET=your_64_char_hex_string_here
JWT_ACCESS_EXPIRY_MIN=15
JWT_REFRESH_EXPIRY_DAYS=7

# Password reset
PASSWORD_RESET_EXPIRY_MIN=60

# CORS
CORS_ALLOWED_ORIGINS=http://localhost:3000

# Stripe
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...

# GoCardless
GOCARDLESS_ACCESS_TOKEN=your_token_here
GOCARDLESS_WEBHOOK_SECRET=your_secret_here
GOCARDLESS_ENV=sandbox

# Twilio
TWILIO_ACCOUNT_SID=ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
TWILIO_AUTH_TOKEN=your_auth_token
TWILIO_FROM_NUMBER=+441234567890
MESSAGING_CHANNEL=sms

# Resend
RESEND_API_KEY=re_...
RESEND_FROM_ADDRESS=PT Scheduler <notifications@yourptapp.com>

# Solver
SOLVER_URL=http://localhost:8000
SOLVER_TIMEOUT_SECONDS=30

# Sentry (optional)
SENTRY_DSN=
```

---

## What to Set Up First (Recommended Order)

If you're setting up from scratch, do these in order:

1. **PostgreSQL** — you can't run the app without a database
2. **JWT_SECRET** — one `openssl` command, 30 seconds
3. **CORS_ALLOWED_ORIGINS** — set to your frontend localhost port
4. At this point the server runs and auth works locally
5. **Stripe** test keys — needed for the billing endpoints
6. **GoCardless** sandbox token — needed for Direct Debit endpoints
7. **Resend** — needed for password reset emails
8. **Twilio** — needed for SMS notifications
9. **Sentry** — optional, add when deploying to production
