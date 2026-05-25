import { test, expect } from "../../fixtures/stack";
import { runIdFor } from "../../helpers/ids";
import { RunDetailPage } from "../../pages/RunDetailPage";

/**
 * READY job on STALE/UNREACHABLE deck must show Blocked — pre-fix showed
 * "Ready" with no signal that tryDispatch was holding dispatch.
 */
test("blocked READY job shows Blocked subtitle when target deck is unhealthy", async ({
  page,
  api,
  submit,
}, testInfo) => {
  const runId = runIdFor(testInfo, "blocked-ready");
  const deckId = "deck-4";

  // Deck must be unhealthy before submit — else tryDispatch races staleness threshold.
  await api.patchChaos(deckId, { silent: true });
  await expect
    .poll(
      async () => {
        const decks = await api.listDecks();
        return decks.find((d) => d.id === deckId)?.last_known_health;
      },
      { timeout: 5_000, intervals: [200, 500] },
    )
    .not.toBe("HEALTHY");

  await submit({
    id: runId,
    deck_jobs: [
      {
        id: "stuck",
        deck_id: deckId,
        depends_on: [],
        steps: [{ type: "work", description: "Will not dispatch" }],
      },
    ],
  });

  const detail = new RunDetailPage(page, runId);
  await detail.goto();

  await expect
    .poll(
      async () => {
        const run = await api.getRun(runId);
        return run.deck_jobs[0]?.status;
      },
      { timeout: 5_000, intervals: [200, 500] },
    )
    .toBe("READY");

  await expect(page.getByText(/Blocked\s+—\s+deck is /i).first()).toBeVisible({
    timeout: 6_000,
  });

  await expect(page.locator(`text=/${deckId} · blocked/i`).first()).toBeVisible({
    timeout: 4_000,
  });
});
