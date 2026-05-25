import { test as base } from "@playwright/test";
import * as apiModule from "../helpers/api";
import type { ChaosPatch } from "../helpers/api";

/**
 * Tracks chaos-touched decks for afterEach teardown. chaos.Hang sleeps until
 * process restart — clearing the flag does not unblock <-ctx.Done()>.
 */
type TestApi = typeof apiModule;

function wrapApi(touched: Set<string>): TestApi {
  return {
    ...apiModule,
    patchChaos: (deckId: string, patch: ChaosPatch) => {
      touched.add(deckId);
      return apiModule.patchChaos(deckId, patch);
    },
    crashDeck: (deckId: string) => {
      touched.add(deckId);
      return apiModule.crashDeck(deckId);
    },
  };
}

interface StackFixtures {
  api: TestApi;
  submit: (dag: apiModule.DagSubmission) => Promise<apiModule.Run>;
}

export const test = base.extend<StackFixtures>({
  api: async ({}, use) => {
    // Previous test may have crashed a deck still respawning.
    await apiModule.waitForDecksHealthy(undefined, 10_000).catch(() => {});

    const touched = new Set<string>();
    const client = wrapApi(touched);
    await use(client);

    // Reset flags, then crash — Hang workers need process restart, not flag clear.
    for (const id of touched) {
      await apiModule.resetChaos(id).catch(() => {});
    }
    for (const id of touched) {
      await apiModule.crashDeck(id).catch(() => {});
    }
    if (touched.size > 0) {
      await apiModule.waitForDecksHealthy(Array.from(touched), 10_000).catch(() => {});
    }
  },
  submit: async ({ api }, use) => {
    const created: string[] = [];
    await use(async (dag) => {
      const run = await api.submitRun(dag);
      created.push(run.id);
      return run;
    });
    for (const id of created) {
      try {
        const r = await api.getRun(id);
        if (r.status === "PENDING" || r.status === "RUNNING" || r.status === "AMBIGUOUS") {
          await api.cancelRun(id, r.version).catch(() => {});
        }
      } catch {
        // fetch failed during orchestrator restart
      }
    }
  },
});

export const expect = test.expect;
