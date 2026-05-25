import { test, expect } from "../../fixtures/stack";
import {
  attachExecutorViaSupervisor,
  detachExecutorViaSupervisor,
  listSupervisorProcesses,
  waitForExecutorAbsent,
} from "../../helpers/api";

/**
 * Full attach→stop→start→restart→detach via Settings UI on deck-9 (sandbox slot).
 * Each step asserts cell aria-label AND supervisor ProcessTable state.
 */
test("settings/fleet: full attach -> stop -> start -> restart -> detach lifecycle", async ({
  page,
}) => {
  const deckId = "deck-9";

  // Guarantee deck-9 detached — idempotent DELETE.
  await detachExecutorViaSupervisor(deckId);
  await waitForExecutorAbsent(deckId, 5_000);

  await page.goto("/settings/fleet");

  const cell = page.getByRole("gridcell", { name: new RegExp(`^${deckId} `) });
  await expect(cell).toHaveAttribute("aria-label", /Empty/);

  await cell.click();
  const popover1 = page.getByRole("dialog", { name: `${deckId} controls` });
  await popover1.getByRole("button", { name: /Attach executor/ }).click();
  const attachModal = page.getByRole("dialog", { name: `Attach executor to ${deckId}` });
  await expect(attachModal).toBeVisible();
  const freshCheckbox = attachModal.getByRole("checkbox", { name: /Fresh state/ });
  await expect(freshCheckbox).not.toBeChecked();
  await attachModal.getByRole("button", { name: /^Attach$|Attaching/ }).click();

  // spawn + 250ms heartbeat + 1s client poll
  await expect(cell).toHaveAttribute("aria-label", /Running/, { timeout: 6_000 });
  await expect(cell).toHaveAttribute("aria-label", /HEALTHY/, { timeout: 6_000 });

  {
    const state = await listSupervisorProcesses();
    const entry = state.executors.find((e) => e.deck_id === deckId);
    expect(entry?.state).toBe("Running");
  }

  await cell.click();
  const popover2 = page.getByRole("dialog", { name: `${deckId} controls` });
  await popover2.getByRole("button", { name: /^Stop$/ }).click();
  await expect(cell).toHaveAttribute("aria-label", /Stopped/, { timeout: 4_000 });

  await cell.click();
  const popover3 = page.getByRole("dialog", { name: `${deckId} controls` });
  await popover3.getByRole("button", { name: /^Start$/ }).click();
  await expect(cell).toHaveAttribute("aria-label", /Running/, { timeout: 6_000 });
  await expect(cell).toHaveAttribute("aria-label", /HEALTHY/, { timeout: 6_000 });

  await cell.click();
  const popover4 = page.getByRole("dialog", { name: `${deckId} controls` });
  await popover4.getByRole("button", { name: /^Restart$/ }).click();
  // Brief Running departure OK — pin return to Running + HEALTHY.
  await expect(cell).toHaveAttribute("aria-label", /Running/, { timeout: 8_000 });
  await expect(cell).toHaveAttribute("aria-label", /HEALTHY/, { timeout: 8_000 });

  await cell.click();
  const popover5 = page.getByRole("dialog", { name: `${deckId} controls` });
  await popover5.getByRole("button", { name: /^Detach$/ }).click();
  const detachModal = page.getByRole("dialog", { name: `Detach executor from ${deckId}` });
  await expect(detachModal).toBeVisible();
  await detachModal.getByRole("button", { name: /^Detach$|Detaching/ }).click();

  // UI collapses STALE/UNREACHABLE via toneFor(); supervisor absent is the pin.
  await expect(cell).not.toHaveAttribute("aria-label", /Running/, { timeout: 8_000 });
  await waitForExecutorAbsent(deckId, 4_000);
});

/**
 * Fresh state checkbox must set fresh_state: true on POST /supervisor/executors.
 * Backend SQLite wipe is out of scope here.
 */
test("settings/fleet: Fresh state checkbox propagates to the attach POST body", async ({
  page,
}) => {
  const deckId = "deck-10";

  await detachExecutorViaSupervisor(deckId);
  await waitForExecutorAbsent(deckId, 5_000);

  await page.goto("/settings/fleet");

  const cell = page.getByRole("gridcell", { name: new RegExp(`^${deckId} `) });
  await expect(cell).toHaveAttribute("aria-label", /Empty/);

  let captured: { fresh_state?: boolean } | null = null;
  await page.route("**/supervisor/executors", async (route) => {
    if (route.request().method() === "POST") {
      const data = route.request().postDataJSON() as { fresh_state?: boolean };
      captured = data;
    }
    await route.continue();
  });

  await cell.click();
  await page.getByRole("button", { name: /Attach executor/ }).click();
  const modal = page.getByRole("dialog", { name: `Attach executor to ${deckId}` });
  await modal.getByRole("checkbox", { name: /Fresh state/ }).check();
  await modal.getByRole("button", { name: /^Attach$|Attaching/ }).click();

  await expect.poll(() => captured?.fresh_state, { timeout: 5_000 }).toBe(true);

  await detachExecutorViaSupervisor(deckId).catch(() => {});
});

test.afterEach(async () => {
  for (const id of ["deck-9", "deck-10"]) {
    await detachExecutorViaSupervisor(id).catch(() => {});
  }
  // Re-attach deck-1..4 if lifecycle accidentally detached them.
  const state = await listSupervisorProcesses().catch(() => null);
  if (state) {
    for (const id of ["deck-1", "deck-2", "deck-3", "deck-4"]) {
      const present = state.executors.some((e) => e.deck_id === id);
      if (!present) {
        await attachExecutorViaSupervisor(id).catch(() => {});
      }
    }
  }
});
