"""
Pillar 2C+3e: run the inefficiency checklist against the wire, console,
and scale-sweep corpora. Writes inefficiencies/inefficiencies.md ranked
worst-first.

    uv run --project analysis python -m analysis.inefficiencies.scan
    uv run --project analysis python -m analysis.inefficiencies.scan --check S1

Each check lives in checks/server/<name>.py or checks/console/<name>.py
and exports `check(inputs: Inputs) -> Finding | None`. Adding a new check
is one new file + a line in CHECK_MODULES below.
"""
from __future__ import annotations

import argparse
import importlib
import json
import sys
from pathlib import Path

import pandas as pd

from ._types import Finding, Inputs


REPO_ROOT = Path(__file__).resolve().parents[2]
WIRE_RUNS = REPO_ROOT / "analysis" / "wire" / "runs"
CONSOLE_RUNS = REPO_ROOT / "analysis" / "console" / "runs"
SCALE_RUNS = REPO_ROOT / "analysis" / "scale" / "runs"
OUT = REPO_ROOT / "analysis" / "inefficiencies" / "inefficiencies.md"


SERVER_CHECKS = [
    "wasted_polls",
    "header_body_ratio",
    "state_overfetch",
    "rebootstrap_thrash",
    "error_envelope_bloat",
    "reconciler_probe_rate",
    "write_contention",
    "missing_compression",
    "request_id_propagation",
    "heartbeat_noop_rate",
    "bootstrap_n_plus_1",
    "run_detail_double_poll",
]
CONSOLE_CHECKS = [
    "backgrounded_tab",
    "multi_tab_cost",
    "bootstrap_on_open",
    "render_storm",
    "events_cache_unbounded",
    "reconnect_recovery",
    "per_resource_refetch",
    "run_detail_redundancy",
]


def latest(dir: Path) -> Path | None:
    if not dir.exists():
        return None
    subs = sorted([p for p in dir.iterdir() if p.is_dir()])
    return subs[-1] if subs else None


def load_wire(latest_run: Path | None) -> tuple[pd.DataFrame | None, list[dict]]:
    if latest_run is None:
        return None, []
    parquet = latest_run / "wire-http.parquet"
    csv = latest_run / "wire-http.csv"
    df = None
    if parquet.exists():
        try:
            df = pd.read_parquet(parquet)
        except Exception:
            df = None
    if df is None and csv.exists():
        df = pd.read_csv(csv)
        if "ts" in df.columns:
            df["ts"] = pd.to_datetime(df["ts"], errors="coerce", utc=True)
    scenario_path = latest_run / "scenario.ndjson"
    scenario: list[dict] = []
    if scenario_path.exists():
        with scenario_path.open() as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    scenario.append(json.loads(line))
                except json.JSONDecodeError:
                    continue
    return df, scenario


def load_console() -> dict:
    """Map scenario_name -> {har, metrics_final, metrics_ndjson}."""
    out: dict = {}
    if not CONSOLE_RUNS.exists():
        return out
    for d in CONSOLE_RUNS.iterdir():
        if not d.is_dir():
            continue
        har_path = d / "network.har"
        har = None
        if har_path.exists():
            try:
                har_blob = json.loads(har_path.read_text())
                har = har_blob.get("log", {}).get("entries", [])
            except json.JSONDecodeError:
                har = []
        metrics_final = None
        mfj = d / "metrics.json"
        if mfj.exists():
            try:
                metrics_final = json.loads(mfj.read_text())
            except json.JSONDecodeError:
                metrics_final = None
        metrics_df = pd.DataFrame()
        mndjson = d / "metrics.ndjson"
        if mndjson.exists():
            rows = []
            with mndjson.open() as f:
                for line in f:
                    line = line.strip()
                    if line:
                        try:
                            rows.append(json.loads(line))
                        except json.JSONDecodeError:
                            pass
            metrics_df = pd.DataFrame(rows)
        out[d.name] = {
            "har": har, "metrics_final": metrics_final,
            "metrics_ndjson": metrics_df,
        }
    return out


def load_scale() -> dict:
    """Map N -> {http, samples, meta}."""
    out: dict = {}
    if not SCALE_RUNS.exists():
        return out
    for d in sorted(SCALE_RUNS.iterdir()):
        if not d.is_dir() or not d.name.startswith("N="):
            continue
        try:
            n = int(d.name.removeprefix("N="))
        except ValueError:
            continue
        http = _load_orch_log(d / "orchestrator.log")
        samples_path = d / "process-samples.csv"
        samples = pd.read_csv(samples_path) if samples_path.exists() else pd.DataFrame()
        meta_path = d / "sweep-meta.json"
        meta = json.loads(meta_path.read_text()) if meta_path.exists() else {}
        out[n] = {"http": http, "samples": samples, "meta": meta}
    return out


def _load_orch_log(p: Path) -> pd.DataFrame:
    if not p.exists():
        return pd.DataFrame()
    rows = []
    with p.open() as f:
        for line in f:
            line = line.strip()
            if not line.startswith("{"):
                continue
            try:
                rows.append(json.loads(line))
            except json.JSONDecodeError:
                pass
    df = pd.DataFrame(rows)
    if df.empty:
        return df
    http = df[df.get("msg") == "http"].copy()
    if http.empty:
        return http
    http["duration_ms"] = pd.to_numeric(http["duration"], errors="coerce") / 1e6
    http["bytes_in"] = pd.to_numeric(http.get("bytes_in"), errors="coerce").fillna(0)
    http["bytes_out"] = pd.to_numeric(http.get("bytes_out"), errors="coerce").fillna(0)
    http["ts"] = pd.to_datetime(http["time"], errors="coerce", utc=True)
    # Bucket into route templates here too.
    import re
    def template(path: str) -> str:
        p = path
        p = re.sub(r"/api/runs/[^/]+/jobs/[^/]+/(retry|resolve)",
                   r"/api/runs/{id}/jobs/{job_id}/\1", p)
        p = re.sub(r"/api/runs/[^/]+/(cancel|state)$", r"/api/runs/{id}/\1", p)
        p = re.sub(r"/api/runs/[^/]+$", "/api/runs/{id}", p)
        p = re.sub(r"/executor/abort/[^/]+$", "/executor/abort/{attempt_id}", p)
        return p
    http["route"] = http["path"].apply(template)
    return http


def run_checks(only: str | None) -> list[Finding]:
    wire_df, scenario = load_wire(latest(WIRE_RUNS))
    console = load_console()
    scale = load_scale()
    inputs = Inputs(
        wire_http=wire_df,
        scenario_log=scenario,
        console_runs=console,
        scale_runs=scale,
        repo_root=REPO_ROOT,
    )

    found: list[Finding] = []
    for kind, mods in (("server", SERVER_CHECKS), ("console", CONSOLE_CHECKS)):
        for name in mods:
            mod = importlib.import_module(
                f"analysis.inefficiencies.checks.{kind}.{name}")
            try:
                f = mod.check(inputs)
            except Exception as e:
                f = Finding(
                    id=f"{kind[0].upper()}?", title=f"{name} check failed",
                    severity="low",
                    summary=f"check raised: {e!r}",
                    detail=f"```\n{e}\n```",
                )
            if f is None:
                continue
            if only and f.id != only:
                continue
            found.append(f)
    return found


SEV_RANK = {"high": 0, "med": 1, "low": 2}


def render(findings: list[Finding]) -> str:
    findings_sorted = sorted(findings, key=lambda f: (SEV_RANK[f.severity], f.id))
    lines = ["# Inefficiencies & anti-patterns\n"]
    lines.append("Generated by `analysis/inefficiencies/scan.py`.\n")
    lines.append("Source corpora: wire NDJSON (Pillar 2A), Playwright HAR + trace + heap "
                 "(Pillar 2B), scale-sweep NDJSON + psutil samples (Pillar 3).\n")
    lines.append("\n## Worst-first summary\n")
    lines.append("| Sev | ID | Title | Summary |")
    lines.append("|---|---|---|---|")
    for f in findings_sorted:
        lines.append(f"| {f.severity} | {f.id} | {f.title} | {f.summary} |")
    lines.append("\n## Details\n")
    for f in findings_sorted:
        lines.append(f"### {f.id} — {f.title} _({f.severity})_\n")
        lines.append(f"**Summary.** {f.summary}\n")
        if f.detail:
            lines.append(f.detail.rstrip() + "\n")
        if f.mitigation:
            lines.append(f"**Mitigation.** {f.mitigation}\n")
        if f.metrics:
            lines.append("**Backing metrics:**\n```json\n"
                         + json.dumps(f.metrics, indent=2, default=str)
                         + "\n```\n")
    return "\n".join(lines)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", default=None, help="filter to a single check ID (S1, C4, …)")
    args = ap.parse_args()
    findings = run_checks(args.check)
    OUT.write_text(render(findings))
    print(f"wrote {OUT} ({len(findings)} findings)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
