"""Hard-coded SolveRequest scenarios for local OR-Tools testing (no agent / DB).

All scenarios use the same canonical week (Monday 02/06/2025) so RFC3339
existing_sessions line up with generated slots. day_of_week: 0 = Monday … 6 = Sunday.
"""

from __future__ import annotations

from datetime import datetime, timedelta, timezone

from models import Client, Coach, ExistingSession, SolveRequest, TimeSlot

# ---------------------------------------------------------------------------
# Canonical week (must stay a Monday)
# ---------------------------------------------------------------------------

WEEK_START = "2025-06-02"

MON_9_17 = TimeSlot(day_of_week=0, start_time="09:00", end_time="17:00")
TUE_9_17 = TimeSlot(day_of_week=1, start_time="09:00", end_time="17:00")
WED_9_17 = TimeSlot(day_of_week=2, start_time="09:00", end_time="17:00")
THU_9_17 = TimeSlot(day_of_week=3, start_time="09:00", end_time="17:00")
FRI_9_17 = TimeSlot(day_of_week=4, start_time="09:00", end_time="17:00")

FULL_WEEK = [MON_9_17, TUE_9_17, WED_9_17, THU_9_17, FRI_9_17]

COACH_ID = "coach-fixture-1"


def default_coach(working_hours: list[TimeSlot] | None = None) -> Coach:
    """Standard Mon–Fri 09:00–17:00 coach unless working_hours is overridden."""
    return Coach(id=COACH_ID, working_hours=FULL_WEEK if working_hours is None else working_hours)


def client(
    cid: str,
    session_count: int = 1,
    priority_score: int = 0,
    preferred_windows: list[TimeSlot] | None = None,
) -> Client:
    """Build a client row matching the solver API."""
    return Client(
        id=cid,
        session_count=session_count,
        priority_score=priority_score,
        preferred_windows=preferred_windows or [],
    )


def rfc3339_utc(dt: datetime) -> str:
    """Format datetime as RFC3339 UTC with Z suffix (matches solver output)."""
    return dt.replace(tzinfo=timezone.utc).isoformat().replace("+00:00", "Z")


def slot_start(day_offset: int, hour: int) -> datetime:
    """Naive UTC datetime for day_offset (0=Mon of WEEK_START) at given hour."""
    week = datetime.strptime(WEEK_START, "%Y-%m-%d")
    return week + timedelta(days=day_offset, hours=hour)


def existing_session(client_id: str, day_offset: int, hour: int) -> ExistingSession:
    """One-hour session blocking the slot starting at day_offset/hour."""
    start = slot_start(day_offset, hour)
    end = start + timedelta(hours=1)
    return ExistingSession(
        client_id=client_id,
        starts_at=rfc3339_utc(start),
        ends_at=rfc3339_utc(end),
    )


# ---------------------------------------------------------------------------
# Scenario builders
# ---------------------------------------------------------------------------


def _simple_three_clients() -> SolveRequest:
    # Mixed preferences: Mon morning, Tue afternoon, no soft window.
    mon_morning = [TimeSlot(day_of_week=0, start_time="09:00", end_time="12:00")]
    tue_pm = [TimeSlot(day_of_week=1, start_time="14:00", end_time="17:00")]
    return SolveRequest(
        week_start=WEEK_START,
        coach=default_coach(),
        clients=[
            client("alice", session_count=1, priority_score=2, preferred_windows=mon_morning),
            client("bob", session_count=1, priority_score=1, preferred_windows=tue_pm),
            client("charlie", session_count=1, priority_score=0, preferred_windows=[]),
        ],
    )


def _heavy_week() -> SolveRequest:
    # Five clients × two sessions: forces spread across days, same-day constraint, and daily cap.
    prefs = [
        [TimeSlot(day_of_week=i, start_time="09:00", end_time="12:00")]
        for i in range(5)
    ]
    ids = ["dana", "erin", "frank", "gina", "hari"]
    return SolveRequest(
        week_start=WEEK_START,
        coach=default_coach(),
        clients=[
            client(ids[i], session_count=2, priority_score=i, preferred_windows=prefs[i])
            for i in range(5)
        ],
    )


def _competing_preferences() -> SolveRequest:
    # Single Mon 09:00 slot preferred by both; high priority should win that soft fight.
    mon_900 = [TimeSlot(day_of_week=0, start_time="09:00", end_time="10:00")]
    return SolveRequest(
        week_start=WEEK_START,
        coach=default_coach(),
        clients=[
            client("high_priority", session_count=1, priority_score=10, preferred_windows=mon_900),
            client("low_priority", session_count=1, priority_score=0, preferred_windows=mon_900),
        ],
    )


def _partially_booked() -> SolveRequest:
    # Existing booking blocks Monday 09:00; new client must land elsewhere.
    return SolveRequest(
        week_start=WEEK_START,
        coach=default_coach(),
        clients=[client("new_client", session_count=1, priority_score=0)],
        existing_sessions=[
            existing_session("existing_client", day_offset=0, hour=9),
        ],
    )


def _stress_infeasible() -> SolveRequest:
    # Only one hour on Monday: two clients each need one session → infeasible.
    narrow = [TimeSlot(day_of_week=0, start_time="09:00", end_time="10:00")]
    return SolveRequest(
        week_start=WEEK_START,
        coach=default_coach(working_hours=narrow),
        clients=[
            client("client_a", session_count=1),
            client("client_b", session_count=1),
        ],
    )


def _recovery_tight() -> SolveRequest:
    # One client needs three sessions; exercises same-day constraint (one session per day).
    return SolveRequest(
        week_start=WEEK_START,
        coach=default_coach(),
        clients=[client("triple", session_count=3, priority_score=5)],
    )


def _ten_clients() -> SolveRequest:
    # Realistic ten-client week with messy, multi-day availability — the kind
    # of input the WhatsApp parser produces.
    #
    # Each client has 2-4 preferred windows across different days and halves of
    # the day (morning / late-afternoon).  Some clients have narrow hard-coded
    # slots ("Thursday 4pm only"); others are wide open.  One client needs two
    # sessions (recovery constraint fires); one has no preferences at all.
    #
    # Coach works Mon–Fri 09:00–17:00 plus Saturday morning (09:00–13:00).
    #
    # The span penalty is exercised when multiple clients' cheapest placements
    # land on the same day in opposite halves.  Because most clients have
    # preferences on several days, the solver can often avoid the gap by
    # choosing a different preferred day rather than creating a split day.
    sat_morning = TimeSlot(day_of_week=5, start_time="09:00", end_time="13:00")
    extended_coach = default_coach(working_hours=FULL_WEEK + [sat_morning])

    return SolveRequest(
        week_start=WEEK_START,
        coach=extended_coach,
        clients=[
            # Very high priority, narrow: only Thursday 16:00.
            client("pt_client_01", session_count=1, priority_score=9,
                   preferred_windows=[
                       TimeSlot(day_of_week=3, start_time="16:00", end_time="17:00"),
                   ]),
            # High priority, mornings on Tue / Wed / Fri.
            client("pt_client_02", session_count=1, priority_score=8,
                   preferred_windows=[
                       TimeSlot(day_of_week=1, start_time="09:00", end_time="12:00"),
                       TimeSlot(day_of_week=2, start_time="09:00", end_time="12:00"),
                       TimeSlot(day_of_week=4, start_time="09:00", end_time="12:00"),
                   ]),
            # High priority, needs 2 sessions; prefers Tue and Thu mornings.
            client("pt_client_03", session_count=2, priority_score=7,
                   preferred_windows=[
                       TimeSlot(day_of_week=1, start_time="09:00", end_time="12:00"),
                       TimeSlot(day_of_week=3, start_time="09:00", end_time="12:00"),
                   ]),
            # Medium priority, late-afternoon on Mon / Wed / Fri.
            client("pt_client_04", session_count=1, priority_score=6,
                   preferred_windows=[
                       TimeSlot(day_of_week=0, start_time="15:00", end_time="17:00"),
                       TimeSlot(day_of_week=2, start_time="15:00", end_time="17:00"),
                       TimeSlot(day_of_week=4, start_time="15:00", end_time="17:00"),
                   ]),
            # Medium priority, morning-only on Mon / Tue / Thu / Fri.
            client("pt_client_05", session_count=1, priority_score=5,
                   preferred_windows=[
                       TimeSlot(day_of_week=0, start_time="09:00", end_time="12:00"),
                       TimeSlot(day_of_week=1, start_time="09:00", end_time="12:00"),
                       TimeSlot(day_of_week=3, start_time="09:00", end_time="12:00"),
                       TimeSlot(day_of_week=4, start_time="09:00", end_time="12:00"),
                   ]),
            # Medium priority, afternoon on Tue or Thu.
            client("pt_client_06", session_count=1, priority_score=5,
                   preferred_windows=[
                       TimeSlot(day_of_week=1, start_time="14:00", end_time="17:00"),
                       TimeSlot(day_of_week=3, start_time="14:00", end_time="17:00"),
                   ]),
            # Medium priority, Wednesday any time or Friday afternoon.
            client("pt_client_07", session_count=1, priority_score=4,
                   preferred_windows=[
                       TimeSlot(day_of_week=2, start_time="09:00", end_time="17:00"),
                       TimeSlot(day_of_week=4, start_time="14:00", end_time="17:00"),
                   ]),
            # Low priority, Monday morning or Thursday afternoon.
            client("pt_client_08", session_count=1, priority_score=3,
                   preferred_windows=[
                       TimeSlot(day_of_week=0, start_time="09:00", end_time="12:00"),
                       TimeSlot(day_of_week=3, start_time="14:00", end_time="17:00"),
                   ]),
            # Low priority, Friday afternoon or Saturday morning.
            client("pt_client_09", session_count=1, priority_score=2,
                   preferred_windows=[
                       TimeSlot(day_of_week=4, start_time="14:00", end_time="17:00"),
                       TimeSlot(day_of_week=5, start_time="09:00", end_time="13:00"),
                   ]),
            # Very low priority, no preferences — goes wherever the coach needs.
            client("pt_client_10", session_count=1, priority_score=0,
                   preferred_windows=[]),
        ],
    )


# ---------------------------------------------------------------------------
# Public registry (name -> request)
# ---------------------------------------------------------------------------

SCENARIOS: dict[str, SolveRequest] = {
    "simple_three_clients": _simple_three_clients(),
    "heavy_week": _heavy_week(),
    "competing_preferences": _competing_preferences(),
    "partially_booked": _partially_booked(),
    "stress_infeasible": _stress_infeasible(),
    "recovery_tight": _recovery_tight(),
    "ten_clients": _ten_clients(),
}
