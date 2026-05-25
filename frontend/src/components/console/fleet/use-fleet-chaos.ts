import { useMemo } from "react";
import { useQueries } from "@tanstack/react-query";
import { apiKeys, getDeckChaos, isChaosActive, type ChaosState } from "@/lib/api";

/**
 * Page-level chaos-state query for a slice of decks. The fleet page
 * passes only the deck ids it actually renders prominently
 * (attention + active) — idle decks don't display the chaos badge,
 * so polling them adds noise without operator value.
 *
 * Each deck still translates to one query under the hood (the chaos
 * endpoint is per-deck) but co-locating the calls here keeps cadence
 * + retry policy consistent and prevents components from re-deciding
 * the same thing inside their own render trees.
 *
 * Errors are swallowed: an executor that's down won't have chaos
 * state, and that's fine — the badge stays off rather than throwing.
 */
export function useFleetChaos(deckIds: ReadonlyArray<string>): ReadonlyMap<string, boolean> {
  // Snapshot the ids by joined string so the queries[] identity only
  // changes when the actual list changes.
  const idKey = deckIds.join("|");

  const queries = useMemo(() => {
    return deckIds.map((id) => ({
      queryKey: apiKeys.chaos(id),
      queryFn: () => getDeckChaos(id),
      refetchInterval: 5000,
      staleTime: 4000,
      retry: false,
    }));
    // deckIds is reconstructed each render; idKey is the stable identity.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [idKey]);

  const result = useQueries({
    queries,
    combine: (results) => {
      const m = new Map<string, boolean>();
      deckIds.forEach((id, i) => {
        const data = (results[i]?.data as ChaosState | undefined) ?? undefined;
        m.set(id, isChaosActive(data));
      });
      return m;
    },
  });

  return result;
}
