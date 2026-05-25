import { test, expect } from "../../fixtures/stack";

/**
 * Pre-fix: idle decks stuck at STALE forever — liveness sweep only escalated
 * inside the in-flight-job loop. Post-fix: staleHeartbeatScan flips STALE →
 * UNREACHABLE once heartbeat age exceeds AmbiguousDeadline (3s in e2e).
 */
test("idle deck escalates STALE -> UNREACHABLE once heartbeat age exceeds AmbiguousDeadline", async ({
  api,
}) => {
  const deckId = "deck-4";

  await api.patchChaos(deckId, { silent: true });

  await expect
    .poll(
      async () => {
        const decks = await api.listDecks();
        return decks.find((d) => d.id === deckId)?.last_known_health;
      },
      { timeout: 5_000, intervals: [200, 400] },
    )
    .toBe("STALE");

  // AmbiguousDeadline 3s + sweep interval 2s + slack.
  await expect
    .poll(
      async () => {
        const decks = await api.listDecks();
        return decks.find((d) => d.id === deckId)?.last_known_health;
      },
      { timeout: 12_000, intervals: [500, 1000] },
    )
    .toBe("UNREACHABLE");
});
