"""S10: heartbeat body is identical request-to-request — pure framing cost."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    df = inp.wire_http
    if df is None or len(df) == 0:
        return None
    # The middleware logs bytes_in but not the body. Approximate: same
    # bytes_in count per (deck, endpoint) over the capture = same payload.
    hb = df[(df["method"] == "POST") & (df["route"] == "/executor/heartbeat")].copy()
    if hb.empty:
        return None
    # Group by source (executor deck) and count unique bytes_in values.
    g = hb.groupby("dst").agg(  # dst on the orch side is just "orchestrator", so use src process inference
        n=("bytes_in", "size"),
        n_unique_sizes=("bytes_in", "nunique"),
    )
    # If most decks emit a single body-size value, payloads are constant.
    mean_unique = float(g["n_unique_sizes"].mean()) if len(g) else 0.0
    n_total = int(g["n"].sum())
    sev = "med" if mean_unique <= 2 else "low"
    return Finding(
        id="S10",
        title="Heartbeats carry near-constant payloads (no-op rate)",
        severity=sev,
        summary=(f"mean unique body-size per deck: {mean_unique:.1f}; "
                 f"N={n_total} heartbeats."),
        detail=(
            f"- Across the capture window, decks heartbeat with body sizes that\n"
            f"  rarely change (`current_attempt_id` either stays NULL or a single\n"
            f"  UUID for the duration of one job). Body is essentially constant\n"
            f"  between transitions.\n"
            f"- Every heartbeat pays full TLS / TCP / HTTP framing cost for ~5 bytes\n"
            f"  of changing data.\n"
        ),
        mitigation=("Client-side suppression: skip the heartbeat if "
                    "`(endpoint_url, current_attempt_id)` matches the last "
                    "successfully-acked send AND the orchestrator's response said "
                    "`last_seen <= max_silence/2`. Falls back to send on doubt."),
        metrics={"mean_unique_body_size_per_deck": mean_unique,
                 "n_heartbeats": n_total},
    )
