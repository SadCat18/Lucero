package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/audit"
	"github.com/SadCat18/lucero/core/internal/process"
)

// 编译期就把 T-145 那四格钉住：少实现一个方法就 build 不过（这正是主控说的"漂移会被编译器抓住"那一半，
// 另一半是"读契约的人别再以为契约说了什么"——那半已由 §7.2 落了文）。
var _ process.Recorder = (*dropRecorder)(nil)

func TestRecordDropWritesOneAuditRowAndNoEvent(t *testing.T) {
	core, err := boot(context.Background(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()
	ctx := context.Background()

	const secret = "sk-abcdefghijklmnop0123456789" // 与 audit.Rules() 的形状规则同形
	journalBefore := countRowsBy(t, core, `SELECT count(*) FROM event_journal`)
	var logged []string
	rec := newDropRecorder(core.store, "codex", func() int64 { return 1234 },
		func(f string, args ...any) { logged = append(logged, f) })

	rec.RecordDrop(process.DroppedEvent{
		Kind: process.KindAdapter, Method: "session/prompt", TurnRef: "turn-9",
		FrameExecutionID: "exec-frame", BoundExecutionID: "exec-bound", // 故意不一致：这一行就是要留住它
		Reason:    "帧里的 execution_id 与工单绑定的不符，用户原文是 " + secret,
		Timestamp: 99,
	})

	rows, err := loadAuditEntries(ctx, core.db.Read)
	if err != nil {
		t.Fatal(err)
	}
	var drop, redaction *audit.Entry
	for i := range rows {
		switch rows[i].Action {
		case "frame_dropped":
			drop = &rows[i]
		case "redaction.frame_dropped":
			redaction = &rows[i]
		}
	}
	if drop == nil {
		t.Fatalf("没落 frame_dropped 那一行，现有 %d 行", len(rows))
	}
	if redaction == nil {
		t.Error("命中了脱敏却没写归因行（§2.2.1 的调用者半边没做）")
	}
	if drop.Outcome != "FAILURE" || drop.ResourceType != "adapter" || drop.ResourceID != "codex" {
		t.Errorf("归属/结果不对: %+v", *drop)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(drop.DetailsJSON), &details); err != nil {
		t.Fatalf("details_json 不是合法 JSON: %v (%s)", err, drop.DetailsJSON)
	}
	// T84-7b 00:2x 新增那条断言：这一行必须能指出是哪个 Adapter。
	if details["adapter_key"] != "codex" {
		t.Errorf("details_json 没指出是哪个 Adapter: %v", details["adapter_key"])
	}
	// 两个 execution id 不许被互相校正（T84-7）。
	if details["frame_execution_id"] != "exec-frame" || details["bound_execution_id"] != "exec-bound" {
		t.Errorf("两个 id 被改了/被合并了: %v / %v", details["frame_execution_id"], details["bound_execution_id"])
	}
	if details["turn_ref"] != "turn-9" || details["method"] != "session/prompt" {
		t.Errorf("工单与方法没原样留住: %v", details)
	}
	if strings.Contains(drop.DetailsJSON, secret) {
		t.Error("密钥原样落进了审计行")
	}
	if !strings.Contains(drop.DetailsJSON, audit.Rules().Marker) {
		t.Errorf("落库的 details 里没有脱敏标记，这条用例没验到东西: %s", drop.DetailsJSON)
	}
	// §7.2:310~311 断的那一对：只写审计行、不写 event_journal。
	if n := countRowsBy(t, core, `SELECT count(*) FROM event_journal`); n != journalBefore {
		t.Errorf("丢弃不该产生事件行：event_journal 从 %d 变成 %d", journalBefore, n)
	}
	// 新的生产者进来，链仍然重算得出来（这条同时是 §2.2.1 那套顺序的第二份证据）。
	entries, err := loadAuditEntries(ctx, core.db.Read)
	if err != nil {
		t.Fatal(err)
	}
	if verr := audit.VerifyChain(entries, audit.Head{}); verr != nil {
		t.Errorf("丢弃这一行没让哈希盖住落库字节: %v", verr)
	}
	if len(logged) != 0 {
		t.Errorf("成功路径不该喊：日志 %v", logged)
	}
}

// TestRecordDropRefusesToBlameNobody 钉"不许拿空串冒充知道"：身份没装配时这一行**不写**，但必须出声。
func TestRecordDropRefusesToBlameNobody(t *testing.T) {
	core, err := boot(context.Background(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()

	var logged []string
	rec := newDropRecorder(core.store, "", func() int64 { return 1 },
		func(f string, _ ...any) { logged = append(logged, f) })
	rec.RecordDrop(process.DroppedEvent{Method: "session/prompt", Reason: "没身份的那次丢弃"})

	if n := countRowsBy(t, core, `SELECT count(*) FROM audit_entries WHERE action = 'frame_dropped'`); n != 0 {
		t.Errorf("身份缺失时写下的审计行是假账，实际落了 %d 行", n)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "归属") {
		t.Errorf("不写要出声，日志 %v", logged)
	}
}

// TestLifecycleMethodsShoutInsteadOfSilentlyPassing 钉生命周期三格在 T-168 之后的行为：
// **一格都不写库**（`Append` 建行 = 行写了、已登记的 `process_started` 静默丢掉，那正是 §1.1 第 13 条禁的形状），
// 但每格都要在日志里留下"等的是 REQ-DB-2 + 没建/没改 + 是哪个 adapter"这三样。
func TestLifecycleMethodsShoutInsteadOfSilentlyPassing(t *testing.T) {
	core, err := boot(context.Background(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()

	var logged []string
	rec := newDropRecorder(core.store, "codex", func() int64 { return 1 },
		func(f string, args ...any) { logged = append(logged, fmt.Sprintf(f, args...)) })
	rec.RecordStart(process.Event{Kind: process.EventStarted, PID: 40, Status: process.StatusStarting})
	rec.RecordExit(process.Event{Kind: process.EventExited, PID: 41, ExitCode: 0})
	rec.RecordCrash(process.Event{Kind: process.EventCrashed, PID: 42, ExitCode: 1, Crashes: 3})

	if len(logged) != 3 {
		t.Fatalf("生命周期三格各该喊一次，实际 %d 条: %v", len(logged), logged)
	}
	for _, line := range logged {
		// 三件都要在：等的是哪条（REQ-DB-2）、没落什么（建行/改行）、是谁的进程（身份不许因为没落库就丢）。
		if !strings.Contains(line, "REQ-DB-2") || !strings.Contains(line, "没") || !strings.Contains(line, "codex") {
			t.Errorf("喊的话要说清等的是哪条、没落哪一格、是谁的进程： %q", line)
		}
	}
	if n := countRowsBy(t, core, `SELECT count(*) FROM process_instances`); n != 0 {
		t.Errorf("T-168：`Append` 建行会丢 `process_started`，这张表今天一格都不许写： %d 行", n)
	}
	if n := countRowsBy(t, core, `SELECT count(*) FROM audit_entries`); n != 0 {
		t.Errorf("生命周期那三格今天无权写任何东西，实际 %d 行审计", n)
	}
	if n := countRowsBy(t, core, `SELECT count(*) FROM event_journal`); n != 0 {
		t.Errorf("同上，事件侧也不许有东西： %d 行", n)
	}
}

// TestNewAuditIDIsUniqueEnoughToInsert 只钉一件事：同一把 key 上连着写两行不许撞 UNIQUE。
// （丢弃没有可复用的上游 id ⇒ 这里现编；真撞上了就是 RecordDrop 静默丢审计。）
func TestNewAuditIDIsUniqueEnoughToInsert(t *testing.T) {
	core, err := boot(context.Background(), testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer core.close()

	rec := newDropRecorder(core.store, "codex", func() int64 { return 1 }, nil)
	for i := 0; i < 3; i++ {
		rec.RecordDrop(process.DroppedEvent{Method: "session/prompt", TurnRef: "t", Reason: "重复丢弃"})
	}
	if n := countRowsBy(t, core, `SELECT count(*) FROM audit_entries WHERE action = 'frame_dropped'`); n != 3 {
		t.Errorf("三次丢弃该落三行，实际 %d 行", n)
	}
	// 三行的 execution_id / task_id 都不许被顺手填成某个具体聚合：丢弃这件事不属于任何 Task。
	var withTask int
	if err := core.db.Read.QueryRow(`SELECT count(*) FROM audit_entries WHERE action = 'frame_dropped' AND task_id IS NOT NULL`).
		Scan(&withTask); err != nil {
		t.Fatal(err)
	}
	if withTask != 0 {
		t.Errorf("有 %d 行被挂到了 Task 上，那是凭空归属", withTask)
	}
}
