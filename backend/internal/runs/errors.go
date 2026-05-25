// Package runs is the operator run lifecycle use-case layer (Submit, Cancel,
// Retry, Resolve). Sentinel errors map to API.md §11 wire codes; anything
// else is 500 INTERNAL_ERROR.
package runs

import (
	"errors"

	"deck-fleet/backend/internal/api/gen"
)

var (
	ErrRunNotFound        = errors.New("runs: run not found")
	ErrJobNotFound        = errors.New("runs: job not found")
	ErrAlreadyTerminalRun = errors.New("runs: run already terminal") // IsTerminalRunStatus; cancel/retry reject
	ErrInvalidTransition  = errors.New("runs: invalid transition")
	ErrVersionMismatch    = errors.New("runs: version mismatch")
	ErrDuplicateRun       = errors.New("runs: duplicate run id") // caller gets existing Run for 409 envelope
	ErrInvalidResolution  = errors.New("runs: invalid resolution")
)

// ValidationError carries dag.Validate entries for 422 DAG_VALIDATION_FAILED.
type ValidationError struct {
	Entries []gen.DagValidationFailedDetailEntry
}

func (e *ValidationError) Error() string { return "runs: dag validation failed" }
