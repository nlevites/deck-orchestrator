package dag_test

import (
	"testing"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/dag"
	"deck-fleet/backend/internal/testutil"

	"github.com/stretchr/testify/require"
)

// extractCodes maps union entries to Code strings. As* methods are plain
// json.Unmarshal wrappers without a discriminant guard, so we match the
// unmarshalled Code field against canonical constants.
func extractCodes(entries []gen.DagValidationFailedDetailEntry) []string {
	codes := make([]string, 0, len(entries))
	for _, e := range entries {
		switch {
		case codeIs(asNoJobs(e), gen.DagValidationCodeDAGHASNOJOBS):
			codes = append(codes, string(gen.DagValidationCodeDAGHASNOJOBS))
		case codeIs(asDupJobID(e), gen.DagValidationCodeDUPLICATEJOBID):
			codes = append(codes, string(gen.DagValidationCodeDUPLICATEJOBID))
		case codeIs(asNoSteps(e), gen.DagValidationCodeJOBHASNOSTEPS):
			codes = append(codes, string(gen.DagValidationCodeJOBHASNOSTEPS))
		case codeIs(asUnknownDeck(e), gen.DagValidationCodeUNKNOWNDECK):
			codes = append(codes, string(gen.DagValidationCodeUNKNOWNDECK))
		case codeIs(asUnknownDep(e), gen.DagValidationCodeUNKNOWNDEPENDENCY):
			codes = append(codes, string(gen.DagValidationCodeUNKNOWNDEPENDENCY))
		case codeIs(asCycle(e), gen.DagValidationCodeDAGHASCYCLE):
			codes = append(codes, string(gen.DagValidationCodeDAGHASCYCLE))
		}
	}
	return codes
}

func codeIs(code string, want gen.DagValidationCode) bool {
	return code == string(want)
}

func asNoJobs(e gen.DagValidationFailedDetailEntry) string {
	v, err := e.AsDagHasNoJobsDetail()
	if err != nil {
		return ""
	}
	return string(v.Code)
}

func asDupJobID(e gen.DagValidationFailedDetailEntry) string {
	v, err := e.AsDuplicateJobIdDetail()
	if err != nil {
		return ""
	}
	return string(v.Code)
}

func asNoSteps(e gen.DagValidationFailedDetailEntry) string {
	v, err := e.AsJobHasNoStepsDetail()
	if err != nil {
		return ""
	}
	return string(v.Code)
}

func asUnknownDeck(e gen.DagValidationFailedDetailEntry) string {
	v, err := e.AsUnknownDeckDetail()
	if err != nil {
		return ""
	}
	return string(v.Code)
}

func asUnknownDep(e gen.DagValidationFailedDetailEntry) string {
	v, err := e.AsUnknownDependencyDetail()
	if err != nil {
		return ""
	}
	return string(v.Code)
}

func asCycle(e gen.DagValidationFailedDetailEntry) string {
	v, err := e.AsDagHasCycleDetail()
	if err != nil {
		return ""
	}
	return string(v.Code)
}

var noopStep = gen.Step{Type: "noop", Description: "test"}

func TestValidate_EmptyJobs_returnsOnlyDAGHasNoJobs(t *testing.T) {
	sub := testutil.DAG(testutil.WithEmptyJobs())

	entries := dag.Validate(sub, nil, nil)

	require.Len(t, entries, 1, "expected exactly 1 entry")
	codes := extractCodes(entries)
	require.Equal(t, []string{string(gen.DagValidationCodeDAGHASNOJOBS)}, codes)
}

func TestValidate_DuplicateJobID_oneEntryPerDuplicate(t *testing.T) {
	sub := testutil.DAG(testutil.WithDuplicateJobID(testutil.DefaultJobID))
	known := []string{testutil.DefaultDeckID}

	entries := dag.Validate(sub, known, nil)

	var found []gen.DuplicateJobIdDetail
	for _, e := range entries {
		v, err := e.AsDuplicateJobIdDetail()
		if err == nil && string(v.Code) == string(gen.DagValidationCodeDUPLICATEJOBID) {
			found = append(found, v)
		}
	}
	require.Len(t, found, 1, "expected exactly 1 DUPLICATE_JOB_ID entry")
	require.Equal(t, testutil.DefaultJobID, found[0].JobId)
}

func TestValidate_JobHasNoSteps_oneEntryPerBareJob(t *testing.T) {
	sub := testutil.DAG(testutil.WithNoSteps(testutil.DefaultJobID))
	known := []string{testutil.DefaultDeckID}

	entries := dag.Validate(sub, known, nil)

	var found []gen.JobHasNoStepsDetail
	for _, e := range entries {
		v, err := e.AsJobHasNoStepsDetail()
		if err == nil && string(v.Code) == string(gen.DagValidationCodeJOBHASNOSTEPS) {
			found = append(found, v)
		}
	}
	require.Len(t, found, 1, "expected exactly 1 JOB_HAS_NO_STEPS entry")
	require.Equal(t, testutil.DefaultJobID, found[0].JobId)
}

func TestValidate_UnknownDeck_entryCarriesDeckAndJobID(t *testing.T) {
	sub := testutil.DAG()

	entries := dag.Validate(sub, []string{}, nil)

	var found []gen.UnknownDeckDetail
	for _, e := range entries {
		v, err := e.AsUnknownDeckDetail()
		if err == nil && string(v.Code) == string(gen.DagValidationCodeUNKNOWNDECK) {
			found = append(found, v)
		}
	}
	require.Len(t, found, 1, "expected exactly 1 UNKNOWN_DECK entry")
	require.Equal(t, testutil.DefaultDeckID, found[0].DeckId)
	require.Equal(t, testutil.DefaultJobID, found[0].JobId)
}

func TestValidate_DecommissionedDeck_preferredOverUnknownDeck(t *testing.T) {
	// DECK_DECOMMISSIONED beats UNKNOWN_DECK for retired slots.
	sub := testutil.DAG()

	entries := dag.Validate(sub, []string{}, []string{testutil.DefaultDeckID})

	var found []gen.DeckDecommissionedDetail
	for _, e := range entries {
		v, err := e.AsDeckDecommissionedDetail()
		if err == nil && string(v.Code) == string(gen.DagValidationCodeDECKDECOMMISSIONED) {
			found = append(found, v)
		}
	}
	require.Len(t, found, 1, "expected exactly 1 DECK_DECOMMISSIONED entry")
	require.Equal(t, testutil.DefaultDeckID, found[0].DeckId)
	require.Equal(t, testutil.DefaultJobID, found[0].JobId)

	for _, e := range entries {
		_, uErr := e.AsUnknownDeckDetail()
		if uErr == nil {
			var v gen.UnknownDeckDetail
			_, _ = e.AsUnknownDeckDetail()
			require.NotEqual(t, string(gen.DagValidationCodeUNKNOWNDECK), string(v.Code),
				"must not emit UNKNOWN_DECK when slot is decommissioned")
		}
	}
}

func TestValidate_UnknownDependency_entryCarriesMissingDep(t *testing.T) {
	const missingDep = "nonexistent-job"
	sub := testutil.DAG(
		testutil.WithJob("job-2", testutil.DefaultDeckID, missingDep),
	)
	known := []string{testutil.DefaultDeckID}

	entries := dag.Validate(sub, known, nil)

	var found []gen.UnknownDependencyDetail
	for _, e := range entries {
		v, err := e.AsUnknownDependencyDetail()
		if err == nil && string(v.Code) == string(gen.DagValidationCodeUNKNOWNDEPENDENCY) {
			found = append(found, v)
		}
	}
	require.Len(t, found, 1, "expected exactly 1 UNKNOWN_DEPENDENCY entry")
	require.Equal(t, missingDep, found[0].MissingDependency)
}

func TestValidate_CycleDetected_pathClosesOnStartNode(t *testing.T) {
	sub := gen.DagSubmission{
		Id: "test-cycle-2",
		DeckJobs: []gen.DagJobSubmission{
			{Id: "A", DeckId: "deck-x", DependsOn: []string{"B"}, Steps: []gen.Step{noopStep}},
			{Id: "B", DeckId: "deck-x", DependsOn: []string{"A"}, Steps: []gen.Step{noopStep}},
		},
	}
	known := []string{"deck-x"}

	entries := dag.Validate(sub, known, nil)

	var cycles []gen.DagHasCycleDetail
	for _, e := range entries {
		v, err := e.AsDagHasCycleDetail()
		if err == nil && string(v.Code) == string(gen.DagValidationCodeDAGHASCYCLE) {
			cycles = append(cycles, v)
		}
	}
	require.Len(t, cycles, 1, "expected exactly 1 DAG_HAS_CYCLE entry")

	path := cycles[0].CyclePath
	require.NotEmpty(t, path, "cycle_path must not be empty")
	require.Equal(t, path[0], path[len(path)-1],
		"cycle_path last element must equal first element (closed cycle)")
}

func TestValidate_CycleSkippedWhenUnknownDepPresent(t *testing.T) {
	// UNKNOWN_DEPENDENCY suppresses cycle detection even when A↔B would cycle.
	sub := gen.DagSubmission{
		Id: "test-cycle-skip",
		DeckJobs: []gen.DagJobSubmission{
			{Id: "A", DeckId: "deck-x", DependsOn: []string{"ghost", "B"}, Steps: []gen.Step{noopStep}},
			{Id: "B", DeckId: "deck-x", DependsOn: []string{"A"}, Steps: []gen.Step{noopStep}},
		},
	}
	known := []string{"deck-x"}

	entries := dag.Validate(sub, known, nil)

	codes := extractCodes(entries)
	require.Contains(t, codes, string(gen.DagValidationCodeUNKNOWNDEPENDENCY),
		"expected UNKNOWN_DEPENDENCY to be present")
	require.NotContains(t, codes, string(gen.DagValidationCodeDAGHASCYCLE),
		"DAG_HAS_CYCLE must not appear when unknown deps are present")
}

func TestValidate_ValidDAG_returnsEmpty(t *testing.T) {
	sub := testutil.DAG(
		testutil.WithJob("job-2", testutil.DefaultDeckID, testutil.DefaultJobID),
	)
	known := []string{testutil.DefaultDeckID}

	entries := dag.Validate(sub, known, nil)

	require.Empty(t, entries, "well-formed DAG should produce no violations")
}

func TestValidate_MultipleViolations_allCollected(t *testing.T) {
	sub := testutil.DAG(testutil.WithNoSteps(testutil.DefaultJobID))

	entries := dag.Validate(sub, []string{}, nil)

	codes := extractCodes(entries)
	require.Contains(t, codes, string(gen.DagValidationCodeJOBHASNOSTEPS),
		"expected JOB_HAS_NO_STEPS")
	require.Contains(t, codes, string(gen.DagValidationCodeUNKNOWNDECK),
		"expected UNKNOWN_DECK")
}

func TestValidate_LongerCycle_pathContainsCycleNodes(t *testing.T) {
	sub := gen.DagSubmission{
		Id: "test-cycle-3",
		DeckJobs: []gen.DagJobSubmission{
			{Id: "A", DeckId: "deck-x", DependsOn: []string{"C"}, Steps: []gen.Step{noopStep}},
			{Id: "B", DeckId: "deck-x", DependsOn: []string{"A"}, Steps: []gen.Step{noopStep}},
			{Id: "C", DeckId: "deck-x", DependsOn: []string{"B"}, Steps: []gen.Step{noopStep}},
		},
	}
	known := []string{"deck-x"}

	entries := dag.Validate(sub, known, nil)

	var cycles []gen.DagHasCycleDetail
	for _, e := range entries {
		v, err := e.AsDagHasCycleDetail()
		if err == nil && string(v.Code) == string(gen.DagValidationCodeDAGHASCYCLE) {
			cycles = append(cycles, v)
		}
	}
	require.Len(t, cycles, 1, "expected exactly 1 DAG_HAS_CYCLE entry")

	path := cycles[0].CyclePath
	require.GreaterOrEqual(t, len(path), 3,
		"3-node cycle path must have at least 3 nodes")
	require.Equal(t, path[0], path[len(path)-1],
		"cycle_path must be closed (last == first)")
}
