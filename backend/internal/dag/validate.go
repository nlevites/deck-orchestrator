// Package dag validates operator-submitted DAGs (pure, no I/O). Collects
// every violation in one response per API.md §8.1; empty means valid.
package dag

import (
	"deck-fleet/backend/internal/api/gen"
)

// Validate runs API.md §8.1 semantic checks in fixed order (deterministic
// output). decommissionedDeckIDs take precedence over unknown: a retired
// slot yields DECK_DECOMMISSIONED, not UNKNOWN_DECK.
func Validate(submission gen.DagSubmission, knownDeckIDs []string, decommissionedDeckIDs []string) []gen.DagValidationFailedDetailEntry {
	var entries []gen.DagValidationFailedDetailEntry

	if len(submission.DeckJobs) == 0 {
		entries = append(entries, mustEntry(func(e *gen.DagValidationFailedDetailEntry) error {
			return e.FromDagHasNoJobsDetail(gen.DagHasNoJobsDetail{
				Code: gen.DagHasNoJobsDetailCode(gen.DagValidationCodeDAGHASNOJOBS),
			})
		}))
		return entries
	}

	seen := make(map[string]struct{}, len(submission.DeckJobs))
	for _, job := range submission.DeckJobs {
		if _, dup := seen[job.Id]; dup {
			entries = append(entries, mustEntry(func(e *gen.DagValidationFailedDetailEntry) error {
				return e.FromDuplicateJobIdDetail(gen.DuplicateJobIdDetail{
					Code:  gen.DuplicateJobIdDetailCode(gen.DagValidationCodeDUPLICATEJOBID),
					JobId: job.Id,
				})
			}))
			continue
		}
		seen[job.Id] = struct{}{}
	}

	known := make(map[string]struct{}, len(knownDeckIDs))
	for _, id := range knownDeckIDs {
		known[id] = struct{}{}
	}
	decommissioned := make(map[string]struct{}, len(decommissionedDeckIDs))
	for _, id := range decommissionedDeckIDs {
		decommissioned[id] = struct{}{}
	}

	for _, job := range submission.DeckJobs {
		if len(job.Steps) == 0 {
			entries = append(entries, mustEntry(func(e *gen.DagValidationFailedDetailEntry) error {
				return e.FromJobHasNoStepsDetail(gen.JobHasNoStepsDetail{
					Code:  gen.JobHasNoStepsDetailCode(gen.DagValidationCodeJOBHASNOSTEPS),
					JobId: job.Id,
				})
			}))
		}
		switch {
		case isKnown(known, job.DeckId):
		case isKnown(decommissioned, job.DeckId):
			entries = append(entries, mustEntry(func(e *gen.DagValidationFailedDetailEntry) error {
				return e.FromDeckDecommissionedDetail(gen.DeckDecommissionedDetail{
					Code:   gen.DeckDecommissionedDetailCode(gen.DagValidationCodeDECKDECOMMISSIONED),
					DeckId: job.DeckId,
					JobId:  job.Id,
				})
			}))
		default:
			entries = append(entries, mustEntry(func(e *gen.DagValidationFailedDetailEntry) error {
				return e.FromUnknownDeckDetail(gen.UnknownDeckDetail{
					Code:   gen.UnknownDeckDetailCode(gen.DagValidationCodeUNKNOWNDECK),
					DeckId: job.DeckId,
					JobId:  job.Id,
				})
			}))
		}
		for _, dep := range job.DependsOn {
			if _, ok := seen[dep]; !ok {
				entries = append(entries, mustEntry(func(e *gen.DagValidationFailedDetailEntry) error {
					return e.FromUnknownDependencyDetail(gen.UnknownDependencyDetail{
						Code:              gen.UnknownDependencyDetailCode(gen.DagValidationCodeUNKNOWNDEPENDENCY),
						JobId:             job.Id,
						MissingDependency: dep,
					})
				}))
			}
		}
	}

	// Cycle detection skipped when depends_on references missing jobs.
	if !hasUnknownDependency(entries) {
		if cycle := detectCycle(submission); len(cycle) > 0 {
			entries = append(entries, mustEntry(func(e *gen.DagValidationFailedDetailEntry) error {
				return e.FromDagHasCycleDetail(gen.DagHasCycleDetail{
					Code:      gen.DagHasCycleDetailCode(gen.DagValidationCodeDAGHASCYCLE),
					CyclePath: cycle,
				})
			}))
		}
	}

	return entries
}

// detectCycle returns a closed witness path via Kahn + DFS on the residual.
func detectCycle(submission gen.DagSubmission) []string {
	indeg := make(map[string]int, len(submission.DeckJobs))
	adj := make(map[string][]string, len(submission.DeckJobs))
	for _, job := range submission.DeckJobs {
		if _, ok := indeg[job.Id]; !ok {
			indeg[job.Id] = 0
		}
		for _, dep := range job.DependsOn {
			adj[dep] = append(adj[dep], job.Id)
			indeg[job.Id]++
		}
	}

	queue := make([]string, 0, len(indeg))
	for id, d := range indeg {
		if d == 0 {
			queue = append(queue, id)
		}
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, next := range adj[n] {
			indeg[next]--
			if indeg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	residual := make(map[string]struct{})
	for id, d := range indeg {
		if d > 0 {
			residual[id] = struct{}{}
		}
	}
	if len(residual) == 0 {
		return nil
	}

	var start string
	for id := range residual {
		start = id
		break
	}

	visited := make(map[string]bool)
	onStack := make(map[string]bool)
	stack := make([]string, 0, len(residual))

	var dfs func(node string) []string
	dfs = func(node string) []string {
		visited[node] = true
		onStack[node] = true
		stack = append(stack, node)
		for _, next := range adj[node] {
			if _, ok := residual[next]; !ok {
				continue
			}
			if onStack[next] {
				start := -1
				for i, n := range stack {
					if n == next {
						start = i
						break
					}
				}
				cycle := append([]string{}, stack[start:]...)
				cycle = append(cycle, next)
				return cycle
			}
			if !visited[next] {
				if c := dfs(next); c != nil {
					return c
				}
			}
		}
		onStack[node] = false
		stack = stack[:len(stack)-1]
		return nil
	}

	return dfs(start)
}

func isKnown(m map[string]struct{}, id string) bool {
	_, ok := m[id]
	return ok
}

// hasUnknownDependency skips cycle detection when deps are ill-formed.
func hasUnknownDependency(entries []gen.DagValidationFailedDetailEntry) bool {
	for i := range entries {
		if _, err := entries[i].AsUnknownDependencyDetail(); err == nil {
			return true
		}
	}
	return false
}

// mustEntry panics on union construction failure (cannot happen here).
func mustEntry(fn func(*gen.DagValidationFailedDetailEntry) error) gen.DagValidationFailedDetailEntry {
	var entry gen.DagValidationFailedDetailEntry
	if err := fn(&entry); err != nil {
		panic(err)
	}
	return entry
}
