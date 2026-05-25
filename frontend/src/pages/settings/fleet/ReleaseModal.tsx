import { useMutation } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { Modal } from "@/components/primitives/Modal";
import { releaseDeck } from "@/lib/api/mutations";
import { useOperatorGate } from "@/lib/connection/operator-gate";
import { useToast } from "@/lib/toasts/use-toast";

interface ReleaseModalProps {
  onClose: () => void;
  deckId: string;
}

export function ReleaseModal({ onClose, deckId }: ReleaseModalProps) {
  const toast = useToast();
  const gate = useOperatorGate();
  const release = useMutation({
    mutationFn: () => releaseDeck(deckId),
    onSuccess: () => {
      toast.push({
        kind: "info",
        title: `Released ${deckId}`,
        body: "Slot is back to EMPTY.",
        timeoutMs: 4000,
      });
      // Cache refresh is intentionally driven by useLiveState's 1s
      // poll, not by invalidateQueries -- apiKeys.decks is registered
      // with cacheOnlyQueryFn (which throws), so invalidating it puts
      // every subscriber into a brief error state.
      onClose();
    },
    onError: (err) =>
      toast.push({
        kind: "error",
        title: `Release ${deckId} failed`,
        body: err instanceof Error ? err.message : "Unknown error.",
      }),
  });
  return (
    <Modal
      open
      onClose={onClose}
      title={`Release ${deckId}`}
      eyebrow="Orchestrator"
      size="sm"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={release.isPending}>
            Cancel
          </Button>
          <Button
            variant="danger"
            onClick={() => release.mutate()}
            disabled={release.isPending || gate.disabled}
            title={gate.disabled ? gate.reason : undefined}
          >
            <Trash2 size={14} />
            {release.isPending ? "Releasing…" : "Release slot"}
          </Button>
        </>
      }
    >
      <p className="text-[13px] leading-5 text-ink-muted">
        Force the orchestrator to mark {deckId} as <code className="font-mono">EMPTY</code>: clears
        endpoint URL and last heartbeat, no executor change. Use this for a slot stuck at{" "}
        <code className="font-mono">UNREACHABLE</code> whose executor you know is gone for good
        (e.g. killed outside the supervisor&apos;s watch).
      </p>
      <p className="mt-2 text-[12px] leading-5 text-ink-muted">
        Refused if any deck_job is currently DISPATCHED / RUNNING / AMBIGUOUS on this slot — cancel
        or resolve first so released attempts aren&apos;t silently abandoned.
      </p>
    </Modal>
  );
}
