import { test, expect } from "../../fixtures/stack";
import { waitForDecksHealthy } from "../../helpers/api";

/**
 * Failure mode #3b via Settings UI — chaos crash must recover, never FatalConfig (exit 78).
 */
test("settings/fleet: chaos crash on a running deck recovers without going FatalConfig", async ({
  page,
  api,
}) => {
  const deckId = "deck-2";

  await page.goto("/settings/fleet");
  const cell = page.getByRole("gridcell", { name: new RegExp(`^${deckId} `) });

  await expect(cell).toHaveAttribute("aria-label", /Running/);
  await expect(cell).toHaveAttribute("aria-label", /HEALTHY/);

  await api.crashDeck(deckId);

  await waitForDecksHealthy([deckId], 10_000);

  await expect(cell).toHaveAttribute("aria-label", /Running/, { timeout: 10_000 });
  await expect(cell).toHaveAttribute("aria-label", /HEALTHY/, { timeout: 10_000 });

  const label = await cell.getAttribute("aria-label");
  expect(label).not.toContain("FatalConfig");
});
