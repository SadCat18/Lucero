package test

import (
	"context"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/audit"
)

// 全仓不变量（T-14，17:20 转给 test）：**任何参与 audit_entries.entry_hash 计算的列，
// 其改写必须发生在算 hash 之前**；否则哈希链会自己烂掉而表面仍"完整"。
// db 的 RedactionHits 与 audit 的哈希链正是在 details_json 这一列上交汇。
//
// signature 不参与哈希（它签的是链，不是链上的一节），所以故意不在下面的篡改表里。

func entryAt(n int, details string) audit.Entry {
	return audit.Entry{
		AuditID:      "audit-" + string(rune('a'+n)),
		PrincipalID:  "local-user",
		Action:       "approval_resolve",
		ResourceType: "approval",
		ResourceID:   "approval-" + string(rune('a'+n)),
		Outcome:      audit.OutcomeSuccess,
		DetailsJSON:  details,
		CreatedAt:    int64(1757000000000 + n),
	}
}

// chain 造一条 N 行的合法链，返回条目与落库时应记的 head。
func chain(t *testing.T, details []string) ([]audit.Entry, audit.Head) {
	t.Helper()
	entries := make([]audit.Entry, 0, len(details))
	prev := "" // 创世前驱 = 空串（Chain.Head 在空表时就是这么返回的）
	for i, d := range details {
		e := entryAt(i, d)
		e.Seq = int64(i + 1)
		e = audit.Seal(e, prev)
		if e.EntryHash == "" {
			t.Fatal("Seal 没算出 entry_hash")
		}
		entries = append(entries, e)
		prev = e.EntryHash
	}
	head := audit.Head{Seq: int64(len(entries)), Hash: prev}
	if err := audit.VerifyChain(entries, head); err != nil {
		t.Fatalf("没被篡改的链竟然校验不过: %v", err)
	}
	return entries, head
}

// hashedColumns 逐列篡改即证明"这一列进了 hash"。少一列就是漏网，本用例会点名报出来。
var hashedColumns = map[string]func(*audit.Entry){
	"seq":            func(e *audit.Entry) { e.Seq += 7 },
	"audit_id":       func(e *audit.Entry) { e.AuditID = "tampered" },
	"principal_id":   func(e *audit.Entry) { e.PrincipalID = "tampered" },
	"action":         func(e *audit.Entry) { e.Action = "tampered" },
	"resource_type":  func(e *audit.Entry) { e.ResourceType = "tampered" },
	"resource_id":    func(e *audit.Entry) { e.ResourceID = "tampered" },
	"project_id":     func(e *audit.Entry) { e.ProjectID = "tampered" },
	"task_id":        func(e *audit.Entry) { e.TaskID = "tampered" },
	"execution_id":   func(e *audit.Entry) { e.ExecutionID = "tampered" },
	"correlation_id": func(e *audit.Entry) { e.CorrelationID = "tampered" },
	"outcome":        func(e *audit.Entry) { e.Outcome = audit.OutcomeDenied },
	"details_json":   func(e *audit.Entry) { e.DetailsJSON = `{"schema":1,"tampered":true}` },
	"created_at":     func(e *audit.Entry) { e.CreatedAt += 1 },
	"previous_hash":  func(e *audit.Entry) { e.PreviousHash = strings.Repeat("f", 64) },
}

// TestTamperingStoredEntryBreaksChain T-14②：篡改已存条目的任一被哈希列，VerifyChain 必须失败。
func TestTamperingStoredEntryBreaksChain(t *testing.T) {
	entries, head := chain(t, []string{`{"schema":1,"a":1}`, `{"schema":1,"b":2}`, `{"schema":1,"c":3}`})
	for col, tamper := range hashedColumns {
		cp := make([]audit.Entry, len(entries))
		copy(cp, entries)
		tamper(&cp[1])
		if err := audit.VerifyChain(cp, head); err == nil {
			t.Errorf("篡改 %s 之后 VerifyChain 仍然通过（这一列没进 hash，或校验漏了它）", col)
		}
	}
}

// TestDetailsWrittenAfterSealIsCaught T-14①：把"先算 hash 后改 details_json"的错误顺序显式造出来，
// 断言它抓得住。这一条同时是另外两条的变异验证：它若不红，说明整个判据是空的。
func TestDetailsWrittenAfterSealIsCaught(t *testing.T) {
	e := entryAt(1, `{"schema":1,"count":1}`)
	e.Seq = 1
	e = audit.Seal(e, "")
	before := e.EntryHash
	e.DetailsJSON = `{"schema":1,"count":999}` // 错误顺序：改在算完之后
	if e.EntryHash != before {
		t.Fatal("EntryHash 竟然跟着 DetailsJSON 变了——它是惰性算的话，本文件的判据要整个重写")
	}
	if err := audit.VerifyChain([]audit.Entry{e}, audit.Head{Seq: 1, Hash: e.EntryHash}); err == nil {
		t.Error("hash 之后改 details_json 竟然校验通过——链会表面完整地烂掉")
	}
	fixed := entryAt(1, `{"schema":1,"count":999}`)
	fixed.Seq = 1
	fixed = audit.Seal(fixed, "")
	if err := audit.VerifyChain([]audit.Entry{fixed}, audit.Head{Seq: 1, Hash: fixed.EntryHash}); err != nil {
		t.Errorf("先写后算的正确顺序被拒了: %v", err)
	}
}

// captureStore 是 fake Appender：把写进 audit_entries 的那一行按**列名**还原，
// 不依赖列顺序（audit 内部重排 auditColumns 也不会让本用例误报）。
type captureStore struct {
	rows []audit.Entry
	seq  int64
	errs []string
}

func (c *captureStore) Append(table string, columns []string, values []any) (int64, error) {
	if table != "audit_entries" {
		c.errs = append(c.errs, "audit 只应该写 audit_entries，实得 "+table)
	}
	col := map[string]any{}
	for i, name := range columns {
		if i < len(values) {
			col[name] = values[i]
		}
	}
	c.seq++
	e := audit.Entry{
		Seq:           c.seq, // 库里是 AUTOINCREMENT，行值里没有，按 audit 自己的预测重建
		AuditID:       asString(col["audit_id"]),
		PrincipalID:   asString(col["principal_id"]),
		Action:        asString(col["action"]),
		ResourceType:  asString(col["resource_type"]),
		ResourceID:    asString(col["resource_id"]),
		ProjectID:     asString(col["project_id"]),
		TaskID:        asString(col["task_id"]),
		ExecutionID:   asString(col["execution_id"]),
		CorrelationID: asString(col["correlation_id"]),
		Outcome:       asString(col["outcome"]),
		DetailsJSON:   asString(col["details_json"]),
		PreviousHash:  asString(col["previous_hash"]),
		EntryHash:     asString(col["entry_hash"]),
		CreatedAt:     asInt64(col["created_at"]),
	}
	// T-14③ 的判据就这一行：落库行的 entry_hash 必须等于"拿落库的 details_json 重算"的结果。
	// RecordRedaction 哪天把脱敏计数写在 Seal 之后，这里立刻炸。
	if got := audit.ComputeEntryHash(e.PreviousHash, e); got != e.EntryHash {
		c.errs = append(c.errs, "落库行的 entry_hash 与它的 details_json 不自洽: 存的是 "+
			e.EntryHash+" 重算是 "+got+" details="+e.DetailsJSON)
	}
	c.rows = append(c.rows, e)
	return c.seq, nil
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

// TestRedactionCountersAreSealedBeforeHash T-14③：db 的 RedactionHits 灌进 details_json 这条路径上，
// 脱敏计数必须在算 hash 之前落定。
func TestRedactionCountersAreSealedBeforeHash(t *testing.T) {
	store := &captureStore{}
	c := &audit.Chain{
		Head:   func(context.Context) (int64, string, error) { return 0, "", nil },
		Insert: store,
		Now:    func() int64 { return 1757000000000 },
	}
	ctx := context.Background()
	base := entryAt(1, `{"schema":1,"decision":"approved"}`)
	base.AuditID = "audit-redaction-1"
	if _, err := c.RecordRedaction(ctx, base, []audit.RedactionHit{
		{Column: "payload_json", Kind: audit.HitKey, Match: "token", Count: 2},
		{Column: "details_json", Kind: audit.HitPattern, Match: "jwt", Count: 1},
	}); err != nil {
		t.Fatalf("RecordRedaction: %v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("落了 %d 行，期望 1", len(store.rows))
	}
	for _, e := range store.errs {
		t.Error(e)
	}
	// 自破坏探针（T-29：变异必须打在待验层的直接产物上）：把 audit 刚交出来的那一行拿回来，
	// 在 hash 之后改它的 details_json，再让同一条判据去查——必须炸。
	// 这一步证明上面那条 hash 自洽断言不是空转；它读的是 audit 交给 Appender 的那一行本身，
	// 不借下游任何消费者的判断。
	bad := store.rows[0]
	bad.DetailsJSON = `{"schema":1,"redactions":[{"column":"payload_json","kind":"key","match":"token","count":99}]}`
	if audit.ComputeEntryHash(bad.PreviousHash, bad) == bad.EntryHash {
		t.Error("改了 details_json 之后 hash 仍然相等——本文件的整条判据都是虚的")
	}
	if err := audit.VerifyChain([]audit.Entry{bad}, audit.Head{Seq: 1, Hash: bad.EntryHash}); err == nil {
		t.Error("拿被改过的行去 VerifyChain 竟然通过，链校验没在管 details_json")
	}
	got := store.rows[0]
	if !strings.Contains(got.DetailsJSON, "token") || !strings.Contains(got.DetailsJSON, "jwt") {
		t.Errorf("脱敏计数没进 details_json: %s", got.DetailsJSON)
	}
	// §2.2：审计里只允许出现计数与规则名，不得带原值。
	for _, leak := range []string{"Bearer ", "ghp_", "sk-", " eyJ"} {
		if strings.Contains(got.DetailsJSON, leak) {
			t.Errorf("details_json 里出现了疑似敏感原值片段 %q: %s", leak, got.DetailsJSON)
		}
	}
	// ③ 的接缝保真度：db 侧只有一个 Match 字段（{column, match, count}），audit 侧是
	// Key / Pattern 两槽，靠 internal/approval/store_sqlite.go 里 "key:" 前缀约定来分流。
	// 该约定没写进任何契约（见 progress 的 REQ-T3）——这里把它钉住：键命中必须落到 Key，
	// 且经 CanonicalKey 归一；否则同一条命中会静默变成"某条正则命中"，而这串字是进 hash 的。
	keyHit := audit.RedactionHit{Column: "payload_json", Kind: audit.HitKey,
		Match: audit.CanonicalKey("Access_Token"), Count: 3}
	patHit := audit.RedactionHit{Column: "payload_json", Kind: audit.HitPattern, Match: "ghp_", Count: 1}
	if keyHit.Kind != audit.HitKey || patHit.Kind != audit.HitPattern {
		t.Error("命中类型标签不是 key/pattern 两值，REQ-T3 的统一形状没落地")
	}
	joined, err := audit.RedactionDetails([]audit.RedactionHit{keyHit, patHit})
	if err != nil {
		t.Fatalf("RedactionDetails: %v", err)
	}
	// CanonicalKey 会去下划线并小写（Access_Token -> accesstoken），断言按归一后的形状。
	for _, want := range []string{audit.CanonicalKey("Access_Token"), "ghp_", "3", "1",
		string(audit.HitKey), string(audit.HitPattern)} {
		if !strings.Contains(joined, want) {
			t.Errorf("脱敏审计明细丢了 %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "Access_Token") {
		t.Errorf("键名没经 CanonicalKey 归一（审计里混着两种写法就没法按名字聚合命中）: %s", joined)
	}
	if strings.Contains(joined, "[REDACTED]") {
		t.Errorf("审计明细里出现了占位符本身（只该有计数与规则名）: %s", joined)
	}

	// 空 hits 不该为"零命中"造一条链上记录。
	if n, err := c.RecordRedaction(ctx, base, nil); err != nil || n != 0 || len(store.rows) != 1 {
		t.Errorf("空 hits 也写了行: n=%d err=%v rows=%d", n, err, len(store.rows))
	}
	if err := audit.VerifyChain(store.rows, audit.Head{Seq: store.seq, Hash: store.rows[0].EntryHash}); err != nil {
		t.Errorf("脱敏记录自身的链校验不过: %v", err)
	}
}

// TestRedactionKindIsMandatory REQ-T3 的剩余一半（§2.2:175-184 已裁定 Kind 必填、废除 key: 前缀约定）。
// 三条实测：
//
//	① audit 侧形状已换（{Column, Kind, Match, Count}，redaction.go:127），
//	   但 `RedactionDetails` 不校验空 Kind —— 忘了填 Kind 的调用会把无标签的命中
//	   写进 details_json，而这一列参与 entry_hash，事后改不动（正是 REQ-T3 要堵的那个洞）；
//	② db 侧仍是单槽 {Column, Match, Count}（platform/db/redact.go:87），没有 Kind；
//	③ 跨包 mapper 仍在用被废除的前缀约定（approval/store_sqlite.go:445 的 strings.CutPrefix(h.Match, "key:")）。
//
// db + audit 同批改完后，本用例改成硬断言：Kind 为空的 hits 必须让 RedactionDetails 报错。
func TestRedactionKindIsMandatory(t *testing.T) {
	// 11:5x 复核 T-27：② 已落（db.RedactionHit 带 Kind HitKind，redact.go:87/97-101）、
	// ③ 已落（approval 的 mapper 改成 audit.HitKind(h.Kind) 直传，不再 CutPrefix "key:"）。
	// 只剩 ①：RedactionDetails 仍然不校验空 Kind（redaction.go:127 直接 Marshal）——
	// 忘了填 Kind 的调用会把无标签命中写进 details_json，而这一列参与 entry_hash，事后改不动。
	// 断言口径（T-29：打在待验层的直接产物上，即 RedactionDetails 自己的返回值）：
	//   if _, err := audit.RedactionDetails([]audit.RedactionHit{{Column: "payload_json", Match: "token"}}); err == nil { 红 }
	t.Skip("blocked(REQ-T3c 给 audit): RedactionDetails 不校验空 Kind；②③ 已随 T-27 落地并复核")
}
