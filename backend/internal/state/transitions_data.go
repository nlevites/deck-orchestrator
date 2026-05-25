package state

import "deck-fleet/backend/internal/api/gen"

// deckJobTransitions mirrors STATE_MACHINE.md §3.2. Diff against the doc to verify.
// CanTransition false → 409 INVALID_TRANSITION; terminal parent run → 409 ALREADY_TERMINAL (§11).
var deckJobTransitions = []transition{
	// PENDING -> ...
	{From: gen.DeckJobStatusPENDING, To: gen.DeckJobStatusREADY, Trigger: TriggerSchedulerReady},
	{From: gen.DeckJobStatusPENDING, To: gen.DeckJobStatusCANCELLED, Trigger: TriggerOperatorCancel},

	// READY -> ...
	{From: gen.DeckJobStatusREADY, To: gen.DeckJobStatusDISPATCHED, Trigger: TriggerDispatcherClaim},
	{From: gen.DeckJobStatusREADY, To: gen.DeckJobStatusCANCELLED, Trigger: TriggerOperatorCancel},

	// DISPATCHED -> ...
	{From: gen.DeckJobStatusDISPATCHED, To: gen.DeckJobStatusRUNNING, Trigger: TriggerExecutorEvent},
	{From: gen.DeckJobStatusDISPATCHED, To: gen.DeckJobStatusCOMPLETED, Trigger: TriggerExecutorEvent},
	{From: gen.DeckJobStatusDISPATCHED, To: gen.DeckJobStatusFAILED, Trigger: TriggerExecutorEvent},
	{From: gen.DeckJobStatusDISPATCHED, To: gen.DeckJobStatusAMBIGUOUS, Trigger: TriggerReconciler},
	{From: gen.DeckJobStatusDISPATCHED, To: gen.DeckJobStatusAMBIGUOUS, Trigger: TriggerLivenessMonitor},
	{From: gen.DeckJobStatusDISPATCHED, To: gen.DeckJobStatusCANCELLED, Trigger: TriggerOperatorCancel},
	{From: gen.DeckJobStatusDISPATCHED, To: gen.DeckJobStatusRUNNING, Trigger: TriggerReconciler},   // missed-event reconcile
	{From: gen.DeckJobStatusDISPATCHED, To: gen.DeckJobStatusCOMPLETED, Trigger: TriggerReconciler}, // missed-event reconcile
	{From: gen.DeckJobStatusDISPATCHED, To: gen.DeckJobStatusFAILED, Trigger: TriggerReconciler},    // missed-event reconcile

	// RUNNING -> ...
	{From: gen.DeckJobStatusRUNNING, To: gen.DeckJobStatusCOMPLETED, Trigger: TriggerExecutorEvent},
	{From: gen.DeckJobStatusRUNNING, To: gen.DeckJobStatusFAILED, Trigger: TriggerExecutorEvent},
	{From: gen.DeckJobStatusRUNNING, To: gen.DeckJobStatusCOMPLETED, Trigger: TriggerReconciler},
	{From: gen.DeckJobStatusRUNNING, To: gen.DeckJobStatusFAILED, Trigger: TriggerReconciler},
	{From: gen.DeckJobStatusRUNNING, To: gen.DeckJobStatusAMBIGUOUS, Trigger: TriggerReconciler},
	{From: gen.DeckJobStatusRUNNING, To: gen.DeckJobStatusAMBIGUOUS, Trigger: TriggerLivenessMonitor},
	{From: gen.DeckJobStatusRUNNING, To: gen.DeckJobStatusCANCELLED, Trigger: TriggerOperatorCancel},

	// AMBIGUOUS -> ...
	{From: gen.DeckJobStatusAMBIGUOUS, To: gen.DeckJobStatusCOMPLETED, Trigger: TriggerOperatorResolution},
	{From: gen.DeckJobStatusAMBIGUOUS, To: gen.DeckJobStatusFAILED, Trigger: TriggerOperatorResolution},
	{From: gen.DeckJobStatusAMBIGUOUS, To: gen.DeckJobStatusCANCELLED, Trigger: TriggerOperatorCancel},

	// FAILED -> READY (retry; new attempt allocated by dispatcher).
	{From: gen.DeckJobStatusFAILED, To: gen.DeckJobStatusREADY, Trigger: TriggerOperatorRetry},
	// FAILED -> CANCELLED (operator gives up on the run; needed because
	// FAILED is non-terminal at the run level per DESIGN.md, so without
	// this transition Cancel on a FAILED run would be a no-op).
	{From: gen.DeckJobStatusFAILED, To: gen.DeckJobStatusCANCELLED, Trigger: TriggerOperatorCancel},

	// COMPLETED, CANCELLED: no outgoing transitions.
}
