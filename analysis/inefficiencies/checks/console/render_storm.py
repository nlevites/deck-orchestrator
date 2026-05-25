"""C4: 50 events in one tick => 50 setQueryData calls (one render per call)."""
from __future__ import annotations

from collections import Counter

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    burst = (inp.console_runs or {}).get("event-burst")
    if not burst:
        return None
    final = burst.get("metrics_final") or {}
    sqdl = final.get("setQueryData") or []
    if not sqdl:
        return None
    n_calls = len(sqdl)
    by_key = Counter(e.get("queryKey", "?") for e in sqdl)
    max_per_key = max(by_key.values()) if by_key else 0
    # Group by start-time bucket to estimate "calls per tick" (1s window).
    import math
    buckets: Counter[int] = Counter()
    for e in sqdl:
        bucket = int(math.floor((e.get("startTime") or 0) / 1000))
        buckets[bucket] += 1
    peak_per_sec = max(buckets.values()) if buckets else 0
    sev = "high" if peak_per_sec > 30 else "med" if peak_per_sec > 10 else "low"
    return Finding(
        id="C4",
        title="Reducer render storm — setQueryData fan-out per event-burst tick",
        severity=sev,
        summary=(f"peak {peak_per_sec} setQueryData calls/sec during burst; "
                 f"total {n_calls} across scenario."),
        detail=(
            f"- Captured during event-burst scenario; per-key fan-out: \n"
            + "\n".join(f"  - `{k}`: {v}" for k, v in by_key.most_common(6))
            + f"\n- Each call triggers TanStack Query subscribers to re-render. "
            f"Worst case: 50 events in one poll tick => 50 React renders.\n"
        ),
        mitigation=("Wrap the event-application for-loop in `qc.queryCache.notify` "
                    "batching, or accumulate per-queryKey state in a local map then "
                    "do a single `setQueryData` at the end of the tick. Either path "
                    "collapses N renders into 1."),
        metrics={"peak_set_query_data_per_sec": peak_per_sec,
                 "total_calls": n_calls,
                 "max_per_key": max_per_key},
    )
