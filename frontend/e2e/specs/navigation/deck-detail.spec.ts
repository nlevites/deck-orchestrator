import { test, expect } from "../../fixtures/stack";
import * as sel from "../../helpers/selectors";

// Regression: DeckCard had hover styling but no click target; inner Links used broken ?deck=.
test("deck card click lands on /decks/:id and chaos button stays put", async ({ page }) => {
  await page.goto("/fleet/grid");

  await expect(sel.deckCard(page, "deck-1")).toBeVisible({ timeout: 10_000 });

  await sel.deckCard(page, "deck-1").click();
  await expect(page).toHaveURL(/\/decks\/deck-1$/);

  await expect(page.getByRole("heading", { name: "deck-1" })).toBeVisible();

  // Chaos button inside card-as-Link on grid — must not navigate away.
  await page.getByRole("button", { name: /Chaos controls/i }).click();
  await expect(page.getByRole("dialog", { name: /Chaos controls/i })).toBeVisible();

  // Two close affordances — target X icon explicitly (strict-mode).
  await page.getByRole("button", { name: "Close dialog" }).click();
  await expect(page).toHaveURL(/\/decks\/deck-1$/);
});
