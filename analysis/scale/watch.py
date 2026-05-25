"""
1Hz process watcher for sweep runs. Samples psutil + SQLite file size
for the orchestrator and any number of loadgen PIDs, writes one CSV row
per process per second.

Stays a separate process from the sweep harness so a hung sweep doesn't
take its own measurements with it.

    uv run --project analysis python -m analysis.scale.watch \\
        --orch-pid 12345 --loadgen-pid 12346 \\
        --db /tmp/orchestrator.db \\
        --out runs/N=1000/process-samples.csv \\
        --duration 300
"""
from __future__ import annotations

import argparse
import csv
import signal
import sys
import time
from pathlib import Path

import psutil


FIELDS = [
    "ts", "label", "pid", "rss_mb", "vms_mb",
    "cpu_pct", "num_threads", "num_fds", "db_mb",
]


def db_size_mb(path: Path | None) -> float:
    if path is None:
        return 0.0
    try:
        return path.stat().st_size / (1024 * 1024)
    except FileNotFoundError:
        return 0.0


def sample_process(p: psutil.Process, label: str, db_path: Path | None) -> dict | None:
    try:
        with p.oneshot():
            mem = p.memory_info()
            cpu = p.cpu_percent(interval=None)  # since last call
            return {
                "ts": time.time(),
                "label": label,
                "pid": p.pid,
                "rss_mb": mem.rss / (1024 * 1024),
                "vms_mb": mem.vms / (1024 * 1024),
                "cpu_pct": cpu,
                "num_threads": p.num_threads(),
                "num_fds": _fd_count(p),
                "db_mb": db_size_mb(db_path) if label == "orchestrator" else 0.0,
            }
    except (psutil.NoSuchProcess, psutil.AccessDenied):
        return None


def _fd_count(p: psutil.Process) -> int:
    try:
        return p.num_fds()  # POSIX
    except (AttributeError, psutil.AccessDenied):
        # Windows fallback (untested but at least won't crash here).
        try:
            return p.num_handles()  # type: ignore[attr-defined]
        except Exception:
            return -1


def parse_args() -> argparse.Namespace:
    ap = argparse.ArgumentParser()
    ap.add_argument("--orch-pid", type=int, required=True)
    ap.add_argument("--loadgen-pid", type=int, action="append", default=[])
    ap.add_argument("--db", type=Path, default=None)
    ap.add_argument("--out", type=Path, required=True)
    ap.add_argument("--duration", type=float, default=0,
                    help="0 = run until SIGINT or orchestrator exits")
    ap.add_argument("--interval", type=float, default=1.0)
    return ap.parse_args()


def main() -> int:
    args = parse_args()
    args.out.parent.mkdir(parents=True, exist_ok=True)

    targets: list[tuple[psutil.Process, str]] = []
    try:
        targets.append((psutil.Process(args.orch_pid), "orchestrator"))
    except psutil.NoSuchProcess:
        print(f"orchestrator pid {args.orch_pid} not found", file=sys.stderr)
        return 1
    for i, pid in enumerate(args.loadgen_pid):
        try:
            targets.append((psutil.Process(pid), f"loadgen-{i}"))
        except psutil.NoSuchProcess:
            print(f"loadgen pid {pid} not found; skipping", file=sys.stderr)

    # Prime cpu_percent — first call always returns 0.
    for p, _ in targets:
        try:
            p.cpu_percent(interval=None)
        except psutil.NoSuchProcess:
            pass

    stop_at = time.time() + args.duration if args.duration > 0 else float("inf")

    interrupted = False
    def _on_sigint(_s, _f):
        nonlocal interrupted
        interrupted = True
    signal.signal(signal.SIGINT, _on_sigint)
    signal.signal(signal.SIGTERM, _on_sigint)

    with args.out.open("w", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=FIELDS)
        writer.writeheader()
        next_tick = time.time()
        while not interrupted and time.time() < stop_at:
            for p, label in targets:
                row = sample_process(p, label, args.db)
                if row is None:
                    if label == "orchestrator":
                        print("orchestrator pid vanished, exiting", file=sys.stderr)
                        return 0
                    continue
                writer.writerow(row)
            f.flush()
            next_tick += args.interval
            now = time.time()
            if next_tick > now:
                time.sleep(next_tick - now)
            else:
                # Fell behind — skip ahead.
                next_tick = now
    return 0


if __name__ == "__main__":
    sys.exit(main())
