"""S2: header:body ratio on heartbeat + poll-204 — proxy for protocol bloat."""
from __future__ import annotations

from ..._types import Finding, Inputs

# slog's bytes_out only captures body bytes. HTTP/1.1 framing+headers from a
# typical orchestrator response: ~150-250 bytes (Content-Type, Content-Length,
# Date, X-Request-Id, Server, optional CORS). We estimate 200B and flag where
# bodies are <100B.
ESTIMATED_HEADER_BYTES = 200


def check(inp: Inputs) -> Finding | None:
    df = inp.wire_http
    if df is None or len(df) == 0:
        return None
    candidates = df[
        df["route"].isin(["/executor/heartbeat", "/executor/poll"])
    ]
    if candidates.empty:
        return None
    avg_body = float(candidates["bytes_out"].mean())
    ratio = ESTIMATED_HEADER_BYTES / max(avg_body, 1)
    sev = "high" if ratio > 5 else "med" if ratio > 2 else "low"
    return Finding(
        id="S2",
        title="Header-to-body ratio on chatty low-payload endpoints",
        severity=sev,
        summary=f"avg body={avg_body:.0f}B vs ~{ESTIMATED_HEADER_BYTES}B headers "
                f"=> headers are {ratio:.1f}× the payload.",
        detail=(
            f"- Mean response body on `/executor/heartbeat` + `/executor/poll`: "
            f"{avg_body:.0f}B (sample N={len(candidates)}).\n"
            f"- Conservative header overhead estimate: {ESTIMATED_HEADER_BYTES}B/request.\n"
            f"- Bytes-on-the-wire is dominated by framing, not content.\n"
        ),
        mitigation=("Batch heartbeats: one POST /executor/heartbeats per supervisor "
                    "covering N decks, body=[{deck_id,…}…]. Or move both loops to "
                    "HTTP/2 multiplexed onto a single keep-alive connection, which "
                    "amortizes the header cost across a longer-lived stream."),
        metrics={"avg_body_bytes": avg_body, "header_to_body_ratio": ratio,
                 "n_requests": len(candidates)},
    )
