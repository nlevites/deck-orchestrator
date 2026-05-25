"""S9: request_id propagation gap between executor outbox -> orchestrator events."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    df = inp.wire_http
    if df is None or len(df) == 0:
        return None
    # We don't have the X-Request-Id header in the middleware logs (slog field
    # `request_id` is the orchestrator-generated one). So this is a static
    # source-scan: does the executor's outbox attach the header?
    repo_root = inp.repo_root
    if repo_root is None:
        return None
    outbox_dir = repo_root / "backend" / "internal" / "executor" / "outbox"
    propagates = False
    if outbox_dir.exists():
        for p in outbox_dir.glob("*.go"):
            src = p.read_text(errors="ignore")
            if "X-Request-Id" in src or "X-Request-ID" in src:
                propagates = True
                break
    if propagates:
        return None
    sev = "med"
    return Finding(
        id="S9",
        title="X-Request-Id not propagated from executor outbox to orchestrator",
        severity=sev,
        summary="executor outgoing requests lack X-Request-Id header.",
        detail=(
            "- Scanned `backend/internal/executor/outbox/*.go`: no `X-Request-Id` header set.\n"
            "- Orchestrator middleware *generates* a request_id on inbound, but "
            "without executor-side propagation we can't trace an event from an "
            "executor's outbox commit through to the orchestrator's audit log.\n"
        ),
        mitigation=("In the outbox's http.Request build, set "
                    "`req.Header.Set(\"X-Request-Id\", attemptID + '-' + kind)`. "
                    "Orchestrator's request-id middleware should prefer the inbound "
                    "header if present (instead of overwriting)."),
        metrics={"executor_outbox_propagates_request_id": False},
    )
