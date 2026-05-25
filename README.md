# deck-fleet

An orchestrator + per-deck executors + React console for running DAGs across
a lab deck fleet. Operators submit a DAG, watch its `deck_job`s flow across
the fleet in real time, and resolve any ambiguity a deck or the network
introduces. See [`DESIGN.md`](DESIGN.md) for the architecture rationale and
trade-offs.

## Prereqs

- Go 1.26+ (`go.mod` pins `go 1.26.0`)
- Node 22+ and npm
- GNU make
- macOS or Linux (process supervisor uses POSIX signals; not tested on Windows)

## Run it locally

Two terminals.

**Backend stack** (orchestrator + 4 executors via the dev supervisor):

```bash
make demo
```

- Supervisor on `:8090`, orchestrator on `:8080`, executors on `:9001..9004`.
- State in `./.demo-state/` (SQLite WAL files). `make demo` wipes it at start.
- Logs stream to stdout, prefixed by component label.

**Frontend**:

```bash
cd frontend
npm install
npm run dev
```

Open <http://localhost:5173>. The Vite dev server proxies `/api/*` and
`/executor/*` to `:8080`, so there's no CORS in dev.

## Submitting a DAG

Easiest path: open <http://localhost:5173/submit>. Six sample DAGs
(linear / parallel / fan-out / fan-in / mixed / same-deck) are bundled in
[`frontend/src/lib/samples/valid.ts`](frontend/src/lib/samples/valid.ts) and
selectable from the form. Submit, then jump to **Runs → \<run\>** to watch.

Or hit the API directly:

```bash
curl -X POST http://localhost:8080/api/runs \
  -H 'Content-Type: application/json' \
  -d '{
    "id":"demo-1",
    "deck_jobs":[
      {"id":"j1","deck_id":"deck-1","depends_on":[],
       "steps":[{"type":"prepare","description":"prep"}]},
      {"id":"j2","deck_id":"deck-2","depends_on":["j1"],
       "steps":[{"type":"measure","description":"read"}]}
    ]
  }'
```

## Reproducing the required failure scenarios

All three of the floor failures from the assignment are reproducible
without code changes. Pick a deck (`deck-2` below) and run the recipe
while a DAG that touches it is in flight.

### 1. Orchestrator restart

```bash
curl -X POST http://localhost:8080/api/admin/restart
```

The handler is at `(*Orchestrator).handleRestart` in
[`backend/internal/app/orchestrator.go`](backend/internal/app/orchestrator.go):
it acks `202 {"status":"restarting"}`, the runtime drains in-flight requests,
the process exits cleanly, and the supervisor respawns it. The console
flashes the `DEGRADED_MODE` banner for ~1–2s while polls fail; in-flight
`deck_job`s keep running on their executors and reconcile against the
event-log-rebuilt projection on the next executor event. No DAGs are lost.

### 2. Network flake between orchestrator and a deck

There are two directions; both are operator-relevant.

**Executor → orchestrator dropped (events stall, heartbeats stall):**

```bash
curl -X POST http://localhost:8080/api/decks/deck-2/chaos \
  -H 'Content-Type: application/json' -d '{"pause_egress": true}'
```

**Orchestrator → executor dropped (probes and aborts fail):**

```bash
curl -X POST http://localhost:8080/api/decks/deck-2/chaos \
  -H 'Content-Type: application/json' -d '{"pause_ingress": true}'
```

Watch the deck flip `HEALTHY → STALE → UNREACHABLE` in the fleet view
(~6 s for STALE, ~15 s for UNREACHABLE on the demo config). When the
attempt deadline fires while the link is down — formula is
`attempt_deadline_base + attempt_deadline_per_step × steps`, so on the
demo defaults (`20s + 2s/step`) that's 22 s for a 1-step job and 26 s
for a 3-step job — the deck_job lands in `AMBIGUOUS` and shows up in
the run-detail "Needs your attention" panel, where the operator either
resolves it (declare physical outcome) or cancels the run. Heal the
link with:

```bash
curl -X POST http://localhost:8080/api/decks/deck-2/chaos/reset
```

### 3. Executor hang or crash

**Hang the worker mid-step** (worker parks, heartbeats continue, attempt
deadline fires → `AMBIGUOUS` after `20s + 2s × steps` on demo defaults):

```bash
curl -X POST http://localhost:8080/api/decks/deck-2/chaos \
  -H 'Content-Type: application/json' -d '{"hang": true}'
```

Clear with `{"hang": false}` on the same endpoint, or `chaos/reset`.

**Crash the executor process** (one-shot `os.Exit` inside the executor;
supervisor respawns it within `respawn_delay`):

```bash
curl -X POST http://localhost:8080/api/decks/deck-2/chaos/crash
```

The reborn executor reads its outbox SQLite (
[`backend/sql/executor/queries/outbox.sql`](backend/sql/executor/queries/outbox.sql)),
re-delivers any in-flight events, and resumes polling. If the deadline
already fired the operator gets an `AMBIGUOUS` `deck_job` to resolve.

### Clearing chaos

```bash
curl -X POST http://localhost:8080/api/decks/deck-2/chaos/reset
```

The Settings → Fleet page also exposes chaos toggles per deck.

## Layout

```
api/         OpenAPI spec; Go + TS types are generated from this
backend/     Go: orchestrator, executor, supervisor; SQLite via sqlc
frontend/    React + Vite console; TanStack Query + react-router
config/      Demo / e2e config files for supervisor + orchestrator
analysis/    Personal investigation toolkit; not part of the deliverable
```

See [`DESIGN.md`](DESIGN.md) for architecture, state machines, failure
handling, and trade-offs.
