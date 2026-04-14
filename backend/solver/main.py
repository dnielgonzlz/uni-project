"""
PT Scheduler — OR-Tools solver microservice.

Run locally:
    cd solver
    python -m venv venv && source venv/bin/activate
    pip install -r requirements.txt
    uvicorn main:app --port 8000 --reload

Deploy to AWS Lambda: package this file + solver.py into a zip with an
OR-Tools Lambda Layer (see scripts/build_lambda_layer.sh).
"""

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from typing import Optional
import logging

from solver import solve_schedule, SolveInput, SolveOutput

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

app = FastAPI(title="PT Scheduler Solver", version="1.0.0")


class TimeSlot(BaseModel):
    day_of_week: int        # 0=Mon … 6=Sun
    start_time: str         # "HH:MM"
    end_time: str


class SolverCoach(BaseModel):
    id: str
    working_hours: list[TimeSlot]


class SolverClient(BaseModel):
    id: str
    session_count: int
    priority_score: int
    preferred_windows: list[TimeSlot]


class ExistingSession(BaseModel):
    client_id: str
    starts_at: str          # RFC3339
    ends_at: str


class SolveRequest(BaseModel):
    week_start: str         # "YYYY-MM-DD"
    coach: SolverCoach
    clients: list[SolverClient]
    existing_sessions: list[ExistingSession] = []


class SolveResponse(BaseModel):
    status: str             # "optimal" | "feasible" | "infeasible"
    sessions: list[ExistingSession] = []
    unscheduled_clients: list[str] = []


@app.get("/healthz")
def healthz():
    return {"status": "ok"}


@app.post("/solve", response_model=SolveResponse)
def solve(req: SolveRequest):
    logger.info("solve request for week %s with %d clients", req.week_start, len(req.clients))

    try:
        result = solve_schedule(SolveInput(
            week_start=req.week_start,
            coach_id=req.coach.id,
            working_hours=[
                (wh.day_of_week, wh.start_time, wh.end_time)
                for wh in req.coach.working_hours
            ],
            clients=[
                {
                    "id": c.id,
                    "session_count": c.session_count,
                    "priority_score": c.priority_score,
                    "preferred_windows": [
                        (pw.day_of_week, pw.start_time, pw.end_time)
                        for pw in c.preferred_windows
                    ],
                }
                for c in req.clients
            ],
            existing_sessions=[
                (s.client_id, s.starts_at, s.ends_at)
                for s in req.existing_sessions
            ],
        ))
    except Exception as exc:
        logger.exception("solver error")
        raise HTTPException(status_code=500, detail=str(exc))

    return SolveResponse(
        status=result.status,
        sessions=[
            ExistingSession(
                client_id=s["client_id"],
                starts_at=s["starts_at"],
                ends_at=s["ends_at"],
            )
            for s in result.sessions
        ],
        unscheduled_clients=result.unscheduled_clients,
    )
