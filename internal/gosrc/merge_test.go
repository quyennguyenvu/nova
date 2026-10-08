package gosrc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMergeDeclsInjectsImportsAndDecls pins the merge contract: every
// non-import declaration of the scaffold is appended, imports the target
// lacks are added (creating a block when the file has none), and a re-run
// does not duplicate imports.
func TestMergeDeclsInjectsImportsAndDecls(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "app.go")
	if err := os.WriteFile(target, []byte("package di\n\ntype HTTPApp struct{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scaffold := "package di\n\nimport (\n\t\"context\"\n)\n\n// WorkerApp bundles the worker.\ntype WorkerApp struct{ Ctx context.Context }\n"
	if err := MergeDecls(target, scaffold); err != nil {
		t.Fatalf("MergeDecls: %v", err)
	}
	got, _ := os.ReadFile(target)
	for _, want := range []string{`"context"`, "type HTTPApp struct{}", "// WorkerApp bundles the worker.", "type WorkerApp struct"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("merged file lacks %q:\n%s", want, got)
		}
	}
	if err := MergeDecls(target, scaffold); err != nil {
		t.Fatalf("second MergeDecls: %v", err)
	}
	got, _ = os.ReadFile(target)
	if strings.Count(string(got), `"context"`) != 1 {
		t.Errorf("import duplicated:\n%s", got)
	}
}
