import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { Modal } from "@/components/primitives/Modal";
import { retryJob, StateMovedError } from "@/lib/api";
import { useOperatorGate } from "@/lib/connection/operator-gate";
import { useToast } from "@/lib/toasts/use-toast";
import type { DeckJob, Run } from "@/lib/api-types";

interface RetryConfirmModalProps {
  open: boolean;
  onClose: () => void;
  run: Run;
  job: DeckJob;
}

/**
 * Per ARCHITECTURE.md §5.1: "retrying an attempt is never done implicitly".
 * Operator must affirm the deck is in a known-good state before we
 * allocate a new attempt_id.
 *
 * The checkbox is a deliberate friction step. Without it the Retry button
 * is one click away from re-running physical work on a deck whose state
 * the operator hasn't actually inspected.
 */
export function RetryConfirmModal({ open, onClose, run, job }: RetryConfirmModalProps) {
  const [acknowledged, setAcknowledged] = useState(false);
  const [stateMoved, setStateMoved] = useState<{ currentVersion: number } | null>(null);
  const toast = useToast();
  const gate = useOperatorGate();

  const reset = () => {
    setAcknowledged(false);
    setStateMoved(null);
  };
  const handleClose = () => {
    reset();
    onClose();
  };

  const mutation = useMutation({
    mutationFn: () =>
      retryJob({
        runId: run.id,
        jobId: job.id,
        expectedVersion: job.version,
      }),
    onSuccess: () => {
      toast.push({
        kind: "success",
        title: `${job.id} re-queued`,
        body: "A new attempt_id will be allocated when the dispatcher next claims the deck.",
        timeoutMs: 8000,
      });
      // Cache refresh is intentionally driven by useLiveState's 1s
      // poll, not by invalidateQueries (the read caches use
      // cacheOnlyQueryFn which throws when invoked).
      handleClose();
    },
    onError: (err) => {
      if (err instanceof StateMovedError) {
        // Keep modal open on version mismatch so the operator sees what changed.
        setStateMoved({ currentVersion: err.currentVersion });
        return;
      }
      toast.push({
        kind: "error",
        title: "Retry failed",
        body: err instanceof Error ? err.message : "Unknown error.",
      });
      handleClose();
    },
  });

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title="Retry failed job"
      eyebrow={`${run.id} · ${job.id}`}
      size="md"
      footer={
        <>
          <Button variant="ghost" onClick={handleClose} disabled={mutation.isPending}>
            Cancel
          </Button>
          <Button
            disabled={!acknowledged || mutation.isPending || gate.disabled}
            onClick={() => mutation.mutate()}
            title={gate.disabled ? gate.reason : undefined}
          >
            <RefreshCw size={14} />
            {mutation.isPending ? "Re-queueing…" : "Retry job"}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {stateMoved !== null ? (
          <div className="rounded-md border border-status-stale/50 bg-status-stale/10 p-3 text-[12.5px] leading-5 text-ink">
            <span className="font-semibold">State moved.</span> {job.id} advanced to v
            {stateMoved.currentVersion} since you opened this modal. Confirm again if retry still
            applies.
          </div>
        ) : null}
        <p className="text-[13px] leading-5 text-ink-muted">
          Retrying allocates a new <span className="font-mono text-ink">attempt_id</span> and
          re-runs the job from the start on{" "}
          <span className="font-mono text-ink">{job.deck_id}</span>. The previous attempt&apos;s
          record stays on file for audit.
        </p>

        {job.recent_attempts?.[0]?.error && (
          <div className="rounded-md border border-[#fbe7e3] bg-[#fff3f1] px-3 py-2 text-[12px] leading-5 text-status-failed">
            Previous failure: {job.recent_attempts[0].error}
          </div>
        )}

        <label className="mt-1 flex cursor-pointer items-start gap-2 rounded-md border border-line bg-surface-subtle p-3">
          <input
            type="checkbox"
            checked={acknowledged}
            onChange={(e) => setAcknowledged(e.target.checked)}
            className="mt-0.5 h-4 w-4 cursor-pointer accent-ink"
          />
          <span className="text-[12px] leading-5 text-ink">
            I&apos;ve verified that <span className="font-mono">{job.deck_id}</span> is in a
            known-good physical state — fresh reagents, no contamination — and that re-running this
            job is intentional.
          </span>
        </label>
      </div>
    </Modal>
  );
}
