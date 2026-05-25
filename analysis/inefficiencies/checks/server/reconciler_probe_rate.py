"""S6: reconciler /executor/state probe rate during chaos-hang."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    df = inp.wire_http
    if df is None or len(df) == 0:
        return None
    # Probes show up on the *executor* side as inbound `/executor/state` GETs.
    probes = df[(df["direction"] == "orch→exec") & (df["route"] == "/executor/state")]
    if probes.empty:
        return None
    # Group by deck (== dst) and compute per-deck rate over the capture window.
    by_deck = probes.groupby("dst").agg(
        n=("status", "size"),
        ts_min=("ts", "min"),
        ts_max=("ts", "max"),
    )
    by_deck["window_s"] = (by_deck["ts_max"] - by_deck["ts_min"]).dt.total_seconds()
    by_deck["rps"] = by_deck["n"] / by_deck["window_s"].clip(lower=1)
    worst_rps = float(by_deck["rps"].max())
    worst_deck = str(by_deck["rps"].idxmax())
    sev = "high" if worst_rps > 1 else "med" if worst_rps > 0.2 else "low"
    return Finding(
        id="S6",
        title="Reconciler probe rate during chaos / silence",
        severity=sev,
        summary=(f"max /executor/state probe rate: {worst_rps:.2f} req/s "
                 f"(on {worst_deck}); {len(probes)} total probes across capture."),
        detail=(
            f"- Reconciler probes per deck:\n"
            + by_deck.head(10).to_markdown() + "\n"
            f"- Probes are only correct if they're rare and decisive — > 1 req/s "
            f"sustained means the reconciler is hot-looping rather than backing off.\n"
        ),
        mitigation=("Backoff schedule on `/executor/state` probes for the same "
                    "(deck, attempt) — see `reconciler.go` Outcome handling. After "
                    "two consecutive `unreachable`, escalate to AMBIGUOUS instead "
                    "of keeping the probe loop alive."),
        metrics={"max_probe_rps": worst_rps, "total_probes": len(probes)},
    )
