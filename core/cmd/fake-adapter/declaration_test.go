package main

// T-423 格一（主控裁「准甲」）：声明与可达之间的**双向**判据，不动任何产品语义。
//
// 这一格要守的现场证据是 h4 那次 SILENT：把 `initializeResult` 里那行
// `debugCapability("debug.hold")` 摘掉之后，替身照样服务 `debug/hold`，全包没有任何一条用
// 例发现声明少了一行（见 adapter.md 第 30 轮）。当时的结论是「`Handshake.Call` 不查能力表
// ⇒ 那行声明不是闸门」，而闸门归不归 Core 查（格一的乙/丙）主控今天没选；他选的是**把谎
// 言钉在判据里**。所以本文件只读、只比、只喊，一条运行时规则都不加。
//
// 两个方向都要，缺一不可：
//   * **声明为有效 ⇒ 这台真接**（A 向）：替身不能声称会做一件它 switch 里没有的方法；
//   * **这台真接 ⇒ 有声明**（B 向）：任何服务着的方法都必须出现在能力声明表里，等级不限
//     （`session/locate` 就是这条的合法形状：接、答 `CAPABILITY_UNAVAILABLE`、声明 UNSUPPORTED）。
//
// 「这台接了哪些方法」是**从源码里读出来的**，不是手抄的：`(*fake).serve` 的 dispatch 就是这个
// 构建唯一的答案来源。手抄一份名单等于造第二个真相，而它漂移的方式正是 h4 那一幕。

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// notACapability is the closed set of frames this build answers that §2/§1 put outside
// the capability table. Each entry has to be a method this build actually serves, and the
// test enforces that - an exception list that quietly outlives its subject is how a
// criterion goes vacuous.
var notACapability = map[string]string{
	adapter.MethodInitialize: "§2: the handshake is the act that produces the declaration, so it cannot be gated on one",
	adapter.MethodHealth:     "§4.1: adapter/health is the probe itself, not something a capability offers",
	adapter.CancelMethod:     "§1: $/cancel is the one frame outside the 域/动作 naming, so no capability names it",
}

// serveDispatch returns the source text of (*fake).serve, the one place that decides which
// method gets which answer.
func serveDispatch(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go from the package dir (%s): %v - this parser is the single source, it may not skip",
			mustGetwd(), err)
	}
	body := strings.ReplaceAll(string(raw), "\r", "")
	start := strings.Index(body, "func (f *fake) serve(")
	if start < 0 {
		t.Fatal(`no "func (f *fake) serve(" in main.go: the dispatch moved, so the parser is stale rather than the build empty`)
	}
	end := dispatchEnd(body[start:])
	if end < 0 {
		t.Fatal("serve() has no closing brace at column 0: the parser's assumption about gofmt'd sources broke")
	}
	return body[start : start+end+3]
}

func mustGetwd() string { wd, _ := os.Getwd(); return wd }

// dispatchEnd finds where serve() closes: the first line that is just "}" after the
// signature. gofmt puts a function's closing brace in column 0, and nested ones are
// indented, so the first hit is the end of the function and not one of its branches.
func dispatchEnd(rest string) int { return strings.Index(rest, "\n}\n") }

var (
	caseLine  = regexp.MustCompile(`(?m)^\tcase ([A-Za-z_][A-Za-z0-9_.]*):`)
	compareEq = regexp.MustCompile(`req\.Method == ([A-Za-z_][A-Za-z0-9_.]*)`)
	// Two shapes have to resolve: a name inside a const block (leading tab) and a standalone
	// `const Name = "value"` at column 0 - adapter/protocol.go:27 is the second kind, and the
	// first version of this regex missed it, which is how a resolver quietly goes short.
	constLine = regexp.MustCompile(`(?m)^\t?(?:const )?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"([^"]+)"$`)
)

// symbolTable maps `Ident` (this package) and `adapter.Ident` to the method string it holds,
// read out of the non-test sources of the two packages that can appear in the dispatch.
func symbolTable(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range []struct{ path, prefix string }{{".", ""}, {"../../internal/adapter", "adapter."}} {
		files, err := filepath.Glob(filepath.Join(dir.path, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("glob %s: %d files (err %v) - the module layout is ours, so this is a broken parser, not a skip",
				dir.path, len(files), err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			for _, m := range constLine.FindAllStringSubmatch(strings.ReplaceAll(string(raw), "\r", ""), -1) {
				out[dir.prefix+m[1]] = m[2]
			}
		}
	}
	return out
}

// servedMethods is the method strings this build answers, in source order, de-duplicated.
func servedMethods(t *testing.T) []string {
	t.Helper()
	syms := symbolTable(t)
	seen := map[string]bool{}
	var out []string
	add := func(ident, where string) {
		name, ok := syms[ident]
		if !ok {
			t.Fatalf("%s names %q, and no const in cmd/fake-adapter or internal/adapter resolves it - "+
				"fix the resolver rather than letting the list go short", where, ident)
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	dispatch := serveDispatch(t)
	for _, m := range caseLine.FindAllStringSubmatch(dispatch, -1) {
		add(m[1], "case")
	}
	for _, m := range compareEq.FindAllStringSubmatch(dispatch, -1) {
		add(m[1], "pre-switch arm")
	}
	// A parser that returns almost nothing must fail here: `TestFakeAdapterServesEveryContract`
	// needs this list as its universe, and an empty universe passes everything.
	if len(out) < 12 {
		t.Fatalf("serve() parsed to %d methods (%v); the dispatch is bigger than that, so fix the parser", len(out), out)
	}
	return out
}

// declaredNames is every capability name this build sends in §2's result, at any support
// level; effectiveNames is the subset Core may actually use (§3's resolveEffective output).
func declaredNames(t *testing.T) []string {
	t.Helper()
	res := initializeResult()
	var out []string
	for _, c := range res.Capabilities {
		out = append(out, c.Name)
	}
	if len(out) < 14 {
		t.Fatalf("initializeResult declares %d capabilities (%v); the floor is 14 - a short list here means the reader is wrong, not that the declaration shrank", len(out), out)
	}
	return out
}

func effectiveNames(t *testing.T) []string {
	t.Helper()
	hs := declaredHandshake()
	var out []string
	for _, c := range hs.EffectiveCapabilities() {
		out = append(out, c.Name)
	}
	// session.locate is declared UNSUPPORTED, so exactly one name is expected to drop out.
	if len(out) < 13 {
		t.Fatalf("%d capabilities are effective (%v), want at least 13", len(out), out)
	}
	return out
}

func TestFakeAdapterDeclarationMatchesDispatch(t *testing.T) {
	served := servedMethods(t)
	declared := declaredNames(t)
	effective := effectiveNames(t)
	// The sizes go on the log on purpose: the floors below catch a parser that collapses, but
	// a parser that quietly loses one entry while staying above the floor is only visible if
	// the numbers themselves are printed every run.
	t.Logf("served=%d declared=%d effective=%d exceptions=%d", len(served), len(declared), len(effective),
		len(notACapability))

	servedSet := map[string]bool{}
	for _, m := range served {
		servedSet[m] = true
	}
	declaredSet := map[string]bool{}
	for _, n := range declared {
		declaredSet[strings.ReplaceAll(n, ".", "/")] = true
	}

	// A: declared-effective must be reachable.
	for _, name := range effective {
		method := strings.ReplaceAll(name, ".", "/")
		if !servedSet[method] {
			t.Errorf("capability %q is declared effective but serve() has no case for %q: the declaration is a claim this build does not honour",
				name, method)
		}
	}
	// B: reachable must be declared. This is the h4 arm.
	for _, method := range served {
		if _, exempt := notACapability[method]; exempt {
			continue
		}
		if !declaredSet[method] {
			t.Errorf("serve() answers %q but initializeResult declares no capability for it (h4's shape): §2's answer would advertise nothing while the method is live",
				method)
		}
	}
	// C: the exception list must stay live and short, or it stops being an exception.
	for method, why := range notACapability {
		if !servedSet[method] {
			t.Errorf("notACapability still lists %q (%s) but this build no longer serves it - delete the entry, don't let it sit as an excuse",
				method, why)
		}
	}
	if len(notACapability) > 3 {
		t.Errorf("notACapability has grown to %d entries; every entry is a method §9's gate does not see", len(notACapability))
	}
}
