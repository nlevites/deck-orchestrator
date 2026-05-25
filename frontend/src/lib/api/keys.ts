/**
 * Centralised query keys. Co-locating them prevents the "two places ask for
 * the same data with subtly-different keys" bug that breaks cache invalidation.
 *
 * Convention: factory functions that return tuples. Pass these directly to
 * useQuery and to queryClient.invalidateQueries.
 */
export const apiKeys = {
  runs: ["runs"] as const,
  run: (id: string) => ["runs", id] as const,
  decks: ["decks"] as const,
  deck: (id: string) => ["decks", id] as const,
  events: ["events"] as const,
  eventsForRun: (runId: string) => ["events", "run", runId] as const,
  chaos: (deckId: string) => ["chaos", deckId] as const,
  supervisor: ["supervisor"] as const,
};
