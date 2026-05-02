# Frontend Handoff — PT Scheduler

**Stack**: React 18 + TypeScript + Vite  
**Backend base URL**: `http://localhost:8080` (dev) — replace with production URL before launch  
**API prefix**: `/api/v1`  
**Interactive API docs**: `http://localhost:8080/swagger/index.html` (available when the server is running)

---

## Table of Contents

1. [Product Overview](#1-product-overview)
2. [User Roles](#2-user-roles)
3. [Auth & Session Management](#3-auth--session-management)
4. [API Response Shape](#4-api-response-shape)
5. [Error Handling Contract](#5-error-handling-contract)
6. [Endpoint Reference](#6-endpoint-reference)
7. [Core User Flows](#7-core-user-flows)
8. [Data Models](#8-data-models)
9. [Payments Integration](#9-payments-integration)
10. [UK Localisation Requirements](#10-uk-localisation-requirements)
11. [Pages & Components Checklist](#11-pages--components-checklist)
12. [Environment Variables](#12-environment-variables)

---

## 1. Product Overview

PT Scheduler is a booking and scheduling platform for UK-based personal trainers (coaches) and their clients. It is an MVP comparable to Cal.com or Gymcatch but domain-specific.

**Core value proposition**:
- A coach declares their weekly working hours and a list of clients (each with a sessions-per-month quota).
- The system uses an AI solver (OR-Tools) to produce an optimal weekly schedule that satisfies hard constraints (no overlaps, working hours, 24 h recovery) and soft constraints (client preferred times, clustering).
- The coach reviews the proposed schedule and confirms or rejects it in one action.
- Clients are notified by SMS and email after confirmation.
- Monthly billing is handled via Stripe (card payments). GoCardless Direct Debit support has been fully implemented in the backend but is **not active in this release** — the merchant application was rejected before the project deadline (see §9).
---

## 2. User Roles

There are exactly two user-facing roles plus an internal admin role.

| Role | Who | What they can do |
|---|---|---|
| `coach` | Personal trainer | Manage their own profile, set working hours, manage client list, trigger/confirm/reject schedule runs, trigger billing, view all sessions |
| `client` | Gym client | Manage their own profile, set preferred time windows, view their own sessions, cancel sessions |
| `admin` | Internal only | Bypasses role checks — not used in the frontend |

**The role is encoded in the JWT and returned at login.** Store it in memory (or a state manager) to drive UI access control — but always rely on the backend for authorization. A client who manually edits localStorage still gets a 403 from the API.

---

## 3. Auth & Session Management

### How it works

The API uses a **short-lived access token (15 minutes)** and a **long-lived refresh token (7 days)**.

- Store the access token **in memory only** (a React context / Zustand store). Never write it to localStorage or cookies — XSS risk.
- Store the refresh token **in an `HttpOnly` cookie** if your deployment supports it, or localStorage as a fallback for this MVP.
- When any API call returns `401`, silently call `POST /auth/refresh` with the stored refresh token, then retry the original request once.
- When `POST /auth/refresh` itself returns `401`, the session is dead — redirect to `/login` and clear all stored tokens.

### Token storage recommendation (MVP)

```
Access token  → React context (memory only, lost on page reload)
Refresh token → localStorage key "pt_refresh_token"
```

On page load, if a refresh token exists in localStorage, immediately call `POST /auth/refresh` to restore the session before rendering the app shell.

### Login flow

```
POST /api/v1/auth/login
→ 200 { access_token, refresh_token, expires_in }

Store both tokens.
Decode the JWT to read { sub (user_id), role } — or call GET /api/v1/coaches/{id}/profile
after login to hydrate the user state.
```

### Logout

```
POST /api/v1/auth/logout   { refresh_token }
→ 200 { message: "logged out" }

Clear both tokens from storage. Redirect to /login.
```

### Registration

Two flows share the same endpoint but differ in required fields:

**Coach registration** (creates user + coach profile):
```json
POST /api/v1/auth/register
{
  "email": "alice@gym.co.uk",
  "password": "min8chars",
  "full_name": "Alice Smith",
  "role": "coach",
  "timezone": "Europe/London",
  "business_name": "Alice's PT" // optional
}
```

**Client registration** (creates user + client profile, links to a coach):
```json
POST /api/v1/auth/register
{
  "email": "bob@gmail.com",
  "password": "min8chars",
  "full_name": "Bob Jones",
  "role": "client",
  "timezone": "Europe/London",
  "coach_id": "uuid-of-their-coach",
  "sessions_per_month": 4 // 1–20
}
```

> **Note**: In the MVP, client registration is done by the coach on behalf of the client (the coach has the client's details). There is no self-service client sign-up flow planned.

### Password reset

```
1. POST /api/v1/auth/forgot-password  { "email": "..." }
   → always 200 (never reveals if email exists)
   → backend emails a link: http://your-app.com/reset-password?token=XYZ

2. User clicks link → your app reads ?token from the URL

3. POST /api/v1/auth/reset-password  { "token": "XYZ", "password": "newpass" }
   → 200 on success, 422 if token expired/used
```

---

## 4. API Response Shape

Every response from the API is wrapped in an envelope:

```ts
// Success
{ "data": <payload> }

// Error
{ "error": "human-readable message" }

// Validation error (422)
{
  "error": "validation failed",
  "fields": {
    "email": "must be a valid email",
    "password": "minimum length is 8"
  }
}
```

When building your API client, unwrap `response.data` before returning to the caller, and surface `response.error` / `response.fields` in the UI.

### Suggested TypeScript API client wrapper

```ts
async function apiRequest<T>(
  path: string,
  options?: RequestInit
): Promise<T> {
  const res = await fetch(`${BASE_URL}/api/v1${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${getAccessToken()}`,
      ...options?.headers,
    },
  });

  const body = await res.json();

  if (!res.ok) {
    // body.error is always a string; body.fields is optional
    throw new ApiError(res.status, body.error, body.fields);
  }

  return body.data as T;
}
```

---

## 5. Error Handling Contract

| HTTP Status | Meaning | What to show |
|---|---|---|
| `400` | Bad request / malformed JSON | Generic "something went wrong" toast |
| `401` | Token missing, expired, or invalid | Silently refresh token; if that fails → redirect to login |
| `403` | Valid token but wrong role | "You don't have permission to do this" message |
| `404` | Resource not found | Contextual 404 message (e.g. "Session not found") |
| `409` | Conflict (e.g. email taken, already charged) | Show the `error` string from the body directly |
| `422` | Validation failure | Show per-field errors from `body.fields` inline on forms |
| `429` | Rate limited | "Too many requests — please wait a moment" |
| `500` | Server error | "Something went wrong on our end" toast; never show raw error |

Rate limits to be aware of:
- Auth endpoints (`/auth/register`, `/auth/login`, `/auth/forgot-password`, `/auth/reset-password`): **10 requests/minute per IP**
- Refresh + logout: **30 requests/minute per IP**
- All other endpoints: **100 requests/minute per IP**

---

## 6. Endpoint Reference

All protected endpoints require `Authorization: Bearer <access_token>`.

### Auth

| Method | Path | Auth | Body | Notes |
|---|---|---|---|---|
| `POST` | `/auth/register` | No | `RegisterRequest` | Returns tokens immediately |
| `POST` | `/auth/login` | No | `LoginRequest` | Returns tokens |
| `POST` | `/auth/logout` | Token (any) | `{ refresh_token }` | Revokes refresh token |
| `POST` | `/auth/refresh` | Token (any) | `{ refresh_token }` | Returns new access token |
| `POST` | `/auth/forgot-password` | No | `{ email }` | Always 200 |
| `POST` | `/auth/reset-password` | No | `{ token, password }` | Token from email link |

### Coach profile

| Method | Path | Auth | Role | Notes |
|---|---|---|---|---|
| `GET` | `/coaches/{coachID}/profile` | Yes | `coach` | Returns `CoachProfile` |
| `PUT` | `/coaches/{coachID}/profile` | Yes | `coach` | Update name/phone/biz/timezone |

### Client profile

| Method | Path | Auth | Role | Notes |
|---|---|---|---|---|
| `GET` | `/clients/{clientID}/profile` | Yes | `client` | Returns `ClientProfile` |
| `PUT` | `/clients/{clientID}/profile` | Yes | `client` | Update name/phone/timezone |

### Availability

| Method | Path | Auth | Role | Notes |
|---|---|---|---|---|
| `GET` | `/coaches/{coachID}/availability` | Yes | `coach` | Returns array of `WorkingHours` |
| `PUT` | `/coaches/{coachID}/availability` | Yes | `coach` | **Replace** the full weekly schedule |
| `GET` | `/clients/{clientID}/preferences` | Yes | `client` | Returns array of `PreferredWindow` |
| `PUT` | `/clients/{clientID}/preferences` | Yes | `client` | **Replace** the full preference set |

> Both availability endpoints are **replace-all** — send the complete schedule every time, not just changed entries.

### Scheduling

| Method | Path | Auth | Role | Notes |
|---|---|---|---|---|
| `POST` | `/schedule-runs` | Yes | `coach` | Triggers solver; returns `ScheduleRun` with proposed sessions |
| `GET` | `/schedule-runs/{runID}` | Yes | Any | Returns `ScheduleRun` + `sessions[]` |
| `POST` | `/schedule-runs/{runID}/confirm` | Yes | `coach` | Confirms all proposed sessions |
| `POST` | `/schedule-runs/{runID}/reject` | Yes | `coach` | Rejects; soft-deletes proposed sessions |
| `GET` | `/sessions` | Yes | Any | List sessions; filter with `?status=proposed\|confirmed\|cancelled\|completed\|pending_cancellation` |
| `POST` | `/sessions/{sessionID}/cancel` | Yes | Any | Cancel request; body `{ reason }`. See §7.4 for two-path behaviour. |
| `POST` | `/sessions/{sessionID}/cancel/approve` | Yes | `coach` | Coach approves within-24h cancellation — session lost, no credit |
| `POST` | `/sessions/{sessionID}/cancel/waive` | Yes | `coach` | Coach waives policy — session cancelled and credit issued to client |

### Billing (coach only)

| Method | Path | Auth | Role | Notes |
|---|---|---|---|---|
| `POST` | `/payments/setup-intent` | Yes | `coach` | Returns Stripe `client_secret` for card setup |
| `POST` | `/payments/mandate` | Yes | `coach` | ⚠️ **NOT ACTIVE — returns 501.** GoCardless application rejected. Preserved for future use. |
| `POST` | `/payments/mandate/complete` | Yes | `coach` | ⚠️ **NOT ACTIVE — returns 501.** See above. |
| `POST` | `/billing/charge` | Yes | `coach` | Manual monthly charge trigger (Stripe only in this release) |

### Webhooks (no JWT, provider signature verification)

| Method | Path | Notes |
|---|---|---|
| `POST` | `/webhooks/stripe` | Do not call from frontend |
| `POST` | `/webhooks/gocardless` | ⚠️ NOT ACTIVE — returns 501. Route registered for future use. Do not call from frontend. |
| `POST` | `/webhooks/twilio` | Do not call from frontend |

### Calendar

| Method | Path | Auth | Notes |
|---|---|---|---|
| `GET` | `/me/calendar-url` | Yes | Returns the user's ICS subscription URL + sync-delay warning |
| `POST` | `/me/calendar-url/regenerate` | Yes | Issues a new token — old URL stops working immediately |
| `GET` | `/calendar/{token}.ics` | No (token in URL) | The actual ICS feed; subscribed to by calendar apps |

### GDPR

| Method | Path | Auth | Notes |
|---|---|---|---|
| `GET` | `/me/export` | Yes | Returns all data for the logged-in user as JSON download |

### Health

| Method | Path | Notes |
|---|---|---|
| `GET` | `/healthz` | Liveness — no auth |
| `GET` | `/readyz` | Readiness (checks DB) — no auth |

---

## 7. Core User Flows

### 7.1 Coach — First-time setup

```
1. Register (POST /auth/register, role: "coach")
2. Set working hours (PUT /coaches/{id}/availability)
   → Send full weekly schedule, e.g.:
     { hours: [
       { day_of_week: 0, start_time: "09:00", end_time: "17:00" },  // Monday
       { day_of_week: 1, start_time: "09:00", end_time: "17:00" },  // Tuesday
       ...
     ]}
3. Register each client (POST /auth/register, role: "client", coach_id: <coach_id>)
4. Set payment method per client:
   - Card:  POST /payments/setup-intent → use client_secret in Stripe.js
   - DD:    ⚠️ NOT ACTIVE in this release (GoCardless application rejected — see §9)
```

### 7.2 Coach — Weekly scheduling flow

This is the **core feature**. The UI should make this feel like a one-click action.

```
Step 1 — Trigger
  POST /schedule-runs  { week_start: "2026-05-04" }
  
  Possible outcomes:
  • 201 ScheduleRun — solver returned sessions; show proposed schedule
  • 422 "no feasible schedule found" — prompt coach to check working hours or client count

Step 2 — Review
  Display the ScheduleRun.sessions[] as a weekly calendar view.
  Each session shows: client name, day, time (in Europe/London).
  
  The run has status "pending_confirmation" and expires after 48 hours
  (show a countdown so the coach knows to act).

Step 3a — Confirm
  POST /schedule-runs/{runID}/confirm
  → Status becomes "confirmed"
  → All sessions become confirmed
  → Clients receive SMS + email automatically (backend handles this)

Step 3b — Reject
  POST /schedule-runs/{runID}/reject
  → Status becomes "rejected"
  → All proposed sessions are discarded
  → Coach can trigger a new run
```

**Edge cases to handle**:
- `409` on confirm/reject: "This schedule has already been confirmed, rejected, or expired" — refresh the run and update the UI.
- Solver may return `unscheduled_clients` in the solver output (visible in `ScheduleRun.solver_output`). If present, warn the coach that some clients could not be scheduled this week.

### 7.3 Client — Set availability preferences

```
GET /clients/{clientID}/preferences   // load existing
PUT /clients/{clientID}/preferences   // save
  {
    windows: [
      { day_of_week: 0, start_time: "07:00", end_time: "09:00" },  // Mon morning
      { day_of_week: 2, start_time: "18:00", end_time: "20:00" },  // Wed evening
    ]
  }
```

Clients can also update preferences via SMS (the backend handles inbound Twilio messages). The UI should note this to clients: "You can also text us your availability."

### 7.4 Cancel a session

The cancellation endpoint has two paths depending on how far away the session is.

#### Path A — Outside the 24-hour window (client cancels ≥24 h before start)

```
POST /sessions/{sessionID}/cancel  { "reason": "holiday" }

Response: CancelSessionResponse
  {
    "session": Session,        // status = "cancelled"
    "credit": SessionCredit,   // always present in this path
    "within_24h_window": false,
    "message": "Session cancelled. A credit has been added to your account."
  }
```

Show the `message` in a success banner. The credit will be applied to a future session.

#### Path B — Inside the 24-hour window (client cancels <24 h before start)

```
POST /sessions/{sessionID}/cancel  { "reason": "feeling ill" }

Response: CancelSessionResponse
  {
    "session": Session,        // status = "pending_cancellation"
    "credit": null,            // no credit yet — coach decides
    "within_24h_window": true,
    "message": "Your cancellation request has been sent to your coach..."
  }
```

Show the `message` as a warning banner. The session card should reflect the `pending_cancellation` status.
**The coach must then act** — the session stays on their calendar until they do.

#### Coach review (appears when a session is in `pending_cancellation`)

The coach sees a notification and must choose one of two actions:

```
// Session lost — no credit issued to client
POST /sessions/{sessionID}/cancel/approve
→ 200 Session  (status = "cancelled")

// Policy waived — credit issued to client
POST /sessions/{sessionID}/cancel/waive
→ 200 CancelSessionResponse
     { session, credit, within_24h_window: true }
```

In both cases the client is notified by SMS and email automatically.

**UI required**: A "Cancellation requests" section in the coach dashboard (or inline on the session card) showing pending requests with the client name, session time, and reason. Two action buttons: **Approve (session lost)** and **Waive policy (issue credit)**.

### 7.5 Calendar subscription

Both coaches and clients can subscribe to a personal ICS feed that shows all their sessions in any calendar app (Google Calendar, Apple Calendar, Outlook).

```
1. GET /me/calendar-url
   → {
       "url": "https://api.pt-scheduler.io/calendar/<token>.ics",
       "warning": "Calendar apps check for updates every 12–24 hours..."
     }

2. Show the URL in a read-only input with a "Copy" button.
   Show the warning message directly below it — do not hide it.

3. Provide a "Subscribe in Google Calendar" deep-link button:
   href="https://calendar.google.com/calendar/r?cid=<url>"
   (replace https:// with webcal:// in the cid value)

4. "Regenerate URL" button → POST /me/calendar-url/regenerate
   → new URL returned; warn the user their old subscriptions will stop working.
```

> **Important — always show this warning whenever the URL is displayed:**
> "Calendar apps (Google Calendar, Apple Calendar, Outlook) check for updates every 12–24 hours. Newly confirmed sessions and cancellations may take up to 24 hours to appear in your calendar."

#### How the feed works

- The URL contains a secret token — it never expires and requires no login.
- The feed includes all confirmed sessions, plus cancelled ones (so calendar apps remove them on the next refresh).
- The token can be regenerated from profile settings if the user wants to revoke access to the old URL.

### 7.6 Coach — Charge a client manually

```
POST /billing/charge
{
  client_id: "uuid",
  provider: "stripe" | "gocardless",
  amount_pence: 12000,       // £120.00 — always integers, never decimals
  billing_year: 2026,
  billing_month: 5
}

200 → Payment created (status: "pending", becomes "paid" via webhook)
409 → "payment already exists for this billing period" — show inline info, not an error
```

### 7.6 Stripe card setup

The frontend must include **Stripe.js** (loaded from `https://js.stripe.com/v3/`). The backend never sees raw card numbers.

```
1. Coach triggers: POST /payments/setup-intent { client_id, email, full_name }
   → { client_secret: "seti_..._secret_...", setup_intent_id: "seti_..." }

2. Mount Stripe Elements using the client_secret:
   const stripe = await loadStripe(STRIPE_PUBLISHABLE_KEY);
   const elements = stripe.elements({ clientSecret });
   // render <PaymentElement />

3. On form submit:
   const { error } = await stripe.confirmSetup({
     elements,
     confirmParams: { return_url: "https://your-app.com/payment-complete" }
   });
   if (error) show error.message to coach

4. On return_url load: check ?setup_intent_status=succeeded
   → show "Card saved successfully"
```

> Stripe publishable key (`pk_test_...` or `pk_live_...`) must be stored as a frontend env variable `VITE_STRIPE_PUBLISHABLE_KEY`. It is **not** served by the backend.

### 7.7 GoCardless Direct Debit setup ⚠️ NOT ACTIVE IN THIS RELEASE

> **Why this is here**: GoCardless Bacs Direct Debit is the natural payment rail for a
> UK PT subscription product. The full integration was designed and built into the backend.
> The GoCardless merchant application was **rejected before the project submission deadline**
> and there was insufficient time to re-apply. The code is preserved as an obvious future
> development path. Both `/payments/mandate` and `/payments/mandate/complete` currently
> return **HTTP 501 Not Implemented**.
>
> To enable this in a future release: obtain a live GoCardless account, set
> `GOCARDLESS_ACCESS_TOKEN` and `GOCARDLESS_WEBHOOK_SECRET` in the environment, and remove
> the `ErrGoCardlessNotAvailable` guards in `internal/billing/service.go`.

Once active, the flow would be:

```
1. Coach triggers: POST /payments/mandate { client_id, redirect_uri: "https://your-app.com/mandate-complete" }
   → { redirect_url: "https://pay.gocardless.com/...", flow_id: "RE..." }

2. Frontend redirects the client to redirect_url:
   window.location.href = response.redirect_url;

3. Client completes the GoCardless form, is sent back to your redirect_uri
   with ?redirect_flow_id=RF... appended.

4. Frontend calls:
   POST /payments/mandate/complete?redirect_flow_id=RF...
   { client_id: "uuid" }
   → { mandate_id, status: "active" }
   Show "Direct Debit set up successfully"
```

---

## 8. Data Models

These are the TypeScript types that map directly to API JSON responses.

```ts
// ── Auth ─────────────────────────────────────────────────────────────────────

interface TokenResponse {
  access_token: string;
  refresh_token?: string;  // omitted on refresh responses
  expires_in: number;      // seconds (900 = 15 min)
}

// ── Users ─────────────────────────────────────────────────────────────────────

interface User {
  id: string;            // UUID
  email: string;
  role: 'coach' | 'client' | 'admin';
  full_name: string;
  phone?: string;        // E.164, e.g. "+447911123456"
  timezone: string;      // IANA, e.g. "Europe/London"
  is_verified: boolean;
  created_at: string;    // ISO 8601 UTC
  updated_at: string;
}

interface Coach {
  id: string;
  user_id: string;
  business_name?: string;
  created_at: string;
  updated_at: string;
}

interface Client {
  id: string;
  user_id: string;
  coach_id: string;
  tenure_started_at: string;
  sessions_per_month: number;  // 1–20
  priority_score: number;
  created_at: string;
  updated_at: string;
}

interface CoachProfile {
  user: User;
  coach: Coach;
}

interface ClientProfile {
  user: User;
  client: Client;
}

// ── Availability ──────────────────────────────────────────────────────────────

// day_of_week: 0 = Monday … 6 = Sunday (ISO week, Monday-first throughout)
interface WorkingHours {
  id: string;
  coach_id: string;
  day_of_week: number;   // 0–6
  start_time: string;    // "HH:MM" in Europe/London local time
  end_time: string;
  created_at: string;
  updated_at: string;
}

interface PreferredWindow {
  id: string;
  client_id: string;
  day_of_week: number;
  start_time: string;
  end_time: string;
  source: 'manual' | 'sms' | 'whatsapp';
  collected_at: string;
  created_at: string;
}

// ── Scheduling ────────────────────────────────────────────────────────────────

type SessionStatus =
  | 'proposed'
  | 'confirmed'
  | 'cancelled'
  | 'completed'
  | 'pending_cancellation';  // within-24h cancel awaiting coach decision

type RunStatus = 'pending_confirmation' | 'confirmed' | 'rejected' | 'expired';

interface Session {
  id: string;
  coach_id: string;
  client_id: string;
  schedule_run_id?: string;
  starts_at: string;               // ISO 8601 UTC — convert to Europe/London for display
  ends_at: string;                 // always starts_at + 60 min
  status: SessionStatus;
  notes?: string;
  cancellation_reason?: string;    // set when status = pending_cancellation
  cancellation_requested_at?: string; // ISO 8601 UTC — when the client submitted the request
  created_at: string;
  updated_at: string;
}

// Returned by POST /sessions/{id}/cancel  AND  POST /sessions/{id}/cancel/waive
interface CancelSessionResponse {
  session: Session;
  credit?: SessionCredit;    // present when a credit was issued
  within_24h_window: boolean; // true = pending coach review; false = cancelled immediately
  message: string;           // human-readable — safe to show directly to the user
}

interface ScheduleRun {
  id: string;
  coach_id: string;
  week_start: string;   // "YYYY-MM-DDTHH:MM:SSZ"
  status: RunStatus;
  expires_at: string;   // 48 h after creation — show countdown
  sessions?: Session[];
  created_at: string;
  updated_at: string;
}

interface SessionCredit {
  id: string;
  client_id: string;
  reason: string;
  source_session_id: string;
  used_session_id?: string;  // null = credit still available
  expires_at: string;
  created_at: string;
}

// ── Billing ───────────────────────────────────────────────────────────────────

type PaymentStatus = 'pending' | 'paid' | 'failed' | 'refunded';
// NOTE: 'gocardless' is defined in the backend but NOT ACTIVE in this release.
// Only 'stripe' is available. See §7.7 for context.
type PaymentProvider = 'stripe' | 'gocardless';

interface Payment {
  id: string;
  client_id: string;
  provider: PaymentProvider;
  provider_ref?: string;
  amount_pence: number;   // GBP pence — divide by 100 for display: £120.00
  currency: string;       // always "GBP"
  billing_year: number;
  billing_month: number;  // 1–12
  status: PaymentStatus;
  created_at: string;
  updated_at: string;
}

interface SetupIntentResponse {
  client_secret: string;
  setup_intent_id: string;
}

interface MandateResponse {
  redirect_url: string;  // redirect client to this
  flow_id: string;
}

// ── GDPR ──────────────────────────────────────────────────────────────────────

interface DataExport {
  exported_at: string;
  user: Record<string, unknown>;
  profile?: Record<string, unknown>;
  sessions: Record<string, unknown>[];
  payments: Record<string, unknown>[];
}
```

---

## 9. Payments Integration

### Money handling rule — CRITICAL

**All monetary amounts are stored and transmitted as integer pence (GBP).** Never use `float` for money.

```ts
// Correct
const display = `£${(payment.amount_pence / 100).toFixed(2)}`;  // "£120.00"

// Wrong — floating point errors
const display = `£${payment.amount_pence * 0.01}`;
```

### Stripe publishable key

This is the **only** Stripe credential the frontend needs. Store it as:
```
VITE_STRIPE_PUBLISHABLE_KEY=pk_test_...
```

Never use the secret key (`sk_...`) on the frontend.

### Payment status lifecycle

```
pending  →  paid      (via Stripe webhook — may take seconds to minutes)
pending  →  failed    (webhook reports failure)
paid     →  refunded  (manual refund — not in MVP scope)
```

The frontend should poll or use optimistic UI for payment status. The payment is created as `pending` immediately — the webhook updates it to `paid` asynchronously. For the MVP, a simple refresh after a few seconds is fine.

### GoCardless Bacs advance notice ⚠️ NOT ACTIVE IN THIS RELEASE

> GoCardless Direct Debit is not active in this release (see §7.7). This section is
> preserved as documentation for when the integration is enabled.

When active: Direct Debit charges have a mandatory 3-working-day advance notice for first payments (2 days for subsequent). The backend enforces this. If a charge is attempted too close to the billing date, the backend will return a 422 with an explanatory message — surface it to the coach.

---

## 10. UK Localisation Requirements

These are non-negotiable for this market.

| Concern | Requirement |
|---|---|
| **Timezone** | All timestamps from the API are UTC. Display them in `Europe/London` (handles BST/GMT automatically). Use `Intl.DateTimeFormat` with `timeZone: 'Europe/London'`. |
| **Date format** | DD/MM/YYYY throughout all user-facing text. Never MM/DD/YYYY. |
| **Currency** | GBP (£) only. Display as `£120.00`. |
| **Week start** | Monday-first in all calendar/week views. Week 0 = Monday. |
| **Phone input** | Accept UK numbers in any format; format display as `+44 7911 123456`. |
| **Time format** | 24-hour clock (`09:00`, `17:30`) in all scheduling UI. |

```ts
// Correct timestamp display
const formatter = new Intl.DateTimeFormat('en-GB', {
  timeZone: 'Europe/London',
  day: '2-digit',
  month: '2-digit',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  hour12: false,
});
formatter.format(new Date(session.starts_at)); // "04/05/2026, 09:00"
```

---

## 11. Pages & Components Checklist

This is not a strict spec — it is a suggested set of pages based on the backend capabilities. Use your judgement on layout and component breakdown.

### Public pages

- [ ] `/login` — email + password form, "forgot password" link
- [ ] `/register` — role selector (coach / client), conditional fields based on role
- [ ] `/forgot-password` — email form, success state ("check your email")
- [ ] `/reset-password?token=...` — new password form, expired-token error state

### Coach pages (role = `coach`)

- [ ] `/dashboard` — summary: upcoming sessions this week, pending schedule runs needing confirmation, any failed payments
- [ ] `/schedule`
  - Weekly calendar view of confirmed sessions (colour-coded by client)
  - "Generate schedule" button → week picker → triggers solver → loading state
  - Schedule run review card: proposed sessions on calendar, confirm/reject buttons, expiry countdown
  - Infeasible/partial result warning (when solver can't schedule all clients)
  - **Cancellation requests panel**: badge count on nav when any session is `pending_cancellation`; list showing client name, session time, reason, and **Approve / Waive** action buttons
- [ ] `/clients`
  - Client list: name, sessions/month, payment method status, next billing date
  - Add client modal (wraps `POST /auth/register` with role=client)
  - Per-client actions: view profile, set payment method
- [ ] `/clients/{clientID}`
  - Client profile detail
  - Payment history (billing_year, billing_month, amount_pence, status)
  - "Charge now" button → triggers `POST /billing/charge` (Stripe only in this release)
  - Set payment method: Stripe card setup only (GoCardless not active — see §7.7)
- [ ] `/availability`
  - Weekly grid: drag or click slots to set working hours
  - Save sends full schedule via `PUT /coaches/{id}/availability`
- [ ] `/profile` — edit full name, business name, phone, timezone

### Client pages (role = `client`)

- [ ] `/dashboard` — upcoming sessions (next 4 weeks), any active credits
- [ ] `/sessions`
  - List of sessions filtered by status (upcoming / past / cancelled / pending_cancellation)
  - Cancel button on upcoming `confirmed` sessions
  - When `within_24h_window = true` in the cancel response: show warning "Your request has been sent to your coach. The session may still be charged depending on their decision."
  - When `within_24h_window = false` and `credit` present: show success banner "Session cancelled. A credit has been added to your account."
  - Sessions with `status = pending_cancellation` should render a distinct "Awaiting coach decision" badge
- [ ] `/preferences`
  - Weekly grid: click slots to mark preferred training windows
  - Save sends full schedule via `PUT /clients/{id}/preferences`
  - Hint: "You can also text your preferences to +44..."
- [ ] `/profile` — edit full name, phone, timezone

### Shared

- [ ] `/me/export` or account settings → "Download my data" (GDPR) — calls `GET /me/export`, triggers a JSON file download

---

## 12. Environment Variables

Create a `.env.local` file at the root of the Vite project:

```bash
# API base URL — no trailing slash
VITE_API_BASE_URL=http://localhost:8080

# Stripe publishable key (safe to expose — NOT the secret key)
VITE_STRIPE_PUBLISHABLE_KEY=pk_test_xxxxxxxxxxxxxxxx
```

Use in code:
```ts
const BASE_URL = import.meta.env.VITE_API_BASE_URL;
const STRIPE_KEY = import.meta.env.VITE_STRIPE_PUBLISHABLE_KEY;
```

For production, set these in your hosting environment (Vercel, Netlify, etc.) — never commit `.env.local`.

---

## Appendix A — Field validation rules

These match the backend validation. Use them to show inline errors client-side before the API call.

| Field | Rule |
|---|---|
| `email` | Valid email format |
| `password` | 8–72 characters |
| `full_name` | 2–100 characters |
| `role` | `"coach"` or `"client"` |
| `timezone` | IANA timezone string (e.g. `"Europe/London"`) |
| `phone` | E.164 format (e.g. `"+447911123456"`) or empty |
| `business_name` | Max 120 characters |
| `sessions_per_month` | Integer 1–20 |
| `week_start` | `"YYYY-MM-DD"` format, must be a Monday |
| `day_of_week` | Integer 0–6 (0 = Monday) |
| `start_time` / `end_time` | `"HH:MM"` 24-hour format, end must be after start |
| `amount_pence` | Integer, minimum 100 (= £1.00) |
| `billing_month` | Integer 1–12 |
| `billing_year` | Integer ≥ 2024 |
| `reason` (cancel) | Required, max 500 characters |

---

## Appendix B — Backend-marked frontend TODOs

The following comments exist in the backend codebase pointing specifically at frontend responsibilities:

1. **`POST /auth/login` → 401**: Redirect to login page. Never reveal which of email or password was wrong ("incorrect email or password" — single message for both).
2. **`POST /auth/refresh` → 401**: Clear stored tokens and redirect to login. Do not show an error — the user's session simply expired.
3. **`POST /auth/register` → 409**: "An account with this email already exists."
4. **`POST /auth/reset-password` → 422**: "This reset link is invalid or has expired. Please request a new one."
5. **`POST /schedule-runs` → 422 infeasible**: "No valid schedule could be found. Check that working hours are set and clients have sessions assigned."
6. **`POST /schedule-runs/{id}/confirm` → 409**: "This schedule has already been confirmed, rejected, or expired." Refresh the run state.
7. **`POST /sessions/{id}/cancel` — `within_24h_window: false`**: Show banner "Session cancelled. A credit has been added to your account."
7b. **`POST /sessions/{id}/cancel` — `within_24h_window: true`**: Show warning "Your request has been sent to your coach. Because it is within 24 hours of the session, they will decide whether the session is waived or lost."
7c. **Coach dashboard** — sessions with `status: pending_cancellation`: Show a distinct action card with the client name, session time, cancellation reason, and two buttons: **Approve (session lost)** and **Waive policy (credit issued)**.
7d. **`POST /sessions/{id}/cancel/waive` — `credit` in response**: Show coach confirmation "Policy waived. The client has been notified and a credit has been issued to their account."
8. **`POST /billing/charge` → 409**: "This client has already been charged for this billing period." Show as informational, not an error.
9. **`POST /payments/setup-intent`**: Use the returned `client_secret` to initialise Stripe Elements — never send card details to the backend.
10. **`POST /payments/mandate`**: ⚠️ NOT ACTIVE — returns 501. GoCardless application was rejected. Do not surface this flow in the UI for this release.
11. **Failed payments** (webhook-updated status): Surface failed payment status in the coach dashboard with a "Retry charge" action.
