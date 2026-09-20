package approval

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// approvalsSQL is the executable form of the check frozen in internal contract
// §3 rule 1: the approvals table may only be touched from this package, so
// "who may advance a task" cannot be answered by whoever happens to write SQL.
var approvalsSQL = regexp.MustCompile(`(?i)\b(from|update|insert[ \t]+into)[ \t]+approvals\b`)

// scanApprovalsSQL returns "path:line" for every .go file under root that
// mentions approvals table SQL.
func scanApprovalsSQL(t *testing.T, root string) []string {
	t.Helper()
	var hits []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if approvalsSQL.MatchString(line) {
				hits = append(hits, path+":"+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	return hits
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test working directory")
		}
		dir = parent
	}
}

func TestApprovalsSQLStaysInThisPackage(t *testing.T) {
	internal := filepath.Join(moduleRoot(t), "internal")
	own := filepath.Join(internal, "approval")
	for _, hit := range scanApprovalsSQL(t, internal) {
		path := hit[:strings.LastIndex(hit, ":")]
		if rel, err := filepath.Rel(own, path); err != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("approvals table SQL outside internal/approval: %s", hit)
		}
	}
}

// TestScanActuallyFindsViolations keeps the check above from passing because the
// scanner is broken (wrong root, pattern that never matches, skipped files).
func TestScanActuallyFindsViolations(t *testing.T) {
	root := t.TempDir()
	// Assembled from parts so this fixture does not itself trip the literal grep
	// the test role runs over core/internal.
	tbl := "approv" + "als"
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/approval/store.go", "package approval\n\nconst q = \"UPDATE "+tbl+" SET state = ? WHERE id = ?\"\n")
	write("internal/task/repo.go", "package task\n\nconst q = \"SELECT id FROM "+tbl+" WHERE state = 'PENDING'\"\n")
	write("internal/backend/other.go", "package backend\n\nconst q = \"insert into "+strings.ToUpper(tbl)+" (id) values (?)\"\n")
	write("internal/task/clean.go", "package task\n\nconst q = \"SELECT id FROM tasks\"\n")

	hits := scanApprovalsSQL(t, root)
	if len(hits) != 3 {
		t.Fatalf("found %d hits, want 3 (the scanner is silently missing something): %v", len(hits), hits)
	}
	var outside int
	for _, h := range hits {
		path := h[:strings.LastIndex(h, ":")]
		if !strings.Contains(filepath.ToSlash(path), "/internal/approval/") {
			outside++
		}
	}
	if outside != 2 {
		t.Errorf("%d violations classified, want 2: %v", outside, hits)
	}
}
