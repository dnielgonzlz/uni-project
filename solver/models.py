"""Pydantic models that mirror the Go SolverRequest / SolverResponse structs."""

from __future__ import annotations

from typing import List, Optional
from pydantic import BaseModel, field_validator


class TimeSlot(BaseModel):
    """A recurring time window within a day, e.g. 09:00–17:00 on Monday.

    day_of_week: 0 = Monday … 6 = Sunday  (ISO 8601 / Go convention)
    start_time / end_time: "HH:MM" in the coach's local timezone (UTC for UK winter,
    UTC+1 for BST summer — the Go service stores them as entered by the coach).
    """

    day_of_week: int
    start_time: str  # "HH:MM"
    end_time: str    # "HH:MM"


class Coach(BaseModel):
    id: str
    working_hours: List[TimeSlot]
    max_sessions_per_day: int = 4

    @field_validator("max_sessions_per_day")
    @classmethod
    def at_least_two(cls, v: int) -> int:
        if v < 2:
            raise ValueError("max_sessions_per_day must be at least 2")
        return v


class Client(BaseModel):
    id: str
    session_count: int       # sessions required this week
    priority_score: int      # higher = more important
    preferred_windows: List[TimeSlot]


class ExistingSession(BaseModel):
    client_id: str
    starts_at: str  # RFC3339
    ends_at: str    # RFC3339


class SolveRequest(BaseModel):
    week_start: str           # "YYYY-MM-DD" — always a Monday
    coach: Coach
    clients: List[Client]
    existing_sessions: List[ExistingSession] = []


# ---------------------------------------------------------------------------


class ProposedSession(BaseModel):
    client_id: str
    starts_at: str  # RFC3339 UTC
    ends_at: str    # RFC3339 UTC


class SolveResponse(BaseModel):
    status: str                         # "optimal" | "feasible" | "infeasible"
    sessions: List[ProposedSession]
    unscheduled_clients: List[str]
