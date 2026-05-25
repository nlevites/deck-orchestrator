/**
 * Dispatcher contract for `applyEvent`. The interesting return-value
 * semantics live here:
 *
 *   - `true`  → reducer reconciled cache state.
 *   - `false` → either unknown kind, or a registered reducer asked for
 *               a rebootstrap (RUN_SUBMITTED today, possibly more in
 *               the future). The polling hooks treat both the same way:
 *               set seqRef = 0 so the next tick takes the bootstrap path.
 *
 * The runs-list latency bug we're guarding against here: if
 * applyRunSubmitted returns `void` instead of `false`, a freshly
 * submitted run waits up to 60s (the periodic-rebootstrap floor) to
 * show up in the /runs cache. We pin the contract so the next
 * regression is loud.
 */
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import { apiKeys } from "@/lib/api/keys";
import type { DeckJob, Event, EventKind, Run, RunSummary } from "@/lib/api-types";
import { applyEvent } from "./apply-event";

function makeEvent(overrides: Partial<Event> & { kind: EventKind | string }): Event {
  return {
    seq: 1,
    occurred_at: "2026-01-01T00:00:00Z",
    payload: {},
    ...overrides,
  } as Event;
}

function summary(id: string, status: RunSummary["status"], version = 1): RunSummary {
  return {
    id,
    status,
    version,
    submitted_at: "2026-01-01T00:00:00Z",
    deck_jobs_summary: { total: 1, by_status: {} },
  };
}

describe("applyEvent dispatcher", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  afterEach(() => {
    qc.clear();
  });

  it("returns false on RUN_SUBMITTED (signal rebootstrap)", () => {
    qc.setQueryData<RunSummary[]>(apiKeys.runs, [summary("run-a", "RUNNING")]);

    const result = applyEvent(qc, makeEvent({ kind: "RUN_SUBMITTED", run_id: "run-b", seq: 5 }));

    expect(result).toBe(false);
    const after = qc.getQueryData<RunSummary[]>(apiKeys.runs);
    expect(after?.map((r) => r.id)).toEqual(["run-a"]);
    const events = qc.getQueryData<Event[]>(apiKeys.events);
    expect(events?.map((e) => e.seq)).toEqual([5]);
  });

  it("returns true on RUN_STATUS_CHANGED and reconciles the list cache", () => {
    qc.setQueryData<RunSummary[]>(apiKeys.runs, [summary("run-a", "RUNNING", 3)]);

    const result = applyEvent(
      qc,
      makeEvent({
        kind: "RUN_STATUS_CHANGED",
        run_id: "run-a",
        seq: 7,
        payload: { to: "COMPLETED" },
      }),
    );

    expect(result).toBe(true);
    const after = qc.getQueryData<RunSummary[]>(apiKeys.runs);
    expect(after).toEqual([
      expect.objectContaining({ id: "run-a", status: "COMPLETED", version: 4 }),
    ]);
    expect(qc.getQueryData<Run>(apiKeys.run("run-a"))).toBeUndefined();
  });

  it("returns false on an unknown kind and leaves domain caches untouched", () => {
    qc.setQueryData<RunSummary[]>(apiKeys.runs, [summary("run-a", "RUNNING")]);

    // Cast through unknown so the test can construct a kind the type
    // system rightly refuses; we still want to verify the runtime
    // behavior so the dispatcher's "drift signal" path is pinned.
    const result = applyEvent(
      qc,
      makeEvent({ kind: "BOGUS_FUTURE_KIND" as unknown as EventKind, seq: 9 }),
    );

    expect(result).toBe(false);
    expect(qc.getQueryData<RunSummary[]>(apiKeys.runs)?.map((r) => r.id)).toEqual(["run-a"]);
    expect(qc.getQueryData<Event[]>(apiKeys.events)?.map((e) => e.seq)).toEqual([9]);
  });
});

describe("by_status tally with cold run-detail cache", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  afterEach(() => {
    qc.clear();
  });

  it("JOB_DISPATCHED decrements READY and increments DISPATCHED", () => {
    qc.setQueryData<RunSummary[]>(apiKeys.runs, [
      {
        id: "run-a",
        status: "RUNNING",
        version: 1,
        submitted_at: "2026-01-01T00:00:00Z",
        deck_jobs_summary: { total: 1, by_status: { READY: 1 } },
      },
    ]);
    applyEvent(
      qc,
      makeEvent({
        kind: "JOB_DISPATCHED",
        run_id: "run-a",
        job_id: "job-1",
        deck_id: "deck-1",
        seq: 2,
        payload: { from: "READY" },
      }),
    );

    const after = qc.getQueryData<RunSummary[]>(apiKeys.runs);
    expect(after?.[0].deck_jobs_summary.by_status).toEqual({ DISPATCHED: 1 });
  });

  it("JOB_RUNNING decrements DISPATCHED and increments RUNNING", () => {
    qc.setQueryData<RunSummary[]>(apiKeys.runs, [
      {
        id: "run-a",
        status: "RUNNING",
        version: 1,
        submitted_at: "2026-01-01T00:00:00Z",
        deck_jobs_summary: { total: 1, by_status: { DISPATCHED: 1 } },
      },
    ]);

    applyEvent(
      qc,
      makeEvent({
        kind: "JOB_RUNNING",
        run_id: "run-a",
        job_id: "job-1",
        deck_id: "deck-1",
        seq: 3,
        payload: { from: "DISPATCHED" },
      }),
    );

    const after = qc.getQueryData<RunSummary[]>(apiKeys.runs);
    expect(after?.[0].deck_jobs_summary.by_status).toEqual({ RUNNING: 1 });
  });

  it("JOB_COMPLETED decrements RUNNING and increments COMPLETED", () => {
    qc.setQueryData<RunSummary[]>(apiKeys.runs, [
      {
        id: "run-a",
        status: "RUNNING",
        version: 1,
        submitted_at: "2026-01-01T00:00:00Z",
        deck_jobs_summary: { total: 4, by_status: { RUNNING: 2, COMPLETED: 2 } },
      },
    ]);

    applyEvent(
      qc,
      makeEvent({
        kind: "JOB_COMPLETED",
        run_id: "run-a",
        job_id: "job-1",
        deck_id: "deck-1",
        seq: 4,
        payload: { from: "RUNNING", outcome_source: "EXECUTOR_EVENT" },
      }),
    );

    const after = qc.getQueryData<RunSummary[]>(apiKeys.runs);
    expect(after?.[0].deck_jobs_summary.by_status).toEqual({ RUNNING: 1, COMPLETED: 3 });
    expect(after?.[0].deck_jobs_summary.total).toBe(4);
  });
});

function makeJob(overrides: Partial<DeckJob> = {}): DeckJob {
  return {
    id: "job-1",
    deck_id: "deck-1",
    depends_on: [],
    steps: [
      { type: "prepare", description: "Step A" },
      { type: "incubate", description: "Step B" },
      { type: "measure", description: "Step C" },
    ],
    status: "RUNNING",
    version: 2,
    last_completed_step: 0,
    total_steps: 3,
    ...overrides,
  };
}

function makeRun(job: DeckJob): Run {
  return {
    id: "run-1",
    status: "RUNNING",
    version: 2,
    submitted_at: "2026-01-01T00:00:00Z",
    dag: { id: "run-1", deck_jobs: [] },
    deck_jobs: [job],
  };
}

describe("applyJobStepCompleted reducer", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient();
  });

  afterEach(() => {
    qc.clear();
  });

  it("advances last_completed_step and bumps version", () => {
    const job = makeJob({ last_completed_step: 0, version: 2 });
    qc.setQueryData<Run>(apiKeys.run("run-1"), makeRun(job));

    applyEvent(
      qc,
      makeEvent({
        kind: "JOB_STEP_COMPLETED",
        run_id: "run-1",
        job_id: "job-1",
        seq: 10,
        payload: { step: 1, total: 3 },
      }),
    );

    const updated = qc.getQueryData<Run>(apiKeys.run("run-1"));
    const updatedJob = updated?.deck_jobs.find((j) => j.id === "job-1");
    expect(updatedJob?.last_completed_step).toBe(1);
    expect(updatedJob?.version).toBe(3);
    expect(updatedJob?.status).toBe("RUNNING");
  });

  it("is monotonic: out-of-order replay does not decrement", () => {
    const job = makeJob({ last_completed_step: 2, version: 4 });
    qc.setQueryData<Run>(apiKeys.run("run-1"), makeRun(job));

    // Deliver step 1 again after step 2 has been recorded.
    applyEvent(
      qc,
      makeEvent({
        kind: "JOB_STEP_COMPLETED",
        run_id: "run-1",
        job_id: "job-1",
        seq: 15,
        payload: { step: 1, total: 3 },
      }),
    );

    const updated = qc.getQueryData<Run>(apiKeys.run("run-1"));
    const updatedJob = updated?.deck_jobs.find((j) => j.id === "job-1");
    expect(updatedJob?.last_completed_step).toBe(2);
    expect(updatedJob?.version).toBe(4);
  });

  it("returns true (handled, no rebootstrap needed)", () => {
    const job = makeJob();
    qc.setQueryData<Run>(apiKeys.run("run-1"), makeRun(job));

    const result = applyEvent(
      qc,
      makeEvent({
        kind: "JOB_STEP_COMPLETED",
        run_id: "run-1",
        job_id: "job-1",
        seq: 20,
        payload: { step: 2, total: 3 },
      }),
    );

    expect(result).toBe(true);
  });
});
