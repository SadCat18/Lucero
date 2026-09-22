package adapter

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The write set is decided in contract §4.1 and enforced here, so the two must be
// checked against each other rather than against a copy of the list pasted into a test:
// a pasted set stays green forever after the contract changes, which is the failure
// mode T-59 ③ and T-83 ③ are about.

var (
	// A §4 method-table row: | `session/send` | C->A | ...
	contractMethodRow = regexp.MustCompile("(?m)^\\|\\s*`([a-z]+/[a-z*]+)`\\s*\\|")
	// A method name inside §4.1's prose: `session/send`, `debug/*`. Underscores and
	// "=" are excluded on purpose so prose tokens like `adapter_key=fake` never pass
	// as a method name.
	contractMethodName = regexp.MustCompile("`([a-z]+/[a-z*]+)`")
)

// contractFile finds contract ② from the package directory. The repo root is walked
// rather than hard-coded so the test still passes from a checkout at another path.
func contractFile(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		p := filepath.Join(dir, "specs", "adapter-protocol", "adapter-protocol-v0.1.md")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Skip("contract ② not reachable from this working tree; the cross-check is skipped, not passed")
	return ""
}

// readContract strips CR because this repo's specs are CRLF and a trailing \r would
// silently break the row anchors - the first version of this test failed that way.
func readContract(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ReplaceAll(string(raw), "\r", "")
}

// section returns the text from the given heading or marker up to the next one, so a
// parse that silently runs past the end can't invent names out of the next section.
func section(body, start string, stops ...string) string {
	i := strings.Index(body, start)
	if i < 0 {
		return ""
	}
	rest := body[i+len(start):]
	end := len(rest)
	for _, s := range stops {
		if j := strings.Index(rest, s); j >= 0 && j < end {
			end = j
		}
	}
	return rest[:end]
}

// TestWriteMethodsMatchContractSection is the interlock: every name this package gates
// as a write must be a method §4 defines and must appear in §4.1's declared set, and
// every name §4.1 declares must be gated here. Either side drifting alone goes red.
func TestWriteMethodsMatchContractSection(t *testing.T) {
	path := contractFile(t)
	if path == "" {
		return
	}
	body := readContract(t, path)

	// §4's method table only, as a set of defined names. Scoping matters: the first version
	// of this test scanned the whole document, so the names §4.1 lists as *not* methods
	// (`approval/cancel`, `session/create`) were counted as defined by its own gap
	// table and a phantom in the write set passed. The anchor stops at §4.1 for that reason.
	defined := map[string]bool{}
	for _, m := range contractMethodRow.FindAllStringSubmatch(
		section(body, "## 4. 方法表", "### 4.1"), -1) {
		defined[m[1]] = true
	}
	if len(defined) < 10 {
		t.Fatalf("§4 method table parsed to %d rows; the anchor is broken, so this test cannot judge anything",
			len(defined))
	}

	writes := map[string]bool{}
	for _, m := range contractMethodName.FindAllStringSubmatch(
		section(body, "**本文口径下的写方法集合**", "**读方法", "\n\n**", "## 5."), -1) {
		writes[m[1]] = true
	}
	// A parse that comes back empty means the prose moved, not that the contract has
	// no writes: failing loudly is the only safe outcome (an empty set passes below).
	if len(writes) < 4 {
		t.Fatalf("§4.1's write set parsed to %d names (%v); fix the anchor rather than trusting the pass",
			len(writes), keys(writes))
	}

	for name := range writeMethods {
		if !defined[name] {
			t.Errorf("writeMethods gates %q, which §4's method table does not define (a phantom name)", name)
		}
	}
	for name := range writes {
		if name == "debug/*" {
			// The family form is gated by isDebugName, not by a map entry.
			if !isDebugName("debug/crash") {
				t.Error("§4.1 lists debug/* as a write family, isDebugName does not match it")
			}
			continue
		}
		if !writeMethods[name] {
			t.Errorf("§4.1 declares %q a write, writeMethods does not gate it", name)
		}
	}
	// §4.1's write-set sentence is prose this package cannot derive session/create from
	// (the row is in §4's table but missing from that list - CO-34 续), so the gate is
	// asserted directly here: dropping it would silently let a session be created against
	// a write_entry:false Adapter, which is the one outcome §3 exists to prevent.
	if !writeMethods["session/create"] {
		t.Error("session/create is a C->A write per §4 and §3's gate no longer covers it")
	}
}

// methodConst matches this package's own notification constants, e.g.
// `MethodTurnFinished = "turn/finished"`.
var methodConst = regexp.MustCompile(`(?m)^	Method\w+\s+=\s+"([a-z]+/[a-z]+)"`)

// notificationRow matches a contract table row whose first cell is a notification name.
var notificationRow = regexp.MustCompile("(?m)^\\|\\s*`(stream/[a-z]+|turn/[a-z]+)`")

// TestStreamNotificationsMatchContractTables is the same interlock as the §4 one, on the
// other side of the boundary: §6's four output rows plus §4.2's two evidence rows against
// this package's six constants, both ways. It exists because a seventh notification can
// only arrive with a ticket story attached - CO-40 withdrew session/push precisely
// because an unsolicited A->C notice has no Core-issued turn_ref to echo - so code that
// grows a notification before the contract does is not a small drift, it is a reopened
// ruling. The reverse direction catches the contract growing a row nobody implemented.
func TestStreamNotificationsMatchContractTables(t *testing.T) {
	body := readContract(t, contractFile(t))
	s6 := section(body, "## 6. 流式契约", "\n## 7.")
	s42 := section(body, "### 4.2 两帧取证信号", "\n## 5.")
	if s6 == "" || s42 == "" {
		t.Fatalf("anchors moved (§6 empty=%v, §4.2 empty=%v); fix the anchor rather than trusting the pass",
			s6 == "", s42 == "")
	}
	fromContract := map[string]bool{}
	for _, m := range notificationRow.FindAllStringSubmatch(s6, -1) {
		fromContract[m[1]] = true
	}
	if len(fromContract) < 4 {
		t.Fatalf("§6's table parsed to %d notification rows (%v); that is a parser failure, not a short contract",
			len(fromContract), keys(fromContract))
	}
	before := len(fromContract)
	for _, m := range notificationRow.FindAllStringSubmatch(s42, -1) {
		fromContract[m[1]] = true
	}
	if len(fromContract) <= before {
		t.Fatalf("§4.2 contributed no rows after %d from §6: its table moved, do not judge green", before)
	}

	// "My side" is read out of stream.go rather than copied here: a hand-written list of
	// the constants is exactly the second source of truth this test exists to catch, and
	// a seventh Method would have been added to the file and not to the list.
	src, err := os.ReadFile("stream.go")
	if err != nil {
		t.Fatalf("read stream.go: %v", err)
	}
	mine := map[string]bool{}
	for _, m := range methodConst.FindAllStringSubmatch(string(src), -1) {
		mine[m[1]] = true
	}
	if len(mine) < 6 {
		t.Fatalf("stream.go's Method* block parsed to %d names (%v); that is a parser failure",
			len(mine), keys(mine))
	}

	for name := range fromContract {
		if !mine[name] {
			t.Errorf("the contract defines notification %q but this package has no constant for it: "+
				"decoding it would fail and inventing a ticket story for it is CO-40's dead knot", name)
		}
		if !CarriesTurnRef(name) {
			t.Errorf("%s is a contract notification yet CarriesTurnRef says it owes no turn_ref (§4.2 裁定 ①: 六条全部带)", name)
		}
	}
	for name := range mine {
		if !fromContract[name] {
			t.Errorf("this package emits %q, which no contract table names: that is an invented notification", name)
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
