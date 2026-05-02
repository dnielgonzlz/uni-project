"""FastAPI entry point for the OR-Tools scheduling microservice.

Endpoints
---------
POST /solve
    Accepts a SolveRequest JSON body and returns a SolveResponse.
    The Go backend calls this with a 30-second HTTP timeout; the solver
    itself is capped at 25 seconds to ensure a response always arrives.

GET  /healthz
    Returns {"status": "ok"} — used by the Go readyz handler.
"""

import logging
import sys

import uvicorn
from fastapi import FastAPI, Request, status
from fastapi.responses import JSONResponse

from mangum import Mangum
from models import SolveRequest, SolveResponse
from scheduler import solve

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

logging.basicConfig(
    stream=sys.stdout,
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# App
# ---------------------------------------------------------------------------

app = FastAPI(
    title="PT Scheduler — OR-Tools Solver",
    description="CP-SAT scheduling microservice for the PT Scheduler backend.",
    version="1.0.0",
)


@app.get("/healthz", include_in_schema=False)
def healthz():
    return {"status": "ok"}


@app.post("/solve", response_model=SolveResponse)
def solve_endpoint(request: SolveRequest):
    """Run the CP-SAT solver and return a proposed schedule."""
    logger.info(
        "Received solve request: week=%s clients=%d existing=%d",
        request.week_start,
        len(request.clients),
        len(request.existing_sessions),
    )
    result = solve(request)
    return result


# ---------------------------------------------------------------------------
# Global exception handler — always return JSON, never a 500 HTML page
# ---------------------------------------------------------------------------

@app.exception_handler(Exception)
async def unhandled_exception_handler(req: Request, exc: Exception):
    logger.exception("Unhandled solver error: %s", exc)
    return JSONResponse(
        status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
        content={"detail": "internal solver error"},
    )


# ---------------------------------------------------------------------------
# AWS Lambda — set handler to main.handler (Mangum wraps the FastAPI ASGI app)
# ---------------------------------------------------------------------------

handler = Mangum(app)


# ---------------------------------------------------------------------------
# Dev entrypoint
# ---------------------------------------------------------------------------

if __name__ == "__main__":
    uvicorn.run("main:app", host="0.0.0.0", port=8081, reload=True)
