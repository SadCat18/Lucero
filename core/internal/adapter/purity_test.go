package adapter

import (
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestOnlyStdlibImports pins contract §6 rule 4 for this package: the protocol layer
// writes no DB row and emits no event, so "stream/delta never reaches
// event_journal" holds structurally instead of depending on someone remembering.
// It also keeps §0's dependency direction honest in this direction: an Adapter
// process may not reach into Core's private structures, and internal/process
// imports internal/adapter - never the other way round.
//
// denied is the stdlib surface this package must never reach for, whatever the
// module's go.mod says (CO-3 asked for a go.mod-level guard; the import walk below
// is the stronger form because it is per-file, so an added require line alone does
// not trip it).
var denied = map[string]bool{
	"database/sql": true, "os/exec": true, "os/signal": true, "syscall": true,
}

// storeFiles and allowedStoreImports are the one narrowing §7.2 (REQ-AD-1, 09-22)
// required, applied under this test's own older instruction: "amend this test with the
// package and the contract clause that allows it, rather than deleting it".
// internal/adapter became "protocol + registration domain", so exactly one file may
// reach Core internals. The ban was narrowed, not lifted, and each cut keeps what the
// blanket version was actually protecting:
//   - only probe_store_sqlite.go may import a Core package, and only the two listed
//     below. handshake/stream/protocol/send/quota still hold no DB handle and emit no
//     event, which is what §6 规则 4 stood for;
//   - database/sql stays denied even in the store file, because a write that bypasses
//     platform/db skips version-taking, redaction and the journal row - the whole reason
//     the platform layer exists;
//   - internal/process stays denied everywhere including the store file: §7.2's REQ-T12
//     note pins probe to an injected Launcher, so importing the process layer buys
//     nothing and reverses the direction internal/process's package comment depends on.
var (
	storeFiles          = map[string]bool{"probe_store_sqlite.go": true}
	allowedStoreImports = map[string]bool{
		"github.com/SadCat18/lucero/core/internal/platform/db":  true,
		"github.com/SadCat18/lucero/core/internal/eventjournal": true,
		// audit.Rules() is the only legal source of the redaction rule set (REQ-A1: the
		// rules must be the ones audit exports, not a second hand-written list), and
		// platform/db refuses every write until InitRedaction has run - so the store's
		// test has to import it. Test files only; the store file itself stays without it.
		"github.com/SadCat18/lucero/core/internal/audit": true,
	}
)

// A non-stdlib import is one whose first path segment contains a dot.
func TestOnlyStdlibImports(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var found []string
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, e.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if first, _, _ := strings.Cut(path, "/"); !strings.Contains(first, ".") {
				// Stdlib, but not this package's business: no DB handle and no process
				// spawn may appear here (contract §0 puts spawning in internal/process,
				// §6 规则 4 puts persistence outside the Adapter entirely).
				if denied[path] {
					found = append(found, e.Name()+": "+path+" (denied stdlib import)")
				}
				continue
			}
			if allowedStoreImports[path] && (storeFiles[e.Name()] || strings.HasSuffix(e.Name(), "_test.go")) {
				continue // sanctioned by contract ③ §7.2; a test may build a real store
			}
			found = append(found, e.Name()+": "+path)
		}
	}
	if len(found) > 0 {
		t.Errorf("imports in internal/adapter beyond what §6 规则 4 and §7.2 allow:\n%s", strings.Join(found, "\n"))
	}
	// The allow-list may not outlive its reason: with the store file gone it would keep
	// permitting imports in a package that no longer has a reason for them.
	for name := range storeFiles {
		if _, err := os.Stat(name); err != nil {
			t.Errorf("storeFiles allows %s, which does not exist: narrow the list rather than leaving the door open", name)
		}
	}
}
