/**
 * FleetManagementPage — dev-only. Cross-joins orchestrator deck health
 * (useLiveState cache) with supervisor process state (useSupervisorState).
 */
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { apiKeys } from "@/lib/api";
import type { Deck } from "@/lib/api-types";
import { useSupervisorState } from "@/lib/live/use-supervisor-state";
import { OrchestratorPanel } from "./fleet/OrchestratorPanel";
import { FleetHeatmap } from "./fleet/FleetHeatmap";
import { joinSlots } from "./fleet/slot-state";

export function FleetManagementPage() {
  const decksQ = useQuery<Deck[]>({
    queryKey: apiKeys.decks,
    queryFn: async () => [],
    enabled: false,
    staleTime: Infinity,
  });
  const supQ = useSupervisorState();

  const slots = useMemo(
    () => joinSlots(decksQ.data ?? [], supQ.data?.executors ?? []),
    [decksQ.data, supQ.data?.executors],
  );
  const supervisorReachable = !supQ.isError && supQ.data !== undefined;

  return (
    <div className="flex flex-col gap-6">
      <OrchestratorPanel processes={supQ.data} loading={supQ.isLoading} />
      <FleetHeatmap slots={slots} supervisorReachable={supervisorReachable} />
    </div>
  );
}
