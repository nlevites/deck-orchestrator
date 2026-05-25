import { test, expect } from "../../fixtures/stack";
import { waitForOrchestratorHealth, waitForDecksHealthy } from "../../helpers/api";

/**
 * Failure mode #1 via Settings UI — complements failure/orchestrator-restart.spec.ts (API).
 * STALE flash not required: e2e timings may recover before stale_threshold crosses.
 */
test("settings/fleet: Restart gracefully button drives a full restart cycle", async ({ page }) => {
  await page.goto("/settings/fleet");

  await expect(page.getByRole("heading", { name: "Orchestrator" })).toBeVisible();
  await expect(page.getByRole("button", { name: /Restart gracefully/ })).toBeVisible();
  for (const id of ["deck-1", "deck-2", "deck-3", "deck-4"]) {
    const cell = page.getByRole("gridcell", { name: new RegExp(`^${id} `) });
    await expect(cell).toHaveAttribute("aria-label", /Running/);
  }

  await page.getByRole("button", { name: /Restart gracefully/ }).click();
  const modal = page.getByRole("dialog", { name: "Restart orchestrator" });
  await expect(modal).toBeVisible();
  await modal.getByRole("button", { name: /^Restart$|Restarting/ }).click();

  await expect(page.getByText(/Orchestrator restarting/).first()).toBeVisible({ timeout: 4_000 });

  await waitForOrchestratorHealth(15_000);
  await waitForDecksHealthy(["deck-1", "deck-2", "deck-3", "deck-4"], 15_000);

  for (const id of ["deck-1", "deck-2", "deck-3", "deck-4"]) {
    const cell = page.getByRole("gridcell", { name: new RegExp(`^${id} `) });
    await expect(cell).toHaveAttribute("aria-label", /Running/, { timeout: 15_000 });
    await expect(cell).toHaveAttribute("aria-label", /HEALTHY/, { timeout: 15_000 });
  }
});
