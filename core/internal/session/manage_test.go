package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
)

// 这一批的写侧第一次落到 sessions 上，所以夹具全部走真库真表（migrations 两份），
// 而不是沿用 repository_test.go 那份手建 schema —— 那一份没有 redaction/平台写路径，测不了 Touch。

func openMigrated(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "lucero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	if err := db.InitRedaction([]string{"token"}, nil, "[REDACTED]"); err != nil {
		t.Fatal(err)
	}
	for _, p := range [][2]string{{"system", "SYSTEM"}, {"local-user", "USER"}} {
		if _, err := d.Read.Exec(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
			VALUES (?, ?, ?, 1, 1)`, p[0], p[1], p[0]); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

// seedSession 建一条 unmanaged 的会话（FK 要 adapter_instances 的父行）。
// 走裸 INSERT 是**夹具**：生产写路径仍然只有 Service 那一条。
func seedSession(t *testing.T, d *db.DB, id string) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES (?, ?, ?, 1, 1)`, "ai-"+id, "key-"+id, id); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read.Exec(`INSERT INTO sessions (id, adapter_instance_id, managed, sync_state, state_confidence,
		created_at, updated_at) VALUES (?, ?, 0, 'UNKNOWN', 'UNKNOWN', 1, 1)`, id, "ai-"+id); err != nil {
		t.Fatal(err)
	}
}

func seedProject(t *testing.T, d *db.DB, id string) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO projects (id, name, canonical_path, project_type, status,
		metadata_json, created_at, updated_at) VALUES (?, ?, ?, 'CODE', 'ACTIVE', '{}', 1, 1)`,
		id, "p-"+id, "d:\\p\\"+id); err != nil {
		t.Fatal(err)
	}
}

func runManage(t *testing.T, d *db.DB, id string, cmd session.ManageCommand, at int64) (session.Session, bool, error) {
	t.Helper()
	var (
		out     session.Session
		changed bool
	)
	svc := session.NewService(session.NewRepository(d.Read), func() int64 { return at })
	err := d.Write(context.Background(), func(tx *db.Tx) error {
		var err error
		out, changed, err = svc.Manage(context.Background(), tx, id, cmd)
		return err
	})
	return out, changed, err
}

func readSessionRow(t *testing.T, d *db.DB, id string) (managed int, project any, version int64, updatedAt int64) {
	t.Helper()
	if err := d.Read.QueryRow(`SELECT managed, project_id, version, updated_at FROM sessions WHERE id = ?`, id).
		Scan(&managed, &project, &version, &updatedAt); err != nil {
		t.Fatal(err)
	}
	return
}

func managedEvents(t *testing.T, d *db.DB, id string) (int, string) {
	t.Helper()
	var n int
	var payload string
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE event_type = 'session_managed' AND aggregate_id = ?`, id).
		Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n > 0 {
		if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal WHERE event_type = 'session_managed' AND aggregate_id = ?
			ORDER BY seq DESC LIMIT 1`, id).Scan(&payload); err != nil {
			t.Fatal(err)
		}
	}
	return n, payload
}

func strPtr(s string) *string { return &s }

func TestManageSetsManagedAndEmitsExactlyOneEvent(t *testing.T) {
	d := openMigrated(t)
	seedSession(t, d, "s-1")

	out, changed, err := runManage(t, d, "s-1", session.ManageCommand{Managed: true, EventID: "e-1", PrincipalID: "local-user"}, 5_000)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("0→1 该报「真改了」")
	}
	if !out.Managed || out.Version != 2 {
		t.Errorf("返回值该是读回的那行（managed=true v2），实际 %+v", out)
	}
	if managed, project, version, updatedAt := readSessionRow(t, d, "s-1"); managed != 1 || version != 2 || updatedAt != 5_000 {
		t.Errorf("库里该是 managed=1 v2 updated_at=5000，实际 %d v%d/%d", managed, version, updatedAt)
	} else if project != nil {
		t.Errorf("没给 project_id 就不该动那一列（更不该写成空串），实际 %#v", project)
	}
	n, payload := managedEvents(t, d, "s-1")
	if n != 1 {
		t.Fatalf("恰一行 session_managed，实际 %d", n)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatal(err)
	}
	// §6 那一格的键就是 `managed` `project_id`（外加平台补的 schema）。
	for _, k := range []string{"managed", "project_id", "schema"} {
		if _, ok := m[k]; !ok {
			t.Errorf("payload 缺 §6 要求的键 %s: %s", k, payload)
		}
	}
	if m["managed"] != true {
		t.Errorf("payload.managed 该是 true，实际 %v", m["managed"])
	}
	// C41 裁定：`project_id` **键必须在、值可空**。这条会话没有归属 ⇒ 要的是 JSON `null`，
	// 不是 `""`（那是"绑到一个空串命名的项目"），也不是把键省掉（那是 REQ-F-22 禁的第二种写法）。
	if v, ok := m["project_id"]; !ok {
		t.Errorf("payload 省掉了 project_id 这一键（裁定要求键必须在）: %s", payload)
	} else if v != nil {
		t.Errorf("没有归属时 project_id 该是 null，实际 %#v（%T）", v, v)
	}
}

func TestManageTwiceWithSameValueIsANoOp(t *testing.T) {
	d := openMigrated(t)
	seedSession(t, d, "s-2")
	if _, _, err := runManage(t, d, "s-2", session.ManageCommand{Managed: true, EventID: "e-a"}, 1_000); err != nil {
		t.Fatal(err)
	}
	_, _, v1, u1 := readSessionRow(t, d, "s-2")

	out, changed, err := runManage(t, d, "s-2", session.ManageCommand{Managed: true, EventID: "e-b"}, 9_000)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("同值再打该报「没改」")
	}
	if out.Version != v1 || !out.Managed {
		t.Errorf("no-op 该原样回库里那一行（v%d、managed=true），实际 %+v", v1, out)
	}
	if _, _, version, updatedAt := readSessionRow(t, d, "s-2"); version != v1 || updatedAt != u1 {
		t.Errorf("no-op 连 updated_at 都不许刷新（那会把第一次生效的时间冲掉）: v%d/%d", version, updatedAt)
	}
	if n, _ := managedEvents(t, d, "s-2"); n != 1 {
		t.Errorf("第二次不许再发一条事件，实际 %d 行", n)
	}
}

func TestManageCanRevokeManaged(t *testing.T) {
	d := openMigrated(t)
	seedSession(t, d, "s-3")
	if _, _, err := runManage(t, d, "s-3", session.ManageCommand{Managed: true, EventID: "e-1"}, 1); err != nil {
		t.Fatal(err)
	}
	// 契约③ §3 那句只说"置 managed"，请求体是布尔位 ⇒ 撤销按合法处理（这一格是 C41 里我自挑的一格）。
	out, changed, err := runManage(t, d, "s-3", session.ManageCommand{Managed: false, EventID: "e-2"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || out.Managed {
		t.Errorf("true→false 该放行并回 managed=false，实际 changed=%v %+v", changed, out)
	}
}

func TestManageAssignsAndClearsProject(t *testing.T) {
	d := openMigrated(t)
	seedSession(t, d, "s-4")
	seedProject(t, d, "prj-1")

	out, changed, err := runManage(t, d, "s-4", session.ManageCommand{
		Managed: true, ProjectID: strPtr("prj-1"), EventID: "e-1",
	}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || out.ProjectID != "prj-1" {
		t.Errorf("该挂上项目并读回来，实际 changed=%v %+v", changed, out)
	}
	if n, payload := managedEvents(t, d, "s-4"); n != 1 {
		t.Fatalf("一行事件，实际 %d", n)
	} else if !json.Valid([]byte(payload)) || payload == "" {
		t.Errorf("payload 坏了: %q", payload)
	}

	// 显式 null（指针指着空串）= 清空归属：库里要是落成 '' 而不是 NULL，外键列上就是"指向一个不存在的项目"。
	out2, changed2, err := runManage(t, d, "s-4", session.ManageCommand{
		Managed: true, ProjectID: strPtr(""), EventID: "e-2",
	}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !changed2 || out2.ProjectID != "" {
		t.Errorf("清空该算一次真改动，实际 changed=%v %+v", changed2, out2)
	}
	if _, project, _, _ := readSessionRow(t, d, "s-4"); project != nil {
		t.Errorf("清空归属要的是 NULL，不是空串（STRICT + 外键列），实际 %#v", project)
	}
	// 解绑那一发的 payload 也要把这件事说出来：键在、值是 null。
	if n, payload := managedEvents(t, d, "s-4"); n != 2 {
		t.Fatalf("两次真改动各一行事件，实际 %d", n)
	} else {
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			t.Fatal(err)
		}
		if v, ok := m["project_id"]; !ok || v != nil {
			t.Errorf("解绑那一发的 payload 该是 {project_id: null}，实际 %#v（%s）", v, payload)
		}
	}
}

func TestManageRejectsUnknownProjectWithoutWriting(t *testing.T) {
	d := openMigrated(t)
	seedSession(t, d, "s-5")

	_, changed, err := runManage(t, d, "s-5", session.ManageCommand{
		Managed: true, ProjectID: strPtr("prj-none"), EventID: "e-1",
	}, 5)
	if !errors.Is(err, session.ErrProjectNotFound) {
		t.Fatalf("未知项目该报 ErrProjectNotFound（映射成 400），实际 %v", err)
	}
	if changed {
		t.Error("被拒的调用不许报「真改了」")
	}
	if managed, _, version, updatedAt := readSessionRow(t, d, "s-5"); managed != 0 || version != 1 || updatedAt != 1 {
		t.Errorf("被拒之后一行都不该动，实际 managed=%d v%d updated_at=%d", managed, version, updatedAt)
	}
	if n, _ := managedEvents(t, d, "s-5"); n != 0 {
		t.Errorf("被拒的写不发事件，实际 %d 行", n)
	}
}

func TestManageValidatesCommandAndSession(t *testing.T) {
	d := openMigrated(t)
	seedSession(t, d, "s-6")

	if _, _, err := runManage(t, d, "s-6", session.ManageCommand{Managed: true, EventID: "  "}, 1); !errors.Is(err, session.ErrInvalidCommand) {
		t.Errorf("空 event_id 该在入口拒（重试要复用同一个），实际 %v", err)
	}
	if _, _, version, _ := readSessionRow(t, d, "s-6"); version != 1 {
		t.Errorf("被拒的调用不留痕，实际 v%d", version)
	}
	if _, _, err := runManage(t, d, "s-none", session.ManageCommand{Managed: true, EventID: "e"}, 1); !errors.Is(err, session.ErrSessionNotFound) {
		t.Errorf("未知会话该报 ErrSessionNotFound（映射成 404），实际 %v", err)
	}
}

// TestManagedPayloadReportsEffectiveValuesNotTheRequest 钉一件很容易含糊过去的事：
// 事件里那两个键该是**生效后**的值。只发请求给的那一格，会让事件说"这条会话没有归属"，
// 而库里那一行明明挂着项目 —— 那是事件替请求撒谎。
func TestManagedPayloadReportsEffectiveValuesNotTheRequest(t *testing.T) {
	d := openMigrated(t)
	seedSession(t, d, "s-7")
	seedProject(t, d, "prj-7")
	if _, _, err := runManage(t, d, "s-7", session.ManageCommand{
		Managed: true, ProjectID: strPtr("prj-7"), EventID: "e-1",
	}, 1); err != nil {
		t.Fatal(err)
	}
	// 第二发**只撤销 managed、没提 project_id**：这次是真改动（true→false），要发第二条事件，
	// 而那条事件的 project_id 该照库里现值 "prj-7"，不是空串。
	if _, changed, err := runManage(t, d, "s-7", session.ManageCommand{Managed: false, EventID: "e-2"}, 2); err != nil || !changed {
		t.Fatalf("true→false 该是真改动，changed=%v err=%v", changed, err)
	}
	n, payload := managedEvents(t, d, "s-7")
	if n != 2 {
		t.Fatalf("两发各一行事件，实际 %d", n)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		t.Fatal(err)
	}
	if m["managed"] != false {
		t.Errorf("payload.managed 该是生效后的 false，实际 %s", payload)
	}
	if m["project_id"] != "prj-7" {
		t.Errorf("请求没提 project_id，payload 就该照库里那一格（不是拿空串冒充\"这次没归属\"）: %s", payload)
	}
	if _, project, _, _ := readSessionRow(t, d, "s-7"); project != "prj-7" {
		t.Errorf("第二发不该动归属，实际 %#v", project)
	}
}
