import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2 } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { Modal } from "@/components/primitives/Modal";
import { resolveJob, StateMovedError } from "@/lib/api";
import { useOperatorGate } from "@/lib/connection/operator-gate";
import { useToast } from "@/lib/toasts/use-toast";
import type { AttemptOutcome, DeckJob, Run } from "@/lib/api-types";
import { cn } from "@/lib/cn";

interface AmbiguousResolutionModalProps {
  open: boolean;
  onClose: () => void;
  run: Run;
  job: DeckJob;
}

/**
 * Two-step resolution per STATE_MACHINE.md §3.2:
 *   1. Declare the *physical outcome*: did the work happen on the deck or not?
 *   2. Confirm — explicit second click acknowledges the operator inspected
 *      the deck and the choice is intentional.
 *
 * The state machine forbids AMBIGUOUS → READY directly; the operator must
 * decide what happened before deciding what to do next. If they pick FAILED,
 * a follow-up Retry from the FAILED state is offered separately (Retry modal).
 */
export function AmbiguousResolutionModal({
  open,
  onClose,
  run,
  job,
}: AmbiguousResolutionModalProps) {
  const [step, setStep] = useState<"declare" | "confirm">("declare");
  const [outcome, setOutcome] = useState<AttemptOutcome | null>(null);
  const [note, setNote] = useState("");
  const [stateMoved, setStateMoved] = useState<{ currentVersion: number } | null>(null);
  const toast = useToast();
  const gate = useOperatorGate();

  const reset = () => {
    setStep("declare");
    setOutcome(null);
    setNote("");
    setStateMoved(null);
  };
  const handleClose = () => {
    reset();
    onClose();
  };

  const trimmedNote = note.trim();
  const mutation = useMutation({
    mutationFn: async (chosen: AttemptOutcome) => {
      await resolveJob({
        runId: run.id,
        jobId: job.id,
        expectedVersion: job.version,
        outcome: chosen,
        operatorNote: trimmedNote || undefined,
      });
      return chosen;
    },
    onSuccess: (chosen) => {
      toast.push({
        kind: "success",
        title: `${job.id} marked ${chosen.toLowerCase()}`,
        body:
          chosen === "COMPLETED"
            ? "Deck slot released; downstream jobs are now eligible to dispatch."
            : "Deck slot released. Retry the job if you want to re-run the work.",
        timeoutMs: 8000,
      });
      // Cache refresh is intentionally driven by useLiveState's 1s
      // poll, not by invalidateQueries. The runs/run/decks/events
      // caches use cacheOnlyQueryFn (which throws); invalidateQueries
      // would route through it and put queries into an error state.
      handleClose();
    },
    onError: (err: unknown) => {
      if (err instanceof StateMovedError) {
        // Step back to declare so the operator can re-evaluate after a version conflict.
        // Keep `outcome` and `note` so the operator doesn't lose their typed context.
        setStateMoved({ currentVersion: err.currentVersion });
        setStep("declare");
        return;
      }
      toast.push({
        kind: "error",
        title: "Resolution failed",
        body: err instanceof Error ? err.message : "Unknown error.",
      });
      handleClose();
    },
  });

  const footer = (
    <>
      <Button variant="ghost" onClick={handleClose} disabled={mutation.isPending}>
        Cancel
      </Button>
      {step === "declare" ? (
        <Button
          disabled={outcome === null || mutation.isPending}
          onClick={() => setStep("confirm")}
        >
          Continue
        </Button>
      ) : (
        <Button
          variant={outcome === "FAILED" ? "danger" : "primary"}
          disabled={mutation.isPending || gate.disabled}
          title={gate.disabled ? gate.reason : undefined}
          onClick={() => outcome && mutation.mutate(outcome)}
        >
          {mutation.isPending ? "Recording…" : `Mark ${outcome?.toLowerCase()}`}
        </Button>
      )}
    </>
  );

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title={
        step === "declare" ? "Resolve ambiguous job" : `Confirm — mark ${outcome?.toLowerCase()}`
      }
      eyebrow={`${run.id} · ${job.id}`}
      size="md"
      footer={footer}
    >
      {step === "declare" ? (
        <div className="flex flex-col gap-4">
          {stateMoved !== null && stateMoved.currentVersion !== job.version ? (
            <div className="rounded-md border border-status-stale/50 bg-status-stale/10 p-3 text-[12.5px] leading-5 text-ink">
              <span className="font-semibold">State moved.</span> {job.id} advanced from v
              {stateMoved.currentVersion} to v{job.version} -- another operator may have already
              resolved this. Re-check before recording an outcome.
            </div>
          ) : null}
          <p className="text-[13px] leading-5 text-ink-muted">
            We can&apos;t tell whether the physical work on{" "}
            <span className="font-mono text-ink">{job.deck_id}</span> ran to completion. Inspect the
            deck before you decide. This records the attempt outcome — it does <em>not</em> retry
            the job.
          </p>
          {job.recent_attempts?.[0]?.error && (
            <div className="rounded-md border border-[#f7eadb] bg-[#fff7ec] px-3 py-2 text-[12px] leading-5 text-status-ambiguous">
              {job.recent_attempts[0].error}
            </div>
          )}
          <div className="grid grid-cols-1 gap-2">
            <OutcomeOption
              selected={outcome === "COMPLETED"}
              onSelect={() => setOutcome("COMPLETED")}
              icon={<CheckCircle2 size={16} strokeWidth={1.7} />}
              tone="completed"
              title="Completed"
              body="The work on the deck finished correctly. Downstream jobs become eligible to dispatch."
            />
            <OutcomeOption
              selected={outcome === "FAILED"}
              onSelect={() => setOutcome("FAILED")}
              icon={<AlertTriangle size={16} strokeWidth={1.7} />}
              tone="failed"
              title="Failed"
              body="The work did not complete. You'll be able to retry this job from a FAILED state if you want to re-run it."
            />
          </div>
          <label className="flex flex-col gap-1.5">
            <span className="text-[12px] font-medium tracking-nav text-ink-muted">
              Operator note <span className="text-ink-sub">(optional)</span>
            </span>
            <textarea
              value={note}
              onChange={(e) => setNote(e.target.value.slice(0, 500))}
              placeholder="What did you see on the deck? (recorded on the attempt and the JOB_RESOLVED event)"
              rows={3}
              className="w-full rounded-md border border-line bg-surface px-3 py-2 text-[13px] leading-5 text-ink placeholder:text-ink-sub focus:border-line-strong focus:outline-none"
            />
            <span className="self-end text-[10px] tracking-nav text-ink-sub">
              {note.length}/500
            </span>
          </label>
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          <p className="text-[13px] leading-5 text-ink-muted">
            You&apos;re recording attempt{" "}
            <span className="font-mono text-ink">{job.current_attempt_id}</span> on{" "}
            <span className="font-mono text-ink">{job.deck_id}</span> as{" "}
            <span
              className={cn(
                "font-semibold",
                outcome === "COMPLETED" ? "text-status-completed" : "text-status-failed",
              )}
            >
              {outcome?.toLowerCase()}
            </span>
            . This is irreversible.
          </p>
          {trimmedNote && (
            <div className="rounded-md border border-line bg-surface-warm px-3 py-2 text-[12px] leading-5 text-ink">
              <div className="font-mono text-[10px] uppercase tracking-[0.12em] text-ink-sub">
                Operator note
              </div>
              <div className="whitespace-pre-wrap">{trimmedNote}</div>
            </div>
          )}
          <p className="text-[12px] leading-5 text-ink-sub">
            Click &quot;Mark {outcome?.toLowerCase()}&quot; to confirm, or step back to change your
            mind.
          </p>
          <button
            type="button"
            onClick={() => setStep("declare")}
            className="self-start text-[12px] font-medium tracking-nav text-ink-nav underline-offset-4 hover:text-ink hover:underline"
          >
            ← Back
          </button>
        </div>
      )}
    </Modal>
  );
}

interface OutcomeOptionProps {
  selected: boolean;
  onSelect: () => void;
  icon: React.ReactNode;
  tone: "completed" | "failed";
  title: string;
  body: string;
}

function OutcomeOption({ selected, onSelect, icon, tone, title, body }: OutcomeOptionProps) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex items-start gap-3 rounded-card border p-3 text-left transition-colors duration-150 ease-out-soft",
        selected
          ? tone === "completed"
            ? "border-status-completed/40 bg-[#f1f8f4]"
            : "border-status-failed/40 bg-[#fff3f1]"
          : "border-line bg-surface hover:border-line-strong",
      )}
    >
      <span
        className={cn(
          "mt-0.5 inline-flex h-7 w-7 items-center justify-center rounded-full",
          tone === "completed"
            ? "bg-[#e7f1ea] text-status-completed"
            : "bg-[#fbe7e3] text-status-failed",
        )}
      >
        {icon}
      </span>
      <span className="flex flex-1 flex-col gap-0.5">
        <span className="text-[14px] font-semibold tracking-sub text-ink">{title}</span>
        <span className="text-[12px] leading-5 text-ink-muted">{body}</span>
      </span>
    </button>
  );
}
