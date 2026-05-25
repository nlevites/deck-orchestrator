/**
 * Deck-scoped event reducers.
 *
 * DECK_REGISTERED is advisory: bootstrap picks up the new deck on the
 * next poll rebootstrap. Synthesizing a Deck row from event payload
 * would require fields the event doesn't carry (last_heartbeat_at).
 *
 * DECK_HEALTH_CHANGED rolls the deck's last_known_health to the
 * payload's `to` value and stamps last_heartbeat_at from
 * occurred_at.
 */
import type { QueryClient } from "@tanstack/react-query";
import { apiKeys } from "@/lib/api/keys";
import type { Deck, DeckHealthRaw, Event } from "@/lib/api-types";
import { setDeckHealth } from "@/lib/live/helpers";

export function applyDeckRegistered(_qc: QueryClient, _e: Event): void {}

export function applyDeckHealthChanged(qc: QueryClient, e: Event): void {
  if (!e.deck_id) return;
  const to = (e.payload?.["to"] ?? null) as DeckHealthRaw | null;
  if (!to) return;
  qc.setQueryData<Deck[]>(apiKeys.decks, (prev) =>
    setDeckHealth(prev, e.deck_id!, to, e.occurred_at),
  );
}

export function applyExecutorConflictLogged(_qc: QueryClient, _e: Event): void {
  // Audit-only event: no entity state changes — the orchestrator's
  // refusal is the visible state, recorded earlier in the same
  // transaction. We leave the event in the events cache via the
  // dispatcher's appendEventToCache; nothing else to do here.
}
