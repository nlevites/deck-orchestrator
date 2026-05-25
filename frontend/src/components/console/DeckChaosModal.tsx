import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bug, RotateCcw, Skull } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { Modal } from "@/components/primitives/Modal";
import {
  apiKeys,
  getDeckChaos,
  patchDeckChaos,
  resetDeckChaos,
  crashDeck,
  type ChaosPatch,
} from "@/lib/api";
import { useOperatorGate } from "@/lib/connection/operator-gate";
import { useToast } from "@/lib/toasts/use-toast";

interface DeckChaosModalProps {
  open: boolean;
  onClose: () => void;
  deckId: string;
}

/**
 * Executor chaos/test-control panel. Flags persist until cleared or reset.
 */
export function DeckChaosModal({ open, onClose, deckId }: DeckChaosModalProps) {
  const toast = useToast();
  const qc = useQueryClient();
  const gate = useOperatorGate();

  const stateQuery = useQuery({
    queryKey: apiKeys.chaos(deckId),
    queryFn: () => getDeckChaos(deckId),
    enabled: open,
    refetchInterval: open ? 1000 : false,
  });

  // The chaos-state query (apiKeys.chaos(deckId)) is a *real* fetched
  // query — setQueryData here is fine. The decks/runs caches are
  // cache-only (cacheOnlyQueryFn throws); their refresh comes from
  // useLiveState's 1s poll.
  const patchMutation = useMutation({
    mutationFn: (patch: ChaosPatch) => patchDeckChaos(deckId, patch),
    onSuccess: (state) => {
      qc.setQueryData(apiKeys.chaos(deckId), state);
    },
    onError: (err) => {
      toast.push({
        kind: "error",
        title: "Chaos patch failed",
        body: err instanceof Error ? err.message : "Unknown error.",
      });
    },
  });

  const resetMutation = useMutation({
    mutationFn: () => resetDeckChaos(deckId),
    onSuccess: (state) => {
      qc.setQueryData(apiKeys.chaos(deckId), state);
      toast.push({
        kind: "success",
        title: "Chaos cleared",
        body: `All flags on ${deckId} are off.`,
        timeoutMs: 4000,
      });
    },
  });

  const crashMutation = useMutation({
    mutationFn: () => crashDeck(deckId),
    onSuccess: () => {
      toast.push({
        kind: "warning",
        title: "Crash sent",
        body: `${deckId} should be UNREACHABLE within a few seconds; supervisor will relaunch it.`,
        timeoutMs: 6000,
      });
      onClose();
    },
    onError: (err) => {
      toast.push({
        kind: "error",
        title: "Crash failed",
        body: err instanceof Error ? err.message : "Unknown error.",
      });
    },
  });

  const state = stateQuery.data;
  const busy =
    patchMutation.isPending || resetMutation.isPending || crashMutation.isPending || gate.disabled;

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="Chaos controls"
      eyebrow={`${deckId}`}
      size="md"
      footer={
        <>
          <Button
            variant="ghost"
            onClick={onClose}
            disabled={patchMutation.isPending || resetMutation.isPending || crashMutation.isPending}
          >
            Close
          </Button>
          <Button
            variant="ghost"
            onClick={() => resetMutation.mutate()}
            disabled={busy}
            title={gate.disabled ? gate.reason : undefined}
          >
            <RotateCcw size={14} />
            Reset all
          </Button>
          <Button
            variant="danger"
            onClick={() => crashMutation.mutate()}
            disabled={busy}
            title={gate.disabled ? gate.reason : undefined}
          >
            <Skull size={14} />
            Crash now
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <p className="text-[13px] leading-5 text-ink-muted">
          Inject failure modes on this executor. Flags persist until cleared. See the system react
          in the deck card and run details.
        </p>

        {stateQuery.isLoading && (
          <div className="text-[12px] text-ink-muted">Loading current state…</div>
        )}

        {stateQuery.isError && (
          <div className="text-[12px] text-status-failed">
            Could not load chaos state: {String(stateQuery.error)}
          </div>
        )}

        {state && (
          <div className="flex flex-col gap-2 rounded-md border border-line bg-surface-subtle p-3">
            <ChaosToggle
              label="Hang"
              hint="Worker blocks at the next step boundary."
              value={state.hang}
              onChange={(v) => patchMutation.mutate({ hang: v })}
              disabled={busy}
            />
            <ChaosToggle
              label="Silent (no heartbeats)"
              hint="Deck goes STALE then UNREACHABLE."
              value={state.silent}
              onChange={(v) => patchMutation.mutate({ silent: v })}
              disabled={busy}
            />
            <ChaosToggle
              label="Drop events"
              hint="Outbox stops delivering; events pile up locally."
              value={state.drop_events}
              onChange={(v) => patchMutation.mutate({ drop_events: v })}
              disabled={busy}
            />
            <ChaosToggle
              label="Pause egress (deck → orchestrator)"
              hint="Outbound HTTP returns an immediate error."
              value={state.pause_egress}
              onChange={(v) => patchMutation.mutate({ pause_egress: v })}
              disabled={busy}
            />
            <ChaosToggle
              label="Pause ingress (orchestrator → deck)"
              hint="Reconcile & abort calls drop. Chaos route still answers."
              value={state.pause_ingress}
              onChange={(v) => patchMutation.mutate({ pause_ingress: v })}
              disabled={busy}
            />
          </div>
        )}

        <p className="flex items-start gap-2 text-[11px] leading-4 text-ink-muted">
          <Bug size={12} className="mt-0.5" />
          Crash is a one-shot process exit. The demo supervisor respawns the executor; the
          orchestrator reconciles whatever it left behind.
        </p>
      </div>
    </Modal>
  );
}

interface ChaosToggleProps {
  label: string;
  hint: string;
  value: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
}

function ChaosToggle({ label, hint, value, onChange, disabled }: ChaosToggleProps) {
  // Optimistic toggle; sync from server via render-time prevValue check (not useEffect).
  const [optimistic, setOptimistic] = useState(value);
  const [prevValue, setPrevValue] = useState(value);
  if (prevValue !== value) {
    setPrevValue(value);
    setOptimistic(value);
  }

  return (
    <label className="flex cursor-pointer items-start gap-3 rounded-md p-2 transition-colors hover:bg-surface">
      <input
        type="checkbox"
        checked={optimistic}
        disabled={disabled}
        onChange={(e) => {
          setOptimistic(e.target.checked);
          onChange(e.target.checked);
        }}
        className="mt-0.5 h-4 w-4 cursor-pointer accent-ink"
      />
      <div className="flex flex-col">
        <span className="text-[13px] font-medium tracking-sub text-ink">{label}</span>
        <span className="text-[11px] leading-4 text-ink-muted">{hint}</span>
      </div>
    </label>
  );
}
