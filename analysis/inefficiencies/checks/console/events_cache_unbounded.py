"""C5: events cache grows linearly over a long-lived session."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    long_lived = (inp.console_runs or {}).get("long-lived")
    if not long_lived:
        return None
    metrics_df = long_lived.get("metrics_ndjson")
    if metrics_df is None or metrics_df.empty or "jsHeapUsed" not in metrics_df.columns:
        return None
    series = metrics_df["jsHeapUsed"].dropna()
    if len(series) < 10:
        return None
    start = float(series.iloc[: len(series) // 10].mean())
    end = float(series.iloc[-len(series) // 10:].mean())
    growth_mb = (end - start) / (1024 * 1024)
    sev = "high" if growth_mb > 30 else "med" if growth_mb > 10 else "low"
    return Finding(
        id="C5",
        title="JS heap drift across a 30-minute session",
        severity=sev,
        summary=f"used heap grew {growth_mb:.1f} MiB across the session.",
        detail=(
            f"- start mean: {start/1024/1024:.1f} MiB\n"
            f"- end mean: {end/1024/1024:.1f} MiB\n"
            f"- delta: {growth_mb:.1f} MiB\n"
            f"- The TanStack events cache in `lib/live/helpers.ts::setEventsCache` "
            f"appends rows without an explicit cap. Long-lived tabs accumulate.\n"
        ),
        mitigation=("Cap the events cache at a sliding window (last N or last 5 min); "
                    "the reducer projects each event into a query slice that doesn't "
                    "need the raw event row to be retained."),
        metrics={"heap_growth_mb": growth_mb,
                 "heap_start_mb": start / (1024 * 1024),
                 "heap_end_mb": end / (1024 * 1024)},
    )
