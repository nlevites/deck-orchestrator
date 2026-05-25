"""
One async task = one simulated deck. Speaks the real /executor/* protocol
against the orchestrator.

Loop per deck (mirrors backend/internal/executor/worker behavior):
    spawn heartbeat loop (cadence = heartbeat_interval)
    spawn poll loop:
        GET /executor/poll?deck_id=...
        if 200:
            POST /executor/events kind=RUNNING
            sleep step_duration * len(steps)  # simulated work
            POST /executor/events kind=COMPLETED
"""
from __future__ import annotations

import asyncio
import json
import logging
import random
import time
from datetime import datetime, timezone
from pathlib import Path

import httpx

from .state_server import FleetState

log = logging.getLogger("loadgen")


def _iso(ts: float | None = None) -> str:
    if ts is None:
        ts = time.time()
    return datetime.fromtimestamp(ts, timezone.utc).isoformat()


async def _emit(ndjson: Path | None, entry: dict) -> None:
    if ndjson is None:
        return
    # Append-only NDJSON. Using sync open per write keeps lines atomic
    # without an aiofile dep; cost is tiny relative to httpx round-trip.
    with ndjson.open("a") as f:
        f.write(json.dumps(entry) + "\n")


async def heartbeat_loop(
    client: httpx.AsyncClient,
    deck_id: str,
    advertise_url: str,
    interval: float,
    ndjson: Path | None,
    deck: "object",  # state_server.DeckState
    stop: asyncio.Event,
) -> None:
    while not stop.is_set():
        t0 = time.time()
        body = {
            "deck_id": deck_id,
            "endpoint_url": advertise_url,
            "current_attempt_id": getattr(deck, "current_attempt_id", None),
        }
        try:
            r = await client.post("/executor/heartbeat", json=body)
            await _emit(ndjson, {
                "ts": t0, "deck_id": deck_id, "op": "heartbeat",
                "status": r.status_code, "duration": time.time() - t0,
                "bytes_out": len(r.content),
            })
        except Exception as e:
            await _emit(ndjson, {
                "ts": t0, "deck_id": deck_id, "op": "heartbeat",
                "status": 0, "error": str(e), "duration": time.time() - t0,
            })
        try:
            await asyncio.wait_for(stop.wait(), timeout=interval)
        except asyncio.TimeoutError:
            pass


async def poll_loop(
    client: httpx.AsyncClient,
    deck_id: str,
    poll_interval: float,
    step_duration: float,
    ndjson: Path | None,
    deck: "object",  # state_server.DeckState
    stop: asyncio.Event,
) -> None:
    while not stop.is_set():
        t0 = time.time()
        try:
            r = await client.get("/executor/poll", params={"deck_id": deck_id})
            await _emit(ndjson, {
                "ts": t0, "deck_id": deck_id, "op": "poll",
                "status": r.status_code, "duration": time.time() - t0,
                "bytes_in": len(r.content),
            })
            if r.status_code == 200:
                disp = r.json()
                attempt_id = disp["attempt_id"]
                steps = disp.get("steps", [])
                rec = deck.record(attempt_id)  # type: ignore[attr-defined]

                # RUNNING event
                await _post_event(client, ndjson, deck_id, attempt_id, "RUNNING", {})

                # Simulate per-step work; STEP_COMPLETED at each.
                for i in range(len(steps)):
                    if stop.is_set() or rec.abort_requested:
                        break
                    await asyncio.sleep(step_duration)
                    await _post_event(client, ndjson, deck_id, attempt_id,
                                       "STEP_COMPLETED",
                                       {"step": i + 1, "total": len(steps)})

                if rec.abort_requested:
                    await _post_event(client, ndjson, deck_id, attempt_id, "FAILED",
                                       {"error": "aborted"})
                    deck.complete(attempt_id, ok=False)  # type: ignore[attr-defined]
                else:
                    await _post_event(client, ndjson, deck_id, attempt_id,
                                       "COMPLETED", {"result": {"loadgen": True}})
                    deck.complete(attempt_id, ok=True)  # type: ignore[attr-defined]
                continue  # no extra sleep — go straight back to polling
        except Exception as e:
            await _emit(ndjson, {
                "ts": t0, "deck_id": deck_id, "op": "poll",
                "status": 0, "error": str(e), "duration": time.time() - t0,
            })
        # Add small jitter so 10k decks don't all poll on the exact same tick.
        jitter = random.uniform(0, poll_interval * 0.1)
        try:
            await asyncio.wait_for(stop.wait(), timeout=poll_interval + jitter)
        except asyncio.TimeoutError:
            pass


async def _post_event(
    client: httpx.AsyncClient,
    ndjson: Path | None,
    deck_id: str,
    attempt_id: str,
    kind: str,
    payload: dict,
) -> None:
    body = {
        "attempt_id": attempt_id,
        "kind": kind,
        "occurred_at": _iso(),
        "payload": payload or {},
    }
    t0 = time.time()
    try:
        r = await client.post("/executor/events", json=body)
        await _emit(ndjson, {
            "ts": t0, "deck_id": deck_id, "op": "event", "kind": kind,
            "attempt_id": attempt_id, "status": r.status_code,
            "duration": time.time() - t0, "bytes_out": len(r.content),
        })
    except Exception as e:
        await _emit(ndjson, {
            "ts": t0, "deck_id": deck_id, "op": "event", "kind": kind,
            "attempt_id": attempt_id, "status": 0, "error": str(e),
            "duration": time.time() - t0,
        })


async def run_deck(
    client: httpx.AsyncClient,
    fleet: FleetState,
    deck_id: str,
    advertise_url: str,
    poll_interval: float,
    heartbeat_interval: float,
    step_duration: float,
    ndjson: Path | None,
    stop: asyncio.Event,
) -> None:
    deck = fleet.ensure(deck_id)
    await asyncio.gather(
        heartbeat_loop(client, deck_id, advertise_url,
                       heartbeat_interval, ndjson, deck, stop),
        poll_loop(client, deck_id, poll_interval, step_duration,
                  ndjson, deck, stop),
    )
