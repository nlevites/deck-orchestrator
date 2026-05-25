import { test, expect } from "../../fixtures/stack";

// Regression: delta omitted decks + relativeAge() only recomputed on re-render
// → "last heartbeat Ns ago" froze at bootstrap while wall clock advanced.
test("deck-detail: last heartbeat stays current on a long-open page", async ({ page }) => {
  await page.goto("/decks/deck-1");

  await expect(page.getByText(/last heartbeat \d+s ago/)).toBeVisible({ timeout: 10_000 });

  // Pre-fix: displayed age would reach ≥4s; post-fix stays 0–3s (250ms heartbeat, 1s poll).
  await page.waitForTimeout(4_000);

  const headerText = await page
    .getByText(/last heartbeat \d+s ago/)
    .first()
    .textContent();
  expect(headerText).toMatch(/last heartbeat (?:0|1|2|3)s ago/);
});
