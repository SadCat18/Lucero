package permission

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
)

const srcNow int64 = 1_700_000_000_000

// openMigrated stands up the real schema. Unlike internal/approval's fixture this
// package only reads, so redaction is not initialised here.
func openMigrated(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "lucero.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return d
}

func exec(t *testing.T, d *db.DB, q string, args ...any) {
	t.Helper()
	if _, err := d.Read.Exec(q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func seedPrincipal(t *testing.T, d *db.DB, id string) {
	t.Helper()
	exec(t, d, `INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
		VALUES (?, 'USER', ?, ?, ?)`, id, id, srcNow, srcNow)
}

func insertRule(t *testing.T, d *db.DB, id, scope, scopeID, action, resource, effect string, priority, enabled int) {
	t.Helper()
	exec(t, d, `INSERT INTO policy_rules
		(id, scope_type, scope_id, action_pattern, resource_pattern, effect, priority, enabled, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		id, scope, nullable(scopeID), action, nullable(resource), effect, priority, enabled, srcNow, srcNow)
}

func insertGrant(t *testing.T, d *db.DB, id, principal, scope, scopeID, action, effect string, starts, expires, revoked int64) {
	t.Helper()
	exec(t, d, `INSERT INTO temporary_grants
		(id, principal_id, scope_type, scope_id, action, effect, starts_at, expires_at, created_at, revoked_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		id, principal, scope, nullable(scopeID), action, effect, starts, nullableMs(expires), srcNow, nullableMs(revoked))
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableMs(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

// These cases are the counterpart to decide_test.go: that file proves the chain
// order against a fake source, this one proves the chain reads the real tables.
func TestSQLiteSourceDrivesDecide(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	src := NewSQLiteSource(d)
	engine := New(src, func() int64 { return srcNow })
	r := request()

	if got, _, err := engine.Decide(ctx, r); err != nil || got != RequireApproval {
		t.Fatalf("empty policy: %s %v, want REQUIRE_APPROVAL from the default rung", got, err)
	}

	insertRule(t, d, "r-allow", "GLOBAL", "", "*", "", "ALLOW", 0, 1)
	if got, _, err := engine.Decide(ctx, r); err != nil || got != Allow {
		t.Fatalf("global allow rule: %s %v, want ALLOW — the row was not read or not matched", got, err)
	}

	insertRule(t, d, "r-deny", "PROJECT", "p-1", "filesystem.*", "", "DENY", 50, 1)
	if got, _, err := engine.Decide(ctx, r); err != nil || got != Deny {
		t.Fatalf("project deny must beat the global allow: %s %v", got, err)
	}

	// A rule for a different project must not reach this request at all.
	exec(t, d, `UPDATE policy_rules SET scope_id = 'p-OTHER' WHERE id = 'r-deny'`)
	if got, _, err := engine.Decide(ctx, r); err != nil || got != Allow {
		t.Fatalf("foreign project rule leaked in: %s %v, want ALLOW", got, err)
	}

	// enabled = 0 is "not a rule", not "a weaker rule".
	exec(t, d, `UPDATE policy_rules SET enabled = 0 WHERE id = 'r-allow'`)
	if got, _, err := engine.Decide(ctx, r); err != nil || got != RequireApproval {
		t.Fatalf("disabled rule still applied: %s %v", got, err)
	}
	exec(t, d, `UPDATE policy_rules SET enabled = 1 WHERE id = 'r-allow'`)

	// Trust domain outranks project, per §7.2. The GLOBAL rule has to go first,
	// or it wins the whole ladder and this asserts nothing about TD vs PROJECT.
	exec(t, d, `DELETE FROM policy_rules`)
	insertRule(t, d, "r-td", "TRUST_DOMAIN", "td-home", "*", "", "REQUIRE_APPROVAL", 0, 1)
	insertRule(t, d, "r-proj", "PROJECT", "p-1", "*", "", "ALLOW", 0, 1)
	if got, _, err := engine.Decide(ctx, r); err != nil || got != RequireApproval {
		t.Fatalf("trust domain must outrank project: %s %v", got, err)
	}
	// Priority and the stricter-on-a-tie rule only apply *inside* one rung, so
	// this state holds nothing but two PROJECT rules.
	exec(t, d, `DELETE FROM policy_rules`)
	insertRule(t, d, "r-lo", "PROJECT", "p-1", "*", "", "REQUIRE_APPROVAL", 0, 1)
	insertRule(t, d, "r-hi", "PROJECT", "p-1", "*", "", "ALLOW", 99, 1)
	if got, _, err := engine.Decide(ctx, r); err != nil || got != Allow {
		t.Fatalf("priority 99 ALLOW should beat priority 0 REQUIRE_APPROVAL: %s %v", got, err)
	}
	exec(t, d, `UPDATE policy_rules SET priority = 0 WHERE id = 'r-hi'`)
	if got, _, err := engine.Decide(ctx, r); err != nil || got != RequireApproval {
		t.Fatalf("an equal priority tie must fall to the stricter effect: %s %v", got, err)
	}
}

func TestSQLiteSourceGrantsAndChecks(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	seedPrincipal(t, d, "local-user")
	seedPrincipal(t, d, "someone-else")
	insertGrant(t, d, "g-live", "local-user", "TASK", "t-1", "filesystem.write", "ALLOW", srcNow-10, srcNow+1000, 0)
	insertGrant(t, d, "g-expired", "local-user", "TASK", "t-1", "network.*", "ALLOW", srcNow-100, srcNow-1, 0)
	insertGrant(t, d, "g-revoked", "local-user", "TASK", "t-1", "shell.exec", "ALLOW", srcNow-10, srcNow+1000, srcNow-5)
	// A grant belonging to someone else must never surface for this principal.
	insertGrant(t, d, "g-other", "someone-else", "TASK", "t-1", "filesystem.write", "ALLOW", srcNow-10, srcNow+1000, 0)

	engine := New(NewSQLiteSource(d), func() int64 { return srcNow })

	got, _, err := engine.Decide(ctx, request())
	if err != nil || got != AllowTemporary {
		t.Fatalf("Decide = %s %v, want ALLOW_TEMPORARY from the live grant", got, err)
	}
	// Only the live, matching, non-revoked row may pass.
	for _, tc := range []struct {
		name, action string
		want         bool
	}{
		{"live", "filesystem.write", true},
		{"expired", "network.send", false},
		{"revoked", "shell.exec", false},
		{"ungranted action", "deploy.production", false},
	} {
		ok, err := engine.CheckTemporaryGrant(ctx, "local-user", "TASK", "t-1", tc.action)
		if err != nil {
			t.Fatal(err)
		}
		if ok != tc.want {
			t.Errorf("%s: CheckTemporaryGrant = %v, want %v", tc.name, ok, tc.want)
		}
	}
	if ok, _ := engine.CheckTemporaryGrant(ctx, "someone-else", "TASK", "t-1", "filesystem.write"); !ok {
		t.Error("the other principal's own grant stopped resolving")
	}
}

func TestSQLiteSourceEmergencyStop(t *testing.T) {
	ctx := context.Background()
	d := openMigrated(t)
	src := NewSQLiteSource(d)
	engine := New(src, func() int64 { return srcNow })
	insertRule(t, d, "r-allow", "GLOBAL", "", "*", "", "ALLOW", 0, 1)

	if got, _, err := engine.Decide(ctx, request()); err != nil || got != Allow {
		t.Fatalf("no stop row present: %s %v, want ALLOW (absence is not a stop)", got, err)
	}

	insertSetting := func(valueJSON string) {
		exec(t, d, `DELETE FROM settings`)
		exec(t, d, `INSERT INTO settings (id, scope_type, scope_id, setting_key, value_json, created_at, updated_at)
			VALUES ('s-stop', 'GLOBAL', NULL, ?, ?, ?, ?)`, SettingEmergencyStop, valueJSON, srcNow, srcNow)
	}

	insertSetting(`{"enabled":true}`)
	if got, _, err := engine.Decide(ctx, request()); err != nil || got != Deny {
		t.Fatalf("emergency stop did not override an ALLOW rule: %s %v", got, err)
	}
	insertSetting(`{"enabled":false}`)
	if got, _, err := engine.Decide(ctx, request()); err != nil || got != Allow {
		t.Fatalf("stop lifted but still denying: %s %v", got, err)
	}
	// An unusable stop value must not be read as "clear to proceed". Malformed
	// JSON cannot even be stored: settings.value_json is CHECKed as json_valid,
	// so the realistic drift is valid JSON of the wrong shape.
	insertSetting(`{"enabled":"yes"}`)
	if got, _, err := engine.Decide(ctx, request()); err == nil || got != Deny {
		t.Fatalf("corrupt stop value: %s %v, want DENY plus an error", got, err)
	}
}

// A source error must never degrade into an ALLOW on its own.
func TestSQLiteSourceErrorsAreDeny(t *testing.T) {
	d := openMigrated(t)
	d.Close() // every read now fails
	engine := New(NewSQLiteSource(d), func() int64 { return srcNow })
	if got, _, err := engine.Decide(context.Background(), request()); err == nil || got != Deny {
		t.Errorf("Decide after the handle closed: %s %v, want DENY plus an error", got, err)
	}
	if ok, err := engine.CheckTemporaryGrant(context.Background(), "local-user", "TASK", "t-1", "x"); err == nil || ok {
		t.Errorf("CheckTemporaryGrant returned %v %v, want false plus an error", ok, err)
	}
}
