import type { Page } from "@playwright/test";
import { expect } from "@playwright/test";
import * as sel from "../helpers/selectors";

/** /fleet — the dashboard (pulse strip, needs-attention list, active runs). */
export class FleetDashboardPage {
  constructor(public readonly page: Page) {}

  async goto(): Promise<void> {
    await this.page.goto("/fleet");
    await expect(this.page.getByRole("heading", { name: "Fleet" })).toBeVisible();
  }

  ambiguousBanner() {
    return sel.ambiguousBanner(this.page);
  }
}

/** /fleet/grid — the deck-card grid view. */
export class FleetGridPage {
  constructor(public readonly page: Page) {}

  async goto(): Promise<void> {
    await this.page.goto("/fleet/grid");
    await expect(sel.deckCard(this.page, "deck-1")).toBeVisible();
  }
}
