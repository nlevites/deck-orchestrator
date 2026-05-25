import type { Page, Locator } from "@playwright/test";
import type { DeckJobStatus, RunStatus } from "./api";

/**
 * Canonical locator factories — UI aria-label renames are one diff here.
 */

export function runRowLink(page: Page, runId: string, status?: RunStatus): Locator {
  if (status) return page.getByRole("link", { name: `Run ${runId}, status ${status}` });
  return page.locator(`a[aria-label^="Run ${runId},"]`);
}

export function runRowAny(page: Page, runId: string): Locator {
  return page.locator(`a[aria-label^="Run ${runId},"]`);
}

export function deckCard(page: Page, deckId: string): Locator {
  return page.locator(`[aria-label^="Deck ${deckId},"]`);
}

export function jobNode(page: Page, jobId: string, status?: DeckJobStatus): Locator {
  if (status) {
    return page.getByRole("group", { name: `Job ${jobId}, status ${status}` });
  }
  return page.locator(`g[aria-label^="Job ${jobId},"]`);
}

export function dagViewer(page: Page, runId: string): Locator {
  // svg + aria-label — getByLabel avoids inconsistent img-role treatment.
  return page.getByLabel(`DAG for ${runId}`);
}

export function eventRow(page: Page, kind: string, seq?: number): Locator {
  if (seq !== undefined) {
    return page.getByRole("listitem", { name: `Event ${kind} seq ${seq}` });
  }
  return page.locator(`li[aria-label^="Event ${kind} seq "]`);
}

export function eventLog(page: Page): Locator {
  return page.getByRole("list", { name: "Event log" });
}

export function ambiguousBanner(page: Page): Locator {
  return page.getByLabel("Runs needing operator resolution");
}

export function connectionBanner(
  page: Page,
  state?: "OFFLINE" | "LIVE_PAUSED" | "DEGRADED_MODE",
): Locator {
  if (state) return page.getByLabel(`Connection ${state}`);
  return page.locator(`[aria-label^="Connection "]`).first();
}

/**
 * Restart moved to Settings > Fleet Management — navigate to /settings/fleet first.
 */
export function restartOrchestratorButton(page: Page): Locator {
  return page.getByRole("button", { name: "Restart gracefully" });
}

export function deckChaosButton(page: Page, deckId: string): Locator {
  return page.getByRole("button", { name: `Chaos controls for ${deckId}` });
}

export function cancelRunModal(page: Page): Locator {
  return page.getByRole("dialog", { name: "Cancel run" });
}

export function retryJobModal(page: Page): Locator {
  return page.getByRole("dialog", { name: "Retry failed job" });
}

export function resolveJobModal(page: Page): Locator {
  return page.getByRole("dialog", { name: /Resolve ambiguous job|Confirm . mark/ });
}

export function toastTitle(page: Page, text: string | RegExp): Locator {
  // No toast role yet — match title text; revisit if role="status" lands.
  return page.getByText(text).first();
}
