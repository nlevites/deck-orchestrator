"""C8: run-detail tab pays for /api/state AND /api/runs/{id}/state."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    multi = (inp.console_runs or {}).get("multi-tab")
    if not multi:
        return None
    har = multi.get("har") or []
    state_bytes = 0
    run_state_bytes = 0
    for e in har:
        url = (e.get("request") or {}).get("url") or ""
        # Strip query string so /api/runs/abc/state?since_seq=N matches.
        path = url.split("?", 1)[0]
        size = max(((e.get("response") or {}).get("content") or {}).get("size") or 0, 0)
        if "/api/runs/" in path and path.endswith("/state"):
            run_state_bytes += size
        elif path.endswith("/api/state"):
            state_bytes += size
    if state_bytes == 0 and run_state_bytes == 0:
        return None
    overlap_estimate_kib = run_state_bytes / 1024 * 0.4  # heuristic: ~40% of run-state events also in /api/state
    sev = "med" if overlap_estimate_kib > 5 else "low"
    return Finding(
        id="C8",
        title="Run-detail global+scoped poll redundancy (multi-tab scenario)",
        severity=sev,
        summary=(f"global={state_bytes/1024:.1f} KiB, scoped={run_state_bytes/1024:.1f} KiB; "
                 f"~{overlap_estimate_kib:.1f} KiB estimated overlap."),
        detail=(
            f"- Multi-tab scenario captured two run-detail tabs polling both `/api/state` "
            f"and `/api/runs/{{id}}/state` concurrently.\n"
            f"- Same events for those run_ids ship through both routes.\n"
            f"- Heuristic 40% overlap estimate on the scoped traffic: ~{overlap_estimate_kib:.1f} KiB.\n"
        ),
        mitigation=("On the client, pause the global `/api/state` poll when a "
                    "run-detail tab is the only consumer of the page; or on the "
                    "server, accept a `?exclude_run_ids=` parameter the FE sets "
                    "from its open-tab registry."),
        metrics={"global_bytes": state_bytes, "scoped_bytes": run_state_bytes,
                 "overlap_estimate_bytes": int(overlap_estimate_kib * 1024)},
    )
