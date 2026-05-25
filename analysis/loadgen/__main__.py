"""
CLI entry for the synthetic-executor fleet simulator.

    uv run --project analysis python -m analysis.loadgen \\
        --decks 1000 --orch http://localhost:8080

Spawns N async tasks. Each impersonates one deck speaking the real
/executor/{poll,heartbeat,events} protocol. A shared aiohttp app on
--state-port serves /decks/<deck_id>/executor/{state,abort/...} so the
orchestrator's reconciler probes succeed.

For N > ~5000 the single CPython process becomes the bottleneck before
the orchestrator does; in that regime the sweep harness shards via
multiprocessing (see analysis/scale/sweep.py).
"""
from __future__ import annotations

import argparse
import asyncio
import logging
import signal
import sys
import time
from pathlib import Path

import httpx

try:
    import uvloop  # type: ignore
    uvloop.install()
except ImportError:
    pass

from .executor_task import run_deck
from .state_server import FleetState, start_server


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    ap = argparse.ArgumentParser()
    ap.add_argument("--decks", type=int, required=True,
                    help="number of simulated decks")
    ap.add_argument("--deck-prefix", default="load",
                    help='deck IDs are "<prefix>-<i>" for i in deck-start..deck-start+decks')
    ap.add_argument("--deck-start", type=int, default=1,
                    help="first deck index (so shards can split a global range)")
    ap.add_argument("--orch", default="http://localhost:8080",
                    help="orchestrator base URL")
    ap.add_argument("--poll-interval", type=float, default=0.5)
    ap.add_argument("--heartbeat-interval", type=float, default=2.0)
    ap.add_argument("--step-duration", type=float, default=0.2)
    ap.add_argument("--state-host", default="127.0.0.1")
    ap.add_argument("--state-port", type=int, default=19999)
    ap.add_argument("--out", type=Path, required=True,
                    help="full NDJSON path (e.g. runs/N=100/loadgen.ndjson). "
                         "Truncated on start so re-runs don't append.")
    ap.add_argument("--duration", type=float, default=0,
                    help="stop after N seconds (0 = run until SIGINT)")
    ap.add_argument("--log-level", default="info")
    ap.add_argument("--max-connections", type=int, default=200,
                    help="httpx connection pool ceiling per worker")
    return ap.parse_args(argv)


async def main_async(args: argparse.Namespace) -> int:
    logging.basicConfig(
        level=args.log_level.upper(),
        format="%(asctime)s loadgen %(levelname)s %(message)s",
    )
    log = logging.getLogger("loadgen")
    args.out.parent.mkdir(parents=True, exist_ok=True)
    ndjson = args.out
    # Truncate so re-runs don't append.
    ndjson.write_text("")

    fleet = FleetState()
    runner = await start_server(fleet, args.state_host, args.state_port)
    log.info("loadgen state server listening on %s:%d", args.state_host, args.state_port)

    stop = asyncio.Event()

    def _trigger_stop(_signum=None, _frame=None):
        if not stop.is_set():
            log.info("stop signal received, draining…")
            stop.set()

    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(sig, _trigger_stop)
        except NotImplementedError:
            pass  # Windows fallback

    limits = httpx.Limits(
        max_connections=args.max_connections,
        max_keepalive_connections=args.max_connections,
    )
    async with httpx.AsyncClient(
        base_url=args.orch, timeout=10.0, limits=limits, http2=False,
    ) as client:
        # Stagger deck spawns so 10k registers don't all race in one tick.
        tasks: list[asyncio.Task] = []
        stagger = min(0.005, 1.0 / max(args.decks, 1))
        for i in range(args.decks):
            deck_id = f"{args.deck_prefix}-{args.deck_start + i}"
            advertise = (
                f"http://{args.state_host}:{args.state_port}/decks/{deck_id}"
            )
            tasks.append(asyncio.create_task(
                run_deck(client, fleet, deck_id, advertise,
                         args.poll_interval, args.heartbeat_interval,
                         args.step_duration, ndjson, stop),
                name=f"deck:{deck_id}",
            ))
            if stagger > 0:
                await asyncio.sleep(stagger)
        log.info("spawned %d deck tasks", len(tasks))

        if args.duration > 0:
            try:
                await asyncio.wait_for(stop.wait(), timeout=args.duration)
            except asyncio.TimeoutError:
                _trigger_stop()
        else:
            await stop.wait()

        # Give tasks a moment to drain their current iteration.
        for t in tasks:
            t.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)

    await runner.cleanup()
    log.info("loadgen exited cleanly")
    return 0


def main() -> int:
    args = parse_args()
    return asyncio.run(main_async(args))


if __name__ == "__main__":
    sys.exit(main())
