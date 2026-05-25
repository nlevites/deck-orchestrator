package testutil

import "deck-fleet/backend/internal/api/gen"

type DAGOpt func(*gen.DagSubmission)

// DAG returns a valid 1-job template (DefaultRunID/DefaultJobID/DefaultDeckID).
func DAG(opts ...DAGOpt) gen.DagSubmission {
	d := gen.DagSubmission{
		Id: DefaultRunID,
		DeckJobs: []gen.DagJobSubmission{
			{
				Id:        DefaultJobID,
				DeckId:    DefaultDeckID,
				DependsOn: []string{},
				Steps:     []gen.Step{{Type: "noop", Description: "test"}},
			},
		},
	}
	for _, opt := range opts {
		opt(&d)
	}
	return d
}

func WithDAGID(id string) DAGOpt {
	return func(d *gen.DagSubmission) { d.Id = id }
}

func WithJob(id, deckID string, deps ...string) DAGOpt {
	cp := append([]string(nil), deps...)
	return func(d *gen.DagSubmission) {
		d.DeckJobs = append(d.DeckJobs, gen.DagJobSubmission{
			Id:        id,
			DeckId:    deckID,
			DependsOn: cp,
			Steps:     []gen.Step{{Type: "noop", Description: "test"}},
		})
	}
}

func WithEmptyJobs() DAGOpt {
	return func(d *gen.DagSubmission) { d.DeckJobs = nil }
}

func WithNoSteps(jobID string) DAGOpt {
	return func(d *gen.DagSubmission) {
		for i := range d.DeckJobs {
			if d.DeckJobs[i].Id == jobID {
				d.DeckJobs[i].Steps = nil
			}
		}
	}
}

func WithDuplicateJobID(id string) DAGOpt {
	return func(d *gen.DagSubmission) {
		d.DeckJobs = append(d.DeckJobs, gen.DagJobSubmission{
			Id:        id,
			DeckId:    DefaultDeckID,
			DependsOn: []string{},
			Steps:     []gen.Step{{Type: "noop", Description: "dup"}},
		})
	}
}
