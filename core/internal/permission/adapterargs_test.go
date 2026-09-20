package permission

import "testing"

// Adapter protocol §9 (C2 ruling): these may never be enabled by orchestration
// or an automatic retry, so anything here must be refused before launch and
// recorded as audit_entries outcome=DENIED.
func TestCheckAdapterArgsRefusesBypassSwitches(t *testing.T) {
	for _, denied := range []string{
		// the three Phase 0 found, verbatim
		"--auth none",
		"--dangerously-bypass-approvals-and-sandbox",
		"bypassPermissions",
		// spellings that mean the same thing
		"--auth=none",
		"--auth-mode none",
		`{"auth":"none"}`,
		"--no-sandbox",
		"--disable_sandbox",
		"--skip-sandbox",
		"--sandbox=off",
		"--auto-approve",
		"--no-approval",
		"--yolo",
		"--dangerously-disable-web-security",
		"--permissions=bypass",
	} {
		if got := CheckAdapterArgs("run", "--model", "gpt-5", denied); len(got) == 0 {
			t.Errorf("bypass switch accepted: %q (args=%v)", denied, got)
		} else if got[0] != denied {
			t.Errorf("flagged %q instead of %q", got[0], denied)
		}
	}
}

// A gate that rejects every launch is not a security control, it is an outage:
// FakeAdapter has to start for the vertical chain to run at all.
func TestCheckAdapterArgsAllowsOrdinaryArgs(t *testing.T) {
	for _, ok := range []string{
		"run", "serve", "--model", "gpt-5", "--output-format", "json",
		"--sandbox", "--sandbox=docker", "--auth", "--auth=token", "--auth-mode token",
		`{"auth":"oauth","sandbox":"docker"}`, "--approvals-file", "approvals.json",
		"--config", `D:\Project\Lucero\adapters\fake\config.json`, "--timeout", "30s",
		"--authorization-mode", "default",
	} {
		if got := CheckAdapterArgs("run", ok); len(got) != 0 {
			t.Errorf("ordinary arg refused: %q -> %v", ok, got)
		}
	}
}

func TestCheckAdapterArgsNoArgsIsClean(t *testing.T) {
	if got := CheckAdapterArgs(); len(got) != 0 {
		t.Errorf("empty call returned %v", got)
	}
	// Only the offending entries come back, so the audit record can name them.
	// "--auth none" arrives as two tokens; the flag is the one reported.
	got := CheckAdapterArgs("run", "--model", "gpt-5", "--yolo", "--auth", "none")
	if len(got) != 2 || got[0] != "--yolo" || got[1] != "--auth" {
		t.Errorf("hits = %v, want [--yolo --auth]", got)
	}
}
