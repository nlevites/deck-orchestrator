/**
 * Single-source-of-truth re-export of every backend domain type the
 * console consumes. Aliases the OpenAPI-generated `components['schemas']`
 * tree into ergonomic top-level names so call sites read
 * `Run`/`Deck`/`Event` instead of the verbose generated form.
 *
 * If the OpenAPI shape changes, this file is the only consumer-touching
 * adjustment needed: `make gen-api` regenerates `api/gen.ts`, the
 * aliases below carry the change through automatically, and TypeScript
 * surfaces every downstream mismatch on the next typecheck.
 */
import type { components } from "@/api/gen";

type Schemas = components["schemas"];

export type Run = Schemas["Run"];
export type RunSummary = Schemas["RunSummary"];
export type DeckJob = Schemas["DeckJob"];
export type JobAttempt = Schemas["JobAttempt"];
export type Deck = Schemas["Deck"];
export type CurrentJob = Schemas["CurrentJob"];
export type DeckJobsSummary = Schemas["DeckJobsSummary"];
export type Step = Schemas["Step"];

export type DagSubmission = Schemas["DagSubmission"];
export type DagJobSubmission = Schemas["DagJobSubmission"];
export type CancelRunRequest = Schemas["CancelRunRequest"];
export type RetryJobRequest = Schemas["RetryJobRequest"];
export type ResolveJobRequest = Schemas["ResolveJobRequest"];

export type Event = Schemas["Event"];
export type EventKind = Schemas["EventKind"];
export type StateSnapshot = Schemas["StateSnapshot"];
export type RunStateSnapshot = Schemas["RunStateSnapshot"];

export type ChaosState = Schemas["ChaosState"];
export type ChaosPatch = Schemas["ChaosPatch"];

export type ErrorResponse = Schemas["ErrorResponse"];
export type ErrorCode = Schemas["ErrorCode"];

// Raw spec values. The widened display unions used by StatusPill
// (DeckHealth includes HEALTHY_IDLE/HEALTHY_BUSY etc) live in
// components/primitives/StatusPill; consumers needing the visual
// representation import from there.
export type RunStatus = Schemas["RunStatus"];
export type DeckJobStatus = Schemas["DeckJobStatus"];
export type DeckHealthRaw = Schemas["DeckHealth"];
export type AttemptOutcome = Schemas["AttemptOutcome"];
export type OutcomeSource = Schemas["OutcomeSource"];
