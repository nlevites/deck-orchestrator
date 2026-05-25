import { test, expect } from "../fixtures/stack";
import * as sel from "../helpers/selectors";

// Gate spec — if this fails, the stack or live bootstrap is broken.
test("smoke: fleet grid renders four deck cards", async ({ page }) => {
  await page.goto("/fleet/grid");
  for (const id of ["deck-1", "deck-2", "deck-3", "deck-4"]) {
    await expect(sel.deckCard(page, id)).toBeVisible();
  }
});
