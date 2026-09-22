// Package eventjournal is the registry of legal event_type values (Event
// Contract §6). It must not import business packages: the catalogue is the
// contract, not the code that emits events.
package eventjournal

// Event types are lowercase snake_case: <domain>_<past-tense-action>.
const (
	ProjectCreated             = "project_created"
	ProjectUpdated             = "project_updated"
	ProjectArchived            = "project_archived"
	AdapterRegistered          = "adapter_registered"
	AdapterHealthChanged       = "adapter_health_changed"
	AdapterCapabilityVerified  = "adapter_capability_verified"
	AdapterDowngraded          = "adapter_downgraded"
	AdapterRestartExhausted    = "adapter_restart_exhausted"
	ModelDiscovered            = "model_discovered"
	SessionDiscovered          = "session_discovered"
	SessionManaged             = "session_managed"
	SessionSynced              = "session_synced"
	MessageRecorded            = "message_recorded"
	TaskCreated                = "task_created"
	TaskQueued                 = "task_queued"
	TaskPreparing              = "task_preparing"
	TaskRunning                = "task_running"
	TaskWaitingApproval        = "task_waiting_approval"
	TaskAgentCompleted         = "task_agent_completed"
	TaskVerifying              = "task_verifying"
	TaskWaitingAcceptance      = "task_waiting_acceptance"
	TaskAccepted               = "task_accepted"
	TaskFailed                 = "task_failed"
	TaskCancelled              = "task_cancelled"
	TaskPaused                 = "task_paused"
	TaskBlocked                = "task_blocked"
	TaskResumed                = "task_resumed"
	TaskInterrupted            = "task_interrupted"
	TaskTransferring           = "task_transferring"
	TaskStateCorrected         = "task_state_corrected"
	ExecutionCreated           = "execution_created"
	ExecutionStarted           = "execution_started"
	ExecutionOutputFlushed     = "execution_output_flushed"
	ExecutionApprovalRequested = "execution_approval_requested"
	ExecutionApprovalResolved  = "execution_approval_resolved"
	ExecutionCancelled         = "execution_cancelled"
	ExecutionFailed            = "execution_failed"
	ExecutionSucceeded         = "execution_succeeded"
	ExecutionInterrupted       = "execution_interrupted"
	ExecutionPaused            = "execution_paused"
	ExecutionResumed           = "execution_resumed"
	ApprovalRequested          = "approval_requested"
	ApprovalResolved           = "approval_resolved"
	ApprovalExpired            = "approval_expired"
	ApprovalCancelled          = "approval_cancelled"
	GrantIssued                = "grant_issued"
	GrantRevoked               = "grant_revoked"
	GrantExpired               = "grant_expired"
	PolicyCreated              = "policy_created"
	PolicyUpdated              = "policy_updated"
	QuotaObserved              = "quota_observed"
	NotificationCreated        = "notification_created"
	NotificationHandled        = "notification_handled"
	NotificationRead           = "notification_read"
	HandoffCreated             = "handoff_created"
	HandoffReady               = "handoff_ready"
	HandoffCompleted           = "handoff_completed"
	HandoffFailed              = "handoff_failed"
	ProcessStarted             = "process_started"
	ProcessExited              = "process_exited"
	ProcessCrashed             = "process_crashed"
	CoreReady                  = "core_ready"
	CoreShutdown               = "core_shutdown"
)

var catalog = map[string]bool{
	ProjectCreated: true, ProjectUpdated: true, ProjectArchived: true,
	AdapterRegistered: true, AdapterHealthChanged: true, AdapterCapabilityVerified: true,
	AdapterDowngraded: true, AdapterRestartExhausted: true,
	ModelDiscovered:   true,
	SessionDiscovered: true, SessionManaged: true, SessionSynced: true,
	MessageRecorded: true,
	TaskCreated:     true, TaskQueued: true, TaskPreparing: true, TaskRunning: true,
	TaskWaitingApproval: true, TaskAgentCompleted: true, TaskVerifying: true,
	TaskWaitingAcceptance: true, TaskAccepted: true, TaskFailed: true,
	TaskCancelled: true, TaskInterrupted: true, TaskTransferring: true,
	TaskPaused: true, TaskBlocked: true, TaskResumed: true,
	TaskStateCorrected: true,
	ExecutionCreated:   true, ExecutionStarted: true, ExecutionOutputFlushed: true,
	ExecutionApprovalRequested: true, ExecutionApprovalResolved: true,
	ExecutionCancelled: true, ExecutionFailed: true, ExecutionSucceeded: true,
	ExecutionInterrupted: true, ExecutionPaused: true, ExecutionResumed: true,
	ApprovalRequested: true, ApprovalResolved: true, ApprovalExpired: true,
	ApprovalCancelled: true,
	GrantIssued:       true, GrantRevoked: true, GrantExpired: true,
	PolicyCreated: true, PolicyUpdated: true,
	QuotaObserved:       true,
	NotificationCreated: true, NotificationHandled: true, NotificationRead: true,
	HandoffCreated: true, HandoffReady: true, HandoffCompleted: true, HandoffFailed: true,
	ProcessStarted: true, ProcessExited: true, ProcessCrashed: true,
	CoreReady: true, CoreShutdown: true,
}

// Registered reports whether an event type is in the catalogue. Writing an
// unregistered type is a P0 (Event Contract §9.1).
func Registered(eventType string) bool { return catalog[eventType] }
