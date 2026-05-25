"""S1: wasted polls — % of GET /executor/poll that return 204."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    df = inp.wire_http
    if df is None or len(df) == 0:
        return None
    polls = df[(df["method"] == "GET") & (df["route"] == "/executor/poll")]
    if polls.empty:
        return None
    n = len(polls)
    n_204 = int((polls["status"] == 204).sum())
    pct = 100.0 * n_204 / n
    sev = "high" if pct > 80 else "med" if pct > 50 else "low"
    return Finding(
        id="S1",
        title="Wasted executor polls (204 No Content rate)",
        severity=sev,
        summary=f"{pct:.0f}% of /executor/poll returned 204 — pure-overhead requests.",
        detail=(
            f"- N={n} poll requests in capture, {n_204} returned 204 ({pct:.1f}%).\n"
            f"- At 0% occupancy every poll is a wasted byte. Defaults: poll every "
            f"500ms => 2 wasted req/s/deck => 200 req/s at 100 decks just to ask "
            f"`anything for me?`\n"
        ),
        mitigation=("Long-poll the executor (block up to N seconds inside the "
                    "handler) or push via SSE/server-sent events. Both shrink "
                    "idle traffic by ~99%."),
        metrics={"n_polls": n, "n_204": n_204, "pct_204": pct},
    )
