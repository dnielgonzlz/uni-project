---
name: Project shape
description: High-level agreed architecture for the Intelligent Scheduling System backend MVP
type: project
---

Greenfield Go backend for an Intelligent Scheduling System for Personal Trainers (UK-localised). Stack: Go + chi + pgx + Postgres, Python FastAPI sibling service for OR-Tools solver, Stripe + GoCardless for payments, Twilio (WhatsApp) + Resend for comms. Domain-driven layout under `internal/` with a `platform/` package for cross-cutting concerns.

**Why:** user asked for a detailed plan on 2026-04-14 and accepted these opinionated picks as the baseline for implementation.

**How to apply:** when implementation work begins, default to these choices. If deviating (e.g., proposing GORM, or gRPC to the solver), justify the change against what was already agreed rather than silently switching.
