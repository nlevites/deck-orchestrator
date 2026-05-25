import type { Page, Locator } from "@playwright/test";
import { expect } from "@playwright/test";
import * as sel from "../helpers/selectors";

/**
 * /runs/:id page driver. Legacy /events, /resolve, /deck deep-links still
 * resolve (drawer inline or redirect to parent).
 */
export class RunDetailPage {
  constructor(
    public readonly page: Page,
    public readonly runId: string,
  ) {}

  async goto(): Promise<void> {
    await this.page.goto(`/runs/${encodeURIComponent(this.runId)}`);
    await expect(this.page.getByRole("heading", { name: this.runId })).toBeVisible();
  }

  async openEvents(): Promise<void> {
    await this.page.goto(`/runs/${encodeURIComponent(this.runId)}/events`);
    await expect(sel.eventLog(this.page)).toBeVisible();
  }

  async openResolve(): Promise<void> {
    await this.page.goto(`/runs/${encodeURIComponent(this.runId)}/resolve`);
  }

  async openDeckTab(): Promise<void> {
    await this.page.goto(`/runs/${encodeURIComponent(this.runId)}/deck`);
  }

  statusPill(): Locator {
    return this.page.locator("header").locator(".inline-flex").first();
  }

  cancelButton(): Locator {
    return this.page.getByRole("button", { name: "Cancel run" });
  }

  retryButton(): Locator {
    // Hero CTA vs AttentionPanel card — match either retry-modal trigger.
    return this.page.getByRole("button", { name: /Retry \d+ failed|^Retry$/ }).first();
  }

  resolveButton(): Locator {
    return this.page.getByRole("button", { name: /Resolve \d+ ambiguous|^Resolve$/ }).first();
  }

  /**
   * Retries confirm on 409 VERSION_MISMATCH — AttemptDeadline can bump
   * run.version mid-modal via AMBIGUOUS.
   */
  async cancelRun(): Promise<void> {
    await this.cancelButton().click();
    const modal = sel.cancelRunModal(this.page);
    await expect(modal).toBeVisible();
    for (let attempt = 0; attempt < 3; attempt++) {
      const checkbox = modal.getByRole("checkbox");
      if (!(await checkbox.isChecked())) {
        await checkbox.check();
      }
      await modal.getByRole("button", { name: /^Cancel run$/ }).click();
      try {
        await expect(modal).toBeHidden({ timeout: 4_000 });
        return;
      } catch {
        // VERSION_MISMATCH — re-ack against fresh version
      }
    }
    await expect(modal).toBeHidden();
  }

  async resolveAmbiguousAs(outcome: "COMPLETED" | "FAILED"): Promise<void> {
    await this.resolveButton().click();
    const modal = sel.resolveJobModal(this.page);
    await expect(modal).toBeVisible();
    // Outcome buttons: accessible name is title + body; match title prefix.
    const label = outcome === "COMPLETED" ? "Completed" : "Failed";
    await modal
      .getByRole("button", { name: new RegExp(`^${label}\\b`) })
      .first()
      .click();
    await modal.getByRole("button", { name: "Continue" }).click();
    await modal
      .getByRole("button", { name: new RegExp(`Mark ${outcome.toLowerCase()}`, "i") })
      .click();
    await expect(modal).toBeHidden();
  }

  async retryFailed(): Promise<void> {
    await this.retryButton().click();
    const modal = sel.retryJobModal(this.page);
    await expect(modal).toBeVisible();
    const checkbox = modal.getByRole("checkbox");
    if (await checkbox.isVisible()) await checkbox.check();
    await modal.getByRole("button", { name: /^Retry job$|^Retry$/ }).click();
    await expect(modal).toBeHidden();
  }
}
