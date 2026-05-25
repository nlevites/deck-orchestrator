import { expect } from "@playwright/test";
import type { DeckJobStatus, RunStatus } from "../helpers/api";
import * as api from "../helpers/api";

/**
 * Poll getRun until status ∈ targets. Intervals match live-state cadence (~1s).
 */
export async function waitForRunStatus(
  runId: string,
  targets: RunStatus | RunStatus[],
  opts: { timeout?: number; intervals?: number[] } = {},
): Promise<api.Run> {
  const set = new Set(Array.isArray(targets) ? targets : [targets]);
  let last: api.Run | undefined;
  await expect
    .poll(
      async () => {
        last = await api.getRun(runId);
        return set.has(last.status);
      },
      { timeout: opts.timeout ?? 15_000, intervals: opts.intervals ?? [200, 500, 1_000] },
    )
    .toBeTruthy();
  if (!last) throw new Error(`waitForRunStatus: run ${runId} disappeared`);
  return last;
}

/** Poll until deck_job status ∈ targets. */
export async function waitForJobStatus(
  runId: string,
  jobId: string,
  targets: DeckJobStatus | DeckJobStatus[],
  opts: { timeout?: number; intervals?: number[] } = {},
): Promise<api.Run> {
  const set = new Set(Array.isArray(targets) ? targets : [targets]);
  let last: api.Run | undefined;
  await expect
    .poll(
      async () => {
        last = await api.getRun(runId);
        const job = last.deck_jobs.find((j) => j.id === jobId);
        return set.has(job?.status ?? "PENDING");
      },
      { timeout: opts.timeout ?? 15_000, intervals: opts.intervals ?? [200, 500, 1_000] },
    )
    .toBeTruthy();
  if (!last) throw new Error(`waitForJobStatus: run ${runId} disappeared`);
  return last;
}
