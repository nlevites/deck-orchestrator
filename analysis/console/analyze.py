"""
Pillar 2B analyzer: ingests HAR + Playwright trace + heap snapshots + the
fixture-emitted metrics ndjson for each scenario, produces per-scenario
charts under console/runs/<scenario>/ plus an aggregate cross-scenario
table.

Usage:
    uv run --project analysis python -m analysis.console.analyze
    uv run --project analysis python -m analysis.console.analyze \\
        --scenario steady-state
"""
from __future__ import annotations

import argparse
import json
import sys
from collections import Counter, defaultdict
from pathlib import Path

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
import pandas as pd


REPO_ROOT = Path(__file__).resolve().parents[2]
RUNS_DIR = REPO_ROOT / "analysis" / "console" / "runs"


def discover_scenarios(only: str | None) -> list[Path]:
    if not RUNS_DIR.exists():
        raise SystemExit(f"no runs under {RUNS_DIR}")
    dirs = [p for p in RUNS_DIR.iterdir() if p.is_dir()]
    if only:
        dirs = [p for p in dirs if p.name == only]
    if not dirs:
        raise SystemExit(f"no scenario subdirs under {RUNS_DIR}")
    return sorted(dirs)


def load_har(scenario_dir: Path) -> list[dict]:
    har_path = scenario_dir / "network.har"
    if not har_path.exists():
        return []
    with har_path.open() as f:
        try:
            data = json.load(f)
        except json.JSONDecodeError:
            return []
    return data.get("log", {}).get("entries", []) or []


def load_metrics_ndjson(scenario_dir: Path) -> pd.DataFrame:
    p = scenario_dir / "metrics.ndjson"
    if not p.exists():
        return pd.DataFrame()
    rows: list[dict] = []
    with p.open() as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                rows.append(json.loads(line))
            except json.JSONDecodeError:
                continue
    return pd.DataFrame(rows)


def load_metrics_json(scenario_dir: Path) -> dict:
    p = scenario_dir / "metrics.json"
    if not p.exists():
        return {}
    return json.loads(p.read_text())


def har_to_df(entries: list[dict]) -> pd.DataFrame:
    rows = []
    for e in entries:
        req = e.get("request", {})
        resp = e.get("response", {})
        rows.append({
            "ts": pd.to_datetime(e.get("startedDateTime"), errors="coerce", utc=True),
            "method": req.get("method"),
            "url": req.get("url"),
            "status": resp.get("status"),
            "bytes_request": req.get("bodySize") or 0,
            "bytes_response": (resp.get("content", {}) or {}).get("size") or 0,
            "duration_ms": e.get("time"),
        })
    df = pd.DataFrame(rows)
    if df.empty:
        return df
    # Bucket into route templates: collapse :{run_id}, :{deck_id}.
    df["route"] = df["url"].apply(route_template)
    return df


def route_template(url: str) -> str:
    import re
    p = url
    p = re.sub(r"https?://[^/]+", "", p)  # strip origin
    p = re.split(r"[?#]", p)[0]
    p = re.sub(r"/api/runs/[^/]+/jobs/[^/]+/(retry|resolve)",
               r"/api/runs/{id}/jobs/{job_id}/\1", p)
    p = re.sub(r"/api/runs/[^/]+/(cancel|state)$", r"/api/runs/{id}/\1", p)
    p = re.sub(r"/api/runs/[^/]+$", "/api/runs/{id}", p)
    p = re.sub(r"/api/decks/[^/]+/chaos(/reset|/crash)?$",
               lambda m: "/api/decks/{deck_id}/chaos" + (m.group(1) or ""), p)
    return p


def plot_bytes_over_time(df: pd.DataFrame, out: Path) -> None:
    if df.empty:
        return
    df = df.dropna(subset=["ts"]).sort_values("ts").copy()
    df["t_s"] = (df["ts"] - df["ts"].iloc[0]).dt.total_seconds()
    df["cum_bytes_kib"] = df["bytes_response"].cumsum() / 1024.0
    fig, ax = plt.subplots(figsize=(10, 4))
    ax.plot(df["t_s"], df["cum_bytes_kib"])
    ax.set_xlabel("seconds since scenario start")
    ax.set_ylabel("cumulative response bytes (KiB)")
    ax.set_title("Wire bytes (response) over time, one tab")
    fig.tight_layout()
    fig.savefig(out, dpi=120)
    plt.close(fig)
    print(f"wrote {out}")


def plot_poll_jitter(df: pd.DataFrame, out: Path) -> None:
    """Inter-arrival between consecutive GET /api/state polls vs 1s target."""
    if df.empty:
        return
    state_polls = df[
        (df["method"] == "GET") & (df["route"] == "/api/state")
    ].dropna(subset=["ts"]).sort_values("ts")
    if len(state_polls) < 2:
        return
    intervals = state_polls["ts"].diff().dt.total_seconds().dropna()
    fig, ax = plt.subplots(figsize=(8, 4))
    ax.hist(intervals, bins=40)
    ax.axvline(1.0, linestyle="--", label="target 1.0s")
    ax.set_xlabel("seconds between consecutive /api/state polls")
    ax.set_ylabel("count")
    ax.set_title(f"Poll cadence — N={len(intervals)} median={intervals.median():.2f}s")
    ax.legend()
    fig.tight_layout()
    fig.savefig(out, dpi=120)
    plt.close(fig)
    print(f"wrote {out}")


def plot_heap(metrics_df: pd.DataFrame, out: Path) -> None:
    if metrics_df.empty or "jsHeapUsed" not in metrics_df.columns:
        return
    m = metrics_df.dropna(subset=["jsHeapUsed"]).copy()
    if m.empty:
        return
    m["t_s"] = (m["ts"] - m["ts"].iloc[0]) / 1000.0
    fig, ax = plt.subplots(figsize=(10, 4))
    ax.plot(m["t_s"], m["jsHeapUsed"] / (1024 * 1024), label="usedJSHeap (MiB)")
    if "jsHeapTotal" in m.columns:
        ax.plot(m["t_s"], m["jsHeapTotal"] / (1024 * 1024),
                label="totalJSHeap (MiB)", alpha=0.5)
    ax.set_xlabel("seconds")
    ax.set_ylabel("MiB")
    ax.set_title("JS heap drift over scenario")
    ax.legend()
    fig.tight_layout()
    fig.savefig(out, dpi=120)
    plt.close(fig)
    print(f"wrote {out}")


def plot_reducer_renders(metrics_df: pd.DataFrame, metrics_final: dict, out: Path) -> None:
    """
    Per-second setQueryData rate from the ndjson stream + total counts.
    Cheap proxy for the C4 render storm check; the full per-key histogram
    comes from the buffered setQueryData log dumped at teardown.
    """
    sqdl = metrics_final.get("setQueryData") or []
    if not sqdl:
        return
    by_key = Counter()
    for entry in sqdl:
        by_key[entry.get("queryKey", "?")] += 1
    fig, ax = plt.subplots(figsize=(8, 4))
    keys = list(by_key.keys())
    counts = [by_key[k] for k in keys]
    order = sorted(range(len(keys)), key=lambda i: counts[i])
    ax.barh([keys[i] for i in order], [counts[i] for i in order])
    ax.set_xlabel("setQueryData call count")
    ax.set_title(f"Reducer activity by queryKey (N={sum(counts)})")
    fig.tight_layout()
    fig.savefig(out, dpi=120)
    plt.close(fig)
    print(f"wrote {out}")


def heap_summary(scenario_dir: Path) -> dict[str, int]:
    out: dict[str, int] = {}
    for snap in scenario_dir.glob("heap-*.heapsnapshot"):
        out[snap.stem.removeprefix("heap-")] = snap.stat().st_size
    return out


def analyze_scenario(scenario_dir: Path) -> dict:
    name = scenario_dir.name
    print(f"\n=== {name} ===")
    har = load_har(scenario_dir)
    df = har_to_df(har)
    metrics_df = load_metrics_ndjson(scenario_dir)
    final = load_metrics_json(scenario_dir)

    if not df.empty:
        plot_bytes_over_time(df, scenario_dir / f"{name}-bytes-over-time.png")
        plot_poll_jitter(df, scenario_dir / f"{name}-poll-jitter.png")
    if not metrics_df.empty:
        plot_heap(metrics_df, scenario_dir / f"{name}-heap.png")
    plot_reducer_renders(metrics_df, final, scenario_dir / f"{name}-renders.png")

    # Aggregate the scenario into a single row for the cross-scenario CSV.
    duration_s = 0.0
    if not df.empty and df["ts"].notna().any():
        ts = df["ts"].dropna()
        duration_s = (ts.max() - ts.min()).total_seconds()
    return {
        "scenario": name,
        "n_requests": len(df),
        "n_metrics_ticks": len(metrics_df),
        "duration_s": duration_s,
        "kib_total": df["bytes_response"].sum() / 1024.0 if not df.empty else 0,
        "kib_per_min": (df["bytes_response"].sum() / 1024.0) / (duration_s / 60.0)
            if duration_s > 0 else 0,
        "n_state_polls": int((df["route"] == "/api/state").sum()) if not df.empty else 0,
        "n_state_run_polls": int((df["route"] == "/api/runs/{id}/state").sum())
            if not df.empty else 0,
        "n_setQueryData": len(final.get("setQueryData") or []),
        "n_visibility_changes": len(final.get("visibility") or []),
        "heap_files": ",".join(sorted(heap_summary(scenario_dir).keys())),
    }


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--scenario", default=None,
                    help="single scenario name (default: all under runs/)")
    args = ap.parse_args()
    scenarios = discover_scenarios(args.scenario)
    summary_rows = [analyze_scenario(p) for p in scenarios]

    summary = pd.DataFrame(summary_rows)
    out = RUNS_DIR / "console-bytes-per-tab-per-min.csv"
    summary.to_csv(out, index=False)
    print(f"\nwrote aggregate {out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
