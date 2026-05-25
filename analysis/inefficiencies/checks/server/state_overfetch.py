"""S3: /api/state ships full decks[] on every poll."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    df = inp.wire_http
    if df is None or len(df) == 0:
        return None
    state = df[df["route"] == "/api/state"]
    if state.empty:
        return None
    # delta polls (since_seq > 0) carry decks + events. bytes_out per response
    # is what we care about.
    avg = float(state["bytes_out"].mean())
    n_decks_estimate = avg / 30  # README claims ~30B/deck (raw JSON, ignoring HTTP framing)
    # Projection: at 1 Hz client poll, MTU per fleet size.
    rows = []
    for fleet, tabs in [(100, 1), (100, 5), (1000, 5), (10_000, 5)]:
        kib_s = (avg * tabs) / 1024.0  # 1 poll/s/tab
        rows.append((fleet, tabs, kib_s))
    sev = "high" if avg > 50_000 else "med" if avg > 10_000 else "low"
    return Finding(
        id="S3",
        title="`/api/state` ships full decks[] on every response",
        severity=sev,
        summary=f"mean response body {avg/1024:.1f} KiB; projected ~{n_decks_estimate:.0f}-deck slice/tick.",
        detail=(
            f"- Mean `/api/state` body: {avg/1024:.2f} KiB (N={len(state)}).\n"
            f"- The decks slice is shipped on every poll — see `frontend/src/lib/live/README.md`.\n"
            f"- Projected client-side ingress at 1 Hz:\n"
            + "\n".join(f"  - fleet={f}, tabs={t}: {k:.1f} KiB/s" for f, t, k in rows)
            + "\n"
        ),
        mitigation=("Emit `decks_delta` (only decks whose row changed since "
                    "`since_seq`) on delta polls; keep the full `decks[]` only on "
                    "the bootstrap path (`since_seq=0`). README already names this "
                    "as the obvious next step."),
        metrics={"mean_bytes_out": avg, "n_state_responses": len(state)},
    )
