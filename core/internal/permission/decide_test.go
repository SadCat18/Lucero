package permission

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const now int64 = 1_000_000

type fakeSource struct {
	stopped              bool
	rules                []Rule
	grants               []Grant
	stoppedErr, rulesErr error
}

func (f fakeSource) EmergencyStopped(context.Context) (bool, error)  { return f.stopped, f.stoppedErr }
func (f fakeSource) Rules(context.Context) ([]Rule, error)           { return f.rules, f.rulesErr }
func (f fakeSource) Grants(context.Context, string) ([]Grant, error) { return f.grants, nil }

func request() Request {
	return Request{
		PrincipalID: "local-user", TrustDomainID: "td-home", ProjectID: "p-1",
		TaskID: "t-1", ExecutionID: "e-1", SessionID: "s-1", AdapterID: "a-1",
		ModelID: "m-1", TaskMode: "PLAN", Action: "filesystem.write", Resource: "repo:src/main.go",
	}
}

func grant(scope Scope, scopeID, action string, start, expiry, revoked int64, effect Effect) Grant {
	return Grant{
		ID: "g-1", PrincipalID: "local-user", ScopeType: scope, ScopeID: scopeID,
		Action: action, Effect: effect, StartsAt: start, ExpiresAt: expiry, RevokedAt: revoked,
	}
}

func TestDecidePriorityChain(t *testing.T) {
	always := Grant{ID: "g-live", PrincipalID: "local-user", ScopeType: ScopeTask, ScopeID: "t-1",
		Action: "filesystem.write", Effect: Allow, StartsAt: 0, ExpiresAt: now + 1000}

	for _, tc := range []struct {
		name       string
		src        fakeSource
		mutate     func(*Request)
		want       Effect
		wantReason Reason
		wantErr    string
	}{
		// rung 1: emergency stop
		{name: "emergency stop beats global allow", src: fakeSource{stopped: true, rules: []Rule{{ScopeType: ScopeGlobal, ActionPattern: "*", Effect: Allow}}}, want: Deny, wantReason: ReasonEmergencyStop},
		{name: "unknown emergency stop is deny", src: fakeSource{stoppedErr: errors.New("settings unreadable")}, want: Deny, wantErr: "emergency-stop"},

		// rung 2: explicit deny, at any scope, above every positive rule and grant
		{name: "global deny beats trust domain allow", src: fakeSource{rules: []Rule{
			{ID: "g-deny", ScopeType: ScopeGlobal, ActionPattern: "*", Effect: Deny},
			{ScopeType: ScopeTrustDomain, ScopeID: "td-home", ActionPattern: "*", Effect: Allow}}}, want: Deny},
		{name: "deny at a low scope still beats global allow", src: fakeSource{rules: []Rule{
			{ScopeType: ScopeGlobal, ActionPattern: "*", Effect: Allow},
			{ScopeType: ScopeSession, ScopeID: "s-1", ActionPattern: "filesystem.*", Effect: Deny}}}, want: Deny},
		{name: "temporary grant cannot overturn a deny", src: fakeSource{
			rules:  []Rule{{ScopeType: ScopeProject, ScopeID: "p-1", ActionPattern: "*", Effect: Deny}},
			grants: []Grant{always}}, want: Deny},
		{name: "deny that does not match the action is inert", src: fakeSource{rules: []Rule{
			{ScopeType: ScopeGlobal, ActionPattern: "network.*", Effect: Deny},
			{ScopeType: ScopeTrustDomain, ActionPattern: "*", Effect: Allow}}}, want: Allow},
		{name: "deny scoped to another project is inert", src: fakeSource{rules: []Rule{
			{ScopeType: ScopeProject, ScopeID: "p-OTHER", ActionPattern: "*", Effect: Deny},
			{ScopeType: ScopeTrustDomain, ActionPattern: "*", Effect: Allow}}}, want: Allow},

		// rungs 3..6: first matching rung wins, exactly as §7.2 orders them
		{name: "trust domain beats project", src: fakeSource{rules: []Rule{
			{ScopeType: ScopeTrustDomain, ActionPattern: "*", Effect: RequireApproval},
			{ScopeType: ScopeProject, ScopeID: "p-1", ActionPattern: "*", Effect: Allow}}}, want: RequireApproval},
		{name: "project beats task mode", src: fakeSource{rules: []Rule{
			{ScopeType: ScopeProject, ScopeID: "p-1", ActionPattern: "*", Effect: Allow},
			{ScopeType: ScopeTaskMode, ScopeID: "PLAN", ActionPattern: "*", Effect: RequireApproval}}}, want: Allow},
		{name: "task mode beats session override", src: fakeSource{rules: []Rule{
			{ScopeType: ScopeTaskMode, ScopeID: "PLAN", ActionPattern: "*", Effect: RequireApproval},
			{ScopeType: ScopeSession, ScopeID: "s-1", ActionPattern: "*", Effect: Allow}}}, want: RequireApproval},
		{name: "session override wins when nothing above matches", src: fakeSource{rules: []Rule{
			{ScopeType: ScopeSession, ScopeID: "s-1", ActionPattern: "filesystem.*", Effect: Allow}}}, want: Allow},
		{name: "session override beats adapter scope", src: fakeSource{rules: []Rule{
			{ScopeType: ScopeSession, ScopeID: "s-1", ActionPattern: "*", Effect: Allow},
			{ScopeType: ScopeAdapter, ScopeID: "a-1", ActionPattern: "*", Effect: RequireApproval}}}, want: Allow},
		{name: "task scope applies to its own task only", src: fakeSource{rules: []Rule{
			{ScopeType: ScopeTask, ScopeID: "t-1", ActionPattern: "*", Effect: Allow}}},
			mutate: func(r *Request) { r.TaskID = "t-OTHER" }, want: RequireApproval},

		// rung 7: temporary grants
		{name: "live grant gives ALLOW_TEMPORARY", src: fakeSource{grants: []Grant{always}},
			want: AllowTemporary, wantReason: "g-live"},
		{name: "expired grant falls through", src: fakeSource{grants: []Grant{func() Grant {
			g := always
			g.ExpiresAt = now - 1
			return g
		}()}}, want: RequireApproval},
		{name: "revoked grant falls through", src: fakeSource{grants: []Grant{func() Grant {
			g := always
			g.RevokedAt = now - 5
			return g
		}()}}, want: RequireApproval},
		{name: "future grant is not active yet", src: fakeSource{grants: []Grant{func() Grant {
			g := always
			g.StartsAt = now + 1
			return g
		}()}}, want: RequireApproval},
		{name: "grant bound to another task does not apply", src: fakeSource{grants: []Grant{func() Grant {
			g := always
			g.ScopeID = "t-OTHER"
			return g
		}()}}, want: RequireApproval},
		{name: "grant for another action does not apply", src: fakeSource{grants: []Grant{func() Grant {
			g := always
			g.Action = "network.*"
			return g
		}()}}, want: RequireApproval},
		{name: "global grant covers any scope", src: fakeSource{grants: []Grant{
			grant(ScopeGlobal, "", "filesystem.*", 0, now+1, 0, Allow)}}, want: AllowTemporary},
		{name: "a DENY grant denies", src: fakeSource{grants: []Grant{
			grant(ScopeTask, "t-1", "filesystem.write", 0, now+1, 0, Deny)}}, want: Deny},
		{name: "grant without expiry stays live", src: fakeSource{grants: []Grant{
			grant(ScopeTask, "t-1", "filesystem.write", 0, 0, 0, Allow)}}, want: AllowTemporary},

		// rung 8: default
		{name: "no rules and no grants falls to the risk default", src: fakeSource{},
			want: RequireApproval, wantReason: ReasonDefaultHighRisk},
		{name: "a registered low-risk action is allowed by default", src: fakeSource{},
			mutate: func(r *Request) { r.Action = "session.read" },
			want:   Allow, wantReason: ReasonDefaultAllow},
		{name: "default applies only when no rule matched", src: fakeSource{rules: []Rule{
			{ID: "r-x", ScopeType: ScopeProject, ScopeID: "p-1", ActionPattern: "*", Effect: Deny}}},
			want: Deny, wantReason: "r-x"},

		// within a rung
		{name: "higher priority wins in the same rung", src: fakeSource{rules: []Rule{
			{ScopeType: ScopeProject, ScopeID: "p-1", ActionPattern: "*", Effect: RequireApproval, Priority: 5},
			{ScopeType: ScopeProject, ScopeID: "p-1", ActionPattern: "*", Effect: Allow, Priority: 10}}}, want: Allow},
		{name: "priority tie falls to the stricter effect", src: fakeSource{rules: []Rule{
			{ScopeType: ScopeProject, ScopeID: "p-1", ActionPattern: "*", Effect: Allow},
			{ScopeType: ScopeProject, ScopeID: "p-1", ActionPattern: "*", Effect: RequireApproval}}}, want: RequireApproval},

		// matching and fail-closed edges
		{name: "resource pattern limits the rule", src: fakeSource{rules: []Rule{
			{ScopeType: ScopeProject, ScopeID: "p-1", ActionPattern: "*", ResourcePattern: "repo:*", Effect: Deny},
			{ScopeType: ScopeGlobal, ActionPattern: "*", Effect: Allow}}},
			mutate: func(r *Request) { r.Resource = "http://evil" }, want: Allow},
		{name: "a pattern that never matches leaves the rule inert, default still guards", src: fakeSource{rules: []Rule{
			{ScopeType: ScopeGlobal, ActionPattern: "[bad", Effect: Allow}}}, want: RequireApproval},
		{name: "unreadable policy is deny", src: fakeSource{rulesErr: errors.New("db down")}, want: Deny, wantErr: "policy unreadable"},
		{name: "missing action is rejected", src: fakeSource{}, mutate: func(r *Request) { r.Action = "" }, want: Deny, wantErr: "PrincipalID and Action"},
		{name: "missing principal is rejected", src: fakeSource{}, mutate: func(r *Request) { r.PrincipalID = "" }, want: Deny, wantErr: "PrincipalID and Action"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := New(tc.src, func() int64 { return now })
			r := request()
			if tc.mutate != nil {
				tc.mutate(&r)
			}
			got, reason, err := e.Decide(context.Background(), r)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("Decide = %s, want %s", got, tc.want)
			}
			if tc.wantReason != "" && reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
			if err == nil && reason == "" {
				t.Errorf("effect %s came back with no reason: %v", got, reason)
			}
		})
	}
}

// §3.1: the default comes from the built-in classifier, not from policy_rules,
// so it must hold on an empty table and fail closed on anything unrecognised.
func TestDefaultRiskClassification(t *testing.T) {
	for _, tc := range []struct {
		action  string
		want    Effect
		wantWhy Reason
	}{
		{"filesystem.write", RequireApproval, ReasonDefaultHighRisk},
		{"command.exec", RequireApproval, ReasonDefaultHighRisk},
		{"db.write", RequireApproval, ReasonDefaultHighRisk},
		{"deploy", RequireApproval, ReasonDefaultHighRisk},
		{"secret.read", RequireApproval, ReasonDefaultHighRisk},
		{"remote.shell", RequireApproval, ReasonDefaultHighRisk},       // remote.*
		{"remote.copy.up", RequireApproval, ReasonDefaultHighRisk},     // remote.*
		{"adapter.debug.dump", RequireApproval, ReasonDefaultHighRisk}, // adapter.debug.*
		{"session.list", Allow, ReasonDefaultAllow},
		{"session.read", Allow, ReasonDefaultAllow},
		{"session.send", Allow, ReasonDefaultAllow},
		{"model.select", Allow, ReasonDefaultAllow},
		{"execution_started", Allow, ReasonDefaultAllow},
		// fail-closed cases: neither list knows them
		{"filesystem.read", RequireApproval, ReasonDefaultUnknown},
		{"totally.new.action", RequireApproval, ReasonDefaultUnknown},
		{"deploy.production", RequireApproval, ReasonDefaultUnknown}, // "deploy" is exact, not a prefix
		{"remote", RequireApproval, ReasonDefaultUnknown},            // "remote.*" needs a dot segment
	} {
		got, why, err := New(fakeSource{}, func() int64 { return now }).Decide(
			context.Background(), Request{PrincipalID: "local-user", Action: tc.action})
		if err != nil {
			t.Fatalf("%s: %v", tc.action, err)
		}
		if got != tc.want || why != tc.wantWhy {
			t.Errorf("%s: %s/%s, want %s/%s", tc.action, got, why, tc.want, tc.wantWhy)
		}
	}
}

// The grant must be reported as its own id so a caller can tell "the user
// approved this" apart from "the engine guessed".
func TestReasonNamesItsSource(t *testing.T) {
	e := New(fakeSource{rules: []Rule{{ID: "rule-42", ScopeType: ScopeProject, ScopeID: "p-1",
		ActionPattern: "*", Effect: Allow}}}, func() int64 { return now })
	if _, why, _ := e.Decide(context.Background(), request()); why != "rule-42" {
		t.Errorf("reason = %q, want the matching rule id", why)
	}
	e2 := New(fakeSource{rules: []Rule{
		{ID: "deny-global", ScopeType: ScopeGlobal, ActionPattern: "*", Effect: Deny},
		{ID: "deny-session", ScopeType: ScopeSession, ScopeID: "s-1", ActionPattern: "*", Effect: Deny}}},
		func() int64 { return now })
	if eff, why, _ := e2.Decide(context.Background(), request()); eff != Deny || why != "deny-global" {
		t.Errorf("deny pass reported %s/%s, want DENY/deny-global", eff, why)
	}
}

func TestCheckTemporaryGrant(t *testing.T) {
	live := Grant{ID: "g", PrincipalID: "local-user", ScopeType: ScopeTask, ScopeID: "t-1",
		Action: "filesystem.*", Effect: Allow, StartsAt: 0, ExpiresAt: now + 1000}
	for _, tc := range []struct {
		name                       string
		grants                     []Grant
		scopeType, scopeID, action string
		want                       bool
	}{
		{name: "exact live match", grants: []Grant{live}, scopeType: "TASK", scopeID: "t-1", action: "filesystem.write", want: true},
		{name: "global grant answers any scope", grants: []Grant{func() Grant {
			g := live
			g.ScopeType, g.ScopeID = ScopeGlobal, ""
			return g
		}()}, scopeType: "TASK", scopeID: "t-99", action: "filesystem.write", want: true},
		{name: "grant with empty scope_id covers any id in scope", grants: []Grant{func() Grant {
			g := live
			g.ScopeID = ""
			return g
		}()}, scopeType: "TASK", scopeID: "t-99", action: "filesystem.write", want: true},
		{name: "wrong scope id", grants: []Grant{live}, scopeType: "TASK", scopeID: "t-2", action: "filesystem.write"},
		{name: "wrong scope type", grants: []Grant{live}, scopeType: "PROJECT", scopeID: "t-1", action: "filesystem.write"},
		{name: "wrong action", grants: []Grant{live}, scopeType: "TASK", scopeID: "t-1", action: "network.send"},
		{name: "expired", grants: []Grant{func() Grant { g := live; g.ExpiresAt = now; return g }()},
			scopeType: "TASK", scopeID: "t-1", action: "filesystem.write"},
		{name: "a DENY grant is not a permission", grants: []Grant{func() Grant { g := live; g.Effect = Deny; return g }()},
			scopeType: "TASK", scopeID: "t-1", action: "filesystem.write"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := New(fakeSource{grants: tc.grants}, func() int64 { return now })
			got, err := e.CheckTemporaryGrant(context.Background(), "local-user", tc.scopeType, tc.scopeID, tc.action)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("CheckTemporaryGrant = %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("missing principal is an error", func(t *testing.T) {
		e := New(fakeSource{}, func() int64 { return now })
		if _, err := e.CheckTemporaryGrant(context.Background(), "", "TASK", "t-1", "x"); err == nil {
			t.Error("accepted an empty principal")
		}
	})
}

func TestGrantActiveBoundary(t *testing.T) {
	g := Grant{ScopeType: ScopeTask, ScopeID: "t-1", Action: "x", Effect: Allow, StartsAt: 100, ExpiresAt: 200}
	for _, tc := range []struct {
		name string
		at   int64
		want bool
	}{
		{"before start", 99, false},
		{"at start", 100, true},
		{"mid window", 150, true},
		{"one ms before expiry", 199, true},
		{"at expiry", 200, false},
		{"after expiry", 201, false},
	} {
		if got := g.Active(tc.at); got != tc.want {
			t.Errorf("%s: Active(%d) = %v, want %v", tc.name, tc.at, got, tc.want)
		}
	}
	revoked := g
	revoked.RevokedAt = 120
	if revoked.Active(130) {
		t.Error("revoked grant reported active")
	}
	noExpiry := g
	noExpiry.ExpiresAt = 0
	if !noExpiry.Active(1 << 40) {
		t.Error("grant with NULL expires_at should not expire")
	}
}

func TestMatchPattern(t *testing.T) {
	for _, tc := range []struct {
		pattern, v string
		want       bool
	}{
		{"", "anything", true},
		{"*", "anything", true},
		{"filesystem.write", "filesystem.write", true},
		{"filesystem.write", "filesystem.read", false},
		{"filesystem.*", "filesystem.write", true},
		{"filesystem.*", "network.write", false},
		// the case path.Match got wrong: '*' must cross '/' in a resource path
		{"repo:*", "repo:src/main.go", true},
		{"*:write", "filesystem:write", true},
		{"*", "a/b/c", true},
		{"a*b*c", "aXXbYYc", true},
		{"[bad", "x", false},   // literal, and matches nothing => inert
		{"[bad", "[bad", true}, // no regex injection from a stray bracket
		{"a.b", "axb", false},  // dots are literal, not regex wildcards
		{"sk-*", "sk-abc", true},
		{"file?", "file1", false}, // '?' is not a wildcard in this language
	} {
		if got := matchPattern(tc.pattern, tc.v); got != tc.want {
			t.Errorf("matchPattern(%q, %q) = %v, want %v", tc.pattern, tc.v, got, tc.want)
		}
	}
}
