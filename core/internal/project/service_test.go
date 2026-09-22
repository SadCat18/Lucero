package project_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/project"
)

func openDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "lucero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	// 脱敏 fail-closed：不初始化就什么都写不进去。规则的所有者是 audit，本包不 import 它（C14）。
	if err := db.InitRedaction([]string{"token"}, nil, "[REDACTED]"); err != nil {
		t.Fatal(err)
	}
	// event_journal.principal_id 是指向 principals 的外键：先把要用的主体建出来（同其他包的夹具）。
	for _, pr := range [][2]string{{"system", "SYSTEM"}, {"local-user", "USER"}} {
		if _, err := d.Read.Exec(`INSERT INTO principals (id, principal_type, display_name, created_at, updated_at)
			VALUES (?, ?, ?, 1, 1)`, pr[0], pr[1], pr[0]); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func draft(id, name, path string) project.Draft {
	return project.Draft{ID: id, EventID: "ev-" + id, Name: name, Path: path,
		PrincipalID: "system", CorrelationID: "c-" + id, CausationID: "c-" + id}
}

// TestCanonicalizeAcceptsTheThreeDocumentedForms 钉领域模型 §7 给的那三个反例必须同键，
// 且不同目录不许被折成同一条（那是把两个项目合成一个，比冲突更坏）。
func TestCanonicalizeAcceptsTheThreeDocumentedForms(t *testing.T) {
	want := project.Canonicalize(`D:\MZO`)
	for _, v := range []string{`D:/MZO`, `\\?\D:\MZO`, `d:\mzo`, `D:\MZO\`, `D:\MZO\.\`} {
		if got := project.Canonicalize(v); got != want {
			t.Errorf("%q 收成 %q，期望 %q", v, got, want)
		}
	}
	if want != `d:\mzo` {
		t.Errorf("规范形本身也要有定论：实际 %q", want)
	}
	for _, other := range []string{`D:\MZO2`, `E:\MZO`, `D:\MZO\a`, ``} {
		if project.Canonicalize(other) == want {
			t.Errorf("%q 被误折成同一个键了", other)
		}
	}
	if project.Canonicalize(`\\srv\share\a`) != strings.ToLower(`\\srv\share\a`) {
		t.Errorf("UNC 前缀被折坏了: %q", project.Canonicalize(`\\srv\share\a`))
	}
	if project.Canonicalize(`D:`) != `d:\` {
		t.Errorf(`裸盘符要落到根: %q`, project.Canonicalize(`D:`))
	}
}

// TestCreateWritesOneRowAndOneEvent 走真库真表：v1、恰一行 project_created、空路径写 NULL 而不是空串。
func TestCreateWritesOneRowAndOneEvent(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	svc := project.NewService(nil)

	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.Create(ctx, tx, draft("p-1", "Lucero 主仓", `D:/Project/Lucero`))
		return err
	}); err != nil {
		t.Fatal(err)
	}

	var (
		status, typ string
		canonical   any
		version     int64
		archived    any
	)
	if err := d.Read.QueryRow(`SELECT status, canonical_path, project_type, version, archived_at FROM projects WHERE id='p-1'`).
		Scan(&status, &canonical, &typ, &version, &archived); err != nil {
		t.Fatal(err)
	}
	if status != "ACTIVE" || version != 1 || typ != "UNKNOWN" {
		t.Errorf("默认值不对: status=%s v=%d type=%s", status, version, typ)
	}
	if s, ok := canonical.(string); !ok || s != project.Canonicalize(`D:/Project/Lucero`) {
		t.Errorf("canonical_path 没收成键: %v", canonical)
	}
	if archived != nil {
		t.Errorf("archived_at 必须是 NULL，实际 %v", archived)
	}
	if n := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type='project_created' AND aggregate_id='p-1'`); n != 1 {
		t.Errorf("project_created 应恰有一行，实际 %d", n)
	}
	// §6 只给两个键：多一个键就是我自己发明契约。
	var payload string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal WHERE event_type='project_created' AND aggregate_id='p-1'`).
		Scan(&payload); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"name"`, `"canonical_path"`, `"schema"`} {
		if !strings.Contains(payload, k) {
			t.Errorf("payload 缺 %s: %s", k, payload)
		}
	}
	if strings.Contains(payload, `"metadata"`) || strings.Contains(payload, `"status"`) {
		t.Errorf("payload 出现了 §6 之外的键: %s", payload)
	}
}

// TestPathConflictAndEmptyPathRules 钉 §3 那句"canonicalize 后的 path 冲突返回 409"，
// 以及"空路径=NULL=可以有任意多个"（那条部分唯一索引明确豁免 NULL）。
func TestPathConflictAndEmptyPathRules(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	svc := project.NewService(nil)

	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := svc.Create(ctx, tx, draft("p-a", "甲", `C:\work\same`))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// 换一种写法（斜杠 + 大小写 + 尾斜杠）必须是同一个键 ⇒ 撞。
	err := d.Write(ctx, func(tx *db.Tx) error {
		_, e := svc.Create(ctx, tx, draft("p-b", "乙", `c:/work/same/`))
		return e
	})
	if !errors.Is(err, project.ErrPathConflict) {
		t.Fatalf("同路径第二个项目该被判冲突，实际 %v", err)
	}
	// 冲突那笔必须整体回滚：既不多行，也不许多事件。
	if n := countRows(t, d, `SELECT count(*) FROM projects`); n != 1 {
		t.Errorf("冲突后项目行数应是 1，实际 %d", n)
	}
	if n := countRows(t, d, `SELECT count(*) FROM event_journal WHERE event_type='project_created'`); n != 1 {
		t.Errorf("冲突那笔不该留下事件，实际 %d 条", n)
	}
	// 空路径写 NULL：两条都允许。
	for _, id := range []string{"p-c", "p-d"} {
		if err := d.Write(ctx, func(tx *db.Tx) error {
			_, e := svc.Create(ctx, tx, draft(id, "没有路径的项目", ""))
			return e
		}); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	var nulls int
	if err := d.Read.QueryRow(`SELECT count(*) FROM projects WHERE canonical_path IS NULL`).Scan(&nulls); err != nil {
		t.Fatal(err)
	}
	if nulls != 2 {
		t.Errorf("空路径应是 NULL 且可多行，实际 %d 行", nulls)
	}
	// 输入本身不合法：空白 name 必须在碰库之前就失败。
	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, e := svc.Create(ctx, tx, draft("p-e", "   ", ""))
		return e
	}); !errors.Is(err, project.ErrInvalid) {
		t.Errorf("空白 name 该报 ErrInvalid，实际 %v", err)
	}
	if n := countRows(t, d, `SELECT count(*) FROM projects WHERE id='p-e'`); n != 0 {
		t.Errorf("非法输入居然留了行: %d", n)
	}
}

func countRows(t *testing.T, d *db.DB, q string) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestGetReadsBackTheStoredRow 是 C18 裁定 ② 的落点：首次响应也必须来自库里那一行。
func TestGetReadsBackTheStoredRow(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	svc := project.NewService(nil)
	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, e := svc.Create(ctx, tx, draft("p-g", "回读", `X:\y`))
		return e
	}); err != nil {
		t.Fatal(err)
	}
	got, err := project.Get(ctx, d.Read, "p-g")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "回读" || got.Status != project.Active || got.Version != 1 ||
		got.CanonicalPath != project.Canonicalize(`X:\y`) || got.CreatedAt == 0 {
		t.Errorf("读回不对: %+v", got)
	}
	if _, err := project.Get(ctx, d.Read, "nope"); err == nil {
		t.Error("不存在的项目该报错，不该返回零值")
	}
}
