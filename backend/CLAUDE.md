# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the backend service for a university project. The repository is organized as a monorepo with two top-level directories:

- `backend/` — this directory (server-side code)
- `frontend/` — client-side code

## Status

This project is newly initialized and contains no source files yet. Update this file as the stack, commands, and architecture are established.

Development Guide for Backend

This is a guide for the coding and development of the product of an Intelligent Scheduling System for Personal Trainers.

We are going to develop an intelligent way of allowing clients to book their sessions with their personal trainers. We need to develop the backend for this purpose. This backend needs to handle:

# Tech Stack
- Golang
- Resend API for emails
- PostgreSQL
- Google OR-Tools
- API should be a RESTful API
- Testing should use Go testing package
- Documentation in Swagger
- Version Control in Git/Gitbhub, using Github Actions for CI/CD pipelines running the tests on each commit
- GoCardless for Direct Debits
- Stripe for Card Charging
- WhatsApp API access through Twilio

1. Auth (Login, Logout, Roles depending if its a coach or a client)
2. Input validation and sanitation
3. CORS
4. Rate limiting to protect the API from abuse or accidental overuse
5. Password reset expiration email
6. Add a comment in parts where we’ll need to add front end error handling
7. Create the most commonly needed database indexes to accelerate the speed
8. Logging to know where the problems are
9. Set up alerts so you know when error rates spike, latency jumps, or critical flows start failing
10. Rollback plan


Hard Constraints:
- No Double Booking: a trainer cannot have overlapping sessions. A client cannot have overlapping sessions with different trainers.
- Working hours: sessions must fall within trainer’s defined working hours. No sessions outside declared availability.
- Recovery Period: 24 hours should be the minimum resting period in between sessions. It should always have a 24 hours time difference.
- Session Count: each client must receive exactly a number of sessions per month that need to be distributed each week. For example, a client on one session a week means 4 sessions a month, so 1 each week.
- Maximum of 4 sessions per day, no more than 5 on exceptions.
- Require a last step personal trainer confirmation before confirming all the sessions.

Soft Constraints:
- Preferred Time Windows: schedule sessions during client’s preferred times when possible.
- Clustering: group by preferred times clients on the same day. 
- Priority Clients: the more sessions and the longer the time the client has been with the coach, the more priority they’ll have.
- Ask the personal trainer what days will the users have availability (for example, a trainer could only be available Monday to Thursday)
- Ask the personal trainer preferred times for coaching


Comparative Analysis:

This project should be an MVP version of softwares like Cal.com, Calendly and Gymcatch. You can look for their features online to understand the scope of this project better. This should also be localised for users in the UK

Extra Feature:

Using the Twilio App, we should receive the information of the availability of the clients, and then based on that, cluster the clients across the week in the actual scheduling software.
