#!/usr/bin/env python3
"""
Pillar 1 — static contract extraction.

Parses api/openapi.yaml + greps cadence constants out of the Go/TS
sources, emits contract.md: every route, who-calls-whom, polling cadences,
steady-state request rates at fleet scale, and the event taxonomy.

No stack boot required. Run from anywhere; paths are resolved relative
to the repo root (..../deck-fleet/).
"""
from __future__ import annotations

import argparse
import re
import sys
from collections import defaultdict
from dataclasses import dataclass, field
from pathlib import Path

import yaml


REPO_ROOT = Path(__file__).resolve().parents[2]
OPENAPI = REPO_ROOT / "api" / "openapi.yaml"
EXECUTOR_CFG = REPO_ROOT / "backend" / "internal" / "executor" / "config" / "config.go"
DEMO_CFG = REPO_ROOT / "config" / "demo.yaml"
LIVE_HOOK = REPO_ROOT / "frontend" / "src" / "lib" / "live" / "use-live-state.ts"
KINDS_GO = REPO_ROOT / "backend" / "internal" / "eventlog" / "kinds.go"
REDUCERS_DIR = REPO_ROOT / "frontend" / "src" / "lib" / "live" / "reducers"


@dataclass
class Route:
    path: str
    method: str
    tag: str
    op_id: str
    summary: str
    request_schema: str | None
    response_schema: str | None
    caller: str  # who calls this route
    host: str  # who hosts it


@dataclass
class Cadence:
    name: str
    interval_seconds: float
    source: str
    caller: str
    target_route: str
    notes: str = ""


@dataclass
class EventKind:
    name: str
    reducer_file: str | None
    reducer_fn: str | None


def parse_openapi() -> list[Route]:
    with OPENAPI.open() as f:
        spec = yaml.safe_load(f)
    routes: list[Route] = []
    for path, methods in spec.get("paths", {}).items():
        for method, op in methods.items():
            if method not in {"get", "post", "put", "delete", "patch"}:
                continue
            tags = op.get("tags", []) or ["untagged"]
            tag = tags[0]
            req = _ref_name(_safe(op, "requestBody", "content", "application/json", "schema"))
            resp = None
            for status in ("200", "201", "202", "204"):
                schema = _safe(op, "responses", status, "content", "application/json", "schema")
                if schema:
                    resp = _ref_name(schema)
                    break
            caller, host = _topology_for(tag, path)
            routes.append(
                Route(
                    path=path,
                    method=method.upper(),
                    tag=tag,
                    op_id=op.get("operationId", ""),
                    summary=op.get("summary", "").strip(),
                    request_schema=req,
                    response_schema=resp,
                    caller=caller,
                    host=host,
                )
            )
    return routes


def _safe(d: dict, *keys: str):
    cur = d
    for k in keys:
        if not isinstance(cur, dict) or k not in cur:
            return None
        cur = cur[k]
    return cur


def _ref_name(schema: dict | None) -> str | None:
    if not schema:
        return None
    if "$ref" in schema:
        return schema["$ref"].rsplit("/", 1)[-1]
    if "type" in schema:
        return schema["type"]
    return None


def _topology_for(tag: str, path: str) -> tuple[str, str]:
    # Topology comes from openapi.yaml tag descriptions; we recompute here
    # so the contract.md row carries explicit caller/host without making
    # the reader cross-reference tag prose.
    if tag in {"runs", "decks", "live", "admin"}:
        return ("frontend", "orchestrator")
    if tag == "chaos":
        # Operator hits orchestrator, orchestrator proxies to executor.
        return ("frontend → orchestrator (proxy)", "orchestrator + executor")
    if tag == "executor-inbound":
        return ("executor", "orchestrator")
    if tag == "executor-outbound":
        return ("orchestrator", "executor")
    return ("?", "?")


def grep_cadences() -> list[Cadence]:
    out: list[Cadence] = []

    # Executor defaults in Go (defaults() returns these).
    exec_src = EXECUTOR_CFG.read_text()
    for name, label, caller, target in [
        ("HeartbeatInterval", "executor heartbeat", "executor", "POST /executor/heartbeat"),
        ("PollInterval", "executor poll", "executor", "GET /executor/poll"),
        ("StepDuration", "executor step simulate", "executor (internal)", "—"),
    ]:
        m = re.search(rf"{name}:\s*(\d+)\s*\*\s*time\.(Second|Millisecond)", exec_src)
        if m:
            n = int(m.group(1))
            secs = n / 1000.0 if m.group(2) == "Millisecond" else float(n)
            out.append(
                Cadence(
                    name=label,
                    interval_seconds=secs,
                    source=f"{EXECUTOR_CFG.relative_to(REPO_ROOT)}::defaults().{name}",
                    caller=caller,
                    target_route=target,
                    notes="overridable via config/env",
                )
            )

    # Demo config overrides — these are what the user actually runs.
    if DEMO_CFG.exists():
        demo = yaml.safe_load(DEMO_CFG.read_text())
        timeouts = demo.get("timeouts") or {}
        for k, v in timeouts.items():
            secs = _parse_duration(str(v))
            if secs is None:
                continue
            out.append(
                Cadence(
                    name=f"orch timeout: {k}",
                    interval_seconds=secs,
                    source=f"{DEMO_CFG.relative_to(REPO_ROOT)}::timeouts.{k}",
                    caller="orchestrator (internal)",
                    target_route="—",
                    notes="liveness/reconcile threshold",
                )
            )

    # Frontend live-poll cadence.
    live_src = LIVE_HOOK.read_text()
    m = re.search(r"POLL_INTERVAL_MS\s*=\s*([\d_]+)", live_src)
    if m:
        secs = int(m.group(1).replace("_", "")) / 1000.0
        out.append(
            Cadence(
                name="console live-state poll",
                interval_seconds=secs,
                source=f"{LIVE_HOOK.relative_to(REPO_ROOT)}::POLL_INTERVAL_MS",
                caller="frontend tab",
                target_route="GET /api/state",
            )
        )
    m = re.search(r"REBOOTSTRAP_INTERVAL_MS\s*=\s*([\d_]+)\s*\*\s*([\d_]+)", live_src)
    if m:
        secs = int(m.group(1).replace("_", "")) * int(m.group(2).replace("_", "")) / 1000.0
        out.append(
            Cadence(
                name="console periodic re-bootstrap (safety net)",
                interval_seconds=secs,
                source=f"{LIVE_HOOK.relative_to(REPO_ROOT)}::REBOOTSTRAP_INTERVAL_MS",
                caller="frontend tab",
                target_route="GET /api/state?since_seq=0",
                notes="long-tail drift guard, not primary freshness mechanism",
            )
        )
    return out


def _parse_duration(s: str) -> float | None:
    # cleanenv-style durations: "500ms", "2s", "15s", "1m30s"...
    pat = re.compile(r"(\d+)(ms|s|m|h)")
    secs = 0.0
    matched = False
    for n, unit in pat.findall(s):
        matched = True
        n = int(n)
        if unit == "ms":
            secs += n / 1000.0
        elif unit == "s":
            secs += float(n)
        elif unit == "m":
            secs += n * 60.0
        elif unit == "h":
            secs += n * 3600.0
    return secs if matched else None


def grep_event_kinds() -> list[EventKind]:
    src = KINDS_GO.read_text()
    pairs = re.findall(r'Kind\w+\s+Kind\s*=\s*"([A-Z_]+)"', src)
    reducer_index = _index_reducers()
    out: list[EventKind] = []
    for name in pairs:
        fn, file = reducer_index.get(name, (None, None))
        out.append(EventKind(name=name, reducer_file=file, reducer_fn=fn))
    return out


def _index_reducers() -> dict[str, tuple[str, str]]:
    """
    Map event-kind string → (function name, source file).
    Scans the reducer module for `case "KIND":` or string-literal mentions.
    """
    index: dict[str, tuple[str, str]] = {}
    if not REDUCERS_DIR.exists():
        return index
    for f in REDUCERS_DIR.glob("*.ts"):
        src = f.read_text()
        # Match function exports: `export function applyJobCompleted(...)`
        for kind in re.findall(r'"([A-Z_]+)"', src):
            # Heuristic: closest preceding `export function applyXxx`.
            occ = src.find(f'"{kind}"')
            head = src[:occ]
            m = list(re.finditer(r"export\s+function\s+(\w+)", head))
            if m:
                fn = m[-1].group(1)
                index.setdefault(kind, (fn, str(f.relative_to(REPO_ROOT))))
    return index


def steady_state_table(cadences: list[Cadence]) -> str:
    # Per-deck req/s for the two executor loops at default intervals.
    per_deck = 0.0
    rows = []
    for c in cadences:
        if c.caller != "executor" or c.interval_seconds <= 0:
            continue
        rps = 1.0 / c.interval_seconds
        per_deck += rps
        rows.append((c.name, c.interval_seconds, rps))

    out = ["| Loop | Interval | Per-deck req/s |", "|---|---|---|"]
    for name, intv, rps in rows:
        out.append(f"| {name} | {intv:g}s | {rps:.2f} |")
    out.append(f"| **sum** | — | **{per_deck:.2f}** |")
    out.append("")
    out.append("| Fleet size | Aggregate executor-inbound req/s |")
    out.append("|---|---|")
    for n in (10, 100, 1_000, 10_000, 100_000):
        out.append(f"| {n:,} | {per_deck * n:,.1f} |")
    out.append("")
    out.append(
        "_Plus 1 req/s/tab on `GET /api/state` from the console; "
        "ignored above because it scales with operators, not fleet._"
    )
    return "\n".join(out)


def render(routes: list[Route], cadences: list[Cadence], kinds: list[EventKind]) -> str:
    parts: list[str] = []
    parts.append("# Deck-fleet wire contract (static)\n")
    parts.append(
        "Generated by `analysis/static/extract.py` from `api/openapi.yaml`, "
        "Go cadence constants, and the frontend live-state hook. "
        "Re-run after any contract change: `make -C analysis static`.\n"
    )

    parts.append("## 1. Routes grouped by who-calls-whom\n")
    by_topo: dict[tuple[str, str], list[Route]] = defaultdict(list)
    for r in routes:
        by_topo[(r.caller, r.host)].append(r)
    for (caller, host), group in sorted(by_topo.items()):
        parts.append(f"### {caller} → {host}\n")
        parts.append("| Method | Path | Op | Summary | Request | Response |")
        parts.append("|---|---|---|---|---|---|")
        for r in sorted(group, key=lambda x: (x.path, x.method)):
            req = f"`{r.request_schema}`" if r.request_schema else "—"
            resp = f"`{r.response_schema}`" if r.response_schema else "—"
            parts.append(
                f"| {r.method} | `{r.path}` | `{r.op_id}` | {r.summary} | {req} | {resp} |"
            )
        parts.append("")

    parts.append("## 2. Polling / heartbeat cadences\n")
    parts.append("| Loop | Interval (s) | Caller | Target | Source | Notes |")
    parts.append("|---|---|---|---|---|---|")
    for c in cadences:
        parts.append(
            f"| {c.name} | {c.interval_seconds:g} | {c.caller} | "
            f"`{c.target_route}` | `{c.source}` | {c.notes} |"
        )
    parts.append("")

    parts.append("## 3. Steady-state request rate\n")
    parts.append(steady_state_table(cadences))
    parts.append("")

    parts.append("## 4. Event kinds and reducers\n")
    parts.append("| EventKind | Reducer fn | Reducer file |")
    parts.append("|---|---|---|")
    for k in kinds:
        fn = f"`{k.reducer_fn}`" if k.reducer_fn else "_(no client reducer)_"
        file = f"`{k.reducer_file}`" if k.reducer_file else "—"
        parts.append(f"| `{k.name}` | {fn} | {file} |")
    parts.append("")

    parts.append("## 5. Notes\n")
    parts.append(
        "- The executor uses *short-poll* on `GET /executor/poll` "
        "(returns 204 immediately when no work is ready). At default "
        "500ms poll interval and 0 % occupancy, every poll is a "
        "wasted byte. See `inefficiencies/inefficiencies.md` S1.\n"
        "- `/api/state` ships the full `decks[]` slice on **every** "
        "client poll (~30 B/deck/Hz). See README.md in `frontend/src/lib/live/`. "
        "Captured in inefficiency S3.\n"
        "- Error envelopes for `VERSION_MISMATCH` / `ALREADY_TERMINAL` "
        "embed the full `current_state: Run` — see inefficiency S5.\n"
    )
    return "\n".join(parts)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=str(Path(__file__).with_name("contract.md")))
    args = ap.parse_args()

    routes = parse_openapi()
    cadences = grep_cadences()
    kinds = grep_event_kinds()
    out = render(routes, cadences, kinds)
    Path(args.out).write_text(out)
    print(
        f"wrote {args.out} "
        f"({len(routes)} routes, {len(cadences)} cadences, {len(kinds)} event kinds)"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
