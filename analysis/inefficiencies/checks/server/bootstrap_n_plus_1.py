"""S11: N+1 read pattern in bootstrap (per-deck/per-run sub-queries)."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    repo_root = inp.repo_root
    if repo_root is None:
        return None
    state_go = repo_root / "backend" / "internal" / "handlers" / "state.go"
    if not state_go.exists():
        return None
    src = state_go.read_text()
    # Heuristic: a for loop calling rowToX inside a list-handler is the classic
    # N+1 pattern. Both bootstrapRuns and bootstrapDecks do this today.
    suspects = []
    for fname, pat in [
        ("bootstrapDecks", "rowToDeck(r.Context()"),
        ("bootstrapRuns", "rowToRunSummary(r.Context()"),
    ]:
        if pat in src:
            suspects.append(fname)
    if not suspects:
        return None
    sev = "med"
    return Finding(
        id="S11",
        title="N+1 SQL pattern in bootstrap handlers",
        severity=sev,
        summary=f"{len(suspects)} bootstrap handlers loop with per-row sub-queries.",
        detail=(
            f"- `handlers/state.go` contains: "
            + ", ".join(f"`{s}`" for s in suspects) + ".\n"
            "- Each iteration of the bootstrap loop calls a `rowToX` helper that\n"
            "  performs sub-queries (e.g. `LatestAttemptForJob`). At fleet size 100,\n"
            "  one bootstrap = O(100) round-trips through the read pool.\n"
        ),
        mitigation=("Replace the per-row sub-queries with a single JOIN'd query "
                    "per resource type. sqlc's `:many` queries make this concise."),
        metrics={"n_plus_1_handlers": suspects},
    )
