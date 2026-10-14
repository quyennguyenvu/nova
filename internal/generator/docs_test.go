package generator

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestDocsClaims is the phase 8 exit criterion: every doc claim the
// 2026-10-04 review flagged is checked against rendered output (or nova's
// own code), so a doc and a template cannot drift apart silently again.
func TestDocsClaims(t *testing.T) {
	t.Parallel()
	httpDir := renderProject(t, httpMatrixConfig("fiber", "postgres", "wire"))
	grpcDir := renderProject(t, grpcMatrixConfig("wire"))
	noDBDir := renderProject(t, noDBMatrixConfig("echo", "fx"))

	t.Run("generated README runs the right subcommand", func(t *testing.T) {
		t.Parallel()
		readme := readFile(t, filepath.Join(grpcDir, "README.md"))
		mustContainAll(t, readme, "go run main.go grpc")
		mustNotContain(t, readme, "go run main.go api")
	})
	t.Run("generated CLAUDE.md names the in-memory repository without a database", func(t *testing.T) {
		t.Parallel()
		src := readFile(t, filepath.Join(noDBDir, "CLAUDE.md"))
		mustContainAll(t, src, "repository/memory")
		mustNotContain(t, src, "repository/none", "none dialect")
	})
	t.Run("docs/01 rejected values are rejected by Validate", func(t *testing.T) {
		t.Parallel()
		for _, c := range []struct{ field, value string }{
			{"Transport", "cron"}, {"Transport", "cli"}, {"ConfigFormat", "toml"}, {"ConfigFormat", "env"},
			{"QueryGen", "raw"}, {"QueryGen", "gorm"},
		} {
			cfg := httpMatrixConfig("fiber", "postgres", "wire")
			reflect.ValueOf(cfg).Elem().FieldByName(c.field).SetString(c.value)
			if err := cfg.Validate(); err == nil {
				t.Errorf("%s=%q accepted; docs/01 says it is rejected", c.field, c.value)
			}
		}
		mustContainAll(t, docFile(t, "01-cli-options.md"), "`cron` and `cli` transports", "`gorm`, `raw`",
			"`toml` and `env`", "`--docker=false`", "`--type`", "`worker` also needs `--queue")
	})
	t.Run("docs/02 names every make gen output", func(t *testing.T) {
		t.Parallel()
		mustContainAll(t, docFile(t, "02-project-layout.md"), "dbgen/", "wire_gen.go", "api/gen/", "user.proto",
			"buf.gen.yaml")
	})
	t.Run("docs/03 and docs/04 agree on the jwt seam", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"03-architecture-rules.md", "04-placement-rationale.md"} {
			mustContainAll(t, docFile(t, name), "security.TokenIssuer")
		}
		assertInwardImports(t, httpDir, "internal/usecase", "internal/domain", "pkg")
	})
	t.Run("docs/08 TxManager guards Register", func(t *testing.T) {
		t.Parallel()
		mustContainAll(t, docFile(t, "08-adapter-layer.md"),
			"`Register` runs its uniqueness check and insert inside `WithinTx`")
		mustContainAll(t, readFile(t, filepath.Join(httpDir, "internal/usecase/user/service.go")), "s.tx.WithinTx(ctx,")
	})
	t.Run("docs/13 CORS and trace claims", func(t *testing.T) {
		t.Parallel()
		mustContainAll(t, docFile(t, "13-cross-cutting.md"), "never `*`", "`trace_id` on every log line")
		mustContainAll(t, readFile(t, filepath.Join(httpDir, "internal/transport/http/middleware/cors.go")),
			"len(cfg.AllowOrigins) == 0")
		mustContainAll(t, readFile(t, filepath.Join(httpDir, "internal/transport/http/middleware/logging.go")),
			`"trace_id"`)
	})
	t.Run("docs/07 and docs/13 describe the gRPC transport as real", func(t *testing.T) {
		t.Parallel()
		mustNotContain(t, docFile(t, "07-transport-layer.md"), "**Placeholder.**", "compiling stub")
		mustNotContain(t, docFile(t, "13-cross-cutting.md"), "gRPC is a stub")
		mustContainAll(t, docFile(t, "07-transport-layer.md"), "rpcstatus", "grpc.health.v1", "Protected()")
		mustNotContain(t, readFile(t, filepath.Join(grpcDir, "internal/transport/grpc/user.go")), "TODO")
	})
	t.Run("docs/12 make targets exist in the generated Makefile", func(t *testing.T) {
		t.Parallel()
		mk := readFile(t, filepath.Join(grpcDir, "Makefile"))
		mustContainAll(t, mk, "proto-gen:", "wire-gen:", "sqlc-gen:", "migrate-down:", "docker-up:")
		mustContainAll(t, docFile(t, "12-build-and-deploy.md"), "buf generate", "buf v1.70.0")
	})
	t.Run("root README and CLAUDE.md list every nova make target", func(t *testing.T) {
		t.Parallel()
		readme, claude := repoFile(t, "README.md"), repoFile(t, "CLAUDE.md")
		for _, line := range strings.Split(repoFile(t, "Makefile"), "\n") {
			name, _, ok := strings.Cut(line, ":")
			if !ok || !strings.Contains(line, "## ") || strings.ContainsAny(name, " \t$") {
				continue
			}
			mustContainAll(t, readme, "make "+name)
			mustContainAll(t, claude, "make "+name)
		}
		mustContainAll(t, readme, "`{cache}`")
	})
}

func docFile(t *testing.T, name string) string {
	t.Helper()
	return repoFile(t, filepath.Join("docs", name))
}

// repoFile reads a file of the nova repository itself; tests run with the
// package directory as cwd.
func repoFile(t *testing.T, rel string) string {
	t.Helper()
	return readFile(t, filepath.Join("..", "..", rel))
}
