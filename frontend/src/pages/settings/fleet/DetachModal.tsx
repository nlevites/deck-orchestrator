import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { Modal } from "@/components/primitives/Modal";
import { apiKeys } from "@/lib/api";
import { detachExecutor } from "@/lib/api/supervisor";
import { useOperatorGate } from "@/lib/connection/operator-gate";
import { useToast } from "@/lib/toasts/use-toast";

interface DetachModalProps {
  onClose: () => void;
  deckId: string;
}

export function DetachModal({ onClose, deckId }: DetachModalProps) {
  const qc = useQueryClient();
  const toast = useToast();
  const gate = useOperatorGate();
  const detach = useMutation({
    mutationFn: () => detachExecutor(deckId),
    onSuccess: () => {
      toast.push({
        kind: "info",
        title: `Detached executor from ${deckId}`,
        timeoutMs: 4000,
      });
      qc.invalidateQueries({ queryKey: apiKeys.supervisor });
      onClose();
    },
    onError: (err) =>
      toast.push({
        kind: "error",
        title: `Detach ${deckId} failed`,
        body: err instanceof Error ? err.message : "Unknown error.",
      }),
  });
  return (
    <Modal
      open
      onClose={onClose}
      title={`Detach executor from ${deckId}`}
      eyebrow="Supervisor"
      size="sm"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={detach.isPending}>
            Cancel
          </Button>
          <Button
            variant="danger"
            onClick={() => detach.mutate()}
            disabled={detach.isPending || gate.disabled}
            title={gate.disabled ? gate.reason : undefined}
          >
            <Trash2 size={14} />
            {detach.isPending ? "Detaching…" : "Detach"}
          </Button>
        </>
      }
    >
      <p className="text-[13px] leading-5 text-ink-muted">
        The supervisor stops the executor and frees its port, then asks the orchestrator to release{" "}
        {deckId} back to <code className="font-mono">EMPTY</code> so the slot is ready for a fresh
        attach. Pending jobs targeting {deckId} stay <code className="font-mono">READY</code> until
        you re-attach.
      </p>
      <p className="mt-2 text-[12px] leading-5 text-ink-muted">
        On-disk state is preserved (leave Fresh state off on re-attach to resume). Refused if any
        deck_job is currently DISPATCHED / RUNNING / AMBIGUOUS on this slot — cancel or resolve
        first.
      </p>
    </Modal>
  );
}
