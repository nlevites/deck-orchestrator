import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { XCircle } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { Modal } from "@/components/primitives/Modal";
import { cancelRun, StateMovedError } from "@/lib/api";
import { useOperatorGate } from "@/lib/connection/operator-gate";
import { useToast } from "@/lib/toasts/use-toast";
import { countJobs } from "@/lib/run-derive";
import type { Run } from "@/lib/api-types";

interface CancelRunConfirmModalProps {
  open: boolean;
  onClose: () => void;
  run: Run;
}

/**
 * Per ARCHITECTURE.md §4.4: abort is best-effort with bounded retry; if the
 * abort signal arrives mid-physical-step, the current step runs to
 * completion and subsequent steps are skipped. The UI says so honestly
 * here, before the operator clicks the irreversible button.
 *
 * "Abort delivery fails" path: orchestrator still marks CANCELLED; if the
 * executor eventually completes the work and reports it, the event is
 * logged as a conflict per STATE_MACHINE.md §10.1.
 */
export function CancelRunConfirmModal({ open, onClose, run }: CancelRunConfirmModalProps) {
  const [acknowledged, setAcknowledged] = useState(false);
  const [stateMoved, setStateMoved] = useState<{ currentVersion: number } | null>(null);
  const toast = useToast();
  const gate = useOperatorGate();

  const counts = countJobs(run.deck_jobs);
  const inFlight = counts.dispatched + counts.running;
  const futureWaste = counts.pending + counts.ready;

  const reset = () => {
    setAcknowledged(false);
    setStateMoved(null);
  };
  const handleClose = () => {
    reset();
    onClose();
  };

  const mutation = useMutation({
    mutationFn: () => cancelRun({ runId: run.id, expectedVersion: run.version }),
    onSuccess: () => {
      toast.push({
        kind: "success",
        title: "Run cancelled",
        body:
          inFlight > 0
            ? `Abort signal dispatched to ${inFlight} in-flight job${inFlight === 1 ? "" : "s"}. Current steps may finish physically; subsequent steps will be skipped.`
            : "All eligible jobs were cancelled before dispatch.",
        timeoutMs: 8000,
      });
      // Cache refresh is intentionally driven by useLiveState's 1s
      // poll, not by invalidateQueries (the read caches use
      // cacheOnlyQueryFn which throws when invoked).
      handleClose();
    },
    onError: (err) => {
      if (err instanceof StateMovedError) {
        // Keep modal open on version mismatch — pre-fix closed silently.
        setStateMoved({ currentVersion: err.currentVersion });
        return;
      }
      toast.push({
        kind: "error",
        title: "Cancel failed",
        body: err instanceof Error ? err.message : "Unknown error.",
      });
      handleClose();
    },
  });

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title="Cancel run"
      eyebrow={run.id}
      size="md"
      footer={
        <>
          <Button variant="ghost" onClick={handleClose} disabled={mutation.isPending}>
            Keep running
          </Button>
          <Button
            variant="danger"
            disabled={!acknowledged || mutation.isPending || gate.disabled}
            onClick={() => mutation.mutate()}
            title={gate.disabled ? gate.reason : undefined}
          >
            <XCircle size={14} />
            {mutation.isPending ? "Cancelling…" : "Cancel run"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {stateMoved !== null ? (
          <div className="rounded-md border border-status-stale/50 bg-status-stale/10 p-3 text-[12.5px] leading-5 text-ink">
            <span className="font-semibold">State moved.</span> Another operator (or a background
            transition) advanced this run while you were reading. It is now at v
            {stateMoved.currentVersion}. Re-check the counts below and confirm again to cancel.
          </div>
        ) : null}
        <p className="text-[13px] leading-5 text-ink-muted">
          We&apos;ll dial an abort signal to each in-flight executor. Abort is{" "}
          <span className="font-semibold text-ink">best-effort</span>: if a deck has already started
          a physical step, that step runs to completion; subsequent steps in the same deck_job are
          skipped.
        </p>

        <ul className="flex flex-col gap-1 rounded-md border border-line bg-surface-subtle p-3 text-[12.5px] leading-5 text-ink">
          <li>
            <span className="font-semibold">{inFlight}</span> deck_job
            {inFlight === 1 ? "" : "s"} currently in flight (DISPATCHED / RUNNING) — best-effort
            abort sent.
          </li>
          <li>
            <span className="font-semibold">{futureWaste}</span> deck_job
            {futureWaste === 1 ? "" : "s"} not yet dispatched (PENDING / READY) — clean cancel, no
            materials consumed.
          </li>
          <li>
            <span className="font-semibold">{counts.ambiguous}</span> ambiguous — released from
            holding deck slots; outcomes stay null.
          </li>
        </ul>

        <label className="flex cursor-pointer items-start gap-2 rounded-md border border-line bg-surface-subtle p-3">
          <input
            type="checkbox"
            checked={acknowledged}
            onChange={(e) => setAcknowledged(e.target.checked)}
            className="mt-0.5 h-4 w-4 cursor-pointer accent-ink"
          />
          <span className="text-[12px] leading-5 text-ink">
            I understand that in-flight physical steps may still complete on the deck, and that this
            cancel is irreversible.
          </span>
        </label>
      </div>
    </Modal>
  );
}
