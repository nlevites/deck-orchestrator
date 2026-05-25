import { test, expect } from "../../fixtures/stack";
import { FleetGridPage } from "../../pages/FleetDashboardPage";
import * as sel from "../../helpers/selectors";

// stale_threshold=1s in e2e — silent deck flips HEALTHY→STALE; UI follows via live state.
test("deck silent (no heartbeats) flips HEALTHY → STALE in the UI, recovers on clear", async ({
  page,
  api,
}) => {
  const grid = new FleetGridPage(page);
  await grid.goto();

  await expect(sel.deckCard(page, "deck-2")).toBeVisible();

  await api.patchChaos("deck-2", { silent: true });

  await expect
    .poll(
      async () => {
        const decks = await api.listDecks();
        return decks.find((d) => d.id === "deck-2")?.last_known_health;
      },
      { timeout: 10_000, intervals: [200, 500] },
    )
    .toBe("STALE");

  await expect
    .poll(
      async () => {
        return await sel.deckCard(page, "deck-2").getAttribute("aria-label");
      },
      { timeout: 8_000 },
    )
    .toContain("STALE");

  await api.patchChaos("deck-2", { silent: false });
  await expect
    .poll(
      async () => {
        const decks = await api.listDecks();
        return decks.find((d) => d.id === "deck-2")?.last_known_health;
      },
      { timeout: 10_000, intervals: [200, 500] },
    )
    .toBe("HEALTHY");
});
