package eventlog

// Kind is the closed enum for events.kind. Keep in sync with the DB CHECK
// constraint. Internal audit surface — not in OpenAPI.
type Kind string

const (
	KindRunSubmitted           Kind = "RUN_SUBMITTED"
	KindRunStatusChanged       Kind = "RUN_STATUS_CHANGED"
	KindJobReady               Kind = "JOB_READY"
	KindJobDispatched          Kind = "JOB_DISPATCHED"
	KindJobRunning             Kind = "JOB_RUNNING"
	KindJobStepCompleted       Kind = "JOB_STEP_COMPLETED"
	KindJobCompleted           Kind = "JOB_COMPLETED"
	KindJobFailed              Kind = "JOB_FAILED"
	KindJobAmbiguous           Kind = "JOB_AMBIGUOUS"
	KindJobCancelled           Kind = "JOB_CANCELLED"
	KindJobResolved            Kind = "JOB_RESOLVED"
	KindJobRetried             Kind = "JOB_RETRIED"
	KindDeckRegistered         Kind = "DECK_REGISTERED"
	KindDeckHealthChanged      Kind = "DECK_HEALTH_CHANGED"
	KindExecutorConflictLogged Kind = "EXECUTOR_CONFLICT_LOGGED"
)
