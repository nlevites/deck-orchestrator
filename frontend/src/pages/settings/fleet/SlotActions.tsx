import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, Power, RotateCcw, Square, Trash2 } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { apiKeys } from "@/lib/api";
import { restartExecutor, startExecutor, stopExecutor } from "@/lib/api/supervisor";
import { useToast } from "@/lib/toasts/use-toast";
import type { SlotRow } from "./slot-state";

interface SlotActionsProps {
  slot: SlotRow;
  supervisorReachable: boolean;
  onAttach: () => void;
  onDetach: () => void;
  onRelease: () => void;
}

export function SlotActions({
  slot,
  supervisorReachable,
  onAttach,
  onDetach,
  onRelease,
}: SlotActionsProps) {
  const qc = useQueryClient();
  const toast = useToast();
  const deckId = slot.deckId;
  const state = slot.process?.state;

  const invalidate = () => qc.invalidateQueries({ queryKey: apiKeys.supervisor });

  const stop = useMutation({
    mutationFn: () => stopExecutor(deckId),
    onSuccess: invalidate,
    onError: (err) =>
      toast.push({
        kind: "error",
        title: `Stop ${deckId} failed`,
        body: err instanceof Error ? err.message : "Unknown error.",
      }),
  });
  const start = useMutation({
    mutationFn: () => startExecutor(deckId),
    onSuccess: invalidate,
    onError: (err) =>
      toast.push({
        kind: "error",
        title: `Start ${deckId} failed`,
        body: err instanceof Error ? err.message : "Unknown error.",
      }),
  });
  const restart = useMutation({
    mutationFn: () => restartExecutor(deckId),
    onSuccess: invalidate,
    onError: (err) =>
      toast.push({
        kind: "error",
        title: `Restart ${deckId} failed`,
        body: err instanceof Error ? err.message : "Unknown error.",
      }),
  });

  if (!slot.process) {
    // No supervisor row. Two paths: attach a new executor, or (if the
    // orchestrator slot is stuck non-EMPTY because the previous
    // executor died outside the supervisor's watch) manually release
    // the slot back to EMPTY.
    const canRelease =
      slot.deck && slot.deck.last_known_health !== "EMPTY" && !slot.deck.decommissioned_at;
    return (
      <>
        <Button
          variant="primary"
          size="sm"
          onClick={onAttach}
          disabled={!supervisorReachable}
          title={!supervisorReachable ? "Supervisor unreachable" : undefined}
        >
          <Plus size={12} />
          Attach executor
        </Button>
        {canRelease ? (
          <Button
            variant="ghost"
            size="sm"
            onClick={onRelease}
            title="Force orchestrator slot back to EMPTY"
          >
            <Trash2 size={12} />
            Release slot
          </Button>
        ) : null}
      </>
    );
  }

  const buttons: React.ReactNode[] = [];
  if (state === "Running" || state === "Starting") {
    buttons.push(
      <Button
        key="restart"
        variant="secondary"
        size="sm"
        onClick={() => restart.mutate()}
        disabled={restart.isPending}
      >
        <RotateCcw size={12} />
        Restart
      </Button>,
      <Button
        key="stop"
        variant="ghost"
        size="sm"
        onClick={() => stop.mutate()}
        disabled={stop.isPending}
      >
        <Square size={12} />
        Stop
      </Button>,
    );
  } else if (state === "Stopped" || state === "FatalConfig" || state === "Crashing") {
    buttons.push(
      <Button
        key="start"
        variant="secondary"
        size="sm"
        onClick={() => start.mutate()}
        disabled={start.isPending}
      >
        <Power size={12} />
        Start
      </Button>,
    );
  }
  buttons.push(
    <Button key="detach" variant="ghost" size="sm" onClick={onDetach}>
      <Trash2 size={12} />
      Detach
    </Button>,
  );
  return <>{buttons}</>;
}
