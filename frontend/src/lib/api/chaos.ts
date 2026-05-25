/**
 * Chaos / test-control API client.
 *
 * Unlike the rest of `lib/api/` — which still talks to the in-process
 * mock store — these calls hit the real orchestrator at `/api/decks/:id/chaos*`
 * via the Vite dev-server proxy. The orchestrator then proxies into the
 * per-deck executor.
 *
 * Persistent-flag model: hang/silent/drop_events/pause_egress/pause_ingress
 * stay set on the executor until the operator clears them. Crash is a
 * one-shot process exit. See docs/DESIGN.md "Chaos / test controls".
 */
import type { components } from "@/api/gen";

export type ChaosState = components["schemas"]["ChaosState"];
export type ChaosPatch = components["schemas"]["ChaosPatch"];

export async function getDeckChaos(deckId: string): Promise<ChaosState> {
  const resp = await fetch(`/api/decks/${encodeURIComponent(deckId)}/chaos`, {
    method: "GET",
    headers: { Accept: "application/json" },
  });
  if (!resp.ok) throw await chaosError(resp, "fetch chaos state");
  return (await resp.json()) as ChaosState;
}

export async function patchDeckChaos(deckId: string, patch: ChaosPatch): Promise<ChaosState> {
  const resp = await fetch(`/api/decks/${encodeURIComponent(deckId)}/chaos`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(patch),
  });
  if (!resp.ok) throw await chaosError(resp, "patch chaos state");
  return (await resp.json()) as ChaosState;
}

export async function resetDeckChaos(deckId: string): Promise<ChaosState> {
  const resp = await fetch(`/api/decks/${encodeURIComponent(deckId)}/chaos/reset`, {
    method: "POST",
    headers: { Accept: "application/json" },
  });
  if (!resp.ok) throw await chaosError(resp, "reset chaos state");
  return (await resp.json()) as ChaosState;
}

export async function crashDeck(deckId: string): Promise<void> {
  const resp = await fetch(`/api/decks/${encodeURIComponent(deckId)}/chaos/crash`, {
    method: "POST",
    headers: { Accept: "application/json" },
  });
  if (!resp.ok) throw await chaosError(resp, "crash executor");
  // Body is `{status:"crashing"}` but we don't surface it — the visible
  // signal is the deck going UNREACHABLE in the next few heartbeat cycles.
}

export function isChaosActive(state: ChaosState | undefined): boolean {
  if (!state) return false;
  return (
    state.hang ||
    state.silent ||
    state.drop_events ||
    state.pause_egress ||
    state.pause_ingress ||
    state.hang_after_step > 0 ||
    state.crash_after_step > 0
  );
}

async function chaosError(resp: Response, context: string): Promise<Error> {
  let body = "";
  try {
    body = await resp.text();
  } catch {
    // ignore
  }
  return new Error(`${context}: ${resp.status} ${body}`);
}
