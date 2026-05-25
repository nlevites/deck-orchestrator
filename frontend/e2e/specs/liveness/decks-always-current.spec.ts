import { test, expect } from "../../fixtures/stack";

// Regression: delta /api/state used to omit decks → heartbeat timestamps frozen ~60s.
// Contract today (post-S3): bootstrap carries full `decks[]`; delta polls carry
// `decks_delta[]` with only touched-since rows. Either way, heartbeat advances
// must reach the client every poll for at least one deck.
test("decks slice carries fresh last_heartbeat_at on every /api/state response", async ({
  api,
}) => {
  const first = await api.getState(0);
  expect(first.decks, "bootstrap response carries decks").toBeTruthy();
  expect(first.decks!.length).toBeGreaterThanOrEqual(4);

  const firstHeartbeats = new Map(first.decks!.map((d) => [d.id, d.last_heartbeat_at]));

  // Executor heartbeat ~250ms in e2e; 1.5s absorbs scheduler jitter.
  await new Promise((r) => setTimeout(r, 1500));

  const second = await api.getState(first.server_seq);
  const secondDecks = second.decks ?? second.decks_delta;
  expect(
    secondDecks,
    "delta response carries decks or decks_delta (always-current contract)",
  ).toBeTruthy();
  expect(secondDecks!.length).toBeGreaterThan(0);

  // At least one deck must advance — avoids demanding all decks on slow CI.
  let advanced = 0;
  for (const d of secondDecks!) {
    const prev = firstHeartbeats.get(d.id);
    if (
      prev &&
      d.last_heartbeat_at &&
      new Date(d.last_heartbeat_at).getTime() > new Date(prev).getTime()
    ) {
      advanced++;
    }
  }
  expect(advanced, "at least one deck heartbeat advanced past bootstrap snapshot").toBeGreaterThan(
    0,
  );
});
