"""S7: SQLite write-conn contention — events/heartbeat p99 climbing with N."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    # Pull p99 of POST /executor/events from each scale-sweep run.
    if not inp.scale_runs:
        return None
    rows = []
    for n, data in inp.scale_runs.items():
        http = data.get("http")
        if http is None or len(http) == 0:
            continue
        ev = http[(http["method"] == "POST") & (http["route"] == "/executor/events")]
        if ev.empty:
            continue
        rows.append({
            "n": n,
            "p50": float(ev["duration_ms"].quantile(0.5)),
            "p99": float(ev["duration_ms"].quantile(0.99)),
        })
    if not rows:
        return None
    rows.sort(key=lambda r: r["n"])
    p99_growth = rows[-1]["p99"] / max(rows[0]["p99"], 1e-3)
    sev = "high" if p99_growth > 10 else "med" if p99_growth > 3 else "low"
    table = "| N | p50 ms | p99 ms |\n|---|---|---|\n" + \
            "\n".join(f"| {r['n']} | {r['p50']:.1f} | {r['p99']:.1f} |" for r in rows)
    return Finding(
        id="S7",
        title="Write-lock contention proxy: `/executor/events` p99 vs N",
        severity=sev,
        summary=(f"p99 grew {p99_growth:.1f}× from N={rows[0]['n']} to N={rows[-1]['n']}."),
        detail=(
            f"- Single SQLite write conn => commits serialize.\n"
            f"{table}\n\n"
            f"- Linear-in-N p99 = healthy. Super-linear = write queue saturating.\n"
        ),
        mitigation=("(a) Move events_log and heartbeat scalar updates onto a "
                    "background writer goroutine fed by a buffered channel; "
                    "(b) Replace single conn with a multi-writer DB (Postgres) "
                    "if fleet exceeds 2–5k decks."),
        metrics={"p99_growth": p99_growth, "rows": rows},
    )
