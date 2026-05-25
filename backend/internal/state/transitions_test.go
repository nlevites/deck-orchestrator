package state

import (
	"testing"

	"deck-fleet/backend/internal/api/gen"

	"github.com/stretchr/testify/require"
)

func TestCanTransition_AllTableRowsAreValid(t *testing.T) {
	for _, tr := range deckJobTransitions {
		ok := CanTransition(tr.From, tr.To, tr.Trigger)
		require.Truef(t, ok, "expected CanTransition(%s, %s, %s) == true", tr.From, tr.To, tr.Trigger)
	}
}

func TestCanTransition_InvalidTuplesReturnFalse(t *testing.T) {
	cases := []struct {
		from    gen.DeckJobStatus
		to      gen.DeckJobStatus
		trigger Trigger
	}{
		{gen.DeckJobStatusCOMPLETED, gen.DeckJobStatusPENDING, TriggerSchedulerReady},
		{gen.DeckJobStatusCOMPLETED, gen.DeckJobStatusRUNNING, TriggerExecutorEvent},
		{gen.DeckJobStatusCANCELLED, gen.DeckJobStatusREADY, TriggerOperatorRetry},
		{gen.DeckJobStatusPENDING, gen.DeckJobStatusRUNNING, TriggerExecutorEvent},
		{gen.DeckJobStatusPENDING, gen.DeckJobStatusREADY, TriggerOperatorRetry},
		{gen.DeckJobStatusREADY, gen.DeckJobStatusPENDING, TriggerSchedulerReady},
		{gen.DeckJobStatusFAILED, gen.DeckJobStatusCOMPLETED, TriggerOperatorCancel},
	}

	for _, c := range cases {
		ok := CanTransition(c.from, c.to, c.trigger)
		require.Falsef(t, ok, "expected CanTransition(%s, %s, %s) == false", c.from, c.to, c.trigger)
	}
}

func TestAllowedNextStates_PENDING(t *testing.T) {
	got := AllowedNextStates(gen.DeckJobStatusPENDING)
	require.ElementsMatch(t, []gen.DeckJobStatus{
		gen.DeckJobStatusREADY,
		gen.DeckJobStatusCANCELLED,
	}, got)
}

func TestAllowedNextStates_COMPLETED(t *testing.T) {
	got := AllowedNextStates(gen.DeckJobStatusCOMPLETED)
	require.Empty(t, got)
}

func TestAllowedNextStates_CANCELLED(t *testing.T) {
	got := AllowedNextStates(gen.DeckJobStatusCANCELLED)
	require.Empty(t, got)
}

func TestAllowedNextStates_FAILED(t *testing.T) {
	got := AllowedNextStates(gen.DeckJobStatusFAILED)
	require.ElementsMatch(t, []gen.DeckJobStatus{
		gen.DeckJobStatusREADY,     // operator retry
		gen.DeckJobStatusCANCELLED, // operator give-up (DESIGN.md)
	}, got)
}

func TestAllowedNextStates_noDuplicates(t *testing.T) {
	allStatuses := []gen.DeckJobStatus{
		gen.DeckJobStatusPENDING,
		gen.DeckJobStatusREADY,
		gen.DeckJobStatusDISPATCHED,
		gen.DeckJobStatusRUNNING,
		gen.DeckJobStatusAMBIGUOUS,
		gen.DeckJobStatusCOMPLETED,
		gen.DeckJobStatusFAILED,
		gen.DeckJobStatusCANCELLED,
	}
	for _, s := range allStatuses {
		nexts := AllowedNextStates(s)
		seen := make(map[gen.DeckJobStatus]int)
		for _, n := range nexts {
			seen[n]++
		}
		for state, count := range seen {
			require.Equalf(t, 1, count, "AllowedNextStates(%s) contains duplicate to-state %s", s, state)
		}
	}
}

func TestIsTerminalJobStatus_terminalStates(t *testing.T) {
	require.True(t, IsTerminalJobStatus(gen.DeckJobStatusCOMPLETED))
	require.True(t, IsTerminalJobStatus(gen.DeckJobStatusCANCELLED))
}

func TestIsTerminalJobStatus_nonTerminalStates(t *testing.T) {
	nonTerminal := []gen.DeckJobStatus{
		gen.DeckJobStatusPENDING,
		gen.DeckJobStatusREADY,
		gen.DeckJobStatusDISPATCHED,
		gen.DeckJobStatusRUNNING,
		gen.DeckJobStatusAMBIGUOUS,
		gen.DeckJobStatusFAILED,
	}
	for _, s := range nonTerminal {
		require.Falsef(t, IsTerminalJobStatus(s), "expected IsTerminalJobStatus(%s) == false", s)
	}
}
