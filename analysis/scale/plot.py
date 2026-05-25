"""
Pillar 3d: load each sweep run, produce the headline charts.

Inputs (per N):
    analysis/scale/runs/N=<N>/orchestrator.log    (NDJSON)
    analysis/scale/runs/N=<N>/process-samples.csv
    analysis/scale/runs/N=<N>/runs.json
    analysis/scale/runs/N=<N>/sweep-meta.json

Outputs (root of runs/):
    latency-by-endpoint.png
    throughput.png
    orch-memory.png
    network-footprint.png
    db-growth.png
    dag-completion-time.png
    breaking-point.md

    uv run --project analysis python -m analysis.scale.plot
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import pandas as pd


REPO_ROOT = Path(__file__).resolve().parents[2]
RUNS_DIR = REPO_ROOT / "analysis" / "scale" / "runs"


def load_orchestrator_log(run_dir: Path) -> pd.DataFrame:
    p = run_dir / "orchestrator.log"
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
                continue
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
    http["route"] = http["path"].apply(_route_template)
    return http


def _route_template(path: str) -> str:
    import re
    p = path
    p = re.sub(r"/api/runs/[^/]+/jobs/[^/]+/(retry|resolve)", r"/api/runs/{id}/jobs/{job_id}/\1", p)
    p = re.sub(r"/api/runs/[^/]+/(cancel|state)$", r"/api/runs/{id}/\1", p)
    p = re.sub(r"/api/runs/[^/]+$", "/api/runs/{id}", p)
    p = re.sub(r"/executor/abort/[^/]+$", "/executor/abort/{attempt_id}", p)
    p = re.sub(r"/api/decks/[^/]+/chaos(/reset|/crash)?$",
               lambda m: "/api/decks/{deck_id}/chaos" + (m.group(1) or ""), p)
    return p


def collect_runs() -> dict[int, dict]:
    if not RUNS_DIR.exists():
        return {}
    runs: dict[int, dict] = {}
    for d in sorted(RUNS_DIR.iterdir()):
        if not d.is_dir() or not d.name.startswith("N="):
            continue
        try:
            n = int(d.name.removeprefix("N="))
        except ValueError:
            continue
        runs[n] = {
            "dir": d,
            "http": load_orchestrator_log(d),
            "samples": _load_samples(d / "process-samples.csv"),
            "runs_state": _load_runs_state(d / "runs.json"),
            "meta": _load_json(d / "sweep-meta.json"),
        }
    return runs


def _load_samples(p: Path) -> pd.DataFrame:
    if not p.exists():
        return pd.DataFrame()
    df = pd.read_csv(p)
    df["ts"] = pd.to_datetime(df["ts"], unit="s", utc=True)
    return df


def _load_runs_state(p: Path) -> dict:
    """
    Load the exact per-run status breakdown emitted by sweep.py's
    snapshot_runs_exact(). Shape: {submitted_total, by_status, by_id}.
    Returns {} when missing or malformed so callers can no-op.
    """
    if not p.exists():
        return {}
    try:
        body = json.loads(p.read_text())
    except json.JSONDecodeError:
        return {}
    if not isinstance(body, dict):
        return {}
    if "submitted_total" in body and "by_status" in body:
        return body
    # Legacy shape (pre-fix): {"runs": [...]}, sampled to 50. Best-effort
    # backfill so old runs.json still renders, but completion% will be
    # the misleading sample-based number.
    if "runs" in body:
        from collections import Counter
        runs = body.get("runs") or []
        counts = Counter(r.get("status", "UNKNOWN") for r in runs)
        return {
            "submitted_total": len(runs),
            "by_status": dict(counts),
            "by_id": {},
            "_legacy_sample": True,
        }
    return {}


def _load_json(p: Path) -> dict:
    if not p.exists():
        return {}
    try:
        return json.loads(p.read_text())
    except json.JSONDecodeError:
        return {}


def plot_latency(runs: dict[int, dict], out: Path) -> None:
    rows = []
    for n, data in runs.items():
        http = data["http"]
        if http.empty:
            continue
        for route, sub in http.groupby("route"):
            rows.append({
                "n": n, "route": route,
                "p50": float(sub["duration_ms"].quantile(0.5)),
                "p95": float(sub["duration_ms"].quantile(0.95)),
                "p99": float(sub["duration_ms"].quantile(0.99)),
            })
    if not rows:
        return
    df = pd.DataFrame(rows)
    top_routes = (df.groupby("route")["p95"].max()
                  .sort_values(ascending=False).head(6).index.tolist())
    fig, ax = plt.subplots(figsize=(10, 6))
    for route in top_routes:
        sub = df[df["route"] == route].sort_values("n")
        ax.plot(sub["n"], sub["p95"], marker="o", label=f"{route} (p95)")
    ax.set_xscale("log")
    ax.set_yscale("log")
    ax.set_xlabel("fleet size N (log)")
    ax.set_ylabel("latency ms (log)")
    ax.set_title("Per-endpoint p95 latency vs fleet size")
    ax.legend(fontsize=8)
    ax.grid(True, which="both", linestyle="--", alpha=0.3)
    fig.tight_layout()
    fig.savefig(out, dpi=120)
    plt.close(fig)
    print(f"wrote {out}")


def plot_throughput(runs: dict[int, dict], out: Path) -> None:
    rows = []
    for n, data in runs.items():
        http = data["http"]
        if http.empty or http["ts"].isna().all():
            continue
        window = (http["ts"].max() - http["ts"].min()).total_seconds()
        if window <= 0:
            continue
        for route, sub in http.groupby("route"):
            rows.append({"n": n, "route": route, "rps": len(sub) / window})
    if not rows:
        return
    df = pd.DataFrame(rows)
    top = (df.groupby("route")["rps"].max()
           .sort_values(ascending=False).head(6).index.tolist())
    fig, ax = plt.subplots(figsize=(10, 6))
    for route in top:
        sub = df[df["route"] == route].sort_values("n")
        ax.plot(sub["n"], sub["rps"], marker="o", label=route)
    ax.set_xscale("log")
    ax.set_yscale("log")
    ax.set_xlabel("fleet size N (log)")
    ax.set_ylabel("requests / second (log)")
    ax.set_title("Sustained throughput per endpoint vs fleet size")
    ax.legend(fontsize=8)
    ax.grid(True, which="both", linestyle="--", alpha=0.3)
    fig.tight_layout()
    fig.savefig(out, dpi=120)
    plt.close(fig)
    print(f"wrote {out}")


def plot_orch_memory(runs: dict[int, dict], out: Path) -> None:
    fig, ax = plt.subplots(figsize=(10, 6))
    for n, data in runs.items():
        samples = data["samples"]
        if samples.empty:
            continue
        orch = samples[samples["label"] == "orchestrator"].copy()
        if orch.empty:
            continue
        orch["t_s"] = (orch["ts"] - orch["ts"].iloc[0]).dt.total_seconds()
        ax.plot(orch["t_s"], orch["rss_mb"], label=f"N={n}")
    ax.set_xlabel("seconds since orchestrator start")
    ax.set_ylabel("RSS (MiB)")
    ax.set_title("Orchestrator memory over time by N")
    ax.legend(fontsize=8)
    fig.tight_layout()
    fig.savefig(out, dpi=120)
    plt.close(fig)
    print(f"wrote {out}")


def plot_network(runs: dict[int, dict], out: Path) -> None:
    rows = []
    for n, data in runs.items():
        http = data["http"]
        if http.empty or http["ts"].isna().all():
            continue
        window = (http["ts"].max() - http["ts"].min()).total_seconds()
        if window <= 0:
            continue
        bytes_in = http["bytes_in"].sum() / window
        bytes_out = http["bytes_out"].sum() / window
        rows.append({"n": n, "bytes_in_per_s": bytes_in, "bytes_out_per_s": bytes_out})
    if not rows:
        return
    df = pd.DataFrame(rows).sort_values("n")
    fig, ax = plt.subplots(figsize=(10, 5))
    ax.plot(df["n"], df["bytes_in_per_s"] / 1024, marker="o", label="ingress KiB/s")
    ax.plot(df["n"], df["bytes_out_per_s"] / 1024, marker="o", label="egress KiB/s")
    ax.set_xscale("log")
    ax.set_yscale("log")
    ax.set_xlabel("fleet size N (log)")
    ax.set_ylabel("KiB / second (log)")
    ax.set_title("Orchestrator network footprint vs N")
    ax.legend()
    fig.tight_layout()
    fig.savefig(out, dpi=120)
    plt.close(fig)
    print(f"wrote {out}")


def plot_db_growth(runs: dict[int, dict], out: Path) -> None:
    fig, ax = plt.subplots(figsize=(10, 5))
    for n, data in runs.items():
        s = data["samples"]
        if s.empty:
            continue
        orch = s[s["label"] == "orchestrator"]
        if orch.empty:
            continue
        t0 = orch["ts"].iloc[0]
        ax.plot((orch["ts"] - t0).dt.total_seconds(),
                orch["db_mb"], label=f"N={n}")
    ax.set_xlabel("seconds")
    ax.set_ylabel("orchestrator.db size (MiB)")
    ax.set_title("SQLite DB growth over time, by N")
    ax.legend(fontsize=8)
    fig.tight_layout()
    fig.savefig(out, dpi=120)
    plt.close(fig)
    print(f"wrote {out}")


def plot_completion(runs: dict[int, dict], out: Path) -> None:
    """Stacked bar of COMPLETED / RUNNING / AMBIGUOUS / FAILED / other per N."""
    rows = []
    for n, data in runs.items():
        rs = data["runs_state"]
        if not rs or not rs.get("submitted_total"):
            continue
        total = rs["submitted_total"]
        by_status = rs.get("by_status", {})
        rows.append({"n": n, "total": total, **by_status})
    if not rows:
        return
    df = pd.DataFrame(rows).fillna(0).sort_values("n")
    # Order: COMPLETED first (good), then RUNNING (in-flight), then AMBIGUOUS / FAILED.
    ordered_statuses = ["COMPLETED", "RUNNING", "DISPATCHED", "PENDING",
                        "READY", "AMBIGUOUS", "FAILED", "CANCELLED"]
    present = [s for s in ordered_statuses if s in df.columns]
    other_cols = [c for c in df.columns
                  if c not in {"n", "total", *present, "_legacy_sample"}]
    stack_cols = present + other_cols

    fig, ax = plt.subplots(figsize=(9, 5))
    bottoms = pd.Series([0.0] * len(df), index=df.index)
    xlabels = df["n"].astype(str).tolist()
    for col in stack_cols:
        pct = (df[col] / df["total"]) * 100
        ax.bar(xlabels, pct, bottom=bottoms, label=col)
        bottoms = bottoms + pct
    ax.set_ylim(0, 105)
    ax.set_xlabel("fleet size N")
    ax.set_ylabel("% of submitted DAGs (by final status at sweep end)")
    ax.set_title("DAG terminal status breakdown vs N (exact, all submitted runs)")
    ax.legend(loc="center left", bbox_to_anchor=(1.02, 0.5), fontsize=8)
    fig.tight_layout()
    fig.savefig(out, dpi=120, bbox_inches="tight")
    plt.close(fig)
    print(f"wrote {out}")


def write_breaking_point(runs: dict[int, dict], out: Path) -> None:
    lines = ["# Breaking-point narrative\n"]
    lines.append("Generated by `analysis/scale/plot.py` from sweep artifacts.\n")
    lines.append("| N | RSS peak (MiB) | DB final (MiB) | submitted | COMPLETED | AMBIGUOUS | RUNNING | p95 events (ms) | notes |")
    lines.append("|---|---|---|---|---|---|---|---|---|")
    for n, data in sorted(runs.items()):
        samples = data["samples"]
        http = data["http"]
        rs = data["runs_state"]
        if samples.empty:
            lines.append(f"| {n} | — | — | — | — | — | — | — | (no samples) |")
            continue
        rss = samples[samples["label"] == "orchestrator"]["rss_mb"].max()
        db = samples[samples["label"] == "orchestrator"]["db_mb"].max()
        events_p95 = None
        if not http.empty:
            ev = http[http["route"] == "/executor/events"]
            if not ev.empty:
                events_p95 = float(ev["duration_ms"].quantile(0.95))
        total = rs.get("submitted_total", 0) if rs else 0
        by_status = rs.get("by_status", {}) if rs else {}
        completed = by_status.get("COMPLETED", 0)
        ambiguous = by_status.get("AMBIGUOUS", 0)
        running = by_status.get("RUNNING", 0)
        def _pct(n_: int) -> str:
            return f"{n_} ({100*n_/total:.0f}%)" if total else "—"
        notes_parts = []
        if data["meta"].get("orch_exited_early"):
            notes_parts.append("orchestrator died mid-run")
        if rs.get("_legacy_sample"):
            notes_parts.append("legacy 50-row sample only")
        notes = "; ".join(notes_parts)
        lines.append(
            f"| {n} | {rss:.1f} | "
            + (f"{db:.1f}" if db is not None else "—") + " | "
            + (str(total) if total else "—") + " | "
            + _pct(completed) + " | "
            + _pct(ambiguous) + " | "
            + _pct(running) + " | "
            + (f"{events_p95:.1f}" if events_p95 is not None else "—") + " | "
            + notes + " |"
        )
    lines.append("")
    lines.append(
        "## Expected failure modes (against the plan's hypotheses)\n"
        "\n"
        "1. **SQLite write-conn queue.** Single write conn caps the orchestrator\n"
        "   around 5-10k commit-bound ops/sec; p95 on `POST /executor/events` and\n"
        "   `POST /executor/heartbeat` should climb sharply once write demand\n"
        "   exceeds that floor (≈ N · 2.5 req/s exec-inbound rate).\n"
        "2. **CPython loadgen ceiling.** Single process tops out near 10–20k req/s.\n"
        "   At 5000+ decks the sweep harness shards (`--shard-threshold`); above\n"
        "   that, total throughput grows linearly with worker count.\n"
        "3. **Open file descriptors.** Each httpx connection + each per-deck\n"
        "   advertise URL = pressure on `ulimit -n`. Watch the `num_fds` column\n"
        "   in `process-samples.csv` (capped at 1024 on a default macOS shell).\n"
        "4. **Reconciler probe storm.** When decks go silent (loadgen restart,\n"
        "   network blip), the orchestrator dials `/executor/state` per silent\n"
        "   deck — N=10k means N=10k probes within the stale_threshold window.\n"
    )
    out.write_text("\n".join(lines))
    print(f"wrote {out}")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.parse_args()
    runs = collect_runs()
    if not runs:
        raise SystemExit(f"no sweep runs in {RUNS_DIR}")
    print(f"loaded {len(runs)} sweep runs: {sorted(runs)}")

    plot_latency(runs, RUNS_DIR / "latency-by-endpoint.png")
    plot_throughput(runs, RUNS_DIR / "throughput.png")
    plot_orch_memory(runs, RUNS_DIR / "orch-memory.png")
    plot_network(runs, RUNS_DIR / "network-footprint.png")
    plot_db_growth(runs, RUNS_DIR / "db-growth.png")
    plot_completion(runs, RUNS_DIR / "dag-completion-time.png")
    write_breaking_point(runs, RUNS_DIR / "breaking-point.md")
    return 0


if __name__ == "__main__":
    sys.exit(main())
