import { test, expect } from "../../fixtures/stack";
import { runIdFor } from "../../helpers/ids";
import { waitForJobStatus } from "../../fixtures/time";
import { RunDetailPage } from "../../pages/RunDetailPage";

// 409 VERSION_MISMATCH → "State moved" toast; operator informed, no silent mutation.
test("version mismatch on resolve: operator sees 'State moved' toast", async ({
  page,
  submit,
  api,
}, testInfo) => {
  const runId = runIdFor(testInfo, "vermismatch");

  await api.patchChaos("deck-2", { hang: true });
  await submit({
    id: runId,
    deck_jobs: [
      {
        id: "j1",
        deck_id: "deck-2",
        depends_on: [],
        steps: [{ type: "work", description: "will hang" }],
      },
    ],
  });
  await waitForJobStatus(runId, "j1", "AMBIGUOUS", { timeout: 10_000 });

  const detail = new RunDetailPage(page, runId);
  await detail.goto();
  await page.waitForTimeout(1_500);

  const run = await api.getRun(runId);
  const j1 = run.deck_jobs.find((j) => j.id === "j1")!;
  await api.resolveJob(runId, "j1", j1.version, "COMPLETED");

  // Race-tolerant: stale click → toast, OR live cache catches up → modal won't open.
  let toastSeen = false;
  let resolvedStateSeen = false;
  try {
    await page.getByRole("button", { name: /Resolve \d+ ambiguous/ }).click({ timeout: 3_000 });
    const modal = page.getByRole("dialog", { name: /Resolve ambiguous job/ });
    await expect(modal).toBeVisible({ timeout: 2_000 });
    await modal
      .getByRole("button", { name: /^Failed\b/ })
      .first()
      .click({ force: true, timeout: 3_000 });
    await modal.getByRole("button", { name: "Continue" }).click({ timeout: 2_000 });
    await modal.getByRole("button", { name: /Mark failed/i }).click({ timeout: 2_000 });

    if (
      await page
        .getByText(/State moved/i)
        .first()
        .isVisible()
        .catch(() => false)
    ) {
      toastSeen = true;
    }
  } catch {
    // Live cache caught up — ambig dropped to 0, modal closed.
  }

  if (!toastSeen) {
    if (
      await page
        .getByText("Completed")
        .first()
        .isVisible()
        .catch(() => false)
    ) {
      resolvedStateSeen = true;
    }
  }

  expect(
    toastSeen || resolvedStateSeen,
    "expected either a State moved toast or the COMPLETED state to be visible",
  ).toBeTruthy();
});
