"""OR-Tools CP-SAT scheduling solver.

Given a coach's working hours, a set of clients with session quotas and preferred
time windows, and any existing confirmed sessions for the week, this module finds
an assignment of sessions to time slots that satisfies all hard constraints and
minimises the total soft-constraint penalty.

Hard constraints
----------------
1. Each client receives exactly ``session_count`` sessions.
2. The coach can hold at most one session per 60-minute slot (no double-booking).
3. Each client may not have more than one session per calendar day.
4. The coach holds at most 4 sessions per day.

Soft constraints (minimise penalty)
------------------------------------
1. Preferred windows: sessions outside a client's preferred windows incur a penalty
   scaled by the client's priority score — higher-priority clients are more strongly
   pushed into their preferred times.
2. Day clustering: sessions are grouped onto fewer distinct days to minimise context
   switching for the coach.
3. Daily idle slots: empty hours between the first and last session on each active
   day are penalised so that sessions cluster into a contiguous block.  Back-to-back
   sessions incur zero penalty; only genuine idle gaps are charged.
"""

from __future__ import annotations

import logging
from collections import defaultdict
from datetime import datetime, timedelta, timezone
from typing import Dict, List, Set, Tuple

from ortools.sat.python import cp_model

from models import Client, ProposedSession, SolveRequest, SolveResponse, TimeSlot

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

SESSION_MINUTES = 60
SOLVER_TIME_LIMIT_SECONDS = 25  # stay well under the 30 s HTTP timeout

# Soft-constraint weights (integer — CP-SAT requires integer coefficients).
# Penalty for placing a session outside a client's preferred window,
# multiplied by the client's (priority_score + 1) so high-priority clients
# are pushed into their preferred slots first.
PREF_PENALTY_BASE = 100

# Penalty per calendar day that the coach has any sessions scheduled.
# Lower than PREF_PENALTY_BASE so preferred windows take precedence.
DAY_ACTIVE_COST = 10

# Penalty per idle slot between sessions on the same day.
# "Idle slots" = span − sessions_on_day + 1, i.e. genuinely empty hours between
# the first and last session.  Back-to-back sessions score 0; a single empty hour
# between two sessions scores 1.  This formulation keeps day-clustering intact:
# packing two clients on the same day back-to-back costs DAY_ACTIVE_COST (10),
# which is cheaper than two separate days (20) — the correct preference ordering.
IDLE_PENALTY_PER_SLOT = 20


# ---------------------------------------------------------------------------
# Slot generation
# ---------------------------------------------------------------------------

def _parse_hhmm(s: str) -> Tuple[int, int]:
    parts = s.split(":")
    return int(parts[0]), int(parts[1])


def _generate_slots(week_start: datetime, working_hours: List[TimeSlot]) -> List[datetime]:
    """Return all 60-minute slot start times derived from the coach's working hours."""
    slots: Set[datetime] = set()
    for wh in working_hours:
        # day_of_week 0 = Monday; week_start is always a Monday.
        day = week_start + timedelta(days=wh.day_of_week)
        sh, sm = _parse_hhmm(wh.start_time)
        eh, em = _parse_hhmm(wh.end_time)

        slot_start = day.replace(hour=sh, minute=sm, second=0, microsecond=0)
        day_end = day.replace(hour=eh, minute=em, second=0, microsecond=0)

        while slot_start + timedelta(minutes=SESSION_MINUTES) <= day_end:
            slots.add(slot_start)
            slot_start += timedelta(minutes=SESSION_MINUTES)

    return sorted(slots)


def coach_slot_starts(week_start: str, working_hours: List[TimeSlot]) -> List[datetime]:
    """Return every 60-minute session slot start in the week (same logic as the solver).

    week_start must be YYYY-MM-DD (Monday). Datetimes are naive, matching internal solve().
    """
    week_start_dt = datetime.strptime(week_start, "%Y-%m-%d")
    return _generate_slots(week_start_dt, working_hours)


def _parse_rfc3339(s: str) -> datetime:
    """Parse an RFC3339 timestamp to a naive UTC datetime."""
    # Handle both 'Z' suffix and '+00:00' offset.
    s = s.replace("Z", "+00:00")
    dt = datetime.fromisoformat(s)
    # Strip tzinfo — solver works with naive UTC datetimes throughout.
    return dt.replace(tzinfo=None)


def _blocked_slots(slots: List[datetime], existing_sessions) -> Set[int]:
    """Return indices of slots that overlap with already-confirmed sessions."""
    blocked: Set[int] = set()
    for es in existing_sessions:
        es_start = _parse_rfc3339(es.starts_at)
        es_end = _parse_rfc3339(es.ends_at)
        for i, slot in enumerate(slots):
            slot_end = slot + timedelta(minutes=SESSION_MINUTES)
            # Two ranges overlap if start1 < end2 AND start2 < end1.
            if slot < es_end and es_start < slot_end:
                blocked.add(i)
    return blocked


def _preferred_slot_indices(
    week_start: datetime,
    preferred_windows: List[TimeSlot],
    slot_indices: List[int],
    slots: List[datetime],
) -> Set[int]:
    """Return the subset of slot_indices that fall inside any preferred window."""
    preferred: Set[int] = set()
    for pw in preferred_windows:
        day = week_start + timedelta(days=pw.day_of_week)
        sh, sm = _parse_hhmm(pw.start_time)
        eh, em = _parse_hhmm(pw.end_time)
        pw_start = day.replace(hour=sh, minute=sm, second=0, microsecond=0)
        pw_end = day.replace(hour=eh, minute=em, second=0, microsecond=0)
        for si in slot_indices:
            s = slots[si]
            if pw_start <= s < pw_end:
                preferred.add(si)
    return preferred


# ---------------------------------------------------------------------------
# Main solver
# ---------------------------------------------------------------------------

def solve(request: SolveRequest) -> SolveResponse:
    week_start = datetime.strptime(request.week_start, "%Y-%m-%d")

    # 1. Generate candidate slots.
    slots = _generate_slots(week_start, request.coach.working_hours)
    if not slots:
        logger.warning("No slots generated — coach has no working hours configured")
        return SolveResponse(
            status="infeasible",
            sessions=[],
            unscheduled_clients=[c.id for c in request.clients],
        )

    n_slots = len(slots)

    # 2. Identify blocked and available slot indices.
    blocked = _blocked_slots(slots, request.existing_sessions)
    available: List[int] = [i for i in range(n_slots) if i not in blocked]

    if not request.clients:
        return SolveResponse(status="optimal", sessions=[], unscheduled_clients=[])

    # 3. Group available slot indices by calendar day for quick lookup.
    slots_by_day: Dict[int, List[int]] = defaultdict(list)
    for si in available:
        slots_by_day[slots[si].toordinal()].append(si)

    # 4. Build CP-SAT model.
    model = cp_model.CpModel()
    n_clients = len(request.clients)

    # Decision variables: x[ci, si] = 1 if client ci is assigned to slot si.
    x: Dict[Tuple[int, int], cp_model.IntVar] = {}
    for ci in range(n_clients):
        for si in available:
            x[ci, si] = model.new_bool_var(f"x_c{ci}_s{si}")

    # ------------------------------------------------------------------
    # Hard constraint 1: each client receives exactly session_count sessions.
    # ------------------------------------------------------------------
    for ci, client in enumerate(request.clients):
        model.add(
            sum(x[ci, si] for si in available) == client.session_count
        )

    # ------------------------------------------------------------------
    # Hard constraint 2: coach holds at most one session per slot.
    # ------------------------------------------------------------------
    for si in available:
        model.add(sum(x[ci, si] for ci in range(n_clients)) <= 1)

    # ------------------------------------------------------------------
    # Hard constraint 3: a client may not have two sessions on the same
    #   calendar day.  Sessions on different days are always allowed
    #   regardless of clock proximity (e.g. Mon 19:00 → Tue 08:00 is fine).
    # ------------------------------------------------------------------
    for ci in range(n_clients):
        for a, si in enumerate(available):
            for sj in available[a + 1:]:
                if slots[si].date() == slots[sj].date():
                    model.add(x[ci, si] + x[ci, sj] <= 1)

    # ------------------------------------------------------------------
    # Hard constraint 4: at most coach.max_sessions_per_day per day.
    # ------------------------------------------------------------------
    daily_cap = request.coach.max_sessions_per_day
    for day_slots in slots_by_day.values():
        model.add(
            sum(x[ci, si] for ci in range(n_clients) for si in day_slots)
            <= daily_cap
        )

    # ------------------------------------------------------------------
    # Soft constraint 1: preferred windows.
    #   Penalty = PREF_PENALTY_BASE * (priority_score + 1)  for each
    #   session placed outside the client's preferred windows.
    #   Higher-priority clients incur a larger penalty → solver pushes them
    #   into preferred slots before lower-priority clients.
    # ------------------------------------------------------------------
    preferred_sets: List[Set[int]] = []
    for ci, client in enumerate(request.clients):
        pref = _preferred_slot_indices(week_start, client.preferred_windows, available, slots)
        preferred_sets.append(pref)

    penalty_terms = []
    for ci, client in enumerate(request.clients):
        coeff = PREF_PENALTY_BASE * (client.priority_score + 1)
        for si in available:
            if si not in preferred_sets[ci]:
                penalty_terms.append(coeff * x[ci, si])

    # ------------------------------------------------------------------
    # Soft constraint 2: day clustering.
    #   Introduce a boolean per day that is 1 when the coach has any session
    #   scheduled. Minimising the count of active days clusters sessions.
    # ------------------------------------------------------------------
    day_active: Dict[int, cp_model.IntVar] = {}
    for day_ord, day_slots in slots_by_day.items():
        da = model.new_bool_var(f"day_active_{day_ord}")
        day_active[day_ord] = da
        # day_active → at least one session on this day (implied by penalty)
        # NOT day_active → no sessions on this day (hard implication)
        all_x_on_day = [x[ci, si] for ci in range(n_clients) for si in day_slots]
        model.add(sum(all_x_on_day) == 0).only_enforce_if(da.Not())
        model.add(sum(all_x_on_day) >= 1).only_enforce_if(da)

    penalty_terms += [DAY_ACTIVE_COST * v for v in day_active.values()]

    # ------------------------------------------------------------------
    # Soft constraint 3: daily idle slots (compactness).
    #   any_used[si] = 1 when at least one client is assigned to slot si.
    #   span_d  = last occupied slot index − first occupied slot index.
    #   idle_d  = span_d − sessions_on_day + 1  (empty hours between sessions).
    #   Two back-to-back sessions: span=1, sessions=2 → idle=0 (no penalty).
    #   09:00 + 15:00 with nothing between: span=6, sessions=2 → idle=5.
    # ------------------------------------------------------------------
    any_used: Dict[int, cp_model.IntVar] = {}
    for si in available:
        au = model.new_bool_var(f"any_used_{si}")
        all_at_si = [x[ci, si] for ci in range(n_clients)]
        model.add_bool_or(all_at_si).only_enforce_if(au)
        model.add(sum(all_at_si) == 0).only_enforce_if(au.Not())
        any_used[si] = au

    for day_ord, day_slots in slots_by_day.items():
        if len(day_slots) < 2:
            continue
        n_day = len(day_slots)
        span_d = model.new_int_var(0, n_day - 1, f"span_{day_ord}")
        for i, si in enumerate(day_slots):
            for j, sj in enumerate(day_slots):
                if j <= i:
                    continue
                model.add(span_d >= (j - i)).only_enforce_if(
                    [any_used[si], any_used[sj]]
                )
        sessions_on_day = sum(
            x[ci, si] for ci in range(n_clients) for si in day_slots
        )
        idle_d = model.new_int_var(0, n_day - 1, f"idle_{day_ord}")
        model.add(idle_d == span_d - sessions_on_day + 1).only_enforce_if(
            day_active[day_ord]
        )
        model.add(idle_d == 0).only_enforce_if(day_active[day_ord].Not())
        penalty_terms.append(IDLE_PENALTY_PER_SLOT * idle_d)

    # Minimise total penalty.
    if penalty_terms:
        model.minimize(sum(penalty_terms))

    # 5. Solve.
    solver = cp_model.CpSolver()
    solver.parameters.max_time_in_seconds = SOLVER_TIME_LIMIT_SECONDS
    solver.parameters.log_search_progress = False

    status_code = solver.solve(model)

    feasible = status_code in (cp_model.OPTIMAL, cp_model.FEASIBLE)
    if not feasible:
        logger.info(
            "Solver returned infeasible for week %s (%d clients, %d slots)",
            request.week_start,
            n_clients,
            len(available),
        )
        return SolveResponse(
            status="infeasible",
            sessions=[],
            unscheduled_clients=[c.id for c in request.clients],
        )

    status_str = "optimal" if status_code == cp_model.OPTIMAL else "feasible"

    # 6. Extract solution.
    proposed: List[ProposedSession] = []
    scheduled_client_ids: Set[str] = set()

    for ci, client in enumerate(request.clients):
        for si in available:
            if solver.value(x[ci, si]) == 1:
                start_dt = slots[si].replace(tzinfo=timezone.utc)
                end_dt = start_dt + timedelta(minutes=SESSION_MINUTES)
                proposed.append(
                    ProposedSession(
                        client_id=client.id,
                        starts_at=start_dt.isoformat().replace("+00:00", "Z"),
                        ends_at=end_dt.isoformat().replace("+00:00", "Z"),
                    )
                )
                scheduled_client_ids.add(client.id)

    unscheduled = [c.id for c in request.clients if c.id not in scheduled_client_ids]

    logger.info(
        "Solved week %s: status=%s sessions=%d unscheduled=%d",
        request.week_start,
        status_str,
        len(proposed),
        len(unscheduled),
    )

    return SolveResponse(
        status=status_str,
        sessions=proposed,
        unscheduled_clients=unscheduled,
    )
