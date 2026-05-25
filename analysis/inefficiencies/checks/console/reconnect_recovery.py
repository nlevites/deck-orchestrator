"""C6: bytes + wall-time from network-back to cache-converged."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    rec = (inp.console_runs or {}).get("reconnect")
    if not rec:
        return None
    har = rec.get("har") or []
    # Find first successful /api/state response after the offline window.
    # Heuristic: the spec did setOffline(true) ~10s in for 30s, then back.
    # The first /api/state response with 2xx after t = ~40s is the recovery one.
    import pandas as pd
    rows = []
    for e in har:
        url = (e.get("request") or {}).get("url") or ""
        if "/api/state" not in url:
            continue
        ts = pd.to_datetime(e.get("startedDateTime"), utc=True, errors="coerce")
        status = (e.get("response") or {}).get("status") or 0
        size = ((e.get("response") or {}).get("content") or {}).get("size") or 0
        rows.append((ts, status, size))
    if not rows:
        return None
    df = pd.DataFrame(rows, columns=["ts", "status", "size"]).dropna().sort_values("ts")
    # Identify the gap (>10s between successful responses) as the offline window.
    df["gap_s"] = df["ts"].diff().dt.total_seconds()
    big_gaps = df[df["gap_s"] > 10]
    if big_gaps.empty:
        return None
    first_recovery = big_gaps.iloc[0]
    return Finding(
        id="C6",
        title="Reconnect recovery cost",
        severity="med",
        summary=(f"first /api/state after blackout: {first_recovery['size']/1024:.2f} KiB; "
                 f"gap was {first_recovery['gap_s']:.1f}s."),
        detail=(
            f"- Offline blackout detected via gap between consecutive /api/state "
            f"responses: {first_recovery['gap_s']:.1f}s.\n"
            f"- First recovery response: status={int(first_recovery['status'])}, "
            f"size={first_recovery['size']/1024:.2f} KiB.\n"
            f"- This is the cost of `seqRef=0` re-bootstrap after a flap.\n"
        ),
        mitigation=("If the gap is short (< stale_threshold), the client could pass "
                    "the last-known `since_seq` and just request the events it missed. "
                    "Server-side `events.kind` aging would let us treat this safely."),
        metrics={"recovery_bytes": int(first_recovery["size"]),
                 "blackout_seconds": float(first_recovery["gap_s"])},
    )
