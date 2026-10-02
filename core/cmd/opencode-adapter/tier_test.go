package main

// tier_test.go pins what §16.7 makes this tier *be*: a declaration that says read-only,
// a write chain that can only ever be refused, and no notification traffic at all.
//
// Three of these are the criteria T-447 ③ asked for in master's own words:
//   · 写链一律走"能力不可用"出口 - TestRefusedMethodsAnswerWithErrorFramesNotResults;
//   · 返回 200/202 或"已受理"算失败 - same test, and the r1 mutation in this round's
//     batch turns the refusal into a result frame to prove the criterion bites;
//   · §16.7 第 4 点: this tier must not be usable as投递取证 evidence, which here means
//     it must not put a turn/claimed, turn/dispatched or turn/finished on the wire even
//     in reply to a full read - TestReadOnlySendsNoNotifications.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// refusedNamesOnTheWire is §16.7 第 1 条's list, read here as the test's own input so a
// later edit to the ruling shows up as a table that no longer matches rather than a
// green test. The contract text is parsed rather than copied.
func refusedNamesFromContract(t *testing.T) []string {
	t.Helper()
	root := repoRootFromTest(t)
	raw, err := os.ReadFile(filepath.Join(root, "specs", "adapter-protocol", "adapter-protocol-v0.1.md"))
	if err != nil {
		t.Skipf("contract ② not reachable from this tree (%v): the tier list is not asserted, not passed", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.Contains(line, "不实现也不接") {
			continue
		}
		var names []string
		for _, seg := range strings.Split(line, "`") {
			s := strings.TrimSpace(seg)
			if s != "" && !strings.Contains(s, " ") && (strings.Contains(s, "/") || s == "guide" || s == "start_now") {
				names = append(names, s)
			}
		}
		if len(names) < 5 {
			t.Fatalf("§16.7 第 1 条 parsed to %d method names (%v): that row changed shape, and a criterion "+
				"reading nothing would pass forever", len(names), names)
		}
		return names
	}
	t.Fatal("§16.7's 「它不实现也不接」 row is gone: the range moved, so this criterion has no input")
	return nil
}

func repoRootFromTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "specs", "adapter-protocol")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("specs/adapter-protocol is not reachable from the test's working directory")
	return ""
}

// TestDeclarationIsTheReadOnlyTier reads the declaration back through the validator Core
// uses. If Core would reject the table, nothing downstream can compensate for it, so a
// green here means "this adapter is acceptable to the real handshake" rather than "our
// own struct literals look right".
func TestDeclarationIsTheReadOnlyTier(t *testing.T) {
	res := initializeResult("")
	if res.WriteEntry {
		t.Error("write_entry is true: §16.7 第 1 条's tier is exactly the opposite, and §3's gate " +
			"would then let a write method through instead of refusing it")
	}
	hs, err := adapter.NewHandshake(adapter.Version{Major: adapter.ProtocolMajor, Minor: adapter.ProtocolMinor}, res)
	if err != nil {
		t.Fatalf("Core would reject our own capability table: %v", err)
	}
	if res.AdapterKey != adapterKey {
		t.Errorf("adapter_key = %q, want %q (adapter_instances is keyed on it)", res.AdapterKey, adapterKey)
	}
	if len(res.SoftwareVersions) < 2 {
		t.Errorf("software_versions has %d entries; §2.1 requires a multi-value map, not one string", len(res.SoftwareVersions))
	}
	var declared []string
	for _, c := range res.Capabilities {
		declared = append(declared, c.Name)
		switch c.SupportLevel {
		case adapter.LevelReadOnly, adapter.LevelUnsupported:
		default:
			t.Errorf("capability %s declares %s: this tier may declare only READ_ONLY reads and UNSUPPORTED stubs",
				c.Name, c.SupportLevel)
		}
		if c.WriteCapable {
			t.Errorf("capability %s declares write_capable=true while §16.7 第 1 条 closes the write chain", c.Name)
		}
		if c.SupportLevel == adapter.LevelReadOnly {
			if _, ok := hs.Effective(c.Name); !ok {
				t.Errorf("capability %s is declared but not effective under Core's own rules", c.Name)
			}
			if c.EffectiveBackend != adapter.BackendLocalData {
				t.Errorf("%s effective_backend = %q, want local_data: §3 keeps that column to answer 「用什么方式」, "+
					"and the mechanism really is reading this product's own database", c.Name, c.EffectiveBackend)
			}
			if c.Confidence == adapter.ConfidenceHigh {
				t.Errorf("%s claims HIGH confidence while the message/session column list is still missing (CO-64)", c.Name)
			}
		}
	}
	// The four §16.7 names, and nothing from the refused list, may be in the offer.
	for _, want := range []string{"session.list", "session.read", "model.list", "quota.read"} {
		if !containsName(declared, want) {
			t.Errorf("capability %s is missing: §16.7 第 1 条 lists it as this tier's whole surface", want)
		}
	}
	for _, name := range refusedNamesFromContract(t) {
		capName := strings.ReplaceAll(name, "/", ".")
		if containsName(declared, capName) {
			t.Errorf("%s is declared as a capability even though §16.7 第 1 条 says 不实现也不接: declaring it "+
				"is what puts this provider back on the dispatch path", name)
		}
	}
}

func containsName(list []string, want string) bool {
	for _, n := range list {
		if n == want {
			return true
		}
	}
	return false
}

// TestRefusedMethodsAnswerWithErrorFramesNotResults is T-447 ③ on the wire.
//
// The two halves are separate on purpose. "Not accepted" alone would pass if the adapter
// answered with a result frame whose contents happened to look like a refusal, and JSON-RPC
// has no notion of a 202: the presence of `result` *is* acceptance (§15.3). So each refused
// method is checked for (a) an error frame, (b) a registered §15.1 name, and (c) no result
// member at all.
func TestRefusedMethodsAnswerWithErrorFramesNotResults(t *testing.T) {
	db := newFixture(t, defaultFixture())
	models := writeCatalogue(t, 1)
	p := startAdapter(t, testConfig(db, models))
	initialize(t, p)

	// Only the §4-shaped names can arrive as a method: §1's reader rejects a method name
	// that is not 域/动作, and start_now / guide are §5 delivery modes rather than
	// methods at all. They get their own test below rather than being smuggled in here.
	refused := []string{"session/create", "session/send", "session/stop",
		"approval/request", "approval/respond"}
	for i, method := range refused {
		// The id is a plain counter, not the method name: an id carrying "/" would make
		// this look like a protocol defect when it is only a test artifact, and §1's
		// reader is right to refuse it.
		reply := p.call(t, fmt.Sprintf("r%d", i), method, map[string]any{"remote_session_id": "ses-000"})
		if reply.Error == nil {
			t.Errorf("%s answered without an error: %s - §16.7 第 2 条 counts 已受理 as a failure, and in this "+
				"protocol a result frame *is* acceptance", method, string(reply.Result))
			continue
		}
		if len(reply.Result) != 0 {
			t.Errorf("%s answered an error AND a result member (%s): a frame cannot both refuse and accept",
				method, reply.Result)
		}
		sym := reply.Error.Symbol()
		if sym != "WRITE_ENTRY_NONE" {
			t.Errorf("%s refused with %s, want WRITE_ENTRY_NONE: §15.1 assigns that name to 「整个 Adapter 声明无写入口」, "+
				"which is what this tier is", method, sym)
		}
		if !strings.Contains(reply.Error.Message, method) {
			t.Errorf("%s's refusal does not name the method it refused: %q", method, reply.Error.Message)
		}
	}
	// The offer still stands afterwards: a refusal must not consume the handshake, or the
	// panel would see a dead adapter after one misclick.
	if h := p.call(t, "h", adapter.MethodHealth, map[string]any{}); h.Error != nil {
		t.Errorf("adapter/health after the refusals: %s", h.Error.Message)
	}
	p.close(t)
}

// TestBareModeNamesCannotBeAcceptedEither covers §16.7's other two names. `start_now`
// and `guide` are §5 delivery modes of session/send, so the only way one arrives as a
// bare method is a caller that broke §1's frame shape. Two claims, one per shape:
//
//   - as a bare method it is refused by §1's reader and the session ends, and it never
//     produces a result frame - that is §16.7 第 2 条's "不得返回已受理" on the illegal
//     path, which is the path a careless implementation would answer with silence;
//   - inside session/send (the legal shape) §3's write_entry gate answers before the
//     mode is consulted, so no delivery mode has to be declared here at all.
//
// Each case starts its own adapter: the first case ends the stream by design, and a
// second write on a dead pipe would block forever rather than prove anything.
func TestBareModeNamesCannotBeAcceptedEither(t *testing.T) {
	db := newFixture(t, defaultFixture())
	models := writeCatalogue(t, 1)

	for _, mode := range []string{"start_now", "guide"} {
		mode := mode
		t.Run(mode+" as a bare method", func(t *testing.T) {
			p := startAdapter(t, testConfig(db, models))
			initialize(t, p)
			frame := fmt.Sprintf(`{"jsonrpc":"2.0","id":"m","method":"%s","params":{}}`+"\n", mode)
			if _, err := io.WriteString(p.in, frame); err != nil {
				t.Fatalf("write %s: %v", mode, err)
			}
			var saw []*adapter.Frame
			for {
				f, ok := p.nextFrame(500 * time.Millisecond)
				if !ok {
					break
				}
				saw = append(saw, f)
			}
			err := <-p.done
			p.drained = true
			for _, f := range saw {
				if len(f.Result) != 0 {
					t.Errorf("%s was answered with a result frame: %s", mode, f.Result)
				}
			}
			if err == nil {
				t.Errorf("run() ended cleanly on a method name that is not 域/动作: §1 makes that a " +
					"session-ending defect, and a clean exit would let it pass as behaviour")
			} else {
				t.Logf("run() ended as §1 requires: %v", err)
			}
		})
	}

	t.Run("session/send refuses before any mode is read", func(t *testing.T) {
		p := startAdapter(t, testConfig(db, models))
		initialize(t, p)
		for _, mode := range []string{"start_now", "guide", "queue"} {
			reply := p.call(t, "send", methodSessionSend, map[string]any{
				"remote_session_id": "ses-000", "text": "hi", "delivery": mode})
			if reply.Error == nil {
				t.Fatalf("delivery %s was accepted (%s): §3 refuses the write, not the mode", mode, reply.Result)
			}
			if reply.Error.Symbol() != "WRITE_ENTRY_NONE" {
				t.Errorf("delivery %s refused with %s, want WRITE_ENTRY_NONE", mode, reply.Error.Symbol())
			}
		}
		p.close(t)
	})
}

// TestReadOnlySendsNoNotifications is ①'s other half, and the reason it exists is the
// ruling that replaced the synthesized A->C stream: a frame this adapter invents would
// have no Core-issued turn_ref to echo (§4.2 裁定 ①), so the pump would drop every one of
// them (§6 规则 5). Silence is therefore not a gap in the implementation but its
// correctness condition - and it is asserted as a measured silence, after the fullest
// read this tier can perform.
func TestReadOnlySendsNoNotifications(t *testing.T) {
	db := newFixture(t, defaultFixture())
	models := writeCatalogue(t, 2)
	p := startAdapter(t, testConfig(db, models))
	initialize(t, p)

	for _, call := range []struct {
		id, method string
		params     any
	}{
		{"l", methodSessionList, map[string]any{"limit": 5}},
		{"r", methodSessionRead, map[string]any{"remote_session_id": "ses-000"}},
		{"q", methodQuotaRead, map[string]any{}},
		{"m", methodModelList, map[string]any{}},
		{"a", methodApprovalList, map[string]any{}},
	} {
		reply := p.call(t, call.id, call.method, call.params)
		if reply.Error != nil {
			t.Fatalf("%s: %s", call.method, reply.Error.Message)
		}
	}
	// A second read with a cursor: the continuation is where an adapter that "helpfully"
	// streamed frames would show them.
	if reply := p.call(t, "r2", methodSessionRead,
		map[string]any{"remote_session_id": "ses-000", "since_cursor": "e1520"}); reply.Error != nil {
		t.Fatalf("session/read continuation: %s", reply.Error.Message)
	}

	// Two measurements, because one alone can be satisfied the wrong way: the bounded wait
	// says nothing was still coming, and the stray tally says nothing was ever filed that
	// answered no request. Without the second half, a notification landing between two
	// calls could be swallowed as somebody's reply - which is exactly what the r7 mutation
	// in this round's batch does, and it is what makes this criterion have teeth.
	if f, arrived := p.quiet(300 * time.Millisecond); arrived {
		if f.ID == "" {
			t.Errorf("a notification reached the wire (%s): this tier has no Core-issued ticket to echo, "+
				"so §4.2 裁定 ① makes any such frame a dropped frame - §16.7's read tier sends none", f.Method)
		} else {
			t.Errorf("a second reply arrived for a request already answered: id=%q method=%q", f.ID, f.Method)
		}
	}
	if strays := p.strayFrames(); len(strays) != 0 {
		var names []string
		for _, f := range strays {
			names = append(names, f.Method)
		}
		t.Errorf("%d frame(s) on the stream answered no request of ours (%s): §16.7 第 4 点 makes this tier's "+
			"claimed/dispatched/finished frames our own invention, so it must send none",
			len(strays), strings.Join(names, " "))
	}
	p.close(t)
}

// TestNoWriteChainIsConstructedInProductCode is the local twin of T-450 ①: this package
// may name the refused methods only as registered data. A test that greps its own
// package is not redundant with the cross-package guard, because the guard's scanner
// excludes _test.go files and would not notice if a future helper started building a
// write request.
func TestNoWriteChainIsConstructedInProductCode(t *testing.T) {
	refused := refusedNamesFromContract(t)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	fset := token.NewFileSet()
	var constructed, registered []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, imp := range f.Imports {
			name := strings.Trim(imp.Path.Value, `"`)
			if name == "net/http" || name == "os/exec" {
				t.Errorf("%s imports %s: §16.7's range is 「除此之外一条命令都不发」, and those two are how a "+
					"request would ever leave this process", e.Name(), name)
			}
		}
		// Two passes, the same way T-450's scanner does it: first the positions that are
		// call arguments, then every literal. A name that only ever appears as a map key
		// or a comparison is "registered"; in a call argument it is "constructed", and
		// that is the half §16.7 closes.
		argPos := map[string]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			for _, a := range call.Args {
				if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					argPos[fset.Position(lit.Pos()).String()] = true
				}
			}
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v := strings.Trim(lit.Value, "`\"")
			// The credential red line, and it is on **literals** rather than on file text: a
			// comment may say "auth.json is not an input" freely, a string that could be
			// joined into a path may not. T-447 forbids even the two providers' key values
			// reaching a file, a log or a report.
			for _, banned := range []string{"auth.json", "connect-token", "OPENCODE_KEY"} {
				if strings.Contains(v, banned) {
					t.Errorf("%s puts %q into a string this process could act on: the credential file is not "+
						"an input of the read tier (§16.7 第 1 条 + T-447 的红线)", e.Name(), banned)
				}
			}
			if !containsName(refused, v) {
				return true
			}
			where := e.Name() + ":" + strconv.Itoa(fset.Position(lit.Pos()).Line)
			if argPos[fset.Position(lit.Pos()).String()] {
				constructed = append(constructed, v+" @"+where)
			} else {
				registered = append(registered, v+" @"+where)
			}
			return true
		})
	}
	if len(constructed) > 0 {
		t.Errorf("写链方法名出现在调用实参位置（%d 处）：%s ⇒ 这是 §16.7 第 1 条的自扩范围，也是 T-450 ① 会判红的形状",
			len(constructed), strings.Join(constructed, "、"))
	}
	t.Logf("refused names as registered data only: %d 处（%s）", len(registered), strings.Join(registered, "、"))
	// 判据按名字点名，不按个数：个数相等也能漏掉一整格（`turn/start` 就这样被我从
	// session/send 的名字顶掉过一轮）。
	for _, name := range refused {
		if !refusedMethods[name] {
			t.Errorf("%s 是 §16.7 第 1 条点名的方法，但本包的拒绝表里没有它：真收到这个名字时会掉进 "+
				"METHOD_NOT_FOUND，而不是要求的「能力不可用」出口", name)
		}
	}
}
