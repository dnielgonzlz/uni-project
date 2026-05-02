# Test Suite — PT Scheduler Backend

This document describes every test in the backend, why it exists, and how to run it.

---

## Table of Contents

1. [How to run the tests](#1-how-to-run-the-tests)
2. [Test strategy overview](#2-test-strategy-overview)
3. [Go unit tests](#3-go-unit-tests)
   - [auth — password, tokens, JWT](#31-auth--password-tokens-jwt)
   - [billing — idempotency keys and Bacs dates](#32-billing--idempotency-keys-and-bacs-dates)
   - [scheduling constraints](#33-scheduling--hard-constraint-validators)
   - [scheduling service — cancellation flows](#34-scheduling--service-cancellation-flows)
4. [Go integration tests](#4-go-integration-tests)
5. [Python solver tests](#5-python-solver-tests)

---

## 1. How to run the tests

### Go unit tests (no database required)

```bash
cd backend
go test ./...
```

All unit tests are safe to run anywhere — no external services required.

### Go integration tests (requires Docker)

Integration tests spin up a real PostgreSQL container via testcontainers. Docker must be running.

```bash
cd backend
go test -tags integration -race ./internal/integration/
```

The `-race` flag enables the Go race detector, which is required in CI.

### Python solver tests (no database required)

```bash
cd solver
make install   # only needed once
make test
```

---

## 2. Test strategy overview

| Layer | Type | Tools | DB required |
|---|---|---|---|
| Auth helpers (password, JWT, tokens) | Unit | `testing` + `testify/require` | No |
| Billing helpers (idempotency, Bacs dates) | Unit | `testing` + `testify/require` | No |
| Scheduling constraints | Unit | `testing` + `testify/require` | No |
| Scheduling service (cancel/approve/waive) | Unit | `testing` + `testify/require` + in-memory fakes | No |
| Auth register/login flow | Integration | `testcontainers-go` + real Postgres | Yes (Docker) |
| Payment idempotency | Integration | `testcontainers-go` + real Postgres | Yes (Docker) |
| DB exclusion constraints (no double-booking) | Integration | `testcontainers-go` + real Postgres | Yes (Docker) |
| OR-Tools solver | Unit (Python) | `pytest` + `ortools` | No |

The guiding principle: **use real infrastructure at the boundary, fakes everywhere else.** Unit tests run in milliseconds and cover logic. Integration tests prove the database schema and constraints actually work.

---

## 3. Go unit tests

### 3.1 Auth — password, tokens, JWT

**File**: `internal/auth/service_test.go`  
**Package**: `auth_test`

These tests cover the cryptographic primitives used by the auth service. They require no database and run in parallel.

---

#### `TestHashAndVerifyPassword`

Hashes the string `"correcthorsebatterystaple"` with argon2id and verifies three things:

| Sub-test | What it proves |
|---|---|
| `correct password verifies` | The correct plaintext passes `VerifyPassword` |
| `wrong password rejected` | A different string returns `ErrInvalidPassword` |
| `empty password rejected` | An empty string returns `ErrInvalidPassword` |

**Why it matters**: argon2id is configured with specific memory/time parameters. This test confirms the hash-then-verify round-trip works end-to-end, not just that the function compiles.

---

#### `TestHashPasswordProducesUniqueHashes`

Hashes the same password twice and asserts the two hashes are different.

**Why it matters**: proves that a unique random salt is embedded in every hash. If this failed, an attacker with a leaked hash table could instantly reverse any password.

---

#### `TestGenerateSecureToken`

Calls `GenerateSecureToken()` twice and asserts:
- Each token is exactly 64 characters (32 random bytes encoded as hex)
- The two tokens are not equal

**Why it matters**: refresh tokens and password-reset tokens are generated this way. 64 hex chars = 256 bits of entropy, which is collision-proof for practical purposes.

---

#### `TestHashToken`

Hashes the string `"abc123"` twice and asserts:
- The same input always produces the same output (deterministic)
- The output differs from the input (it is actually hashed)

**Why it matters**: tokens are stored in the database as SHA-256 hashes, not in plaintext. This function is called both when storing and when looking up a token. If it weren't deterministic, lookup would always fail.

---

#### `TestGenerateAndParseAccessToken`

Generates a JWT for a known user ID and role, then parses it back and asserts the claims match.

**Why it matters**: the JWT is the core auth mechanism. This test proves the full sign → verify round-trip works with the correct secret, and that the `UserID` and `Role` claims survive serialisation.

---

#### `TestParseAccessToken_WrongSecret`

Generates a JWT signed with `"correct-secret"`, then attempts to parse it with `"wrong-secret"`. Asserts `ErrInvalidToken`.

**Why it matters**: prevents token forgery. If this test passed when using the wrong secret, any attacker could sign their own JWTs.

---

#### `TestParseAccessToken_Expired`

Generates a JWT with a 0-minute expiry, waits 1 second, then parses it. Asserts `ErrInvalidToken`.

**Why it matters**: access tokens expire after 15 minutes in production. This test confirms the expiry check is actually enforced by the parser, not silently ignored.

---

#### `TestFixedClock`

Constructs a `clock.Fixed` with a known time and calls `Now()`. Asserts the returned value equals the fixed time.

**Why it matters**: `clock.Fixed` is used throughout unit tests to make time-dependent logic (token expiry, 24-hour cancellation window) deterministic. If `Fixed.Now()` returned the real clock, those tests would be flaky.

---

### 3.2 Billing — idempotency keys and Bacs dates

**File**: `internal/billing/billing_test.go`  
**Package**: `billing`

> **Note on GoCardless**: The GoCardless Direct Debit integration was fully implemented but
> is not active in this release — the merchant application was rejected before the project
> deadline. The `BacsEarliestChargeDate` tests and the `gocardless` idempotency key tests
> are preserved as working infrastructure for when the integration is enabled. They test
> pure calculation logic with no external dependencies.

These tests cover two pure functions that have no side effects.

---

#### `TestIdempotencyKey_Format`

Calls `IdempotencyKey("stripe", uuid, 2025, 1)` and asserts the output is exactly `"stripe-<uuid>-2025-01"`.

**Why it matters**: the idempotency key is stored in the database with a `UNIQUE` constraint. If the format were inconsistent, a retry with the same logical payment could produce a different key and charge the client twice.

---

#### `TestIdempotencyKey_DecemberPadding`

Asserts that month 12 is formatted as `"12"`, not `"12.0"` or any other unexpected form.

**Why it matters**: ensures consistent zero-padding for single-digit months (January = `"01"`, not `"1"`). A mismatch between how a key is stored and how a retry generates it would break idempotency.

---

#### `TestIdempotencyKey_UniquePerMonth`

Generates keys for January and February for the same client. Asserts they differ.

**Why it matters**: each billing period is independent. Without this guarantee, charging a client in February could be mistaken for a retry of January's charge.

---

#### `TestIdempotencyKey_UniquePerProvider`

Generates keys for `"stripe"` and `"gocardless"` for the same client and month. Asserts they differ.

**Why it matters**: a client can switch payment provider mid-year. The keys must not collide across providers.

---

#### `TestIdempotencyKey_UniquePerClient`

Generates keys for two different client UUIDs. Asserts they differ.

**Why it matters**: baseline sanity — keys must be scoped to an individual client.

---

#### `TestBacsEarliestChargeDate_SubsequentPayment_SkipsWeekend`

Input: Friday 2025-01-03, subsequent payment (2 working days advance).  
Expected output: Tuesday 2025-01-07.

**Why it matters**: GoCardless Bacs requires 2 working days' advance notice for subsequent payments. Saturday and Sunday are not working days. Charging on a weekend would fail at the provider level.

---

#### `TestBacsEarliestChargeDate_FirstPayment_ThreeWorkingDays`

Input: Monday 2025-01-06, first payment (3 working days advance).  
Expected output: Thursday 2025-01-09.

**Why it matters**: first mandates require 3 working days. Getting this wrong would result in a rejected charge at Bacs, which is a compliance issue.

---

#### `TestBacsEarliestChargeDate_Thursday_SubsequentSpansWeekend`

Input: Thursday 2025-01-09, subsequent (2 working days).  
Expected output: Monday 2025-01-13 (skips Saturday + Sunday).

**Why it matters**: the most common edge case for the weekend-skip logic.

---

#### `TestBacsEarliestChargeDate_Wednesday_FirstPayment_SpansWeekend`

Input: Wednesday 2025-01-08, first payment (3 working days).  
Expected output: Monday 2025-01-13 (Thursday + Friday + Monday, skipping weekend).

**Why it matters**: a 3-day advance that crosses a weekend needs two weekend days skipped, not one.

---

#### `TestBacsEarliestChargeDate_AlwaysFutureDate`

Input: `time.Now()`.  
Asserts the returned date is strictly after `time.Now()`.

**Why it matters**: the earliest charge date must always be in the future. A past date would be immediately rejected by GoCardless.

---

#### `TestBacsEarliestChargeDate_NeverOnWeekend`

Iterates over every day of a full week (Mon–Sun) and calls `BacsEarliestChargeDate` for each. Asserts the result is never Saturday or Sunday.

**Why it matters**: covers all starting-day permutations to ensure no edge case produces a weekend result.

---

#### `TestPad2_SingleDigit` / `TestPad2_TwoDigits` / `TestPad4_Year`

Sanity checks on the zero-padding helpers used internally by `IdempotencyKey`.

---

### 3.3 Scheduling — hard constraint validators

**File**: `internal/scheduling/constraints_test.go`  
**Package**: `scheduling_test`

These tests cover the pre-solver feasibility checks applied to every scheduling request before calling the OR-Tools microservice.

---

#### `TestCheckRecoveryPeriod_NoConflict`

One existing session on Monday 09:00. Proposes Tuesday 10:00 (25-hour gap). Asserts no error.

---

#### `TestCheckRecoveryPeriod_TooClose_After`

One session Monday 09:00–10:00. Proposes Monday 20:00 (10-hour gap from end). Asserts `ConstraintError` with code `"recovery_period"`.

**Why it matters**: the 24-hour recovery period is a hard constraint defined in the spec. A client must not have back-to-back sessions.

---

#### `TestCheckRecoveryPeriod_TooClose_Before`

Session Tuesday 15:00. Proposes Tuesday 08:00 (session ends at 15:00, proposed session is less than 24 h before). Asserts error.

**Why it matters**: the recovery check must work in both directions — a proposed session that is too close to an *upcoming* confirmed session is also a violation.

---

#### `TestCheckRecoveryPeriod_ExactlyAtBoundary`

Session Monday 09:00–10:00. Proposes Tuesday 10:00 (exactly 24 h after the previous session ends). Asserts no error.

**Why it matters**: the boundary is inclusive — exactly 24 h is allowed.

---

#### `TestCheckDailyLimit_UnderLimit`

Two sessions already on Monday. Proposes a third. Asserts no error.

---

#### `TestCheckDailyLimit_AtLimit`

Four sessions already on Monday. Proposes a fifth. Asserts `ConstraintError` with code `"daily_limit"`.

**Why it matters**: coaches have a maximum of 4 sessions per day (5 only with an explicit exception flag). This prevents overloading.

---

#### `TestCheckDailyLimit_ExceptionAllows5`

Four sessions on Monday, exception flag `true`. Proposes a fifth. Asserts no error.

---

#### `TestCheckDailyLimit_ExceptionDoesNotAllow6`

Five sessions on Monday, exception flag `true`. Proposes a sixth. Asserts error.

**Why it matters**: even the exception has a hard cap of 5.

---

#### `TestCheckWithinWorkingHours_Inside`

Working hours: Monday 09:00–17:00. Proposes Monday 10:00. Asserts no error.

---

#### `TestCheckWithinWorkingHours_Outside_TooLate`

Working hours: Monday 09:00–17:00. Proposes Monday 16:30 (session ends 17:30, outside window). Asserts `ConstraintError` with code `"outside_working_hours"`.

**Why it matters**: a session must both *start* and *end* within working hours.

---

#### `TestCheckWithinWorkingHours_WrongDay`

Working hours: Monday only. Proposes Tuesday 10:00. Asserts error.

---

#### `TestCancellationEarnsCredit_Enough_Notice`

Session starts 48 hours from now. Asserts `CancellationEarnsCredit` returns `true`.

---

#### `TestCancellationEarnsCredit_Not_Enough_Notice`

Session starts 11 hours from now. Asserts `CancellationEarnsCredit` returns `false`.

**Why it matters**: this function is the decision point between the two cancellation paths (immediate cancel + credit vs. pending coach review). Getting the boundary wrong would charge clients incorrectly.

---

### 3.4 Scheduling — service cancellation flows

**File**: `internal/scheduling/service_test.go`  
**Package**: `scheduling`

These tests use in-memory fakes for all external dependencies (database, users repository, notifier) and `clock.Fixed` for deterministic time. They cover the three methods added for the coach-confirmation cancellation policy.

---

#### `TestApproveCancellation_HappyPath`

Sets up a session in `pending_cancellation`, calls `ApproveCancellation` as the correct coach, and asserts:
- Returned session has status `cancelled`
- No credit was created
- The notifier received a `NotifySessionCancelled` call with `credit_issued = false`

---

#### `TestApproveCancellation_WrongStatus` (sub-tests: `confirmed`, `proposed`, `cancelled`)

Calls `ApproveCancellation` on a session that is **not** in `pending_cancellation`. Asserts a `ConstraintError` with code `"invalid_status"` for each starting status.

**Why it matters**: the approve endpoint is only valid for sessions already in the pending-review state. Approving a confirmed session would cancel a live booking incorrectly.

---

#### `TestApproveCancellation_Forbidden_WrongCoach`

Creates two coaches. Calls `ApproveCancellation` using the *other* coach's user ID. Asserts `ErrForbidden`.

**Why it matters**: prevents a coach from approving or waiving cancellations on another coach's sessions.

---

#### `TestApproveCancellation_NotFound`

Calls `ApproveCancellation` with a random UUID that does not exist in the store. Asserts `ErrNotFound`.

---

#### `TestApproveCancellation_RepoError`

Injects a failing `UpdateSessionStatus` via the fake store's callback. Asserts the error is propagated.

**Why it matters**: DB failures must bubble up, not be silently swallowed.

---

#### `TestWaiveCancellation_HappyPath`

Sets up a pending session with `CancellationReason = "feeling ill"`. Calls `WaiveCancellation` with a fixed clock (2025-06-01 09:00 UTC). Asserts:
- Returned session has status `cancelled`
- Credit is not nil
- Credit's `ClientID`, `SourceSessionID`, and `Reason` are correct
- Credit expires exactly one month after the fixed clock time
- The notifier received a `NotifySessionCancelled` call with `credit_issued = true`

---

#### `TestWaiveCancellation_WrongStatus` (sub-tests: `confirmed`, `proposed`, `cancelled`)

Same pattern as the approve equivalent. Asserts `ConstraintError` with `"invalid_status"`.

---

#### `TestWaiveCancellation_Forbidden_WrongCoach`

Asserts `ErrForbidden` when called by a coach who does not own the session.

---

#### `TestWaiveCancellation_NotFound`

Asserts `ErrNotFound` for a non-existent session UUID.

---

#### `TestWaiveCancellation_CreditStillReturnedIfCreateFails`

Injects a failing `CreateSessionCredit`. Asserts that:
- The service returns **no error** (credit errors are non-fatal)
- The returned session is still `cancelled`
- The returned credit is `nil`
- The notifier is called with `credit_issued = false`

**Why it matters**: the session is already cancelled at the point where credit creation fails. Rolling back the cancellation to preserve atomicity is worse than issuing a nil credit — the coach can issue a credit manually. The service must not propagate this error.

---

#### `TestCancelSession_OutsideWindow_ImmediateCancelAndCredit`

Session is 48 hours away. Calls `CancelSession`. Asserts:
- `WithinWindow = false`
- Session status is `cancelled`
- Credit is returned and belongs to the correct client
- Notifier called with `credit_issued = true`

---

#### `TestCancelSession_InsideWindow_PendingCancellation`

Session is 12 hours away. Calls `CancelSession`. Asserts:
- `WithinWindow = true`
- Session status is `pending_cancellation`
- No credit returned

---

#### `TestCancelSession_InvalidStatus`

Session is already `cancelled`. Calls `CancelSession`. Asserts `ConstraintError` with `"invalid_status"`.

---

## 4. Go integration tests

**File**: `internal/integration/integration_test.go`  
**Build tag**: `integration`

These tests run against a real Postgres 16 database spun up in a Docker container by [testcontainers-go](https://golang.testcontainers.org/). The schema is initialised by running the actual migration files before the test suite begins. This means the tests catch any mismatch between application code and the real database schema.

### `TestMain`

Bootstraps the test suite:
1. Starts a `postgres:16-alpine` container with database `ptscheduler_test`
2. Runs all four migration scripts as init scripts (`000001` initial schema, `000002` outbox, `000003` pending cancellation, `000004` calendar token)
3. Waits for Postgres to accept connections (two occurrences of the ready log line)
4. Opens a `pgxpool.Pool` and stores it in the package-level `testDB`
5. Runs all tests, then terminates the container

### `TestAuthRegisterAndLogin`

End-to-end test of the auth service against a live database.

| Sub-test | What it proves |
|---|---|
| `register new coach` | A new coach can be registered; returns access and refresh tokens |
| `duplicate email rejected` | A second registration with the same email returns `ErrEmailTaken` |
| `login with correct credentials` | Login with the registered email and password returns an access token |
| `login with wrong password rejected` | Wrong password returns `ErrInvalidPassword` |

**Why integration (not unit)?** The duplicate-email check relies on a `UNIQUE` constraint on the `users` table. It cannot be tested with a fake — only a real DB enforces this at the storage layer.

### `TestPaymentIdempotency`

Creates a coach, client, and billing repository against the live DB.

| Sub-test | What it proves |
|---|---|
| `first payment insert succeeds` | A new payment row is created with the given idempotency key |
| `second insert with same idempotency key is a no-op` | A retry with the same key does not create a duplicate row and does not return an error; if a row is returned, it carries the **original** provider ref |

**Why integration?** The idempotency guarantee comes from `ON CONFLICT DO NOTHING` on a `UNIQUE (idempotency_key)` constraint. Only a real database can prove this works.

### `TestSessionExclusionConstraint`

Creates a coach, client, and scheduling repository. Inserts one confirmed session at Monday 09:00–10:00, then attempts to insert an overlapping session at 09:30–10:30.

| Sub-test | What it proves |
|---|---|
| `first session inserts OK` | A confirmed session is accepted |
| `overlapping session for same coach is rejected by DB exclusion constraint` | The btree_gist exclusion constraint rejects the overlap |

**Why integration?** The `EXCLUDE USING gist (coach_id WITH =, tstzrange(starts_at, ends_at) WITH &&)` constraint is a PostgreSQL-specific feature. It cannot be replicated with an in-memory fake — this test is the only proof it works.

---

## 5. Python solver tests

**File**: `solver/test_scheduler.py`  
**Runner**: `pytest`

These tests exercise the OR-Tools CP-SAT model directly (no HTTP layer). All 13 tests run in under 2 seconds.

### Feasibility

| Test | What it proves |
|---|---|
| `test_single_client_one_session` | Solver returns a session for one client with quota = 1 |
| `test_multiple_clients_each_get_their_quota` | Three clients each with quota = 2 each receive exactly 2 sessions (6 total) |
| `test_no_clients_returns_optimal` | Empty client list returns `status = "optimal"` with no sessions |
| `test_no_working_hours_returns_infeasible` | Coach with no working hours → `status = "infeasible"` |
| `test_infeasible_too_many_sessions_for_available_slots` | One slot, two clients each needing 1 session → `status = "infeasible"` |

### Hard constraints

| Test | Constraint verified |
|---|---|
| `test_no_double_booking` | No two sessions share the same start time (coach can't be in two places) |
| `test_recovery_period_respected` | Gap between end of session N and start of session N+1 is ≥ 24 hours for every client |
| `test_existing_sessions_are_blocked` | Solver never places a new session in a slot occupied by an existing confirmed session |
| `test_daily_limit_not_exceeded` | Even when 5 clients all prefer Monday, at most 4 sessions land on Monday |

### Soft constraints

| Test | Constraint verified |
|---|---|
| `test_preferred_windows_honoured` | With ample capacity, a session lands in the client's preferred Tuesday 10:00–12:00 window |
| `test_high_priority_client_gets_preferred_window_over_low_priority` | When two clients compete for one preferred slot, the high-priority client wins it |

### Output format

| Test | What it proves |
|---|---|
| `test_output_timestamps_are_rfc3339_utc` | All `starts_at` and `ends_at` values end in `"Z"` and parse cleanly as RFC3339 |
| `test_ends_at_is_60_minutes_after_starts_at` | Every session is exactly 60 minutes long |
