/**
 * Operator-initiated mutations against the orchestrator API.
 *
 * Each function POSTs to its real endpoint and returns the updated
 * `Run`. The shared `request<T>` helper handles error parsing; the
 * mutation modals catch `StateMovedError` to surface the version-
 * mismatch path (operator A acted on a stale snapshot). All other
 * errors surface as `ApiError`.
 *
 * The mutations do NOT manually update the TanStack Query cache —
 * the live polling hook in `lib/live/` picks up the orchestrator's
 * resulting events within ~1s. Modals may still call
 * `queryClient.invalidateQueries` as a defensive nudge; with the
 * cache-only `queryFn` config in `lib/api/query-config.ts` those
 * calls are no-ops, but they're cheap to leave in place.
 */
import { request, StateMovedError, ApiError } from "@/lib/api/request";
import type {
  AttemptOutcome,
  CancelRunRequest,
  DagSubmission,
  Deck,
  ResolveJobRequest,
  RetryJobRequest,
  Run,
} from "@/lib/api-types";

export { StateMovedError, ApiError };

export async function submitRun(dag: DagSubmission): Promise<Run> {
  return request<Run>("/api/runs", { method: "POST", body: dag });
}

export async function cancelRun({
  runId,
  expectedVersion,
}: {
  runId: string;
  expectedVersion: number;
}): Promise<Run> {
  const body: CancelRunRequest = { expected_version: expectedVersion };
  return request<Run>(`/api/runs/${encodeURIComponent(runId)}/cancel`, {
    method: "POST",
    body,
  });
}

export async function retryJob({
  runId,
  jobId,
  expectedVersion,
}: {
  runId: string;
  jobId: string;
  expectedVersion: number;
}): Promise<Run> {
  const body: RetryJobRequest = { expected_version: expectedVersion };
  return request<Run>(
    `/api/runs/${encodeURIComponent(runId)}/jobs/${encodeURIComponent(jobId)}/retry`,
    { method: "POST", body },
  );
}

/**
 * POST /api/decks/{deck_id}/release — operator-deliberate vacate.
 *
 * Returns the slot to EMPTY. Used by the supervisor's detach flow and
 * exposed in Settings so an operator can manually clear an
 * UNREACHABLE slot whose executor died outside the supervisor's
 * watch. Refused with 409 SLOT_HAS_INFLIGHT_WORK if any deck_job is
 * non-terminal on the slot -- the operator must cancel the run or
 * resolve the AMBIGUOUS attempt first.
 */
export async function releaseDeck(deckId: string): Promise<Deck> {
  return request<Deck>(`/api/decks/${encodeURIComponent(deckId)}/release`, {
    method: "POST",
  });
}

export async function resolveJob({
  runId,
  jobId,
  expectedVersion,
  outcome,
  operatorNote,
}: {
  runId: string;
  jobId: string;
  expectedVersion: number;
  outcome: AttemptOutcome;
  operatorNote?: string;
}): Promise<Run> {
  const body: ResolveJobRequest = {
    expected_version: expectedVersion,
    resolution: outcome,
    ...(operatorNote ? { operator_note: operatorNote } : {}),
  };
  return request<Run>(
    `/api/runs/${encodeURIComponent(runId)}/jobs/${encodeURIComponent(jobId)}/resolve`,
    { method: "POST", body },
  );
}
