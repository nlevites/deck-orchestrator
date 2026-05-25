import { test, expect } from "../../fixtures/stack";
import { runIdFor } from "../../helpers/ids";
import { waitForRunStatus, waitForJobStatus } from "../../fixtures/time";
import { RunDetailPage } from "../../pages/RunDetailPage";

// Hang deck-2 before submit — j2 RUNNING blocks version bumps until attempt_deadline (~4s).
test("cancel a running run from the run header", async ({ page, submit, api }, testInfo) => {
  const runId = runIdFor(testInfo, "cancel");

  await api.patchChaos("deck-2", { hang: true });
  await submit({
    id: runId,
    deck_jobs: [
      {
        id: "j1",
        deck_id: "deck-1",
        depends_on: [],
        steps: [{ type: "work", description: "j1" }],
      },
      {
        id: "j2",
        deck_id: "deck-2",
        depends_on: ["j1"],
        steps: [{ type: "work", description: "j2 will hang here" }],
      },
    ],
  });

  await waitForJobStatus(runId, "j2", "RUNNING");
  // Post-S1 (long-poll), dispatch is near-instant so attempt_deadline (4s)
  // starts ~1s earlier than under short-poll. Trim post-RUNNING waits so we
  // click Cancel well before the deadline flips the run to AMBIGUOUS.

  const detail = new RunDetailPage(page, runId);
  await detail.goto();
  // Bootstrap into cache before Cancel button renders.
  await page.waitForTimeout(300);

  await detail.cancelRun();

  await waitForRunStatus(runId, "CANCELLED");
  await api.resetChaos("deck-2");

  await expect(page.getByText("Cancelled").first()).toBeVisible();
});
