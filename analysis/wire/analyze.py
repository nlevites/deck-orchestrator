"""
Pillar 2A analyzer: ingests per-process NDJSON logs from a wire-capture
run, produces wire-summary.csv, wire-volume.png, wire-cadence.png, and
wire-flow.md (Mermaid sequence diagram of one DAG run).

Usage:
    uv run --project analysis python -m analysis.wire.analyze \\
        --run analysis/wire/runs/20260524T220000

If --run is omitted the latest under analysis/wire/runs/ is used.
"""
from __future__ import annotations

import argparse
import json
import sys
from collections import Counter, defaultdict
from datetime import datetime
from pathlib import Path

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import pandas as pd


REPO_ROOT = Path(__file__).resolve().parents[2]
RUNS_DIR = REPO_ROOT / "analysis" / "wire" / "runs"


def latest_run() -> Path:
    if not RUNS_DIR.exists():
        raise SystemExit(f"no runs under {RUNS_DIR}")
    runs = sorted([p for p in RUNS_DIR.iterdir() if p.is_dir()])
    if not runs:
        raise SystemExit(f"no runs under {RUNS_DIR}")
    return runs[-1]


def load_ndjson(run_dir: Path) -> pd.DataFrame:
    """Load every *.log NDJSON file in run_dir/logs/, tag with `process`."""
    rows: list[dict] = []
    log_dir = run_dir / "logs"
    if not log_dir.exists():
        raise SystemExit(f"no logs/ in {run_dir}")
    for f in sorted(log_dir.glob("*.log")):
        proc = f.stem  # e.g. "orchestrator" or "executor-deck-1"
        with f.open() as fh:
            for line in fh:
                line = line.strip()
                if not line or not line.startswith("{"):
                    # supervisor.go writes a "[ts] starting…" banner first;
                    # skip those bare-text lines.
                    continue
                try:
                    obj = json.loads(line)
                except json.JSONDecodeError:
                    continue
                obj["process"] = proc
                rows.append(obj)
    if not rows:
        raise SystemExit(f"no NDJSON rows found in {log_dir}")
    df = pd.DataFrame(rows)
    return df


def normalize_http(df: pd.DataFrame) -> pd.DataFrame:
    """Filter to msg=='http' rows, parse duration, tag direction."""
    http = df[df.get("msg") == "http"].copy()
    if http.empty:
        raise SystemExit("no rows with msg='http' — was log.format=json?")

    # slog encodes time.Duration as nanoseconds in JSON.
    # bytes_out, bytes_in arrive as ints already.
    http["duration_ms"] = pd.to_numeric(http["duration"], errors="coerce") / 1e6
    http["bytes_in"] = pd.to_numeric(http.get("bytes_in"), errors="coerce").fillna(0)
    http["bytes_out"] = pd.to_numeric(http.get("bytes_out"), errors="coerce").fillna(0)
    http["ts"] = pd.to_datetime(http["time"], errors="coerce", utc=True)

    # Topology tagging mirrors openapi.yaml's tag descriptions.
    def classify(row) -> tuple[str, str, str]:
        proc = row["process"]
        path = str(row.get("path", ""))
        host = proc.split("-", 1)[0]  # "orchestrator" or "executor"
        if host == "orchestrator":
            if path.startswith("/api/"):
                if "/chaos" in path or "/release" in path or path == "/api/admin/restart":
                    src = "frontend"
                else:
                    src = "frontend"
                return (src, "orchestrator", "fe→orch")
            if path.startswith("/executor/"):
                return ("executor", "orchestrator", "exec→orch")
            return ("?", "orchestrator", "other")
        if host == "executor":
            if path.startswith("/executor/"):
                return ("orchestrator", proc, "orch→exec")
            return ("?", proc, "other")
        return ("?", "?", "other")

    classified = http.apply(classify, axis=1, result_type="expand")
    http[["src", "dst", "direction"]] = classified
    # Bucket path into route templates so /api/runs/abc123 collapses with /api/runs/xyz.
    http["route"] = http["path"].apply(route_template)
    return http


def route_template(path: str) -> str:
    """Collapse path-id segments into {id} placeholders."""
    parts = path.split("/")
    out = []
    for p in parts:
        if not p:
            out.append(p)
            continue
        # uuid-ish or long-token: replace
        if len(p) >= 8 and ("-" in p or any(c.isdigit() for c in p)):
            # special-case obvious ID positions
            pass
        out.append(p)
    # Pattern-by-pattern templating; cheap and good enough for our routes.
    p = path
    import re
    p = re.sub(r"/api/runs/[^/]+/jobs/[^/]+/(retry|resolve)", r"/api/runs/{id}/jobs/{job_id}/\1", p)
    p = re.sub(r"/api/runs/[^/]+/(cancel|state)$", r"/api/runs/{id}/\1", p)
    p = re.sub(r"/api/runs/[^/]+$", "/api/runs/{id}", p)
    p = re.sub(r"/api/decks/[^/]+/(chaos|chaos/reset|chaos/crash|release)$",
               r"/api/decks/{deck_id}/\1", p)
    p = re.sub(r"/executor/abort/[^/]+$", "/executor/abort/{attempt_id}", p)
    return p


def write_summary_csv(http: pd.DataFrame, out: Path) -> None:
    g = http.groupby(["direction", "route", "method"]).agg(
        count=("status", "size"),
        p50_ms=("duration_ms", lambda s: float(s.quantile(0.5))),
        p95_ms=("duration_ms", lambda s: float(s.quantile(0.95))),
        p99_ms=("duration_ms", lambda s: float(s.quantile(0.99))),
        bytes_in_total=("bytes_in", "sum"),
        bytes_out_total=("bytes_out", "sum"),
        non_2xx=("status", lambda s: int((s >= 300).sum())),
    ).reset_index().sort_values("count", ascending=False)
    g.to_csv(out, index=False)
    print(f"wrote {out} ({len(g)} route×direction buckets)")


def plot_volume(http: pd.DataFrame, out: Path) -> None:
    g = http.groupby(["direction", "route"]).agg(
        bytes_in=("bytes_in", "sum"),
        bytes_out=("bytes_out", "sum"),
    ).reset_index()
    g["bytes_total_kib"] = (g["bytes_in"] + g["bytes_out"]) / 1024.0
    g = g.sort_values("bytes_total_kib", ascending=True).tail(20)

    fig, ax = plt.subplots(figsize=(10, max(4, 0.35 * len(g))))
    labels = [f"[{r.direction}] {r.route}" for r in g.itertuples()]
    ax.barh(labels, g["bytes_total_kib"])
    ax.set_xlabel("Total bytes (KiB) — in + out")
    ax.set_title("Wire volume by route × direction")
    fig.tight_layout()
    fig.savefig(out, dpi=120)
    plt.close(fig)
    print(f"wrote {out}")


def plot_cadence(http: pd.DataFrame, out: Path) -> None:
    """Histogram inter-arrival of heartbeats and polls per deck."""
    fig, axes = plt.subplots(1, 2, figsize=(12, 4))

    for ax, route, title in [
        (axes[0], "/executor/heartbeat", "POST /executor/heartbeat"),
        (axes[1], "/executor/poll", "GET /executor/poll"),
    ]:
        rows = http[(http["route"] == route) & (http["direction"] == "exec→orch")]
        if rows.empty:
            ax.set_title(f"{title}\n(no data)")
            continue
        deltas: list[float] = []
        for proc, sub in rows.groupby("process"):
            sub = sub.sort_values("ts")
            d = sub["ts"].diff().dt.total_seconds().dropna()
            deltas.extend(d.tolist())
        if not deltas:
            ax.set_title(f"{title}\n(no inter-arrival data)")
            continue
        ax.hist(deltas, bins=40)
        ax.set_xlabel("Inter-arrival (s)")
        ax.set_title(f"{title}\nN={len(deltas)} median={pd.Series(deltas).median():.2f}s")
    fig.tight_layout()
    fig.savefig(out, dpi=120)
    plt.close(fig)
    print(f"wrote {out}")


def write_flow_md(http: pd.DataFrame, scenario_log: Path | None, out: Path) -> None:
    """Mermaid sequence diagram of the first DAG submitted in the scenario."""
    if not scenario_log or not scenario_log.exists():
        out.write_text("# wire flow\n\n(no scenario.ndjson found)\n")
        return
    entries = [json.loads(l) for l in scenario_log.read_text().splitlines() if l.strip()]
    submits = [e for e in entries if e.get("event") == "submit_dag"]
    if not submits:
        out.write_text("# wire flow\n\n(no DAG submission in scenario log)\n")
        return
    first = submits[0]
    rid = first["run_id"]
    t_start = pd.to_datetime(first["ts"], unit="s", utc=True)
    t_end = t_start + pd.Timedelta(seconds=20)
    window = http[(http["ts"] >= t_start) & (http["ts"] <= t_end)].sort_values("ts")
    # Trim to relevant routes for legibility.
    window = window[window["route"].str.startswith(("/api/", "/executor/"))]
    lines = [
        "# Wire flow — one DAG run",
        "",
        f"Run id: `{rid}` (first submission in the scenario log).",
        "",
        "Trimmed to the first 20s after submit_dag, scenario.ndjson timestamps.",
        "",
        "```mermaid",
        "sequenceDiagram",
        "    participant FE as console",
        "    participant ORCH as orchestrator",
        "    participant EXEC as executor",
        "",
    ]
    for r in window.itertuples():
        actor_src = {"frontend": "FE", "executor": "EXEC", "orchestrator": "ORCH"}.get(r.src, "?")
        actor_dst = {"orchestrator": "ORCH", "frontend": "FE"}.get(r.dst, "EXEC")
        label = f"{r.method} {r.route} ({r.status})"
        lines.append(f"    {actor_src}->>{actor_dst}: {label}")
    lines.append("```")
    out.write_text("\n".join(lines) + "\n")
    print(f"wrote {out}")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--run", type=Path, default=None,
                    help="run dir; default=latest under analysis/wire/runs/")
    args = ap.parse_args()

    run_dir = args.run.resolve() if args.run else latest_run()
    print(f"analyzing {run_dir}")

    df = load_ndjson(run_dir)
    print(f"loaded {len(df)} log rows across {df['process'].nunique()} processes")
    http = normalize_http(df)
    print(f"{len(http)} http rows after filter")

    out = run_dir / "wire-summary.csv"
    write_summary_csv(http, out)
    plot_volume(http, run_dir / "wire-volume.png")
    plot_cadence(http, run_dir / "wire-cadence.png")
    write_flow_md(http, run_dir / "scenario.ndjson", run_dir / "wire-flow.md")

    # Persist the normalized frame for downstream consumers (inefficiencies).
    http_out = run_dir / "wire-http.parquet"
    try:
        http.to_parquet(http_out, index=False)
        print(f"wrote {http_out}")
    except Exception as e:
        # parquet engine missing — fall back to feather/csv.
        http.to_csv(run_dir / "wire-http.csv", index=False)
        print(f"parquet failed ({e}); wrote wire-http.csv instead")

    return 0


if __name__ == "__main__":
    sys.exit(main())
