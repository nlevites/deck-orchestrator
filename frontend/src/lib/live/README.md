# Live polling layer

The React console is a projection of the orchestrator's event log. This
module owns that projection: it polls `/api/state` (and
`/api/runs/{id}/state` on the run-detail screen) on a 1s cadence, then
applies each response to the TanStack Query cache.

## State delivery model

Two distinct categories of orchestrator state need two distinct
delivery mechanisms:

| Kind of state | Examples | How it lands in the cache |
|---|---|---|
| Semantic transitions | `RUN_SUBMITTED`, `JOB_COMPLETED`, `DECK_HEALTH_CHANGED` | Events table → applied via per-kind reducers (`reducers/*.ts` + `apply-event.ts`) |
| Continuous scalars | `decks[*].last_heartbeat_at` | Bootstrap ships full `decks`; delta polls ship `decks_delta` (touched-since rows), merged by id into the existing cache |

Conflating these — pretending heartbeats are events, or rebuilding the
world periodically as a freshness mechanism — is a Band-Aid. The
events table is the audit log; flooding it with 4 events/sec/deck of
heartbeat noise would drown the operationally meaningful signal.

`/api/state` returns the full `decks[]` slice on bootstrap and a
`decks_delta` slice on subsequent polls. The delta is the union of
(a) decks referenced by any event since `since_seq` and (b) decks
whose `last_heartbeat_at` advanced inside the freshness window
(~5s, orchestrator-chosen). Steady-state cost is bounded by
**activity** rather than fleet size, so a 10 000-deck fleet costs
roughly the same per poll as a 100-deck fleet when both are idle.

## Mental model

```
                 1s tick
fetchState() ────────────► /api/state?since_seq=N
       │
       ├─ snap.decks ─────► setQueryData(decks, snap.decks)   // EVERY response
       │                                                       (authoritative
       │                                                        last_heartbeat_at)
       │
       ├─ bootstrap?  ──► setQueryData(runs, snap.runs)
       │                  setEventsCache(snap.events)
       │
       └─ delta?       ──► for each event, applyEvent(qc, event)
                            │
                            ├─ reducer → setQueryData on touched cache(s)
                            ├─ reducer returns false → seqRef = 0 (re-bootstrap)
                            ├─ unknown kind → seqRef = 0 (re-bootstrap)
                            └─ gap in seq → seqRef = 0 (re-bootstrap)
```

Pages and components never call `fetch` for read traffic; they
`useQuery({ queryKey: apiKeys.runs, queryFn: throw, staleTime: Infinity })`
and read from the cache the live hook populates.

## Drift safety nets

Four triggers, one fallback. All four set `seqRef = 0`, causing the
next tick to re-bootstrap and pick up whatever was missed.

1. **Gap detection** (global only). If `event.seq !== last + 1`, an
   event was dropped — re-bootstrap.
2. **Unknown event kind.** If the dispatcher has no reducer for a
   kind, re-bootstrap. The bootstrap response replaces every entity
   cache, so whatever the missing reducer would have done is healed.
3. **Reducer requests rebootstrap.** A reducer can return `false` to
   say "I handled this event, but my cache projection isn't faithful
   from the payload alone — please rebootstrap." `RUN_SUBMITTED` uses
   this because RunSummary needs `deck_jobs_summary` + version, which
   the event payload doesn't carry.
4. **Long-tail periodic re-bootstrap (5min).** Defense in depth for
   events-cache drift on long-lived tabs. NOT the primary freshness
   mechanism — the always-applied `decks` slice is. The 5min number
   exists only because "rebooting once an hour" felt too far apart;
   if we ever find a real reducer bug it'll fire faster than this
   safety net catches it.

The run-scoped hook skips gap detection because the server filters
events by `run_id`, so seq values are sparse by design.

## Adding a new event kind

1. Add the new kind to `EventKind` in `api/openapi.yaml` and regen.
2. Add the new kind to `backend/internal/eventlog/kinds.go` and the
   `events.kind` CHECK constraint in the migration.
3. Write the reducer:
   - In `reducers/<scope>.ts`, define `applyXxx(qc, event)`.
   - Read whichever fields you need from `event.payload` and call the
     helpers in `helpers.ts` to write the appropriate cache slice(s).
   - If the event payload doesn't carry enough info to faithfully
     project the cache row, return `false` instead of `void` to ask
     the dispatcher for a rebootstrap.
4. Register the reducer in `apply-event.ts`'s `REDUCERS` table.

If you skip step 4, the dispatcher's default branch will fire and the
UI will fall back to bootstrap. Visible but cheap. We log unknown
kinds to the console in dev to make the omission loud.

## What this is NOT

- Not a substitute for the mutation REST endpoints. Mutations stay on
  their own `lib/api/*.ts` modules and use `useMutation` directly; the
  live stream picks up their server-side effects on the next tick.
- Not authoritative for entity state when the page first mounts. There
  is a brief loading window between mount and the first bootstrap
  response landing in the cache. Pages render skeletons during this
  window.
- Not free at scale. Each `/api/state` response carries the events
  delta plus, on first poll, a full snapshot. Once `decks_delta` is
  in play, steady-state per-poll cost is dominated by event volume
  rather than fleet size. The first-tab-of-the-day still pays the
  bootstrap cost (a few KB at 100 decks; grows linearly with fleet);
  long-lived tabs trend toward "only changed things ship".
