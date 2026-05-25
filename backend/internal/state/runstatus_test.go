package state_test

import (
	"testing"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/state"
	storegen "deck-fleet/backend/internal/store/gen"

	"github.com/stretchr/testify/require"
)

func jobs(statuses ...gen.DeckJobStatus) []storegen.DeckJobs {
	out := make([]storegen.DeckJobs, len(statuses))
	for i, s := range statuses {
		out[i] = storegen.DeckJobs{Status: string(s)}
	}
	return out
}

func TestDeriveRunStatus(t *testing.T) {
	cases := []struct {
		name     string
		input    []storegen.DeckJobs
		expected gen.RunStatus
	}{
		{
			name:     "all_completed",
			input:    jobs(gen.DeckJobStatusCOMPLETED),
			expected: gen.COMPLETED,
		},
		{
			name:     "any_ambiguous",
			input:    jobs(gen.DeckJobStatusCOMPLETED, gen.DeckJobStatusAMBIGUOUS),
			expected: gen.AMBIGUOUS,
		},
		{
			name:     "any_dispatched",
			input:    jobs(gen.DeckJobStatusPENDING, gen.DeckJobStatusDISPATCHED),
			expected: gen.RUNNING,
		},
		{
			name:     "any_running",
			input:    jobs(gen.DeckJobStatusCOMPLETED, gen.DeckJobStatusRUNNING),
			expected: gen.RUNNING,
		},
		{
			name:     "failed_no_active",
			input:    jobs(gen.DeckJobStatusFAILED, gen.DeckJobStatusCOMPLETED),
			expected: gen.FAILED,
		},
		{
			name:     "failed_with_ready_still_active",
			input:    jobs(gen.DeckJobStatusFAILED, gen.DeckJobStatusREADY),
			expected: gen.PENDING,
		},
		{
			name:     "cancelled_no_active",
			input:    jobs(gen.DeckJobStatusCANCELLED, gen.DeckJobStatusCOMPLETED),
			expected: gen.CANCELLED,
		},
		{
			name:     "cancelled_with_failed",
			input:    jobs(gen.DeckJobStatusCANCELLED, gen.DeckJobStatusFAILED),
			expected: gen.FAILED,
		},
		{
			name:     "all_pending",
			input:    jobs(gen.DeckJobStatusPENDING),
			expected: gen.PENDING,
		},
		{
			name:     "empty_input",
			input:    jobs(),
			expected: gen.PENDING,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := state.DeriveRunStatus(tc.input)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestIsTerminalRunStatus(t *testing.T) {
	terminal := []gen.RunStatus{gen.COMPLETED, gen.CANCELLED}
	for _, s := range terminal {
		require.Truef(t, state.IsTerminalRunStatus(s), "expected IsTerminalRunStatus(%s) == true", s)
	}

	// FAILED is non-terminal at the run level: a FAILED run is awaiting
	// operator decision (retry or cancel). See DESIGN.md.
	nonTerminal := []gen.RunStatus{gen.PENDING, gen.RUNNING, gen.AMBIGUOUS, gen.FAILED}
	for _, s := range nonTerminal {
		require.Falsef(t, state.IsTerminalRunStatus(s), "expected IsTerminalRunStatus(%s) == false", s)
	}
}
