"""
aiohttp app serving /executor/state and /executor/abort/{attempt_id} for
all simulated decks on one shared listener. The orchestrator's reconciler
dials these on suspicion of drift; without them, every drifted attempt
silently goes UNREACHABLE.

Per-deck advertise URLs use path-prefixing: deck-N advertises
   http://<host>:<port>/decks/deck-N
and the server strips the prefix when routing the canonical /executor/*
paths underneath.
"""
from __future__ import annotations

import time
from dataclasses import dataclass, field
from typing import Any

from aiohttp import web


@dataclass
class AttemptRecord:
    attempt_id: str
    state: str = "IN_PROGRESS"  # or RECEIVED|COMPLETED|FAILED
    received_at: float = field(default_factory=time.time)
    started_at: float | None = None
    terminal_at: float | None = None
    abort_requested: bool = False


@dataclass
class DeckState:
    deck_id: str
    current_attempt_id: str | None = None
    attempts: dict[str, AttemptRecord] = field(default_factory=dict)

    def record(self, attempt_id: str) -> AttemptRecord:
        rec = self.attempts.get(attempt_id)
        if rec is None:
            rec = AttemptRecord(attempt_id=attempt_id, started_at=time.time())
            self.attempts[attempt_id] = rec
        self.current_attempt_id = attempt_id
        return rec

    def complete(self, attempt_id: str, ok: bool) -> None:
        rec = self.attempts.get(attempt_id)
        if rec is None:
            return
        rec.state = "COMPLETED" if ok else "FAILED"
        rec.terminal_at = time.time()
        if self.current_attempt_id == attempt_id:
            self.current_attempt_id = None


class FleetState:
    """Thread-safe per-deck record store; the aiohttp event loop owns the data."""

    def __init__(self) -> None:
        self.decks: dict[str, DeckState] = {}

    def ensure(self, deck_id: str) -> DeckState:
        d = self.decks.get(deck_id)
        if d is None:
            d = DeckState(deck_id=deck_id)
            self.decks[deck_id] = d
        return d


def build_app(fleet: FleetState) -> web.Application:
    routes = web.RouteTableDef()

    @routes.get("/decks/{deck_id}/executor/state")
    async def get_state(req: web.Request) -> web.Response:
        deck_id = req.match_info["deck_id"]
        attempt_id = req.query.get("attempt_id")
        deck = fleet.decks.get(deck_id)
        if deck is None:
            return web.json_response({"error": {"code": "UNKNOWN", "message": "no such deck"}},
                                     status=404)
        if attempt_id:
            rec = deck.attempts.get(attempt_id)
            if rec is None:
                return web.json_response({"error": {"code": "UNKNOWN_ATTEMPT",
                                                     "message": "no such attempt"}},
                                          status=404)
            return web.json_response(_attempt_to_dict(rec))
        # Overall.
        out = {
            "deck_id": deck_id,
            "current_attempt_id": deck.current_attempt_id,
            "recent_attempts": [_attempt_to_dict(r) for r in
                                sorted(deck.attempts.values(),
                                       key=lambda r: r.received_at,
                                       reverse=True)[:10]],
        }
        return web.json_response(out)

    @routes.post("/decks/{deck_id}/executor/abort/{attempt_id}")
    async def post_abort(req: web.Request) -> web.Response:
        deck_id = req.match_info["deck_id"]
        attempt_id = req.match_info["attempt_id"]
        deck = fleet.decks.get(deck_id)
        if deck is None or attempt_id not in deck.attempts:
            return web.json_response({"error": {"code": "UNKNOWN_ATTEMPT",
                                                 "message": "no such attempt"}},
                                      status=404)
        rec = deck.attempts[attempt_id]
        if rec.terminal_at is not None:
            return web.json_response({
                "status": "already_terminal",
                "attempt_id": attempt_id,
                "final_state": rec.state,
            })
        rec.abort_requested = True
        return web.json_response({
            "status": "abort_requested",
            "attempt_id": attempt_id,
        })

    @routes.get("/health")
    async def health(_: web.Request) -> web.Response:
        return web.json_response({"status": "ok", "decks": len(fleet.decks)})

    app = web.Application()
    app.add_routes(routes)
    return app


def _attempt_to_dict(rec: AttemptRecord) -> dict[str, Any]:
    from datetime import datetime, timezone
    def iso(ts: float | None) -> str | None:
        if ts is None:
            return None
        return datetime.fromtimestamp(ts, timezone.utc).isoformat()
    return {
        "attempt_id": rec.attempt_id,
        "state": rec.state,
        "received_at": iso(rec.received_at) or "",
        "started_at": iso(rec.started_at),
        "terminal_at": iso(rec.terminal_at),
    }


async def start_server(fleet: FleetState, host: str, port: int) -> web.AppRunner:
    app = build_app(fleet)
    runner = web.AppRunner(app, access_log=None)
    await runner.setup()
    site = web.TCPSite(runner, host, port)
    await site.start()
    return runner
