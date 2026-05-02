#!/usr/bin/env python3
"""Run a named hard-coded scenario through the OR-Tools solver (no HTTP).

Usage (from solver/):
  python run_fixture.py --list
  python run_fixture.py --scenario simple_three_clients
  python run_fixture.py --scenario ten_clients
  python run_fixture.py --scenario heavy_week --print-input

Each run prints coach availability (per-day slot list), a proposed timetable, then the solver JSON.
"""

from __future__ import annotations

import argparse
import sys
from collections import defaultdict
from datetime import datetime, timedelta

from fixtures import SCENARIOS
from models import ProposedSession, SolveRequest
from scheduler import coach_slot_starts, solve

# Calendar day names for availability text (day_of_week 0 = Monday).
_WEEKDAY_NAMES = (
    "Monday",
    "Tuesday",
    "Wednesday",
    "Thursday",
    "Friday",
    "Saturday",
    "Sunday",
)


def _json_dumps(model) -> str:
    # Pydantic v2
    return model.model_dump_json(indent=2)


def _format_personal_trainer_availability(request: SolveRequest) -> str:
    """Human-readable coach hours plus one line per 60-minute bookable slot, grouped by day."""
    lines: list[str] = []
    week_mon = datetime.strptime(request.week_start, "%Y-%m-%d")
    lines.append(f"Week (Monday): {_WEEKDAY_NAMES[0]} {week_mon.strftime('%d/%m/%Y')}")
    lines.append("(All times below are the same wall-clock values the API uses, labelled UTC in JSON.)")
    lines.append(f"Personal trainer id: {request.coach.id}")
    lines.append("")
    lines.append("Working hours (as stored on the coach):")
    for wh in request.coach.working_hours:
        dname = _WEEKDAY_NAMES[wh.day_of_week]
        lines.append(f"  {dname}: {wh.start_time} – {wh.end_time}")

    slots = coach_slot_starts(request.week_start, request.coach.working_hours)
    lines.append("")
    lines.append(f"Bookable 1-hour session windows this week ({len(slots)} slots):")
    by_date: defaultdict = defaultdict(list)
    for s in slots:
        by_date[s.date()].append(s)

    for day in sorted(by_date.keys()):
        wd = _WEEKDAY_NAMES[day.weekday()]
        lines.append("")
        lines.append(f"{wd} {day.strftime('%d/%m/%Y')}")
        for start in sorted(by_date[day]):
            end = start + timedelta(hours=1)
            lines.append(f"    {start.strftime('%H:%M')} – {end.strftime('%H:%M')}")

    return "\n".join(lines)


def _parse_session_utc(iso_z: str) -> datetime:
    """Parse RFC3339 …Z into naive UTC wall time for display."""
    return datetime.fromisoformat(iso_z.replace("Z", "+00:00")).replace(tzinfo=None)


def _format_proposed_timetable(sessions: list[ProposedSession]) -> str:
    """Group proposed sessions by calendar day with UK date and local time labels (UTC wall)."""
    if not sessions:
        return "  (no sessions proposed)"

    by_date: defaultdict = defaultdict(list)
    for sess in sessions:
        t0 = _parse_session_utc(sess.starts_at)
        by_date[t0.date()].append(sess)

    lines: list[str] = []
    for day in sorted(by_date.keys()):
        wd = _WEEKDAY_NAMES[day.weekday()]
        lines.append(f"{wd} {day.strftime('%d/%m/%Y')}")
        day_sessions = sorted(by_date[day], key=lambda s: _parse_session_utc(s.starts_at))
        for s in day_sessions:
            t0 = _parse_session_utc(s.starts_at)
            t1 = _parse_session_utc(s.ends_at)
            lines.append(f"    {t0.strftime('%H:%M')}–{t1.strftime('%H:%M')}  {s.client_id}")
        lines.append("")

    return "\n".join(lines).rstrip()


def main() -> int:
    parser = argparse.ArgumentParser(description="Run solver on a named fixtures scenario.")
    parser.add_argument(
        "--list",
        action="store_true",
        help="Print scenario names and exit.",
    )
    parser.add_argument(
        "--scenario",
        type=str,
        default=None,
        help="Key from fixtures.SCENARIOS (e.g. simple_three_clients).",
    )
    parser.add_argument(
        "--print-input",
        action="store_true",
        help="Print the SolveRequest JSON before the response.",
    )
    args = parser.parse_args()

    if args.list:
        for name in sorted(SCENARIOS.keys()):
            print(name)
        return 0

    if not args.scenario:
        parser.error("pass --scenario NAME or use --list")

    if args.scenario not in SCENARIOS:
        sys.stderr.write(f"Unknown scenario {args.scenario!r}. Known: {', '.join(sorted(SCENARIOS))}\n")
        return 2

    request = SCENARIOS[args.scenario]

    if args.print_input:
        print("--- request ---")
        print(_json_dumps(request))

    print("--- personal trainer availability ---")
    print(_format_personal_trainer_availability(request))

    response = solve(request)

    print("")
    print("--- proposed timetable (solver) ---")
    print(f"Status: {response.status}")
    if response.unscheduled_clients:
        print(f"Unscheduled clients: {', '.join(response.unscheduled_clients)}")
    print("")
    print(_format_proposed_timetable(response.sessions))

    print("--- solver response (JSON) ---")
    print(_json_dumps(response))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
