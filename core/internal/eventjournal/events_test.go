package eventjournal

import "testing"

// Event Contract §1: lowercase snake_case, and PascalCase (which appears in the
// handoff's timeline sketch) is explicitly not a protocol name.
func TestRegistered(t *testing.T) {
	for _, ok := range []string{TaskCreated, TaskAccepted, ExecutionApprovalResolved, CoreReady} {
		if !Registered(ok) {
			t.Fatalf("%q is a constant but not in the catalogue", ok)
		}
	}
	// Event Contract §6 rows the adapter Supervisor emits; they were once
	// constants only, which made Registered() lie.
	for _, s := range []string{"adapter_downgraded", "adapter_restart_exhausted", "policy_created", "policy_updated"} {
		if !Registered(s) {
			t.Fatalf("%q is in the contract catalogue but not registered", s)
		}
	}
	for _, bad := range []string{"TaskCreated", "taskCreated", "task_created_v2", ""} {
		if Registered(bad) {
			t.Fatalf("%q must not be registered", bad)
		}
	}
}
