"""S4: how often does the client send since_seq=0 vs delta polls?"""
from __future__ import annotations

import re

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    df = inp.wire_http
    if df is None or len(df) == 0:
        return None
    state = df[df["route"] == "/api/state"].copy()
    if state.empty:
        return None
    # We logged path without query string, but the orchestrator's middleware
    # *also* logs query if present — check the raw `path` instead of route.
    # Slog field `path` may or may not include the query; we look for `since_seq=0`
    # in any logged URL-ish field.
    def _is_bootstrap(row) -> bool:
        p = str(row.get("path", ""))
        if "since_seq" in p:
            m = re.search(r"since_seq=(\d+)", p)
            if m:
                return m.group(1) == "0"
        # If the middleware doesn't include the query, we can't tell;
        # treat as a single-bootstrap-on-mount baseline.
        return False
    state["bootstrap"] = state.apply(_is_bootstrap, axis=1)
    n_total = len(state)
    n_boot = int(state["bootstrap"].sum())
    # Expected baseline: 1 bootstrap per tab-mount + 1 per 5 min from REBOOTSTRAP_INTERVAL_MS
    # safety net. In a 60s capture that's just 1.
    sev = "high" if n_boot > 5 else "med" if n_boot > 2 else "low"
    return Finding(
        id="S4",
        title="Re-bootstrap frequency on `GET /api/state`",
        severity=sev,
        summary=f"{n_boot} of {n_total} state polls used `since_seq=0` (bootstrap).",
        detail=(
            f"- Total /api/state requests: {n_total}; bootstraps: {n_boot}.\n"
            f"- One bootstrap per tab-mount is expected; the 5-min safety net "
            f"in `use-live-state.ts` adds 1 per 5 min per tab.\n"
            f"- High count = reducer-returns-false or gap-detected; signals a "
            f"reducer bug or unstable event stream.\n"
        ),
        mitigation=("If above expected baseline, audit `lib/live/reducers/*.ts` "
                    "for reducers that return false unnecessarily. Each unintended "
                    "bootstrap reships the entire RunSummary array."),
        metrics={"n_state_polls": n_total, "n_bootstraps": n_boot},
    )
