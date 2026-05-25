/**
 * Single import surface for every component that needs to mutate
 * orchestrator state or talk to the chaos / admin surfaces.
 *
 * Reads do NOT live here — the React console reads from the TanStack
 * Query cache populated by `useLiveState` (see `lib/live/`). Pages
 * `useQuery({ queryKey, queryFn: cacheOnlyQueryFn, staleTime:
 * Infinity })` to subscribe; never call lib/api for reads.
 */
export { apiKeys } from "./keys";
export {
  submitRun,
  cancelRun,
  retryJob,
  resolveJob,
  releaseDeck,
  StateMovedError,
  ApiError,
} from "./mutations";
export {
  getDeckChaos,
  patchDeckChaos,
  resetDeckChaos,
  crashDeck,
  isChaosActive,
  type ChaosState,
  type ChaosPatch,
} from "./chaos";
export { restartOrchestrator } from "./admin";
