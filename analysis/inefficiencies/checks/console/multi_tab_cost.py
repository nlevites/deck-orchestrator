"""C2: per-tab cost is linear in tabs (no BroadcastChannel coalescing)."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    multi = (inp.console_runs or {}).get("multi-tab")
    steady = (inp.console_runs or {}).get("steady-state")
    if not multi or not steady:
        return None

    def total_bytes(scenario: dict) -> int:
        har = scenario.get("har") or []
        return sum((e.get("response", {}).get("content", {}) or {}).get("size") or 0 for e in har)

    bytes_multi = total_bytes(multi)
    bytes_steady = total_bytes(steady)
    if bytes_steady <= 0:
        return None
    ratio = bytes_multi / bytes_steady
    sev = "high" if ratio > 2.5 else "med" if ratio > 1.8 else "low"
    return Finding(
        id="C2",
        title="Multi-tab cost is linear in tab count",
        severity=sev,
        summary=f"multi-tab/steady-state bytes ratio = {ratio:.1f}× (3 tabs ≈ 3×).",
        detail=(
            f"- steady-state (1 tab) bytes: {bytes_steady/1024:.1f} KiB\n"
            f"- multi-tab (3 tabs) bytes: {bytes_multi/1024:.1f} KiB\n"
            f"- ratio ~= number of tabs → no coalescing.\n"
        ),
        mitigation=("Use a `SharedWorker` or `BroadcastChannel` so one tab "
                    "drives the poll loop and broadcasts state deltas to siblings. "
                    "Cuts orchestrator request rate by N-1/N."),
        metrics={"bytes_multi": bytes_multi, "bytes_steady": bytes_steady,
                 "tab_multiplier": ratio},
    )
