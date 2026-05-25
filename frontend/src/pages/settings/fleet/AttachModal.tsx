import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { Modal } from "@/components/primitives/Modal";
import { apiKeys } from "@/lib/api";
import { attachExecutor } from "@/lib/api/supervisor";
import { useOperatorGate } from "@/lib/connection/operator-gate";
import { useToast } from "@/lib/toasts/use-toast";

interface AttachModalProps {
  onClose: () => void;
  deckId: string;
}

export function AttachModal({ onClose, deckId }: AttachModalProps) {
  const [freshState, setFreshState] = useState(false);
  const qc = useQueryClient();
  const toast = useToast();
  const gate = useOperatorGate();

  const attach = useMutation({
    mutationFn: () => attachExecutor(deckId, freshState),
    onSuccess: () => {
      toast.push({
        kind: "info",
        title: `Attached executor to ${deckId}`,
        body: "It should heartbeat in within a second.",
        timeoutMs: 4000,
      });
      qc.invalidateQueries({ queryKey: apiKeys.supervisor });
      onClose();
    },
    onError: (err) =>
      toast.push({
        kind: "error",
        title: `Attach ${deckId} failed`,
        body: err instanceof Error ? err.message : "Unknown error.",
      }),
  });

  return (
    <Modal
      open
      onClose={onClose}
      title={`Attach executor to ${deckId}`}
      eyebrow="Supervisor"
      size="sm"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={attach.isPending}>
            Cancel
          </Button>
          <Button
            variant="primary"
            onClick={() => attach.mutate()}
            disabled={attach.isPending || gate.disabled}
            title={gate.disabled ? gate.reason : undefined}
          >
            <Plus size={14} />
            {attach.isPending ? "Attaching…" : "Attach"}
          </Button>
        </>
      }
    >
      <p className="text-[13px] leading-5 text-ink-muted">
        The supervisor will spawn an executor binary, allocate a port, and watch the process. The
        executor heartbeats the orchestrator, which transitions {deckId} from{" "}
        <code className="font-mono">EMPTY</code> to <code className="font-mono">HEALTHY</code>.
      </p>
      <label className="mt-3 flex items-center gap-2 text-[12.5px] text-ink">
        <input
          type="checkbox"
          checked={freshState}
          onChange={(e) => setFreshState(e.target.checked)}
        />
        Fresh state (delete local SQLite before launch)
      </label>
      <p className="mt-1 text-[11.5px] text-ink-sub">
        Off by default so the executor resumes any in-flight outbox from before a previous detach.
      </p>
    </Modal>
  );
}
