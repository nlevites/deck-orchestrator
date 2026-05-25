/**
 * useSupervisorState — polls the dev-only supervisor sidecar for the
 * process-table snapshot. Mounted by the Settings > Fleet Management
 * page (and only there).
 *
 * Polling is 1 Hz to match the orchestrator's `useLiveState` cadence;
 * the Fleet Management grid cross-joins these two streams by `deck_id`
 * to render the Process + Health columns.
 *
 * Errors are swallowed by the query client and the data goes stale; in
 * a dev-only setup that's the right behaviour (the supervisor may be
 * down on purpose, e.g. while running the orchestrator standalone).
 */
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { apiKeys } from "@/lib/api/keys";
import { listSupervisorProcesses, type ProcessesResponse } from "@/lib/api/supervisor";

const POLL_INTERVAL_MS = 1_000;

export function useSupervisorState(): UseQueryResult<ProcessesResponse> {
  return useQuery({
    queryKey: apiKeys.supervisor,
    queryFn: listSupervisorProcesses,
    refetchInterval: POLL_INTERVAL_MS,
    refetchOnWindowFocus: false,
    refetchOnReconnect: true,
    // The supervisor is dev-only; surface errors quietly so the UI can
    // render "supervisor unavailable" without spamming the console.
    retry: false,
    staleTime: 0,
  });
}
