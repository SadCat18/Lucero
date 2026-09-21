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

// A non-stdlib import is one whose first path segment contains a dot; if a later
// round genuinely needs one, amend this test with the package and the contract
// clause that allows it, rather than deleting it.
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
			if first, _, _ := strings.Cut(path, "/"); strings.Contains(first, ".") {
				found = append(found, e.Name()+": "+path)
			}
			// Stdlib, but not this package's business: no DB handle and no process
			// spawn may appear here (contract §0 puts spawning in internal/process,
			// §6 规则 4 puts persistence outside the Adapter entirely).
			if denied[path] {
				found = append(found, e.Name()+": "+path+" (denied stdlib import)")
			}
		}
	}
	if len(found) > 0 {
		t.Errorf("non-stdlib imports in internal/adapter, want none (§6 规则 4 + §0):\n%s", strings.Join(found, "\n"))
	}
}
