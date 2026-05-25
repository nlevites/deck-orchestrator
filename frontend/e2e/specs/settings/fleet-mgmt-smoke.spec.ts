import { test, expect } from "../../fixtures/stack";

/**
 * Settings > Fleet Management smoke — grid, prelaunched health, empty slot popover.
 * Empty-cell pick is dynamic: lifecycle spec attach/detach + deferred UNREACHABLE→EMPTY aging.
 */
test("settings/fleet renders the heatmap and the slot popover", async ({ page }) => {
  await page.goto("/settings/fleet");

  const cells = page.locator('[role="gridcell"]');
  await expect(cells).toHaveCount(16);

  for (const id of ["deck-1", "deck-2", "deck-3", "deck-4"]) {
    const cell = page.getByRole("gridcell", { name: new RegExp(`^${id} `) });
    await expect(cell).toHaveAttribute("aria-label", /Running/);
    await expect(cell).toHaveAttribute("aria-label", /HEALTHY/);
  }

  // Which slot is Empty depends on test order — don't pin a fixed deck-id.
  const empty = page.locator('[role="gridcell"][aria-label*="Empty"]').first();
  await expect(empty).toBeVisible();
  const emptyLabel = await empty.getAttribute("aria-label");
  expect(emptyLabel).toMatch(/^deck-/);
  const emptyDeckId = emptyLabel!.split(" ")[0]!;

  await empty.click();
  const popover = page.getByRole("dialog", { name: `${emptyDeckId} controls` });
  await expect(popover).toBeVisible();
  await expect(popover.getByRole("button", { name: /Attach executor/ })).toBeVisible();

  await page.keyboard.press("Escape");
  await expect(popover).not.toBeVisible();
});
