import { test, expect } from "../../fixtures/stack";
import { runIdFor } from "../../helpers/ids";
import { waitForRunStatus, waitForJobStatus } from "../../fixtures/time";
import { RunDetailPage } from "../../pages/RunDetailPage";

// Retry needs FAILED job on non-terminal run. Two hangs → AMBIGUOUS; resolve one FAILED.
// attempt_deadline=4s makes a single long-RUNNING job fragile for this setup.
test("retry a failed job via the Retry modal", async ({ page, submit, api }, testInfo) => {
  const runId = runIdFor(testInfo, "retry");

  await api.patchChaos("deck-2", { hang: true });
  await api.patchChaos("deck-3", { hang: true });
  await submit({
    id: runId,
    deck_jobs: [
      {
        id: "j-fast",
        deck_id: "deck-2",
        depends_on: [],
        steps: [{ type: "work", description: "fast hang" }],
      },
      {
        id: "j-slow",
        deck_id: "deck-3",
        depends_on: [],
        steps: [{ type: "work", description: "slow hang" }],
      },
    ],
  });

  await waitForJobStatus(runId, "j-fast", "AMBIGUOUS", { timeout: 12_000 });

  const run1 = await api.getRun(runId);
  const fast = run1.deck_jobs.find((j) => j.id === "j-fast")!;
  await api.resolveJob(runId, "j-fast", fast.version, "FAILED");

  // chaos.Hang blocks on <-ctx.Done() — crash unsticks deck-2 for retry dispatch.
  await api.crashDeck("deck-2").catch(() => {});

  const detail = new RunDetailPage(page, runId);
  await detail.goto();
  // Bootstrap settle — stale run.version → 409 VERSION_MISMATCH on Retry.
  await page.waitForTimeout(1_500);

  await detail.retryFailed();

  await api.waitForDecksHealthy(["deck-2"]);

  const run2 = await api.getRun(runId);
  const slow = run2.deck_jobs.find((j) => j.id === "j-slow")!;
  await api.resolveJob(runId, "j-slow", slow.version, "COMPLETED");
  await api.resetChaos("deck-3");

  await waitForRunStatus(runId, "COMPLETED", { timeout: 20_000 });
  await expect(page.getByText("Completed").first()).toBeVisible();

  const final = await api.getRun(runId);
  const fastFinal = final.deck_jobs.find((j) => j.id === "j-fast")!;
  expect((fastFinal.recent_attempts ?? []).length).toBeGreaterThanOrEqual(2);
});
