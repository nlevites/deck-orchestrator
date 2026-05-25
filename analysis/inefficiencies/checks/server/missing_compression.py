"""S8: missing gzip on /api/state responses ≥ 4 KiB."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    # Static check: grep for compress middleware in the orchestrator.
    repo_root = inp.repo_root
    if repo_root is None:
        return None
    api_dir = repo_root / "backend" / "internal" / "api"
    has_compress = any(p.name.startswith("mw_compress") or
                       "Content-Encoding" in p.read_text(errors="ignore")
                       for p in api_dir.glob("*.go"))
    if has_compress:
        return None  # already mitigated; no finding

    # Dynamic backing: how much would we save on /api/state?
    df = inp.wire_http
    big_state_bytes = 0
    n_big = 0
    if df is not None and len(df):
        state = df[df["route"] == "/api/state"]
        big = state[state["bytes_out"] >= 4096]
        n_big = len(big)
        # JSON typically compresses ~5x.
        big_state_bytes = int(big["bytes_out"].sum())
    saved = int(big_state_bytes * 0.8)
    sev = "high" if n_big > 0 else "med"  # always worth doing; severity scales
    return Finding(
        id="S8",
        title="No gzip/br compression middleware on the orchestrator",
        severity=sev,
        summary=("orchestrator does not advertise or emit Content-Encoding; "
                 f"would save ~{saved/1024:.1f} KiB across {n_big} ≥4 KiB /api/state responses."),
        detail=(
            f"- Grep for `mw_compress*.go` / `Content-Encoding` in "
            f"`backend/internal/api/`: no match.\n"
            f"- `/api/state` responses ≥ 4 KiB in capture: N={n_big} totaling "
            f"{big_state_bytes/1024:.1f} KiB.\n"
            f"- JSON compresses ~5× with gzip; ~80% wire savings on those responses.\n"
        ),
        mitigation=("Add a `Logging`-style middleware that wraps the ResponseWriter "
                    "with `gzip.NewWriter` when the request has `Accept-Encoding: gzip`. "
                    "One file, ~40 LOC. Zero impact on bootstraps under 4 KiB."),
        metrics={"big_state_responses": n_big,
                 "estimated_bytes_saved": saved},
    )
