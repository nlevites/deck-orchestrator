package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"deck-fleet/backend/internal/api"
	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/runs"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
)

// Deps is the closed set of collaborators the operator handlers need.
type Deps struct {
	Store          *store.DB
	Logger         *slog.Logger
	AbortScheduler runs.AbortScheduler // nil skips abort dials (tests)
}

// Operator is the §8 handler bundle; server.go registers each method as http.HandlerFunc.
type Operator struct {
	deps Deps
}

func NewOperator(deps Deps) *Operator { return &Operator{deps: deps} }

// SubmitRun: algebra in internal/runs.Submit; this handler decodes,
// maps domain errors to wire envelopes, and encodes.
func (o *Operator) SubmitRun(w http.ResponseWriter, r *http.Request) {
	body, err := api.DecodeAndValidate[gen.DagSubmission](w, r)
	if err != nil {
		return
	}

	run, err := runs.Submit(r.Context(), o.deps.Store, body)
	switch {
	case err == nil:
		api.WriteJSON(w, http.StatusCreated, run)
	case errors.Is(err, runs.ErrDuplicateRun):
		api.WriteDuplicateResource(w, r, run, fmt.Sprintf("A run with id %q already exists.", body.Id))
	default:
		var ve *runs.ValidationError
		if errors.As(err, &ve) {
			api.WriteDagValidationFailed(w, r, ve.Entries,
				fmt.Sprintf("DAG validation failed with %d error(s).", len(ve.Entries)))
			return
		}
		o.deps.Logger.Error("submitRun: tx", "run_id", body.Id, "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
	}
}

func (o *Operator) ListRuns(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"), 50, 200)
	statusFilter := r.URL.Query().Get("status")

	var rows []storegen.Runs
	var err error
	if statusFilter == "" {
		rows, err = o.deps.Store.ReadQueries.ListRuns(r.Context(), limit)
	} else {
		rows, err = o.deps.Store.ReadQueries.ListRunsByStatus(r.Context(), storegen.ListRunsByStatusParams{
			Status: statusFilter,
			Limit:  limit,
		})
	}
	if err != nil {
		o.deps.Logger.Error("listRuns: query", "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
		return
	}

	out := make([]gen.RunSummary, 0, len(rows))
	for _, row := range rows {
		s, sErr := rowToRunSummary(r.Context(), o.deps.Store.ReadQueries, row)
		if sErr != nil {
			o.deps.Logger.Error("listRuns: summary", "run_id", row.ID, "err", sErr)
			api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, sErr.Error())
			return
		}
		out = append(out, s)
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"runs": out})
}

func (o *Operator) GetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	row, err := o.deps.Store.ReadQueries.GetRun(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteSimpleError(w, r, gen.ErrorCodeRUNNOTFOUND, fmt.Sprintf("run %q does not exist", id))
		return
	}
	if err != nil {
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
		return
	}
	run, rErr := rowToRun(r.Context(), o.deps.Store.ReadQueries, row, true)
	if rErr != nil {
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, rErr.Error())
		return
	}
	api.WriteJSON(w, http.StatusOK, run)
}

// CancelRun: algebra in internal/runs.Cancel.
func (o *Operator) CancelRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	body, err := api.DecodeAndValidate[gen.CancelRunRequest](w, r)
	if err != nil {
		return
	}

	run, err := runs.Cancel(r.Context(), o.deps.Store, o.deps.AbortScheduler, id, body.ExpectedVersion)
	switch {
	case err == nil:
		api.WriteJSON(w, http.StatusOK, run)
	case errors.Is(err, runs.ErrRunNotFound):
		api.WriteSimpleError(w, r, gen.ErrorCodeRUNNOTFOUND, fmt.Sprintf("run %q does not exist", id))
	case errors.Is(err, runs.ErrAlreadyTerminalRun):
		api.WriteAlreadyTerminal(w, r, run.Status, run,
			fmt.Sprintf("Run is already in terminal state %s; cancel is a no-op.", run.Status))
	case errors.Is(err, runs.ErrVersionMismatch):
		api.WriteVersionMismatch(w, r, run.Version, run)
	default:
		o.deps.Logger.Error("cancelRun: tx", "run_id", id, "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
	}
}

// RetryJob: algebra in internal/runs.Retry.
func (o *Operator) RetryJob(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	jobID := r.PathValue("job_id")
	body, err := api.DecodeAndValidate[gen.RetryJobRequest](w, r)
	if err != nil {
		return
	}

	run, err := runs.Retry(r.Context(), o.deps.Store, runID, jobID, body.ExpectedVersion)
	switch {
	case err == nil:
		api.WriteJSON(w, http.StatusOK, run)
	case errors.Is(err, runs.ErrRunNotFound):
		api.WriteSimpleError(w, r, gen.ErrorCodeRUNNOTFOUND, fmt.Sprintf("run %q does not exist", runID))
	case errors.Is(err, runs.ErrJobNotFound):
		api.WriteSimpleError(w, r, gen.ErrorCodeJOBNOTFOUND, fmt.Sprintf("job %q does not exist in run %q", jobID, runID))
	case errors.Is(err, runs.ErrAlreadyTerminalRun):
		api.WriteAlreadyTerminal(w, r, run.Status, run,
			fmt.Sprintf("Run is already in terminal state %s; retry is forbidden.", run.Status))
	case errors.Is(err, runs.ErrInvalidTransition):
		var jobStatus gen.DeckJobStatus
		for _, dj := range run.DeckJobs {
			if dj.Id == jobID {
				jobStatus = dj.Status
				break
			}
		}
		api.WriteInvalidTransition(w, r, jobStatus, run, "Cannot retry a job that is not in FAILED state.")
	case errors.Is(err, runs.ErrVersionMismatch):
		var jobVersion int64
		for _, dj := range run.DeckJobs {
			if dj.Id == jobID {
				jobVersion = dj.Version
				break
			}
		}
		api.WriteVersionMismatch(w, r, jobVersion, run)
	default:
		o.deps.Logger.Error("retryJob: tx", "run_id", runID, "job_id", jobID, "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
	}
}

// ResolveJob: algebra in internal/runs.Resolve.
func (o *Operator) ResolveJob(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	jobID := r.PathValue("job_id")
	body, err := api.DecodeAndValidate[gen.ResolveJobRequest](w, r)
	if err != nil {
		return
	}
	var note string
	if body.OperatorNote != nil {
		note = *body.OperatorNote
	}

	run, err := runs.Resolve(r.Context(), o.deps.Store, o.deps.AbortScheduler, runID, jobID, body.Resolution, note, body.ExpectedVersion)
	switch {
	case err == nil:
		api.WriteJSON(w, http.StatusOK, run)
	case errors.Is(err, runs.ErrInvalidResolution):
		api.WriteSimpleError(w, r, gen.ErrorCodeINVALIDRESOLUTION,
			"resolution must be COMPLETED or FAILED")
	case errors.Is(err, runs.ErrRunNotFound):
		api.WriteSimpleError(w, r, gen.ErrorCodeRUNNOTFOUND, fmt.Sprintf("run %q does not exist", runID))
	case errors.Is(err, runs.ErrJobNotFound):
		api.WriteSimpleError(w, r, gen.ErrorCodeJOBNOTFOUND, fmt.Sprintf("job %q does not exist in run %q", jobID, runID))
	case errors.Is(err, runs.ErrInvalidTransition):
		var jobStatus gen.DeckJobStatus
		for _, dj := range run.DeckJobs {
			if dj.Id == jobID {
				jobStatus = dj.Status
				break
			}
		}
		api.WriteInvalidTransition(w, r, jobStatus, run, "Cannot resolve a job that is not in AMBIGUOUS state.")
	case errors.Is(err, runs.ErrVersionMismatch):
		var jobVersion int64
		for _, dj := range run.DeckJobs {
			if dj.Id == jobID {
				jobVersion = dj.Version
				break
			}
		}
		api.WriteVersionMismatch(w, r, jobVersion, run)
	default:
		o.deps.Logger.Error("resolveJob: tx", "run_id", runID, "job_id", jobID, "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
	}
}

func parseLimit(s string, def, max int64) int64 {
	if s == "" {
		return def
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
