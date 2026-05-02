"""Unit tests for the OR-Tools scheduler.

Run with:  pytest test_scheduler.py -v
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
from typing import List

import pytest

from pydantic import ValidationError

from models import Client, Coach, ExistingSession, SolveRequest, TimeSlot
from scheduler import solve

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

WEEK_START = "2025-06-02"  # Monday

MON_9_17 = TimeSlot(day_of_week=0, start_time="09:00", end_time="17:00")
TUE_9_17 = TimeSlot(day_of_week=1, start_time="09:00", end_time="17:00")
WED_9_17 = TimeSlot(day_of_week=2, start_time="09:00", end_time="17:00")
THU_9_17 = TimeSlot(day_of_week=3, start_time="09:00", end_time="17:00")
FRI_9_17 = TimeSlot(day_of_week=4, start_time="09:00", end_time="17:00")

FULL_WEEK = [MON_9_17, TUE_9_17, WED_9_17, THU_9_17, FRI_9_17]

COACH_ID = "coach-1"


def make_coach(working_hours=None) -> Coach:
    return Coach(id=COACH_ID, working_hours=FULL_WEEK if working_hours is None else working_hours)


def make_client(
    cid: str,
    session_count: int = 1,
    priority: int = 0,
    preferred: list | None = None,
) -> Client:
    return Client(
        id=cid,
        session_count=session_count,
        priority_score=priority,
        preferred_windows=preferred or [],
    )


def rfc3339(dt: datetime) -> str:
    return dt.replace(tzinfo=timezone.utc).isoformat().replace("+00:00", "Z")


def slot(day_offset: int, hour: int) -> datetime:
    """Return a UTC datetime for the given day offset (0=Mon) and hour in the test week."""
    week = datetime(2025, 6, 2)  # Monday
    return week + timedelta(days=day_offset, hours=hour)


# ---------------------------------------------------------------------------
# Basic feasibility
# ---------------------------------------------------------------------------


def test_single_client_one_session():
    req = SolveRequest(
        week_start=WEEK_START,
        coach=make_coach(),
        clients=[make_client("c1", session_count=1)],
    )
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")
    assert len(resp.sessions) == 1
    assert resp.sessions[0].client_id == "c1"
    assert resp.unscheduled_clients == []


def test_multiple_clients_each_get_their_quota():
    clients = [make_client(f"c{i}", session_count=2) for i in range(3)]
    req = SolveRequest(week_start=WEEK_START, coach=make_coach(), clients=clients)
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")
    assert len(resp.sessions) == 6  # 3 clients × 2 sessions
    assert resp.unscheduled_clients == []
    # Each client appears exactly twice
    from collections import Counter
    counts = Counter(s.client_id for s in resp.sessions)
    for c in clients:
        assert counts[c.id] == 2


def test_no_clients_returns_optimal():
    req = SolveRequest(week_start=WEEK_START, coach=make_coach(), clients=[])
    resp = solve(req)
    assert resp.status == "optimal"
    assert resp.sessions == []


def test_no_working_hours_returns_infeasible():
    req = SolveRequest(
        week_start=WEEK_START,
        coach=make_coach(working_hours=[]),
        clients=[make_client("c1")],
    )
    resp = solve(req)
    assert resp.status == "infeasible"
    assert "c1" in resp.unscheduled_clients


def test_infeasible_too_many_sessions_for_available_slots():
    # Coach works only Monday 09:00–10:00 → 1 slot.
    # Two clients each want 1 session → impossible (coach can only hold 1 per slot).
    req = SolveRequest(
        week_start=WEEK_START,
        coach=make_coach(
            working_hours=[TimeSlot(day_of_week=0, start_time="09:00", end_time="10:00")]
        ),
        clients=[make_client("c1", 1), make_client("c2", 1)],
    )
    resp = solve(req)
    assert resp.status == "infeasible"


# ---------------------------------------------------------------------------
# Hard constraint: no coach double-booking
# ---------------------------------------------------------------------------


def test_no_double_booking():
    """Two clients must never share the same slot."""
    req = SolveRequest(
        week_start=WEEK_START,
        coach=make_coach(),
        clients=[make_client("c1", 3), make_client("c2", 3)],
    )
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")
    starts = [s.starts_at for s in resp.sessions]
    assert len(starts) == len(set(starts)), "Two sessions share the same start time"


# ---------------------------------------------------------------------------
# Hard constraint: one session per calendar day per client
# ---------------------------------------------------------------------------


def test_recovery_period_respected():
    """No two sessions for the same client on the same calendar day."""
    req = SolveRequest(
        week_start=WEEK_START,
        coach=make_coach(),
        clients=[make_client("c1", session_count=3)],
    )
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")

    client_sessions = [s for s in resp.sessions if s.client_id == "c1"]
    dates = [
        datetime.fromisoformat(s.starts_at.replace("Z", "+00:00")).date()
        for s in client_sessions
    ]
    assert len(dates) == len(set(dates)), "Two sessions share the same calendar day"


# ---------------------------------------------------------------------------
# Hard constraint: existing sessions block slots
# ---------------------------------------------------------------------------


def test_existing_sessions_are_blocked():
    """The solver must not place a new session in an already-booked slot."""
    # Block Monday 09:00.
    blocked_start = slot(0, 9)
    existing = [
        ExistingSession(
            client_id="c-existing",
            starts_at=rfc3339(blocked_start),
            ends_at=rfc3339(blocked_start + timedelta(hours=1)),
        )
    ]
    req = SolveRequest(
        week_start=WEEK_START,
        coach=make_coach(),
        clients=[make_client("c1", session_count=1)],
        existing_sessions=existing,
    )
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")
    blocked_str = rfc3339(blocked_start)
    for s in resp.sessions:
        assert s.starts_at != blocked_str, "Solver placed session in a blocked slot"


# ---------------------------------------------------------------------------
# Hard constraint: daily session limit (max 4 per day)
# ---------------------------------------------------------------------------


def test_daily_limit_not_exceeded():
    """Coach must not hold more than 4 sessions per day."""
    # 5 clients, each needing 1 session.  Give them all a Monday preference
    # so the solver wants to put everyone on Monday.
    mon_pref = [TimeSlot(day_of_week=0, start_time="09:00", end_time="17:00")]
    clients = [make_client(f"c{i}", session_count=1, priority=10, preferred=mon_pref) for i in range(5)]
    req = SolveRequest(week_start=WEEK_START, coach=make_coach(), clients=clients)
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")

    # Count sessions on Monday (2025-06-02)
    monday_prefix = "2025-06-02"
    monday_count = sum(1 for s in resp.sessions if s.starts_at.startswith(monday_prefix))
    assert monday_count <= 4, f"Daily limit exceeded: {monday_count} sessions on Monday"


# ---------------------------------------------------------------------------
# Soft constraint: preferred windows honoured when unconstrained
# ---------------------------------------------------------------------------


def test_preferred_windows_honoured():
    """When there's ample capacity, the solver should assign sessions in preferred windows."""
    # Client prefers Tuesday 10:00–12:00.
    tue_pref = [TimeSlot(day_of_week=1, start_time="10:00", end_time="12:00")]
    client = make_client("c1", session_count=1, priority=5, preferred=tue_pref)
    req = SolveRequest(week_start=WEEK_START, coach=make_coach(), clients=[client])
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")
    assert len(resp.sessions) == 1
    # Session should be in the preferred Tuesday 10:00–11:00 or 11:00–12:00 window.
    assert resp.sessions[0].starts_at.startswith("2025-06-03T10") or \
           resp.sessions[0].starts_at.startswith("2025-06-03T11"), (
        f"Session not in preferred window: {resp.sessions[0].starts_at}"
    )


# ---------------------------------------------------------------------------
# Soft constraint: priority ordering
# ---------------------------------------------------------------------------


def test_high_priority_client_gets_preferred_window_over_low_priority():
    """High-priority client should land in preferred window when low-priority client competes."""
    # Both clients prefer Monday 09:00.  Only one can get it.
    mon_900 = [TimeSlot(day_of_week=0, start_time="09:00", end_time="10:00")]
    high = make_client("high", session_count=1, priority=10, preferred=mon_900)
    low = make_client("low", session_count=1, priority=0, preferred=mon_900)

    req = SolveRequest(week_start=WEEK_START, coach=make_coach(), clients=[high, low])
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")

    high_session = next(s for s in resp.sessions if s.client_id == "high")
    assert high_session.starts_at == "2025-06-02T09:00:00Z", (
        "High-priority client did not get the contested preferred slot"
    )


# ---------------------------------------------------------------------------
# Output format
# ---------------------------------------------------------------------------


def test_output_timestamps_are_rfc3339_utc():
    """All returned timestamps must be valid RFC3339 with UTC 'Z' suffix."""
    req = SolveRequest(
        week_start=WEEK_START,
        coach=make_coach(),
        clients=[make_client("c1", 2)],
    )
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")
    for s in resp.sessions:
        assert s.starts_at.endswith("Z"), f"starts_at not UTC: {s.starts_at}"
        assert s.ends_at.endswith("Z"), f"ends_at not UTC: {s.ends_at}"
        # Must parse cleanly.
        datetime.fromisoformat(s.starts_at.replace("Z", "+00:00"))
        datetime.fromisoformat(s.ends_at.replace("Z", "+00:00"))


def test_ends_at_is_60_minutes_after_starts_at():
    req = SolveRequest(
        week_start=WEEK_START,
        coach=make_coach(),
        clients=[make_client("c1", 1)],
    )
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")
    for s in resp.sessions:
        t1 = datetime.fromisoformat(s.starts_at.replace("Z", "+00:00"))
        t2 = datetime.fromisoformat(s.ends_at.replace("Z", "+00:00"))
        assert t2 - t1 == timedelta(hours=1)


# ---------------------------------------------------------------------------
# Edge cases: idle-slot penalty behaviour
# ---------------------------------------------------------------------------


def test_back_to_back_same_day_preferred_over_split():
    """Two clients that both prefer Monday should be packed on the same day
    back-to-back, not split across two days.

    With raw span the cost would be: same day (10 day_active + 20 span) = 30
    vs split days (10 + 10) = 20 — the wrong winner.  The idle formulation
    fixes this: back-to-back idle = 0, so same day costs only 10, which beats
    the 20 of two separate days.
    """
    mon_pref = [TimeSlot(day_of_week=0, start_time="09:00", end_time="17:00")]
    req = SolveRequest(
        week_start=WEEK_START,
        coach=make_coach(),
        clients=[
            make_client("c1", session_count=1, priority=5, preferred=mon_pref),
            make_client("c2", session_count=1, priority=5, preferred=mon_pref),
        ],
    )
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")
    assert all(s.starts_at.startswith("2025-06-02") for s in resp.sessions), (
        "Both clients should be on Monday, not split across days"
    )


def test_recovery_gap_not_bypassed_by_idle_penalty():
    """The idle penalty must never tempt the solver to put two sessions for
    the same client on the same day in order to close an intra-day gap.

    Client 'multi' gets two sessions; the first lands on Monday morning and
    the same-day constraint forces the second to a different day.  Client
    'afternoon' fills Monday afternoon, creating an unavoidable gap on Monday
    that the solver must accept without violating the same-day rule.
    """
    mon_morning = [TimeSlot(day_of_week=0, start_time="09:00", end_time="12:00")]
    mon_afternoon = [TimeSlot(day_of_week=0, start_time="14:00", end_time="17:00")]
    req = SolveRequest(
        week_start=WEEK_START,
        coach=make_coach(),
        clients=[
            make_client("multi", session_count=2, priority=5, preferred=mon_morning),
            make_client("afternoon", session_count=1, priority=5, preferred=mon_afternoon),
        ],
    )
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")
    assert resp.unscheduled_clients == []

    multi_sessions = sorted(
        [s for s in resp.sessions if s.client_id == "multi"],
        key=lambda s: s.starts_at,
    )
    assert len(multi_sessions) == 2
    d1 = datetime.fromisoformat(multi_sessions[0].starts_at.replace("Z", "+00:00")).date()
    d2 = datetime.fromisoformat(multi_sessions[1].starts_at.replace("Z", "+00:00")).date()
    assert d1 != d2, f"Same-day constraint violated: both sessions on {d1}"


def test_four_back_to_back_sessions_stay_on_same_day():
    """Four clients all preferring Monday should pack into one contiguous
    block on that day.  The idle penalty is zero for contiguous sessions, so
    day-clustering keeps all four together rather than spreading them across
    four separate days.

    This would fail with raw span: same-day cost = 10 + 3×20 = 70 vs four
    separate days = 40.  With idle: same-day cost = 10 + 0 = 10, which wins.
    """
    mon_pref = [TimeSlot(day_of_week=0, start_time="09:00", end_time="17:00")]
    clients = [
        make_client(f"c{i}", session_count=1, priority=5, preferred=mon_pref)
        for i in range(4)
    ]
    req = SolveRequest(week_start=WEEK_START, coach=make_coach(), clients=clients)
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")

    monday_sessions = [s for s in resp.sessions if s.starts_at.startswith("2025-06-02")]
    assert len(monday_sessions) == 4, (
        f"Expected all 4 on Monday, got {len(monday_sessions)}"
    )
    starts = sorted(
        datetime.fromisoformat(s.starts_at.replace("Z", "+00:00"))
        for s in monday_sessions
    )
    for i in range(len(starts) - 1):
        assert starts[i + 1] - starts[i] == timedelta(hours=1), (
            f"Sessions not back-to-back: {starts[i + 1] - starts[i]} gap"
        )


def test_existing_mid_day_session_forces_gap_without_infeasible():
    """An existing confirmed session in the middle of the day blocks a slot
    that the solver cannot use.  Two new clients with morning and afternoon
    preferences on the same day must still be scheduled, accepting the
    unavoidable gap caused by the blocked middle slot.
    """
    mid_day = slot(0, 13)  # Monday 13:00 — blocks the centre of the day
    existing = [
        ExistingSession(
            client_id="existing",
            starts_at=rfc3339(mid_day),
            ends_at=rfc3339(mid_day + timedelta(hours=1)),
        )
    ]
    mon_morning = [TimeSlot(day_of_week=0, start_time="09:00", end_time="12:00")]
    mon_afternoon = [TimeSlot(day_of_week=0, start_time="14:00", end_time="17:00")]
    req = SolveRequest(
        week_start=WEEK_START,
        coach=make_coach(),
        clients=[
            make_client("morning_client", session_count=1, priority=5, preferred=mon_morning),
            make_client("afternoon_client", session_count=1, priority=5, preferred=mon_afternoon),
        ],
        existing_sessions=existing,
    )
    resp = solve(req)
    assert resp.status in ("optimal", "feasible")
    assert len(resp.sessions) == 2
    assert resp.unscheduled_clients == []
    for s in resp.sessions:
        assert s.starts_at != rfc3339(mid_day), "New session placed in blocked slot"


# ---------------------------------------------------------------------------
# Validation: coach constraints
# ---------------------------------------------------------------------------


def test_max_sessions_per_day_below_two_rejected():
    """Setting max_sessions_per_day to 1 must raise a validation error; the
    minimum allowed value is 2 since a single-session day defeats the purpose
    of scheduling a weekly block for the coach.
    """
    with pytest.raises(ValidationError):
        Coach(id="coach", working_hours=[MON_9_17], max_sessions_per_day=1)
