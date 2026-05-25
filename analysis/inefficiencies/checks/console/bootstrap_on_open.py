"""C3: bootstrap cost on every tab open."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    steady = (inp.console_runs or {}).get("steady-state")
    if not steady:
        return None
    har = steady.get("har") or []
    # First /api/state response is the bootstrap (since_seq=0 implied).
    state_resps = [e for e in har
                   if "/api/state" in (e.get("request", {}).get("url") or "")]
    if not state_resps:
        return None
    first = state_resps[0]
    # HAR uses -1 for "unknown body size" (e.g. compressed) — coerce to 0.
    boot_bytes = max(
        (first.get("response", {}).get("content", {}) or {}).get("size") or 0, 0
    )
    sev = "med" if boot_bytes > 8000 else "low"
    return Finding(
        id="C3",
        title="Bootstrap-on-open ships full snapshot each new tab",
        severity=sev,
        summary=f"first /api/state response: {boot_bytes/1024:.2f} KiB.",
        detail=(
            f"- Each new tab issues `GET /api/state?since_seq=0`. Bootstrap response "
            f"in steady-state run: {boot_bytes/1024:.2f} KiB.\n"
            f"- At 100 decks the snapshot scales linearly with fleet size + recent runs.\n"
            f"- A team of operators opening 5 tabs a day pays this cost 5×.\n"
        ),
        mitigation=("Cache the snapshot in `localStorage` keyed on the orchestrator's "
                    "`server_seq`; new tabs hydrate from local + only fetch the delta. "
                    "Pair with a `BroadcastChannel` to invalidate across tabs."),
        metrics={"bootstrap_bytes": boot_bytes},
    )
