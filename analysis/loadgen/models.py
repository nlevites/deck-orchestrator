"""
Subset of OpenAPI shapes the loadgen interacts with. Hand-maintained
against api/openapi.yaml (no codegen — keeps the loadgen dependency-light).
A drift smoke test could later compare keys against parsed spec; out of
scope for now.
"""
from __future__ import annotations

from typing import TypedDict, NotRequired


class Step(TypedDict):
    type: str
    description: str


class Heartbeat(TypedDict):
    deck_id: str
    endpoint_url: str
    current_attempt_id: NotRequired[str | None]


class DispatchPayload(TypedDict):
    attempt_id: str
    run_id: str
    job_id: str
    steps: list[Step]


class ExecutorEventRequest(TypedDict):
    attempt_id: str
    kind: str  # "RUNNING" | "COMPLETED" | "FAILED" | "STEP_COMPLETED"
    occurred_at: str
    payload: NotRequired[dict]
