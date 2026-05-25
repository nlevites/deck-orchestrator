/**
 * Cache-only useQuery configuration helpers.
 *
 * The console's read path is a projection of the orchestrator's event
 * log, populated by `useLiveState` in `lib/live/`. Pages never call
 * `fetch` for reads — they use `useQuery({ queryKey, queryFn:
 * cacheOnlyQueryFn, staleTime: Infinity })` to read from the cache the
 * live hook writes to.
 *
 * `cacheOnlyQueryFn` is a sentinel. Combined with `staleTime: Infinity`
 * and TanStack Query's "don't refetch a fresh cache" semantics, it
 * shouldn't actually fire in normal operation. If it does (e.g. a
 * `refetchQueries` somewhere bypasses staleTime), it throws so the
 * mistake surfaces loudly during development rather than silently
 * blanking the cache.
 */
export function cacheOnlyQueryFn<T>(): Promise<T> {
  throw new Error(
    "cacheOnlyQueryFn invoked: this cache is populated by useLiveState; do not refetch directly",
  );
}
