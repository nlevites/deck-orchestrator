"""
Pillar 3c sweep harness.

For each N in --sizes, boots a *bare* orchestrator (no supervisor, no
executors — the synthetic loadgen does the executor role), launches the
loadgen with --decks=N (sharded via --procs above 5000), drives DAG
submissions covering all decks, watches until completion or timeout,
then tears everything down and persists artifacts.

Per-N artifacts land at analysis/scale/runs/N=<N>/:
    orchestrator.log         (orchestrator JSON NDJSON: per-request middleware rows)
    orchestrator.db          (final SQLite file — kept for db-growth analysis)
    loadgen.ndjson           (per-request log from loadgen; sharded -> loadgen-0.ndjson ...)
    process-samples.csv      (1Hz psutil rows for orchestrator + each loadgen)
    runs.json                (final state of all submitted DAGs)
    sweep-meta.json          (start/end ts, kill reason, args)

    uv run --project analysis python -m analysis.scale.sweep \\
        --sizes 10,100,1000 --duration 90
"""
from __future__ import annotations

import argparse
import asyncio
import contextlib
import json
import os
import shutil
import signal
import socket
import subprocess
import sys
import time
import uuid
from collections import Counter
from pathlib import Path

import httpx


REPO_ROOT = Path(__file__).resolve().parents[2]
RUNS_DIR = REPO_ROOT / "analysis" / "scale" / "runs"
DEFAULT_SIZES = [10, 100, 500, 1000, 5000, 10_000]
ORCH_BIN = REPO_ROOT / "backend" / "bin" / "orchestrator"


def parse_args() -> argparse.Namespace:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--sizes",
        default=",".join(str(n) for n in DEFAULT_SIZES),
        help="comma-separated fleet sizes",
    )
    ap.add_argument("--duration", type=float, default=120,
                    help="seconds to run loadgen + DAG submission per N")
    ap.add_argument("--orch-port-base", type=int, default=21000)
    ap.add_argument("--loadgen-state-port", type=int, default=29999)
    ap.add_argument("--shard-threshold", type=int, default=100,
                    help="N at and above which we shard loadgen across procs")
    ap.add_argument("--shard-size", type=int, default=100,
                    help="max decks per loadgen shard. Lower = more loadgen "
                         "procs, less self-saturation. 100 leaves comfortable "
                         "headroom under one CPython process even when "
                         "stale_threshold is tight; raise it if RAM is an "
                         "issue at very large N (100 shards × ~80MiB = ~8GB "
                         "at N=10000).")
    ap.add_argument("--loadgen-max-conn", type=int, default=500,
                    help="httpx connection pool per loadgen shard")
    ap.add_argument("--keep-db", action="store_true",
                    help="leave orchestrator.db in place after each run")
    return ap.parse_args()


def free_port(start: int) -> int:
    for p in range(start, start + 500):
        with socket.socket() as s:
            try:
                s.bind(("127.0.0.1", p))
                return p
            except OSError:
                continue
    raise RuntimeError(f"no free port near {start}")


@contextlib.contextmanager
def orchestrator(state_dir: Path, port: int, fleet_size: int):
    """Boot a bare orchestrator with JSON logs; yields (Popen, db_path)."""
    if not ORCH_BIN.exists():
        raise SystemExit(f"orchestrator binary missing at {ORCH_BIN}; run `make build`")
    state_dir.mkdir(parents=True, exist_ok=True)
    log_path = state_dir / "orchestrator.log"
    db_path = state_dir / "orchestrator.db"

    env = os.environ.copy()
    env.update({
        "HTTP_ADDR": f":{port}",
        "DB_PATH": str(db_path),
        "LOG_FORMAT": "json",
        "LOG_LEVEL": "info",
        "FLEET_SIZE": str(fleet_size),
        # CORS doesn't matter here; loadgen + sweep are server-to-server.
    })
    # Pass the wire/orchestrator.analysis.yaml as a base so timeouts etc.
    # match the demo/wire experience. Env overrides override yaml.
    base_cfg = REPO_ROOT / "analysis" / "wire" / "orchestrator.analysis.yaml"
    args = [str(ORCH_BIN), "-config", str(base_cfg)] if base_cfg.exists() else [str(ORCH_BIN)]

    log_fh = log_path.open("wb")
    proc = subprocess.Popen(
        args, env=env, stdout=log_fh, stderr=log_fh,
        start_new_session=True,  # so signal goes to the orchestrator only
    )
    try:
        _wait_for_orchestrator(port, deadline=20)
        yield proc, db_path
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=8)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=4)
        log_fh.close()


def _wait_for_orchestrator(port: int, deadline: float) -> None:
    end = time.time() + deadline
    while time.time() < end:
        try:
            with httpx.Client(timeout=1.0) as c:
                r = c.get(f"http://127.0.0.1:{port}/health")
                if r.status_code == 200:
                    return
        except httpx.HTTPError:
            pass
        time.sleep(0.2)
    raise RuntimeError(f"orchestrator never became healthy on :{port}")


@contextlib.contextmanager
def loadgen_shards(
    out_dir: Path, orch_url: str, decks: int, state_port: int,
    shard_threshold: int, shard_size: int, max_connections: int = 500,
):
    """
    Spawn one loadgen process for fleets < shard_threshold, else
    ceil(decks / shard_size) processes each handling a deck-id slice.
    Yields list[Popen].
    """
    out_dir.mkdir(parents=True, exist_ok=True)
    procs: list[subprocess.Popen] = []
    # Each shard owns a [start, end) slice of the deck-1..deck-N range.
    # Always use prefix `deck` so heartbeats land on the orchestrator's
    # seeded fleet slots regardless of sharding.
    if decks < shard_threshold:
        plan = [(1, decks + 1, state_port, out_dir / "loadgen.ndjson")]
    else:
        n_shards = (decks + shard_size - 1) // shard_size
        plan = []
        for i in range(n_shards):
            start = i * shard_size + 1
            end = min((i + 1) * shard_size + 1, decks + 1)
            plan.append((start, end, state_port + i, out_dir / f"loadgen-{i}.ndjson"))

    project_dir = REPO_ROOT / "analysis"
    for start, end, sport, out_path in plan:
        n = end - start
        if n <= 0:
            continue
        out_path.parent.mkdir(parents=True, exist_ok=True)
        cmd = [
            "uv", "run", "--project", str(project_dir),
            "python", "-m", "analysis.loadgen",
            "--decks", str(n),
            "--deck-prefix", "deck",
            "--deck-start", str(start),
            "--orch", orch_url,
            "--state-port", str(sport),
            "--out", str(out_path),
            "--max-connections", str(max_connections),
        ]
        log_path = out_path.with_suffix(".stderr.log")
        with log_path.open("wb") as fh:
            p = subprocess.Popen(cmd, stdout=fh, stderr=fh,
                                  start_new_session=True)
        procs.append(p)
        # Stagger shard starts so they don't race on registration.
        time.sleep(0.5)
    try:
        yield procs
    finally:
        for p in procs:
            try:
                os.killpg(os.getpgid(p.pid), signal.SIGTERM)
            except (ProcessLookupError, PermissionError):
                pass
        for p in procs:
            try:
                p.wait(timeout=10)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(os.getpgid(p.pid), signal.SIGKILL)
                except (ProcessLookupError, PermissionError):
                    pass
                p.wait(timeout=4)


def wait_for_decks(orch_url: str, target: int, deadline: float) -> int:
    end = time.time() + deadline
    last = 0
    with httpx.Client(timeout=5.0) as c:
        while time.time() < end:
            try:
                r = c.get(f"{orch_url}/api/decks")
                if r.status_code == 200:
                    body = r.json()
                    last = len(body.get("decks", []))
                    if last >= target:
                        return last
            except httpx.HTTPError:
                pass
            time.sleep(2.0)
    return last


def submit_fanout_dags(orch_url: str, decks: int, n_dags: int) -> list[str]:
    """Submit n_dags DAGs whose deck_jobs collectively cover all decks."""
    if decks <= 0 or n_dags <= 0:
        return []
    submitted: list[str] = []
    jobs_per_dag = max(1, decks // n_dags)
    with httpx.Client(timeout=10.0) as c:
        for i in range(n_dags):
            run_id = f"sweep-{i}-{uuid.uuid4().hex[:6]}"
            lo = i * jobs_per_dag
            hi = (i + 1) * jobs_per_dag if i < n_dags - 1 else decks
            deck_jobs = []
            for j in range(lo, hi):
                deck_jobs.append({
                    "id": f"j{j}",
                    "deck_id": f"deck-{j + 1}",
                    "depends_on": [],
                    "steps": [{"type": "transfer", "description": "sweep step"}],
                })
            body = {"id": run_id, "deck_jobs": deck_jobs}
            r = c.post(f"{orch_url}/api/runs", json=body)
            if 200 <= r.status_code < 300:
                submitted.append(run_id)
            else:
                print(f"  submit {run_id} -> {r.status_code}: {r.text[:120]}",
                      file=sys.stderr)
    return submitted


def snapshot_runs_exact(orch_url: str, submitted: list[str], out: Path,
                        concurrency: int = 50) -> dict:
    """
    Fetch GET /api/runs/{id} for every submitted run_id and persist an
    exact status breakdown — the old `/api/runs?limit=50` sampled only
    the last-submitted runs (worst soak time), which biased completion
    downward as N grew.
    """
    if not submitted:
        out.write_text(json.dumps({"submitted_total": 0, "by_status": {},
                                    "by_id": {}}))
        return {}

    async def _go() -> dict[str, str]:
        sem = asyncio.Semaphore(concurrency)
        results: dict[str, str] = {}
        async with httpx.AsyncClient(base_url=orch_url, timeout=15.0) as cli:
            async def fetch(rid: str) -> None:
                async with sem:
                    try:
                        r = await cli.get(f"/api/runs/{rid}")
                        if r.status_code == 200:
                            results[rid] = r.json().get("status", "UNKNOWN")
                        else:
                            results[rid] = f"HTTP_{r.status_code}"
                    except Exception as e:
                        results[rid] = f"ERR_{type(e).__name__}"
            await asyncio.gather(*(fetch(r) for r in submitted))
        return results

    by_id = asyncio.run(_go())
    counts = Counter(by_id.values())
    payload = {
        "submitted_total": len(submitted),
        "by_status": dict(counts),
        "by_id": by_id,
    }
    out.write_text(json.dumps(payload, indent=2))
    return payload


def run_one(n: int, args: argparse.Namespace) -> dict:
    run_dir = RUNS_DIR / f"N={n}"
    if run_dir.exists():
        shutil.rmtree(run_dir)
    run_dir.mkdir(parents=True, exist_ok=True)

    orch_port = free_port(args.orch_port_base)
    orch_url = f"http://127.0.0.1:{orch_port}"
    state_port = free_port(args.loadgen_state_port)

    meta = {
        "n": n, "orch_port": orch_port,
        "state_port_start": state_port, "duration": args.duration,
        "start_ts": time.time(),
    }
    print(f"\n=== N={n} ===  orch={orch_url}", flush=True)

    submitted_ids: list[str] = []
    with orchestrator(run_dir, orch_port, fleet_size=n) as (orch_proc, db_path):
        with loadgen_shards(run_dir, orch_url, n, state_port,
                              args.shard_threshold, args.shard_size,
                              max_connections=args.loadgen_max_conn) as lgs:
            # Start watcher.
            watcher_args = [
                "uv", "run", "--project", str(REPO_ROOT / "analysis"),
                "python", "-m", "analysis.scale.watch",
                "--orch-pid", str(orch_proc.pid),
                "--db", str(db_path),
                "--out", str(run_dir / "process-samples.csv"),
                "--duration", str(args.duration + 60),
            ]
            for p in lgs:
                watcher_args += ["--loadgen-pid", str(p.pid)]
            watcher = subprocess.Popen(watcher_args)
            try:
                registered = wait_for_decks(orch_url, n, deadline=120)
                print(f"  registered {registered}/{n} decks")
                meta["registered_decks"] = registered

                # Submit DAGs once a healthy chunk of decks is up.
                if registered > 0:
                    n_dags = max(1, registered // 3)
                    submitted_ids = submit_fanout_dags(orch_url, registered, n_dags)
                    meta["submitted_runs"] = len(submitted_ids)
                    print(f"  submitted {len(submitted_ids)} DAGs")

                # Soak for --duration to let runs complete + steady-state form.
                end = time.time() + args.duration
                while time.time() < end:
                    time.sleep(5)
                    if orch_proc.poll() is not None:
                        meta["orch_exited_early"] = True
                        break
            finally:
                snapshot = snapshot_runs_exact(
                    orch_url, submitted_ids, run_dir / "runs.json")
                if snapshot:
                    meta["final_by_status"] = snapshot["by_status"]
                    print(f"  final status: {snapshot['by_status']}")
                watcher.terminate()
                try:
                    watcher.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    watcher.kill()

    meta["end_ts"] = time.time()
    (run_dir / "sweep-meta.json").write_text(json.dumps(meta, indent=2))
    print(f"  done. artifacts in {run_dir}")
    return meta


def main() -> int:
    args = parse_args()
    sizes = [int(s) for s in args.sizes.split(",")]
    RUNS_DIR.mkdir(parents=True, exist_ok=True)
    all_meta = []
    for n in sizes:
        try:
            all_meta.append(run_one(n, args))
        except Exception as e:
            print(f"  FAILED at N={n}: {e}", file=sys.stderr)
            all_meta.append({"n": n, "failed": str(e)})
    summary = RUNS_DIR / "sweep-summary.json"
    summary.write_text(json.dumps(all_meta, indent=2))
    print(f"\nwrote {summary}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
