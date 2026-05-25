"""C1: backgrounded tab continues polling."""
from __future__ import annotations

from ..._types import Finding, Inputs


def check(inp: Inputs) -> Finding | None:
    # First, check the source code unconditionally: is there ANY mention of
    # document.visibilityState in the live hook?
    repo_root = inp.repo_root
    if repo_root is None:
        return None
    src = (repo_root / "frontend" / "src" / "lib" / "live" / "use-live-state.ts").read_text()
    gated = "visibilityState" in src or "visibilitychange" in src
    if gated:
        return None

    # Optional dynamic backing: did the reconnect scenario record polls
    # while the visibility flipped to 'hidden'?
    dynamic_count = 0
    for name, data in (inp.console_runs or {}).items():
        final = data.get("metrics_final") or {}
        visibility = final.get("visibility") or []
        if not visibility:
            continue
        # Count timestamps where state was 'hidden' — proxy for polls that
        # fired during that window since `instrument-fetch` runs regardless.
        hidden_ts = [v["ts"] for v in visibility if v.get("state") == "hidden"]
        if hidden_ts:
            dynamic_count += 1

    return Finding(
        id="C1",
        title="Console keeps polling while tab is hidden",
        severity="high",
        summary=("`use-live-state.ts` does not gate the poll loop on "
                 "`document.visibilityState`."),
        detail=(
            "- Static scan: no `visibilityState` reference in `frontend/src/lib/live/use-live-state.ts`.\n"
            "- Impact: a backgrounded operator tab keeps issuing 1 req/s to `/api/state`. \n"
            "  At an ops shift with 5 stale tabs forgotten on a workstation, that's 5 req/s \n"
            "  of pure noise the orchestrator commits to the read pool.\n"
            f"- Scenarios with visibility transitions recorded: {dynamic_count}.\n"
        ),
        mitigation=("Gate the poll: wrap `tick()` with `if (document.visibilityState === \"hidden\") return;` "
                    "and add a `visibilitychange` listener that fires an immediate "
                    "catch-up tick when the tab comes back."),
        metrics={"gated_on_visibility": gated,
                 "scenarios_with_visibility_change": dynamic_count},
    )
