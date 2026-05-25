import { test, expect } from "../../fixtures/stack";
import { runIdFor } from "../../helpers/ids";
import { waitForJobStatus, waitForRunStatus } from "../../fixtures/time";
import { RunDetailPage } from "../../pages/RunDetailPage";
import * as sel from "../../helpers/selectors";

// C4 regression: dual applyEvent writers inflated run.version 2× → 409 on Cancel.
// Short post-mount settle (~1 poll) — cancel.spec.ts uses long waits; both coexist.
test("cancel succeeds immediately after mount (no version inflation)", async ({
  page,
  submit,
  api,
}, testInfo) => {
  // Two rounds — single-trial fluke could mask regression.
  for (let round = 1; round <= 2; round++) {
    const runId = runIdFor(testInfo, `c4-round${round}`);

    await api.patchChaos("deck-2", { hang: true });
    await submit({
      id: runId,
      deck_jobs: [
        {
          id: "j1",
          deck_id: "deck-2",
          depends_on: [],
          steps: [{ type: "work", description: "hangs here" }],
        },
      ],
    });
    await waitForJobStatus(runId, "j1", "RUNNING", { timeout: 8_000 });

    const detail = new RunDetailPage(page, runId);
    await detail.goto();
    // ~1 poll cycle — enough for bootstrap, not enough to mask 2× version inflation.
    await page.waitForTimeout(350);

    await expect(detail.cancelButton()).toBeEnabled({ timeout: 3_000 });

    await detail.cancelRun();

    await waitForRunStatus(runId, "CANCELLED", { timeout: 8_000 });

    await expect(sel.toastTitle(page, /State moved/i)).toHaveCount(0);

    await api.resetChaos("deck-2");
    await api.crashDeck("deck-2").catch(() => {});
    await api.waitForDecksHealthy(["deck-2"], 8_000);
  }
});
