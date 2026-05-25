# E2E Case Corpus

This document maps every Playwright spec to the assignment-doc requirement
it exercises, the DAG it submits, the chaos / admin primitives it uses,
and the UI + backend assertions it makes. Use it as a debrief reference:
"show me how you tested X" → look up X here, then open the spec file.

The framework spawns a hermetic stack on dedicated ports (orchestrator
:18080, executors :19001-:19004, Vite :15173) under
`${TMPDIR}/dfo-e2e/`. See `frontend/e2e/playwright.config.ts` (the
`webServer` entry spawns the supervisor with `-clean`). Compressed
timings live in `config/e2e.yaml`:
step_duration 200ms, heartbeat 250ms, stale 1s, ambiguous_deadline 3s,
attempt_deadline 4s.

Reading guide:
- **Assignment hook** quotes or paraphrases the line in
  `deck_fleet_orchestrator_assignment.md` that motivates the case.
- **DAG** is the topology the test submits (programmatic, unique-ID per
  test via `runIdFor(testInfo, kind)`).
- **Chaos / admin** is what the test injects to drive the scenario.
- **UI assertion** is what the operator sees in their browser.
- **Backend assertion** is what we verify on the orchestrator's state /
  event log to prove no silent loss / no duplicate dispatch.

---

## 1. DAG topology (happy-path correctness)

Sample DAGs in the Submit page picker (frontend/src/lib/samples/) cover
six topologies. These specs prove the orchestrator correctly schedules
each, and the UI renders progress in real time. They are the regression
net: every other spec assumes happy-path works.

| Spec | DAG | UI assertion | Backend assertion |
| --- | --- | --- | --- |
| [topology/linear.spec.ts](specs/topology/linear.spec.ts) | 3-job linear chain | all 3 job nodes flip COMPLETED in order | run.status = COMPLETED |
| [topology/parallel.spec.ts](specs/topology/parallel.spec.ts) | 2 independent jobs | both tracks in `DISPATCHED/RUNNING` simultaneously at some tick | both jobs COMPLETED |
| [topology/fan-out.spec.ts](specs/topology/fan-out.spec.ts) | 1 source → 3 branches | source flips COMPLETED before any branch leaves PENDING; all 4 nodes COMPLETED | dependency order respected; no early dispatch |
| [topology/fan-in.spec.ts](specs/topology/fan-in.spec.ts) | 3 extracts → 1 pool | all 4 nodes COMPLETED | pool has exactly ONE attempt — proves no duplicate scheduling at the join |
| [topology/mixed.spec.ts](specs/topology/mixed.spec.ts) | fan-out + fan-in | prep → warm+cool → compare flow visible | run COMPLETED |
| [topology/same-deck.spec.ts](specs/topology/same-deck.spec.ts) | two jobs serialize on deck-3 | run COMPLETED | process-a and process-b never co-occupy deck-3 across the run's lifetime |
| [_smoke.spec.ts](specs/_smoke.spec.ts) | none (no submit) | 4 deck cards render on `/fleet/grid` from the live `/api/state` bootstrap | — |

**Assignment hook:** "Three components. Orchestrator. Accepts DAGs,
dispatches deck_jobs, tracks state at the deck_job and DAG level."

---

## 2. Required failure modes (assignment doc, §"Failures the system will see")

The three named failure modes plus executor crash (a natural extension of
"executor hangs or crashes"). Every case verifies the invariants from the
assignment: **operator-visible terminal state**, **no silent loss**, **no
duplicate physical dispatch**.

### 2a. Orchestrator process restarts mid-run

**Spec:** [failure/orchestrator-restart.spec.ts](specs/failure/orchestrator-restart.spec.ts)

**Assignment hook:** "The orchestrator process restarts."

**DAG:** linear (3 jobs).

**Chaos / admin:** `POST /api/admin/restart` (the operator-facing
"Restart gracefully" button on the Settings > Fleet Management page,
proxied via the supervisor sidecar; pre-supervisor this lived in the
header). The entrypoint observes `RestartCh`, cancels its
internal runCtx so the Liveness Monitor / Reconciler / AbortDialer drop
out of their loops, drains the HTTP server within `shutdown_grace`, and
exits 0. The supervisor respawns the process within ~1 second.

**UI assertion:** ConnectionBanner is hidden after recovery (state has
returned to OK).

**Backend assertion:** Every job has exactly ONE attempt — the
startup-reconcile path did NOT spawn duplicates.

### 2b. Network flake between orchestrator and deck

**Spec:** [failure/network-flake.spec.ts](specs/failure/network-flake.spec.ts)

**Assignment hook:** "The network between orchestrator and a deck flakes
for seconds or minutes at a time."

**DAG:** linear (3 jobs).

**Chaos / admin:** `chaos patch deck-2 { pause_egress: true }` to make
deck-2's outbound HTTP fail. Cleared mid-run.

**UI assertion:** Run reaches COMPLETED (implicit — the page would have
shown the run frozen mid-progress while egress was paused).

**Backend assertion:** Run COMPLETED; every job has exactly ONE attempt.
The orchestrator did NOT re-dispatch j2 during the flake — the
executor's outbox replayed and the orchestrator accepted the queued
events idempotently.

### 2c. Executor hangs

**Spec:** [failure/executor-hang.spec.ts](specs/failure/executor-hang.spec.ts)

**Assignment hook:** "An executor hangs or crashes."

**DAG:** linear (3 jobs); j2 lands on deck-2.

**Chaos / admin:** `chaos patch deck-2 { hang: true }` before submit. The
worker reaches the first step boundary, observes `chaosHang() == true`,
and blocks on `<-ctx.Done()` until process restart.

**UI assertion:**
- Job node `j2` flips to `AMBIGUOUS` status in the DAG viewer.
- The fleet dashboard's `AmbiguousRunsBanner` ("Runs needing operator
  resolution") becomes visible.

**Backend assertion:** j2.status = AMBIGUOUS (escalated via the
`attempt_deadline` + reconciler path, reason `DEADLINE_EXCEEDED`).

### 2d. Executor process crashes

**Spec:** [failure/executor-crash.spec.ts](specs/failure/executor-crash.spec.ts)

**Assignment hook:** "An executor hangs or crashes."

**DAG:** linear (3 jobs); j2 lands on deck-2.

**Chaos / admin:** `chaos patch hang: true` to get j2 to a RUNNING+hung
state recorded at the orchestrator, then `chaos crash` to force
`os.Exit(1)` on deck-2. Supervisor respawns within ~1s.

**UI assertion:** Run reaches a terminal-or-resolvable state (Failed or
Cancelled visible). No "stuck forever" silent loss.

**Backend assertion:** j2 lands in one of `{COMPLETED, FAILED, AMBIGUOUS}`
— ALL three are operator-correct depending on whether the outbox replay
or the reconciler escalation won. In all cases, j2 has EXACTLY ONE
attempt id — proves the system never re-dispatched the same logical
work twice.

---

## 3. Ambiguity resolution (operator-visible state transitions)

The assignment's most-discussed invariant: "every deck_job needs to end
in an explicit, operator-visible state. No deck_job should be silently
lost or blindly retried in a way that can duplicate physical work."

| Spec | Operator action | UI assertion | Backend assertion |
| --- | --- | --- | --- |
| [ambiguity/resolve-completed-via-modal.spec.ts](specs/ambiguity/resolve-completed-via-modal.spec.ts) | open `Resolve N ambiguous` button → pick Completed → Continue → Mark completed | DAG viewer flips j1 + j2 to COMPLETED | `j1.recent_attempts[0].outcome_source = OPERATOR_RESOLUTION`; run COMPLETED |
| [lifecycle/retry.spec.ts](specs/lifecycle/retry.spec.ts) | resolve to FAILED (via API for focus) → click Retry on the run header | run reaches COMPLETED after retry | failed job's attempt ledger has ≥2 entries (FAILED + retry COMPLETED); no duplicate dispatch on the ORIGINAL attempt |
| [failure/executor-hang.spec.ts](specs/failure/executor-hang.spec.ts) | (none — automatic surfacing only) | AMBIGUOUS banner + DAG viewer pill | j2.status = AMBIGUOUS via DEADLINE_EXCEEDED |

The Resolve modal flow exercises STATE_MACHINE.md §8.2 (AMBIGUOUS →
COMPLETED via OPERATOR_RESOLUTION). The Retry flow exercises §3.2
(FAILED → READY).

---

## 4. Lifecycle controls (cancel, retry)

| Spec | Operator action | UI assertion | Backend assertion |
| --- | --- | --- | --- |
| [lifecycle/cancel.spec.ts](specs/lifecycle/cancel.spec.ts) | click `Cancel run` → ack checkbox → confirm | status pill flips to Cancelled | run.status = CANCELLED; in-flight job CANCELLED; deck slot released |
| [lifecycle/retry.spec.ts](specs/lifecycle/retry.spec.ts) | (see Ambiguity, §3) | — | — |

The cancel test deliberately uses `hang: true` on deck-2 so the run has
a stable RUNNING state (j2 hung between dispatch and attempt-deadline)
when the operator clicks Cancel — this avoids racing the orchestrator's
own version-bumping cadence.

---

## 5. Concurrency (two-operator semantics)

The assignment explicitly grades: "How does the UI handle ... two
operators acting at the same time, an action rejected because state moved
underneath?"

| Spec | Scenario | UI assertion |
| --- | --- | --- |
| [concurrency/multi-tab.spec.ts](specs/concurrency/multi-tab.spec.ts) | Two browser contexts viewing the same run; tab A cancels via API; tab B's cached state updates within one poll tick | Tab B's pill flips Running → Cancelled without a manual refresh |
| [concurrency/version-mismatch.spec.ts](specs/concurrency/version-mismatch.spec.ts) | Tab loads AMBIGUOUS; another operator (API) resolves to COMPLETED; tab user tries to mark FAILED with stale `expected_version` | Either: "State moved" toast appears (we won the race against the live tick), OR the live cache reflects the resolved state before the click lands. Both outcomes are correct: no silent stale mutation. |

---

## 6. Idempotency (no duplicate physical work)

| Spec | Scenario | Backend assertion |
| --- | --- | --- |
| [idempotency/duplicate-submit.spec.ts](specs/idempotency/duplicate-submit.spec.ts) | Same DAG id submitted twice | second submission returns 409 `DUPLICATE_RESOURCE`; original run untouched; list returns exactly one run |

The orchestrator's idempotency on submit is the load-bearing guarantee
that an operator hammering the Submit button doesn't create N runs.

---

## 7. DAG validation (operator catches errors at submit time)

The frontend's `validateDag` library reports JSON shape, cycles, missing
deps, duplicate ids, and unknown decks before the operator can hit
Submit. Server-side validation is the source of truth; integration tests
cover the per-code paths. Here we verify the operator-facing UX.

| Spec test | Trigger | UI assertion |
| --- | --- | --- |
| `cycle: CYCLE_DETECTED` | DAG with `j1 → j2 → j3 → j1` cycle | inline `CYCLE_DETECTED` error in the ValidationPanel; Submit button disabled |
| `dangling dep: MISSING_DEP` | DAG with `depends_on: ["nonexistent"]` | inline `MISSING_DEP` error; Submit disabled |
| `unknown deck: UNKNOWN_DECK` | DAG referencing `deck-99-does-not-exist` | inline `UNKNOWN_DECK` error; Submit disabled |
| `duplicate job id: DUPLICATE_JOB_ID` | DAG with two jobs sharing the same `id` | inline `DUPLICATE_JOB_ID` error; Submit disabled |
| `backend rejects DAG with cycle` | Direct API POST bypassing frontend validation | 422 with `code: DAG_VALIDATION_FAILED` |

Spec: [validation/dag-validation.spec.ts](specs/validation/dag-validation.spec.ts)

---

## 8. Liveness signals (deck health + connection banner)

| Spec | Trigger | UI assertion |
| --- | --- | --- |
| [liveness/deck-stale.spec.ts](specs/liveness/deck-stale.spec.ts) | `chaos patch deck-2 silent: true` → heartbeats stop | deck card's aria-label flips to include `STALE` within `stale_threshold` (1s); clearing the flag restores HEALTHY |
| [liveness/connection-banner.spec.ts](specs/liveness/connection-banner.spec.ts) (state cases) | URL override `?connection=offline\|sse\|degraded` | each banner state renders with the distinct title text and aria-label (`Connection OFFLINE`, etc.) |
| [liveness/connection-banner.spec.ts](specs/liveness/connection-banner.spec.ts) (restart case) | `POST /api/admin/restart` + force-kill orchestrator | `window.__dfoE2eSentinel` survives the restart — proves the SPA did not full-page-reload, just reconnected the live stream |

The ConnectionBanner is currently driven by `navigator.onLine` + URL
override. Wiring it to actual orchestrator reachability is a deferred
Phase 2 task (see `ConnectionContext.tsx`); the spec asserts what's
there, not what's promised.

---

## Out of scope (deliberately deferred)

These cases are valuable but not in this PR. Each has a one-line
justification.

- **100-deck performance**: the architecture supports it; we have 4
  executors locally. Polling cadence is a knob.
- **Long-tab drift (60s periodic re-bootstrap)**: would slow the suite
  by ~70%; the integration tests cover the snapshot/delta protocol
  directly.
- **Cross-browser projects** (Firefox, WebKit): trivial to add to
  `playwright.config.ts`; Chromium-only this PR.
- **Visual regression**: future axe-core + screenshot diff PR.
- **CI workflow**: `make e2e` is one-liner-ready; not adding the
  GitHub Action file here.
- **Orchestrator graceful-shutdown context propagation**: the entrypoint
  used to leave its runtime ctx live on the `RestartCh` branch, so
  `Monitor.Run(ctx)` kept ticking and `bgWG.Wait()` blocked forever.
  Fixed by deriving a local `runCtx` in `RunOrchestrator` that BOTH
  shutdown triggers (parent ctx cancel and RestartCh) cancel. See
  `backend/internal/app/orchestrator_entrypoint.go`. The
  supervisor's process-kill admin endpoint remains available as an
  escape hatch for unrelated graceful-shutdown stalls (DB lock, etc.)
  but the orchestrator-restart specs no longer need it.

---

## Flake mitigations (in-suite)

- **Live cache version drift on Cancel/Resolve/Retry click**: the
  TanStack Query reducers and the orchestrator's version sometimes
  diverge by ±1 around multi-event transitions (e.g. JOB_RESOLVED +
  JOB_FAILED). Specs that click a mutation button wait ~1.2-1.5s after
  navigation so the live stream converges; the Retry spec re-loads the
  page after the API resolve so the bootstrap snapshot carries the
  authoritative version.
- **Hang chaos leaves the worker in `<-ctx.Done()`**: clearing chaos
  doesn't unblock the worker. The `api` fixture's afterEach crashes any
  deck that had its chaos patched, then waits for the supervisor to
  respawn before the next test starts.
- **Stale processes across runs**: the Playwright webServer invokes
  the supervisor with `-clean`, which wipes the e2e state dir before
  starting. If a stale process still holds an e2e port, the supervisor
  fails fast with a clear bind error.
- **Deck health vs port readiness**: `waitForDecksHealthy` probes each
  deck's chaos endpoint (via the orchestrator proxy) in addition to
  checking `last_known_health`, because the orchestrator's view stays
  HEALTHY for `stale_threshold` (1s) after a crash even while the new
  process is still binding its port.

## Summary

| Surface | Specs | Pass time |
| --- | --- | --- |
| Smoke | 1 | < 1s |
| Topology | 6 | ~10s |
| Lifecycle | 2 | ~15s |
| Failure modes | 4 | ~25s |
| Ambiguity | 1 (others covered above) | ~12s |
| Concurrency | 2 | ~17s |
| Idempotency | 1 | ~1s |
| Validation | 5 | ~9s |
| Liveness | 6 | ~9s |
| **Total** | **28** | **~90s wall time** |
