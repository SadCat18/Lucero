package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// openSchema 只为测试服务：直接执行 db 角色的两份 .sql 建出真表，不复制 DDL、不改文件。
// 生产写路径仍然只有 db.Create / db.Touch 一条，本包一行写 SQL 都没有。
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

// seed 建三个不同软件的 Session 挂在同一个 Task 上：Codex 起源、Qoder 执行、OpenCode 复核。
// 其中 sess-codex 故意留 NULL 与默认的 UNKNOWN，用来验证读侧不折算。
func seed(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	for _, a := range [][2]string{{"ad-codex", "codex"}, {"ad-qoder", "qoder"}, {"ad-opencode", "opencode"}} {
		mustExec(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
			VALUES (?, ?, ?, 1, 1)`, a[0], a[1], a[1])
	}
	mustExec(`INSERT INTO tasks (id, title, created_at, updated_at) VALUES ('task-1', '跑通垂直链', 1, 1)`)
	mustExec(`INSERT INTO sessions (id, adapter_instance_id, project_id, remote_session_id, title, managed,
			created_at, updated_at)
		VALUES ('sess-codex', 'ad-codex', NULL, NULL, NULL, 0, 1, 1)`)
	mustExec(`INSERT INTO sessions (id, adapter_instance_id, project_id, remote_session_id, title, managed,
			sync_state, sync_cursor, state_confidence, last_synced_at, created_at, updated_at)
		VALUES ('sess-qoder', 'ad-qoder', NULL, 'remote-q', '重构会话', 1, 'SYNCED', '20', 'HIGH', 9, 1, 1)`)
	mustExec(`INSERT INTO sessions (id, adapter_instance_id, remote_session_id, managed, sync_state,
			state_confidence, archived_at, created_at, updated_at)
		VALUES ('sess-opencode', 'ad-opencode', 'remote-o', 1, 'CONFLICT', 'LOW', 77, 1, 1)`)
	for i, l := range [][3]string{{"link-1", "sess-codex", "ORIGIN"}, {"link-2", "sess-qoder", "EXECUTION"},
		{"link-3", "sess-opencode", "REVIEW"}} {
		mustExec(`INSERT INTO task_session_links (id, task_id, session_id, role, ordinal, created_at)
			VALUES (?, 'task-1', ?, ?, ?, 1)`, l[0], l[1], l[2], i)
	}
	mustExec(`INSERT INTO messages (id, session_id, remote_message_id, role, kind, sequence_no, text_content, created_at)
		VALUES ('m-1', 'sess-qoder', 'r1', 'USER', 'MESSAGE', 1, '第一句', 100)`)
	mustExec(`INSERT INTO messages (id, session_id, remote_message_id, role, kind, sequence_no, text_content, created_at)
		VALUES ('m-2', 'sess-qoder', 'r2', 'ASSISTANT', 'MESSAGE', 2, '第二句', 200)`)
	// m-3 没有 sequence_no：靠 COALESCE 退到 created_at=300 排在最后；少了 COALESCE 它会被 SQLite 排到最前。
	mustExec(`INSERT INTO messages (id, session_id, role, kind, created_at, synced_at)
		VALUES ('m-3', 'sess-qoder', 'TOOL', 'TOOL_RESULT', 300, 310)`)
}

func TestForTaskReturnsSessionsOfEveryProvider(t *testing.T) {
	db := openSchema(t)
	seed(t, db)
	repo := NewRepository(db)

	links, err := repo.ForTask(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 3 {
		t.Fatalf("一个 Task 应挂着三家软件的 3 个 Session，实际 %d", len(links))
	}
	adapters := map[string]bool{}
	for i, l := range links {
		if int64(i) != l.Ordinal {
			t.Errorf("第 %d 条 ordinal = %d，排序没按 ordinal", i, l.Ordinal)
		}
		if l.Session.ID == "" || l.AdapterInstanceID == "" {
			t.Errorf("link 没带上 Session 本体: %+v", l)
		}
		adapters[l.AdapterInstanceID] = true
	}
	if len(adapters) != 3 {
		t.Errorf("三个 Session 应来自三个不同 adapter 实例，实际 %v", adapters)
	}
	if links[0].Role != LinkOrigin || links[1].Role != LinkExecution || links[2].Role != LinkReview {
		t.Errorf("role 读错了: %v %v %v", links[0].Role, links[1].Role, links[2].Role)
	}
	for _, r := range links {
		if !r.Role.Valid() {
			t.Errorf("%s 不在 §20 的六个 role 里", r.Role)
		}
	}
}

// TestGetDoesNotCoerceUnknown 守 §13 与 §57：Adapter 无法确认时读回来必须还是 UNKNOWN，
// 不是 SYNCED、不是 UNAVAILABLE，也不是"没同步过 = 0"。
func TestGetDoesNotCoerceUnknown(t *testing.T) {
	db := openSchema(t)
	seed(t, db)
	repo := NewRepository(db)
	ctx := context.Background()

	plain, err := repo.Get(ctx, "sess-codex")
	if err != nil {
		t.Fatal(err)
	}
	if plain.SyncState != SyncUnknown || plain.StateConfidence != ConfidenceUnknown {
		t.Errorf("默认值被改判了: sync=%q confidence=%q", plain.SyncState, plain.StateConfidence)
	}
	if plain.Managed || plain.Archived || plain.LastSyncedAt != 0 {
		t.Errorf("NULL 列被造出了值: %+v", plain)
	}
	if plain.Version != 1 {
		t.Errorf("ADR-017 首版应为 1，实际 %d", plain.Version)
	}

	archived, err := repo.Get(ctx, "sess-opencode")
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Archived || archived.SyncState != SyncConflict {
		t.Errorf("archived_at / sync_state 读错: %+v", archived)
	}

	if _, err := repo.Get(ctx, "sess-nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("缺行应透传 sql.ErrNoRows，实际 %v", err)
	}
}

// TestSyncCursorKeepsNullApartFromAPosition 钉 REQ-F-21 第 ③④ 条：
// 同步域里 NULL = "从未同步 → 下次从头拉"，与分页的"没有下一页"**语义相反**，
// 所以它不能被折成空串再让出口去猜。库里万一存了空串（新裁定下这是非法值），
// 也读成"无位置"而不是造出第三种状态 —— 合法值只有"非空字符串"与"null"两种。
func TestSyncCursorKeepsNullApartFromAPosition(t *testing.T) {
	db := openSchema(t)
	seed(t, db)
	repo := NewRepository(db)
	ctx := context.Background()

	nover, err := repo.Get(ctx, "sess-codex") // 夹具没写 sync_cursor，就是 NULL
	if err != nil {
		t.Fatal(err)
	}
	if nover.SyncCursor != nil {
		t.Errorf("NULL 游标被读成了 %q，应该是 nil", *nover.SyncCursor)
	}

	pos, err := repo.Get(ctx, "sess-qoder") // 夹具里 sync_cursor = '20'
	if err != nil {
		t.Fatal(err)
	}
	if pos.SyncCursor == nil || *pos.SyncCursor != "20" {
		t.Fatalf("非空游标读丢了: %+v", pos.SyncCursor)
	}

	if _, err := db.Exec(`INSERT INTO sessions (id, adapter_instance_id, sync_state, sync_cursor,
		state_confidence, created_at, updated_at)
		VALUES ('sess-empt', 'ad-codex', 'SYNCED', '', 'HIGH', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	empty, err := repo.Get(ctx, "sess-empt")
	if err != nil {
		t.Fatal(err)
	}
	if empty.SyncCursor != nil {
		t.Errorf("空串游标被当成一个位置返回了（%q）；REQ-F-21 只承认非空串与 null 两种值", *empty.SyncCursor)
	}
}

func TestListMessagesOrdersBySequenceThenTime(t *testing.T) {
	db := openSchema(t)
	seed(t, db)
	repo := NewRepository(db)

	all, err := repo.ListMessages(context.Background(), "sess-qoder", 10)
	if err != nil {
		t.Fatal(err)
	}
	// m-1(seq 1) m-2(seq 2) m-3(seq NULL → created_at 300)
	if len(all) != 3 || all[0].ID != "m-1" || all[1].ID != "m-2" || all[2].ID != "m-3" {
		var got []string
		for _, m := range all {
			got = append(got, m.ID)
		}
		t.Fatalf("顺序应为 m-1,m-2,m-3，实际 %v", got)
	}
	if all[2].SequenceNo != 0 || all[2].RemoteMessageID != "" {
		t.Errorf("NULL 的 sequence_no/remote_message_id 不该造出值: %+v", all[2])
	}
	if all[0].Role != RoleUser || all[2].Kind != KindToolResult {
		t.Errorf("role/kind 读错: %+v %+v", all[0], all[2])
	}
	if limited, err := repo.ListMessages(context.Background(), "sess-qoder", 2); err != nil || len(limited) != 2 {
		t.Errorf("limit 没生效: %d %v", len(limited), err)
	}
}

// §6 目录里触发者为 session 的四行，独立手抄，与包内常量 + payload 键双向对账。
var catalog6 = []struct {
	eventType string
	keys      []string
	payload   any
}{
	{EventDiscovered, []string{"remote_session_id", "adapter_instance_id"},
		DiscoveredPayload{RemoteSessionID: "r", AdapterInstanceID: "a"}},
	{EventManaged, []string{"managed", "project_id"}, ManagedPayload{Managed: true, ProjectID: "p"}},
	{EventSynced, []string{"sync_state", "sync_cursor", "remote_revision", "confidence"},
		SyncedPayload{SyncState: SyncSynced, SyncCursor: "20", RemoteRevision: "rev", Confidence: ConfidenceHigh}},
	{EventMessageRecorded, []string{"role", "kind", "sequence_no", "remote_message_id"},
		MessageRecordedPayload{Role: RoleUser, Kind: KindMessage, SequenceNo: 1, RemoteMessageID: "r1"}},
}

func TestEventCatalogMatchesContract6(t *testing.T) {
	mine := []string{EventDiscovered, EventManaged, EventSynced, EventMessageRecorded}
	if len(mine) != len(catalog6) {
		t.Fatalf("包内 %d 个事件类型，§6 的 session 行 %d 条", len(mine), len(catalog6))
	}
	registered := map[string]bool{}
	for _, c := range catalog6 {
		if registered[c.eventType] {
			t.Fatalf("测试表重复登记 %s", c.eventType)
		}
		registered[c.eventType] = true
	}
	for _, e := range mine {
		if !registered[e] {
			t.Errorf("%s 没在 §6 登记（§9.1 P0）", e)
		}
	}
	for _, c := range catalog6 {
		m := map[string]any{}
		b, err := json.Marshal(c.payload)
		if err != nil {
			t.Fatalf("%s: %v", c.eventType, err)
		}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: %v", c.eventType, err)
		}
		got := []string{}
		for k := range m {
			got = append(got, k)
		}
		sort.Strings(got)
		want := append([]string{}, c.keys...)
		sort.Strings(want)
		// 不比对 schema：那个键由写侧注入，归属等 C8 裁定。
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s payload 键 = %v，§6 要求 %v", c.eventType, got, want)
		}
	}
}

// 词表必须与 000001 的 CHECK 一致，否则写侧一落库就撞约束。
func TestWordListsMatchSchemaChecks(t *testing.T) {
	db := openSchema(t)
	seed(t, db)
	for _, bad := range []string{"GUESS", "OFFLINE", "0", "Failed"} {
		if _, err := db.Exec(`INSERT INTO sessions (id, adapter_instance_id, sync_state, created_at, updated_at)
			VALUES ('s-bad', 'ad-codex', ?, 1, 1)`, bad); err == nil {
			t.Errorf("schema 的 CHECK 竟然放过了 sync_state=%q，词表要重新对", bad)
			db.Exec(`DELETE FROM sessions WHERE id = 's-bad'`)
		}
	}
	for _, good := range []SyncState{SyncUnknown, SyncSynced, SyncSyncing, SyncLocalAhead,
		SyncRemoteAhead, SyncConflict, SyncDegraded, SyncUnavailable} {
		if _, err := db.Exec(`INSERT INTO sessions (id, adapter_instance_id, sync_state, created_at, updated_at)
			VALUES (?, 'ad-codex', ?, 1, 1)`, "s-"+string(good), string(good)); err != nil {
			t.Errorf("%s 被 schema 拒了：词表与 CHECK 不符: %v", good, err)
		}
		if !good.Valid() {
			t.Errorf("%s 应在我方词表里", good)
		}
	}
	if SyncState("GUESS").Valid() || Confidence("GUESS").Valid() || LinkRole("OWNER").Valid() {
		t.Error("词表校验放水")
	}
}
