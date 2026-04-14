"""
OR-Tools CP-SAT scheduling solver.

Model design
============
Time is discretised into 30-minute slots across the week.
Week = 7 days × 48 slots/day = 336 slots total.
Sessions are exactly 2 slots (60 minutes).

Hard constraints
----------------
1. Each client receives exactly `session_count` sessions.
2. Each session starts within the coach's working hours.
3. No two sessions overlap for the same coach (at most one session per slot).
4. No two sessions overlap for the same client.
5. Minimum 24h (48 slots) between any two sessions for the same client.
6. Maximum 4 sessions per day for the coach (5 on exception — not implemented yet).

Soft constraints (maximised in objective)
-----------------------------------------
A. Sessions that fall within a client's preferred windows earn a preference bonus.
B. Higher-priority clients' preference bonus is weighted by their priority_score.
"""

from __future__ import annotations

import dataclasses
from datetime import datetime, date, timedelta, timezone
from typing import NamedTuple

from ortools.sat.python import cp_model


SLOTS_PER_HOUR = 2          # 30-min resolution
SLOTS_PER_DAY = 24 * SLOTS_PER_HOUR   # 48
SESSION_SLOTS = 2           # 60 min = 2 × 30-min slots
RECOVERY_SLOTS = 48         # 24 h
MAX_SESSIONS_PER_DAY = 4


def _time_to_slot(hhmm: str) -> int:
    """Convert 'HH:MM' to a slot index within a single day (0-47)."""
    h, m = map(int, hhmm.split(":"))
    return h * SLOTS_PER_HOUR + m // 30


def _slot_to_time(slot: int) -> str:
    """Convert a day-relative slot index back to 'HH:MM'."""
    h = slot // SLOTS_PER_HOUR
    m = (slot % SLOTS_PER_HOUR) * 30
    return f"{h:02d}:{m:02d}"


def _week_slot(day: int, day_slot: int) -> int:
    """Absolute slot index within the week."""
    return day * SLOTS_PER_DAY + day_slot


def _datetime_to_week_slot(dt: datetime, week_start: date) -> int:
    """Convert a UTC datetime to a week-relative slot index."""
    delta = dt.date() - week_start
    day = delta.days
    day_slot = dt.hour * SLOTS_PER_HOUR + dt.minute // 30
    return _week_slot(day, day_slot)


@dataclasses.dataclass
class SolveInput:
    week_start: str                   # "YYYY-MM-DD"
    coach_id: str
    working_hours: list               # [(day_of_week, start_time, end_time), ...]
    clients: list                     # [{"id", "session_count", "priority_score", "preferred_windows"}, ...]
    existing_sessions: list = dataclasses.field(default_factory=list)
                                      # [(client_id, starts_at_rfc3339, ends_at_rfc3339), ...]


@dataclasses.dataclass
class SolveOutput:
    status: str                       # "optimal" | "feasible" | "infeasible"
    sessions: list = dataclasses.field(default_factory=list)
                                      # [{"client_id", "starts_at", "ends_at"}, ...]
    unscheduled_clients: list = dataclasses.field(default_factory=list)


def solve_schedule(inp: SolveInput) -> SolveOutput:
    week_start_date = date.fromisoformat(inp.week_start)
    model = cp_model.CpModel()

    # ------------------------------------------------------------------ #
    # Build the set of valid start slots for each working-hours entry.    #
    # A session is valid in a slot if [slot, slot+SESSION_SLOTS) is fully #
    # contained within the working hours window.                          #
    # ------------------------------------------------------------------ #
    valid_coach_slots: set[int] = set()
    for (dow, st, et) in inp.working_hours:
        start_day_slot = _time_to_slot(st)
        end_day_slot = _time_to_slot(et)
        for s in range(start_day_slot, end_day_slot - SESSION_SLOTS + 1):
            valid_coach_slots.add(_week_slot(dow, s))

    if not valid_coach_slots:
        return SolveOutput(status="infeasible", unscheduled_clients=[c["id"] for c in inp.clients])

    # Block slots already occupied by confirmed sessions.
    blocked_coach_slots: set[int] = set()
    client_blocked_slots: dict[str, set[int]] = {}

    for (cid, starts_at, ends_at) in inp.existing_sessions:
        dt = datetime.fromisoformat(starts_at.replace("Z", "+00:00"))
        abs_slot = _datetime_to_week_slot(dt, week_start_date)
        for offset in range(SESSION_SLOTS):
            blocked_coach_slots.add(abs_slot + offset)
        if cid not in client_blocked_slots:
            client_blocked_slots[cid] = set()
        # Block 48 slots before and after for recovery period
        for offset in range(-RECOVERY_SLOTS, SESSION_SLOTS + RECOVERY_SLOTS):
            client_blocked_slots[cid].add(abs_slot + offset)

    valid_coach_slots -= blocked_coach_slots

    # ------------------------------------------------------------------ #
    # Decision variables                                                   #
    # x[client_idx][slot] = 1 if client has a session starting at `slot`  #
    # ------------------------------------------------------------------ #
    x = {}
    for ci, client in enumerate(inp.clients):
        for slot in valid_coach_slots:
            # Skip slots blocked by client's own recovery period
            cblocked = client_blocked_slots.get(client["id"], set())
            if slot in cblocked:
                continue
            x[(ci, slot)] = model.new_bool_var(f"x_{ci}_{slot}")

    # ------------------------------------------------------------------ #
    # Hard constraint 1: each client gets exactly session_count sessions  #
    # ------------------------------------------------------------------ #
    for ci, client in enumerate(inp.clients):
        client_vars = [v for (c, _), v in x.items() if c == ci]
        model.add(sum(client_vars) == client["session_count"])

    # ------------------------------------------------------------------ #
    # Hard constraint 3: coach has at most 1 session per time slot        #
    # (no two sessions overlap — covers the whole SESSION_SLOTS window)   #
    # ------------------------------------------------------------------ #
    all_slots = sorted(valid_coach_slots)
    for slot in all_slots:
        # All sessions that would occupy this slot
        occupying = []
        for offset in range(SESSION_SLOTS):
            for ci in range(len(inp.clients)):
                key = (ci, slot - offset)
                if key in x:
                    occupying.append(x[key])
        if len(occupying) > 1:
            model.add(sum(occupying) <= 1)

    # ------------------------------------------------------------------ #
    # Hard constraint 4: client cannot have two sessions on the same slot #
    # ------------------------------------------------------------------ #
    for ci, client in enumerate(inp.clients):
        for slot in all_slots:
            occupying = []
            for offset in range(SESSION_SLOTS):
                key = (ci, slot - offset)
                if key in x:
                    occupying.append(x[key])
            if len(occupying) > 1:
                model.add(sum(occupying) <= 1)

    # ------------------------------------------------------------------ #
    # Hard constraint 5: 24h (48 slots) recovery between sessions for     #
    # the same client.                                                     #
    # ------------------------------------------------------------------ #
    for ci in range(len(inp.clients)):
        client_slots = sorted(s for (c, s) in x if c == ci)
        for i, s1 in enumerate(client_slots):
            for s2 in client_slots[i + 1:]:
                if s2 - s1 < SESSION_SLOTS + RECOVERY_SLOTS:
                    # These two sessions are too close — cannot both be chosen
                    model.add_bool_or([x[(ci, s1)].negated(), x[(ci, s2)].negated()])

    # ------------------------------------------------------------------ #
    # Hard constraint 6: max MAX_SESSIONS_PER_DAY per day for the coach   #
    # ------------------------------------------------------------------ #
    for day in range(7):
        day_vars = [v for (ci, slot), v in x.items()
                    if slot // SLOTS_PER_DAY == day]
        if day_vars:
            model.add(sum(day_vars) <= MAX_SESSIONS_PER_DAY)

    # ------------------------------------------------------------------ #
    # Soft constraints: maximise preference score                          #
    # ------------------------------------------------------------------ #
    preference_terms = []
    for ci, client in enumerate(inp.clients):
        pref_slots: set[int] = set()
        for (dow, st, et) in client["preferred_windows"]:
            s_slot = _time_to_slot(st)
            e_slot = _time_to_slot(et)
            for s in range(s_slot, e_slot - SESSION_SLOTS + 1):
                pref_slots.add(_week_slot(dow, s))

        weight = max(1, client["priority_score"])
        for (c, slot), var in x.items():
            if c == ci and slot in pref_slots:
                preference_terms.append(weight * var)

    if preference_terms:
        model.maximize(sum(preference_terms))

    # ------------------------------------------------------------------ #
    # Solve                                                                #
    # ------------------------------------------------------------------ #
    solver = cp_model.CpSolver()
    solver.parameters.max_time_in_seconds = 25.0  # leave 5s headroom for HTTP
    solver.parameters.num_search_workers = 4
    status = solver.solve(model)

    if status not in (cp_model.OPTIMAL, cp_model.FEASIBLE):
        return SolveOutput(
            status="infeasible",
            unscheduled_clients=[c["id"] for c in inp.clients],
        )

    # ------------------------------------------------------------------ #
    # Extract solution                                                     #
    # ------------------------------------------------------------------ #
    sessions = []
    scheduled_clients: set[int] = set()

    week_start_dt = datetime.combine(week_start_date, datetime.min.time(), tzinfo=timezone.utc)

    for (ci, slot), var in x.items():
        if solver.value(var) == 1:
            scheduled_clients.add(ci)
            starts_dt = week_start_dt + timedelta(minutes=slot * 30)
            ends_dt = starts_dt + timedelta(minutes=60)
            sessions.append({
                "client_id": inp.clients[ci]["id"],
                "starts_at": starts_dt.strftime("%Y-%m-%dT%H:%M:%SZ"),
                "ends_at":   ends_dt.strftime("%Y-%m-%dT%H:%M:%SZ"),
            })

    unscheduled = [
        inp.clients[ci]["id"]
        for ci in range(len(inp.clients))
        if ci not in scheduled_clients
    ]

    return SolveOutput(
        status="optimal" if status == cp_model.OPTIMAL else "feasible",
        sessions=sorted(sessions, key=lambda s: s["starts_at"]),
        unscheduled_clients=unscheduled,
    )
