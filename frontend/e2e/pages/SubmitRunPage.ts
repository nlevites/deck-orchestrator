import type { Page } from "@playwright/test";
import { expect } from "@playwright/test";
import type { DagSubmission } from "../helpers/api";

/** /submit — paste JSON, click Submit, expect navigation to /runs/{id}. */
export class SubmitRunPage {
  constructor(public readonly page: Page) {}

  async goto(): Promise<void> {
    await this.page.goto("/submit");
    await expect(this.page.getByRole("heading", { name: "New run" })).toBeVisible();
    await this.waitForDecksLoaded();
  }

  async waitForDecksLoaded(): Promise<void> {
    // useLiveState polls every 1s on mount — decks cache needed for UNKNOWN_DECK validation.
    await this.page.waitForTimeout(1_200);
  }

  editButton() {
    return this.page.getByRole("button", { name: /Edit JSON/ });
  }

  modalDialog() {
    return this.page.getByRole("dialog", { name: /Edit DAG JSON/ });
  }

  textarea() {
    return this.modalDialog().getByRole("textbox");
  }

  applyButton() {
    return this.modalDialog().getByRole("button", { name: /^Apply$/ });
  }

  submitButton() {
    return this.page.getByRole("button", { name: /^(Submit run|Submitting…)$/ });
  }

  /**
   * Scoped to Edit JSON modal — validation list renders there today.
   */
  validationItem(code: string) {
    return this.modalDialog().getByText(code).first();
  }

  async openEditor(): Promise<void> {
    await this.editButton().click();
    await expect(this.modalDialog()).toBeVisible();
  }

  async fillJson(dag: DagSubmission): Promise<void> {
    await this.textarea().fill(JSON.stringify(dag, null, 2));
  }

  async applyAndClose(): Promise<void> {
    await this.applyButton().click();
    await expect(this.modalDialog()).toBeHidden();
  }

  async pasteJson(dag: DagSubmission): Promise<void> {
    await this.openEditor();
    await this.fillJson(dag);
    await this.applyButton().click();
    await expect(this.modalDialog()).toBeHidden();
  }

  async submit(dag: DagSubmission): Promise<void> {
    await this.pasteJson(dag);
    await expect(this.submitButton()).toBeEnabled();
    await Promise.all([
      this.page.waitForURL(`**/runs/${encodeURIComponent(dag.id)}**`),
      this.submitButton().click(),
    ]);
  }
}
