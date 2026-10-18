package generator

import (
	"maps"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
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
		assertRejectedValues(t)
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
		register := funcBody(t, readFile(t, filepath.Join(httpDir, "internal/usecase/user/service.go")), "Register")
		mustContainAll(t, register, "s.tx.WithinTx(ctx,", "s.userRepo.Create(")
	})
	t.Run("docs/07 cronjob quotes the rendered cron transport", func(t *testing.T) {
		t.Parallel()
		dir := renderProject(t, cronMatrixConfig("postgres", "wire"))
		doc := docFile(t, "07-transport-layer.md")
		quoted := []string{
			"func (j *ScanJob) Run(ctx context.Context) error {",
			`return errors.Wrapf(err, "user-scan: resume with --after-id=%d", res.LastID)`,
			"Name() string",
		}
		mustContainAll(t, doc, quoted...)
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/cronjob/user/scan.go")), quoted[:2]...)
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/cronjob/job.go")), quoted[2])
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/cronjob/runner.go")),
			`"cron: job finished"`, `"cron: job failed"`, `"cron: job interrupted"`)
		mustContainAll(t, doc,
			"`cron: job finished`", "`cron: job failed`", "`cron: job interrupted`", "`cron.timeout`")
		mustNotContain(t, doc, "**Not scaffolded.** Cron", "not wired into `buildFileList`")
		mustNotContain(t, docFile(t, "13-cross-cutting.md"), "`cmd/cron.go` are not rendered")
		mustNotContain(t, docFile(t, "03-architecture-rules.md"), "`transport/cronjob/` are patterns to grow into")
		mustNotContain(t, docFile(t, "README.md"), "- `transport/cronjob/`")
		mustContainAll(t, docFile(t, "02-project-layout.md"), "[cron]", "cronjob/", "userscan/", "cmd/cron.go")
	})
	t.Run("docs/13 CORS never widens and tracing runs before the access log", func(t *testing.T) {
		t.Parallel()
		assertCORSAndTraceOrder(t, httpDir)
	})
	t.Run("docs/13 throttles exactly login and registration", func(t *testing.T) {
		t.Parallel()
		mustContainAll(t, docFile(t, "13-cross-cutting.md"), "login and registration")
		registrar := readFile(t, filepath.Join(httpDir, "internal/transport/http/v1/user/registrar.go"))
		if n := strings.Count(registrar, "LoginRateLimit("); n != 2 {
			t.Errorf("registrar throttles %d routes, docs/13 says login and registration", n)
		}
		mustContainAll(t, readFile(t, filepath.Join(grpcDir, "internal/transport/grpc/user.go")),
			"func (*UserHandler) Throttled()")
	})
	t.Run("docs/12 limit defaults equal the compose file", func(t *testing.T) {
		t.Parallel()
		compose, err := templateFS.ReadFile("templates/docker-compose.yaml.tmpl")
		if err != nil {
			t.Fatal(err)
		}
		inCompose := limitDefaults(string(compose), `\$\{(LIMIT_[A-Z_]+):-([^}]+)\}`)
		inDocs := limitDefaults(docFile(t, "12-build-and-deploy.md"), "\\| `(LIMIT_[A-Z_]+)` +\\| `([^`]+)`")
		if len(inCompose) < 10 {
			t.Fatalf("found only %d LIMIT_* defaults in the compose template: the pattern is stale", len(inCompose))
		}
		if !maps.Equal(inCompose, inDocs) {
			t.Errorf("docs/12 limit table %v, compose defaults %v", inDocs, inCompose)
		}
	})
	t.Run("docs/12 every Makefile target is in make help and .env.example sources", func(t *testing.T) {
		t.Parallel()
		mustContainAll(t, docFile(t, "12-build-and-deploy.md"), "Every target is discoverable", "sourceable")
		mysqlDir := renderProject(t, httpMatrixConfig("gin", "mysql", "fx"))
		for _, dir := range []string{httpDir, grpcDir, mysqlDir} {
			assertMakefileHygiene(t, readFile(t, filepath.Join(dir, "Makefile")))
			assertEnvSourceable(t, dir)
		}
	})
	t.Run("docs/07 and docs/13 describe the gRPC transport as real", func(t *testing.T) {
		t.Parallel()
		mustNotContain(t, docFile(t, "07-transport-layer.md"), "**Placeholder.**", "compiling stub")
		mustNotContain(t, docFile(t, "13-cross-cutting.md"), "gRPC is a stub")
		mustContainAll(t, docFile(t, "07-transport-layer.md"), "rpcstatus", "grpc.health.v1", "Public()")
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
		assertNovaTargetsListed(t)
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

// funcBody returns the source of method name, from its declaration to the
// next top-level func.
func funcBody(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, ") "+name+"(")
	if start < 0 {
		t.Fatalf("no method %s", name)
	}
	end := strings.Index(src[start:], "\nfunc ")
	if end < 0 {
		return src[start:]
	}
	return src[start : start+end]
}

// limitDefaults maps each LIMIT_* variable to its default as pattern finds them.
func limitDefaults(src, pattern string) map[string]string {
	out := map[string]string{}
	for _, m := range regexp.MustCompile(pattern).FindAllStringSubmatch(src, -1) {
		out[m[1]] = m[2]
	}
	return out
}

func assertRejectedValues(t *testing.T) {
	t.Helper()
	for _, c := range []struct{ field, value string }{
		{"Transport", "cli"}, {"ConfigFormat", "toml"}, {"ConfigFormat", "env"},
		{"QueryGen", "raw"}, {"QueryGen", "gorm"},
	} {
		cfg := httpMatrixConfig("fiber", "postgres", "wire")
		reflect.ValueOf(cfg).Elem().FieldByName(c.field).SetString(c.value)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s=%q accepted; docs/01 says it is rejected", c.field, c.value)
		}
	}
	mustContainAll(t, docFile(t, "01-cli-options.md"), "the `cli` transport", "`gorm`, `raw`",
		"`toml` and `env`", "`--docker=false`", "`--type`", "`worker` also needs `--queue")
}

// assertCORSAndTraceOrder: no framework's CORS code allows "*", and every
// router installs its OTel middleware before Logging, which is what puts
// trace_id on the access line.
func assertCORSAndTraceOrder(t *testing.T, fiberDir string) {
	t.Helper()
	mustContainAll(t, docFile(t, "13-cross-cutting.md"), "never `*`", "`trace_id` on every log line")
	for _, fw := range []string{"fiber", "gin", "chi", "echo"} {
		dir := fiberDir
		if fw != "fiber" {
			dir = renderProject(t, noDBMatrixConfig(fw, "fx"))
		}
		mw := filepath.Join(dir, "internal/transport/http/middleware")
		for _, line := range strings.Split(readFile(t, filepath.Join(mw, "cors.go")), "\n") {
			if code, _, _ := strings.Cut(line, "//"); strings.Contains(code, `"*"`) {
				t.Errorf("%s cors.go allows \"*\": %s", fw, strings.TrimSpace(line))
			}
		}
		router := readFile(t, filepath.Join(dir, "internal/transport/http/router.go"))
		otel := strings.Index(router, "otel"+fw+".")
		logging := strings.Index(router, "middleware.Logging(")
		if otel < 0 || logging < 0 || otel > logging {
			t.Errorf("%s: the OTel middleware must run before Logging so trace_id is set", fw)
		}
	}
}

func assertNovaTargetsListed(t *testing.T) {
	t.Helper()
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
}

// TestDocsLinksResolve: every relative link in nova's docs points at a file
// that exists and, when it carries a #fragment, at a heading that exists —
// so renaming a section or moving a file breaks the build, not a reader.
func TestDocsLinksResolve(t *testing.T) {
	t.Parallel()
	docs, err := filepath.Glob(filepath.Join("..", "..", "docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	docs = append(docs, filepath.Join("..", "..", "README.md"), filepath.Join("..", "..", "CLAUDE.md"))
	link := regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)
	for _, doc := range docs {
		for _, m := range link.FindAllStringSubmatch(stripFences(readFile(t, doc)), -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			path, anchor, _ := strings.Cut(target, "#")
			file := doc
			if path != "" {
				file = filepath.Join(filepath.Dir(doc), path)
			}
			if !fileExists(file) {
				t.Errorf("%s links to missing %s", filepath.Base(doc), target)
				continue
			}
			if anchor != "" && strings.HasSuffix(file, ".md") && !headingAnchors(t, file)[anchor] {
				t.Errorf("%s links to missing heading %s", filepath.Base(doc), target)
			}
		}
	}
}

var fence = regexp.MustCompile("(?s)```.*?```")

func stripFences(src string) string { return fence.ReplaceAllString(src, "") }

// headingAnchors returns the GitHub anchors of every heading in a Markdown
// file: lowercased, punctuation other than "-" and "_" dropped, spaces as
// "-", and "-1", "-2"… appended to repeats.
func headingAnchors(t *testing.T, file string) map[string]bool {
	t.Helper()
	drop := regexp.MustCompile(`[^\p{L}\p{N}_\- ]`)
	seen := map[string]int{}
	anchors := map[string]bool{}
	for _, line := range strings.Split(stripFences(readFile(t, file)), "\n") {
		text, ok := strings.CutPrefix(strings.TrimLeft(line, "#"), " ")
		if !ok || !strings.HasPrefix(line, "#") {
			continue
		}
		slug := strings.ReplaceAll(drop.ReplaceAllString(strings.ToLower(strings.TrimSpace(text)), ""), " ", "-")
		if n := seen[slug]; n > 0 {
			anchors[slug+"-"+strconv.Itoa(n)] = true
		} else {
			anchors[slug] = true
		}
		seen[slug]++
	}
	return anchors
}
