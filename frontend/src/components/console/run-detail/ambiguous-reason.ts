import type { DeckJob } from "@/lib/api-types";

export function ambiguousReasonLabel(reason: NonNullable<DeckJob["ambiguous_reason"]>): string {
  switch (reason) {
    case "DEADLINE_EXCEEDED":
      return "Deadline exceeded";
    case "DEADLINE_ELAPSED":
      return "Deck unreachable past deadline";
    case "EXECUTOR_REPORTED_UNKNOWN":
      return "Executor reported unknown state";
  }
}

export function ambiguousReasonExplain(job: DeckJob): string {
  const completed = job.last_completed_step ?? 0;
  const total = job.total_steps ?? 0;
  const stepFragment = total > 0 ? ` Executor was on step ${completed}/${total} when checked.` : "";
  switch (job.ambiguous_reason) {
    case "DEADLINE_EXCEEDED":
      return (
        "The orchestrator's per-attempt hang ceiling fired and the executor still reported IN_PROGRESS." +
        stepFragment +
        " Resolve to declare the physical outcome."
      );
    case "DEADLINE_ELAPSED":
      return (
        "The deck's heartbeat went silent past the AmbiguousDeadline; no authoritative outcome." +
        stepFragment +
        " Resolve to declare the physical outcome."
      );
    case "EXECUTOR_REPORTED_UNKNOWN":
      return (
        "The executor explicitly told the orchestrator it lost track of this attempt." +
        stepFragment +
        " Resolve to declare the physical outcome."
      );
    case null:
    case undefined:
      return "Ambiguous — declare the physical outcome before continuing.";
  }
}
