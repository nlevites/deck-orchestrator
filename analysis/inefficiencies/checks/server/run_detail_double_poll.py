"""S12: run-detail tab polls /api/state AND /api/runs/{id}/state in parallel."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    df = inp.wire_http
    if df is None or len(df) == 0:
        return None
    state = df[df["route"] == "/api/state"]
    run_state = df[df["route"] == "/api/runs/{id}/state"]
    if state.empty and run_state.empty:
        return None
    state_bytes = int(state["bytes_out"].sum()) if not state.empty else 0
    run_bytes = int(run_state["bytes_out"].sum()) if not run_state.empty else 0
    n_state = len(state)
    n_run = len(run_state)
    # Severity: cost of double-poll is proportional to (#run-detail-tabs × payload).
    sev = "low" if n_run == 0 else "med" if n_run < n_state else "high"
    return Finding(
        id="S12",
        title="Run-detail tabs double-poll (global + scoped)",
        severity=sev,
        summary=(f"N={n_state} /api/state, N={n_run} /api/runs/{{id}}/state polls in capture; "
                 f"{state_bytes/1024:.1f} + {run_bytes/1024:.1f} KiB."),
        detail=(
            f"- A run-detail page receives every server-side change about that run\n"
            f"  via BOTH `/api/state` (run summary + delta events for that run_id) AND\n"
            f"  `/api/runs/{{id}}/state` (the scoped variant). Both responses ship the\n"
            f"  same events.\n"
            f"- Captured this run: state={state_bytes/1024:.1f} KiB across {n_state} polls; "
            f"run_state={run_bytes/1024:.1f} KiB across {n_run} polls.\n"
        ),
        mitigation=("Make `/api/state` filter events for run_ids any client has a run-detail "
                    "tab open on, OR replace one of the two with a no-op on the client when "
                    "the other covers it. Out-of-scope to fix; in-scope to flag."),
        metrics={"n_state": n_state, "n_run_state": n_run,
                 "state_bytes": state_bytes, "run_state_bytes": run_bytes},
    )
