package session_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/SadCat18/lucero/core/internal/platform/db"
	"github.com/SadCat18/lucero/core/internal/session"
)

// discover 的域半边测的是"去重 upsert"这四字，而不是"能不能跑通"：
// 首次建两行 + 恰两行事件；第二次同样的清单必须**一行都不新建**、返回同一批 id（yaml 那条 description 就这么写的）。

func seedInstance(t *testing.T, d *db.DB, id, key string) {
	t.Helper()
	if _, err := d.Read.Exec(`INSERT INTO adapter_instances (id, adapter_key, display_name, created_at, updated_at)
		VALUES (?, ?, ?, 1, 1)`, id, key, key); err != nil {
		t.Fatal(err)
	}
}

func twoRows() []session.DiscoveredRemote {
	return []session.DiscoveredRemote{
		{RemoteSessionID: "rsn-1", Title: "修 redaction 洞", ProjectPathHint: "D:/Project/Lucero", UpdatedAt: 111},
		{RemoteSessionID: "rsn-2", ProjectPathHint: "D:/tmp/elsewhere", UpdatedAt: 222},
	}
}

func runDiscover(t *testing.T, d *db.DB, instanceID string, rows []session.DiscoveredRemote) (session.DiscoverResult, error) {
	t.Helper()
	var (
		out session.DiscoverResult
		n   int
	)
	svc := session.NewService(session.NewRepository(d.Read), func() int64 { return 7_000 })
	err := d.Write(context.Background(), func(tx *db.Tx) error {
		var err error
		out, err = svc.UpsertDiscovered(context.Background(), tx, session.DiscoverCommand{
			AdapterInstanceID: instanceID, Rows: rows,
			NewID: func() string {
				n++
				return "sess-" + instanceID + "-" + string(rune('a'+n))
			},
		})
		return err
	})
	return out, err
}

func discoveredEvents(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.Read.QueryRow(`SELECT count(*) FROM event_journal WHERE event_type = 'session_discovered'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUpsertDiscoveredCreatesOnceThenReuses(t *testing.T) {
	d := openMigrated(t)
	seedInstance(t, d, "ai-1", "fake")

	first, err := runDiscover(t, d, "ai-1", twoRows())
	if err != nil {
		t.Fatal(err)
	}
	if first.Created != 2 || len(first.Items) != 2 {
		t.Fatalf("首次该建两行，实际 created=%d items=%v", first.Created, first.Items)
	}
	if first.Items[0].SessionID == "" || first.Items[0].RemoteSessionID != "rsn-1" {
		t.Errorf("返回的是 (本地 id, 远端 id) 成对，实际 %+v", first.Items[0])
	}
	if n := discoveredEvents(t, d); n != 2 {
		t.Fatalf("两行 = 两行事件，实际 %d", n)
	}

	second, err := runDiscover(t, d, "ai-1", twoRows())
	if err != nil {
		t.Fatal(err)
	}
	if second.Created != 0 {
		t.Errorf("重复 discover 不该建新行，实际 created=%d", second.Created)
	}
	for i := range second.Items {
		if second.Items[i].SessionID != first.Items[i].SessionID {
			t.Errorf("重复 discover 必须返回同一批 id（第 %d 条 %q≠%q）", i, second.Items[i].SessionID, first.Items[i].SessionID)
		}
	}
	if n := discoveredEvents(t, d); n != 2 {
		t.Errorf("第二次一条事件都不许发，实际共 %d 行", n)
	}
	if rows := count(t, d, `SELECT count(*) FROM sessions`); rows != 2 {
		t.Errorf("库里该还是 2 行，实际 %d", rows)
	}
}

func TestDiscoveredRowShapeMatchesTheRuling(t *testing.T) {
	d := openMigrated(t)
	seedInstance(t, d, "ai-2", "fake")
	if _, err := runDiscover(t, d, "ai-2", twoRows()); err != nil {
		t.Fatal(err)
	}
	// 归属留空是**契约原意**（§6:176 那格的 payload 没有 project_id；绑项目是 :manage 的事，C41/T-196 已裁）。
	// 这一条专门钉它，是因为"顺手把 hint 解析成项目"看起来很像帮上游省一步。
	var (
		project, title sql.NullString
		managed        int
		createdAt      int64
		updatedAt      int64
		meta           string
	)
	if err := d.Read.QueryRow(`SELECT project_id, title, managed, created_at, updated_at, metadata_json
		FROM sessions WHERE remote_session_id = 'rsn-1'`).
		Scan(&project, &title, &managed, &createdAt, &updatedAt, &meta); err != nil {
		t.Fatal(err)
	}
	if project.Valid {
		t.Errorf("discover 不许绑项目（该留 NULL），实际 %#v", project.String)
	}
	if !title.Valid || title.String != "修 redaction 洞" {
		t.Errorf("§4 给的 title 该落下来（rsn-1 有标题），实际 %#v", title)
	}
	if managed != 0 {
		t.Errorf("discover 建出来该是未受管理，实际 %d", managed)
	}
	if createdAt != 7_000 || updatedAt != 7_000 {
		t.Errorf("created_at/updated_at 该取注入的时钟（7000），实际 %d/%d", createdAt, updatedAt)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		t.Fatalf("metadata_json 不是合法 JSON（列上有 CHECK）: %v (%s)", err, meta)
	}
	// §4 那四个字段里两个没有列：进 metadata_json 而不是丢，也不新开列（迁移是 db 的地盘）。
	if m["remote_updated_at"] != float64(111) || m["project_path_hint"] != "D:/Project/Lucero" {
		t.Errorf("§4 给的两个无列字段该留在 metadata_json 里，实际 %s", meta)
	}
	if m["schema"] != float64(1) {
		t.Errorf("metadata_json 缺 schema: %s", meta)
	}

	// 事件载荷只允许 §6 登记的两键（多塞一个键就是替契约发明字段）。
	var payload string
	if err := d.Read.QueryRow(`SELECT payload_json FROM event_journal WHERE event_type = 'session_discovered' LIMIT 1`).
		Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"remote_session_id", "adapter_instance_id", "schema"} {
		if _, ok := p[k]; !ok {
			t.Errorf("payload 缺键 %s: %s", k, payload)
		}
	}
	if len(p) != 3 {
		t.Errorf("payload 只该有 §6 登记的两键 + 平台补的 schema，实际 %s", payload)
	}
	if p["adapter_instance_id"] != "ai-2" {
		t.Errorf("payload.adapter_instance_id 该是这次归属的那个实例，实际 %v", p["adapter_instance_id"])
	}
}

func TestUpsertDiscoveredRejectsUnusablePages(t *testing.T) {
	cases := []struct {
		name string
		cmd  session.DiscoverCommand
	}{
		{"没有实例 id", session.DiscoverCommand{Rows: twoRows(), NewID: func() string { return "x" }}},
		{"没有 NewID", session.DiscoverCommand{AdapterInstanceID: "ai-3", Rows: twoRows()}},
		{"有一行缺 remote_session_id", session.DiscoverCommand{
			AdapterInstanceID: "ai-3",
			Rows:              []session.DiscoveredRemote{{RemoteSessionID: "ok"}, {Title: "没名字"}},
			NewID:             func() string { return "x" }}},
		{"同一页里 remote id 重复", session.DiscoverCommand{
			AdapterInstanceID: "ai-3",
			Rows:              []session.DiscoveredRemote{{RemoteSessionID: "dup"}, {RemoteSessionID: "dup"}},
			NewID:             func() string { return "x" }}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := openMigrated(t)
			seedInstance(t, d, "ai-3", "fake")
			svc := session.NewService(session.NewRepository(d.Read))
			err := d.Write(context.Background(), func(tx *db.Tx) error {
				_, err := svc.UpsertDiscovered(context.Background(), tx, tc.cmd)
				return err
			})
			if !errors.Is(err, session.ErrInvalidCommand) {
				t.Fatalf("该在入口拒（%s），实际 %v", tc.name, err)
			}
			// 被拒的整批不留痕：半页写进去就再也说不清"这次发现了什么"。
			if n := count(t, d, `SELECT count(*) FROM sessions`); n != 0 {
				t.Errorf("被拒的一批不该留下任何行，实际 %d", n)
			}
			if n := discoveredEvents(t, d); n != 0 {
				t.Errorf("被拒的一批不发事件，实际 %d", n)
			}
		})
	}
}

func TestUpsertDiscoveredScopesByInstance(t *testing.T) {
	d := openMigrated(t)
	seedInstance(t, d, "ai-a", "codex")
	seedInstance(t, d, "ai-b", "qoder")
	if _, err := runDiscover(t, d, "ai-a", twoRows()); err != nil {
		t.Fatal(err)
	}
	// 同一批 remote id 属于另一台实例 ⇒ 是**新的两行**，不是"去重命中"：
	// remote_session_id 只在它自己的 Provider 里有意义，跨实例去重会把两台软件的会话并成一条。
	out, err := runDiscover(t, d, "ai-b", twoRows())
	if err != nil {
		t.Fatal(err)
	}
	if out.Created != 2 {
		t.Errorf("另一台实例上的同名 remote 会话该各建一行，实际 created=%d", out.Created)
	}
	if n := count(t, d, `SELECT count(*) FROM sessions`); n != 4 {
		t.Errorf("库里该有 4 行（每台实例两行），实际 %d", n)
	}
}

func count(t *testing.T, d *db.DB, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := d.Read.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
