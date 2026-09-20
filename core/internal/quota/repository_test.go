package quota

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// openSchema 只为测试服务：执行 db 角色的两份 .sql 建真表，不复制 DDL、不改文件。
func openSchema(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1) // 单连接：:memory: 库每个连接都是一个新的库
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"000001_core_schema.sql", "000002_aggregate_version.sql"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "migrations", f))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(b)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func seedAdapters(t *testing.T, db *sql.DB) {
	for _, a := range [][2]string{{"ad-codex", "codex"}, {"ad-qoder", "qoder"}} {
		mustExec(t, db, `INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
			VALUES (?, ?, ?, 1, 1)`, a[0], a[1], a[1])
	}
}

// 同一外部账号挂在两家软件下 = 领域模型「Quota Account」说的"两个软件可能共享同一 Provider 额度"。
func TestAccountsForAdapterSharedAcrossAdapters(t *testing.T) {
	db := openSchema(t)
	seedAdapters(t, db)
	mustExec(t, db, `INSERT INTO quota_accounts
		(id, adapter_instance_id, external_account_ref, display_label, quota_source, created_at, updated_at)
		VALUES ('qa-codex', 'ad-codex', 'acct-1', '主号', 'native', 1, 1)`)
	mustExec(t, db, `INSERT INTO quota_accounts
		(id, adapter_instance_id, external_account_ref, created_at, updated_at)
		VALUES ('qa-qoder', 'ad-qoder', 'acct-1', 2, 2)`)
	mustExec(t, db, `INSERT INTO quota_accounts
		(id, adapter_instance_id, display_label, created_at, updated_at)
		VALUES ('qa-codex-2', 'ad-codex', '副号', 3, 3)`)

	repo := NewRepository(db)
	ctx := context.Background()

	codex, err := repo.AccountsForAdapter(ctx, "ad-codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(codex) != 2 {
		t.Fatalf("一家软件下应有两个额度账号，实际 %d", len(codex))
	}
	if codex[0].ID != "qa-codex" || codex[1].ID != "qa-codex-2" {
		t.Errorf("顺序应按 created_at： %+v", codex)
	}
	if codex[0].ExternalAccountRef != "acct-1" || codex[1].ExternalAccountRef != "" {
		t.Errorf("NULL 的 external_account_ref 被造出了值: %+v", codex[1])
	}
	if codex[0].Version != 1 {
		t.Errorf("ADR-017 首版应为 1，实际 %d", codex[0].Version)
	}
	qoder, err := repo.Account(ctx, "qa-qoder")
	if err != nil {
		t.Fatal(err)
	}
	if qoder.ExternalAccountRef != codex[0].ExternalAccountRef {
		t.Error("两家软件共享的同一外部账号读不出来了")
	}
}

// TestLatestSnapshotsKeepsNullsUnknown 守 §12 红线：读不到的额度必须是 nil，不是 0。
func TestLatestSnapshotsKeepsNullsUnknown(t *testing.T) {
	db := openSchema(t)
	seedAdapters(t, db)
	mustExec(t, db, `INSERT INTO quota_accounts (id, adapter_instance_id, created_at, updated_at)
		VALUES ('qa-1', 'ad-codex', 1, 1)`)
	// 5H_WINDOW 三条：旧的、最新的、以及最新时刻的同 id 竞争者。
	mustExec(t, db, `INSERT INTO quota_snapshots (id, quota_account_id, quota_type, quota_key,
		percentage_used, observed_at) VALUES ('s-old', 'qa-1', 'PERCENT', '5H_WINDOW', 90, 100)`)
	mustExec(t, db, `INSERT INTO quota_snapshots (id, quota_account_id, quota_type, quota_key,
		percentage_used, reset_at, observed_at) VALUES ('s-new', 'qa-1', 'PERCENT', '5H_WINDOW', 12, 900, 200)`)
	mustExec(t, db, `INSERT INTO quota_snapshots (id, quota_account_id, quota_type, quota_key,
		percentage_used, observed_at) VALUES ('s-tie', 'qa-1', 'PERCENT', '5H_WINDOW', 99, 200)`)
	// WEEKLY 只有裸读数：其余数值列全 NULL，confidence 走 DDL 默认 UNKNOWN。
	mustExec(t, db, `INSERT INTO quota_snapshots (id, quota_account_id, quota_type, quota_key, observed_at)
		VALUES ('s-weekly', 'qa-1', 'TIME_WINDOW', 'WEEKLY', 150)`)

	got, err := NewRepository(db).LatestSnapshots(context.Background(), "qa-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		var ids []string
		for _, s := range got {
			ids = append(ids, s.ID)
		}
		t.Fatalf("每个 quota_key 只能出一条，实际 %v", ids)
	}
	fiveH, weekly := got[0], got[1]
	if fiveH.Key != "5H_WINDOW" || weekly.Key != "WEEKLY" {
		t.Fatalf("应按 quota_key 排序: %q %q", fiveH.Key, weekly.Key)
	}
	if fiveH.ID != "s-tie" {
		t.Errorf("同刻两条时应有确定的取胜者，实际 %s", fiveH.ID)
	}
	if fiveH.PercentageUsed == nil || *fiveH.PercentageUsed != 99 || fiveH.ResetAt != nil {
		t.Errorf("最新一条读错: %+v pct=%v reset=%v", fiveH, fiveH.PercentageUsed, fiveH.ResetAt)
	}
	if weekly.Confidence != ConfidenceUnknown || weekly.ObservationKind != Observed {
		t.Errorf("DDL 默认值没读到: confidence=%q kind=%q", weekly.Confidence, weekly.ObservationKind)
	}
	if weekly.PercentageUsed != nil || weekly.PercentageRemaining != nil ||
		weekly.UsedValue != nil || weekly.RemainingValue != nil || weekly.ResetAt != nil {
		t.Errorf("NULL 额度被折算成了确定值（§12 红线）: %+v", weekly)
	}
	if !weekly.Type.Valid() || weekly.Type != TypeTimeWindow {
		t.Errorf("quota_type 读错: %q", weekly.Type)
	}
}

// §31：预测不能伪装成观测值。词表必须与 000001 的 CHECK 一致。
func TestWordListsMatchSchemaChecks(t *testing.T) {
	db := openSchema(t)
	seedAdapters(t, db)
	mustExec(t, db, `INSERT INTO quota_accounts (id, adapter_instance_id, created_at, updated_at)
		VALUES ('qa-1', 'ad-codex', 1, 1)`)

	for _, bad := range []string{"GUESS", "OBSERVEDISH", "0", "UNKNOWN"} {
		_, err := db.Exec(`INSERT INTO quota_snapshots (id, quota_account_id, quota_type, quota_key,
			observation_kind, observed_at) VALUES (?, 'qa-1', 'PERCENT', 'K', ?, 1)`, "bad-kind-"+bad, bad)
		if err == nil {
			t.Errorf("schema 的 CHECK 竟然放过了 observation_kind=%q", bad)
			db.Exec(`DELETE FROM quota_snapshots WHERE id = ?`, "bad-kind-"+bad)
		}
	}
	for _, good := range []ObservationKind{Observed, Estimated} {
		if _, err := db.Exec(`INSERT INTO quota_snapshots (id, quota_account_id, quota_type, quota_key,
			observation_kind, confidence, observed_at) VALUES (?, 'qa-1', 'PERCENT', 'K', ?, 'MEDIUM', 1)`,
			string(good), string(good)); err != nil {
			t.Errorf("%s 被 schema 拒了：词表与 CHECK 不符: %v", good, err)
		}
		if !good.Valid() {
			t.Errorf("%s 应在我方词表里", good)
		}
	}
	for _, bad := range []string{"SECOND_LIFE", "UNKNOWN"} {
		if _, err := db.Exec(`INSERT INTO quota_snapshots (id, quota_account_id, quota_type, quota_key,
			observed_at) VALUES (?, 'qa-1', ?, 'K', 1)`, "bad-type-"+bad, bad); err == nil {
			t.Errorf("schema 的 CHECK 竟然放过了 quota_type=%q", bad)
			db.Exec(`DELETE FROM quota_snapshots WHERE id = ?`, "bad-type-"+bad)
		}
	}
	for _, ty := range []Type{TypePercent, TypeCredits, TypeTokens, TypeRequests,
		TypeCurrency, TypeTimeWindow, TypeCustom} {
		if !ty.Valid() {
			t.Errorf("%s 应在我方词表里", ty)
		}
	}
	if Type("PERCENTAGE").Valid() || ObservationKind("PREDICTED").Valid() || Confidence("SOME").Valid() {
		t.Error("词表校验放水")
	}
}

// §6 目录里 quota 的唯一一行，与包内常量 + payload 键对账。
func TestEventCatalogMatchesContract6(t *testing.T) {
	want := []string{"confidence", "observation_kind", "quota_account_id", "quota_key"}
	b, err := json.Marshal(ObservedPayload{QuotaAccountID: "qa", QuotaKey: "5H_WINDOW",
		ObservationKind: Observed, Confidence: ConfidenceMedium})
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for k := range m {
		got = append(got, k)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("quota_observed payload 键 = %v，§6 要求 %v", got, want)
	}
	if EventObserved != "quota_observed" {
		t.Errorf("事件类型名不符 §1 规范: %q", EventObserved)
	}
}
