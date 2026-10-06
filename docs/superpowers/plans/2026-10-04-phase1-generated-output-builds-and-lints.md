# Phase 1: Generated Output Builds and Lints Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Heavy steps (`make test-all`, bootstrap test, lint) run one at a time from the main thread; see Global Constraints.

**Goal:** Every supported `nova new` output builds, passes its own `make lint`, and is proven to do so by nova's test suite using the real toolchain.

**Architecture:** Two generators render `text/template` files from embedded trees (`internal/generator/generator.go` for `nova new`, `component.go`/`component_render.go` for `nova add`). Fixes land in the templates and in the render path (gofmt on write), and the proof lands in `internal/generator/generator_test.go`: gofmt assertion, no-database matrix rows, and a sequential real-toolchain bootstrap test (`sqlc` → `go mod tidy` → `wire` → `go build` → `golangci-lint run`).

**Tech Stack:** Go 1.25+, `text/template`, `go/format`, sqlc v1.30, google/wire, golangci-lint v2 (golden config), GNU make.

**Status (2026-10-05):** all seven tasks implemented and verified in the working tree; every commit step is pending the user's `/grimoire-core:commit`. Final verification: `make test-all` green in 97 s (32 vet rows + 4 real bootstraps incl. `golangci-lint run`), `make lint` 0 issues, formatter diff 0 files across fiber/gin/chi/echo/worker renders.

**Deviations from the plan as written:**

- Task 2/5: `bootstrapProject` sets `PWD` explicitly (the generated Makefile's `rm` lines use `$(PWD)`, which make inherits from the environment) and skips when `make` is absent.
- Task 6: the bootstrap test turns `Search` off. Linting a project that imports the typed go-elasticsearch client took 6–8 minutes per project; the vet matrix still covers that adapter.
- Task 6: `reformat-tags: false` was added to the generated golines settings instead of porting golines' tag realignment (it reorders `env` before `yaml` and pads fields that lack a key).
- Task 6: import-group separators in `wire.go.tmpl`, `fx.go.tmpl`, `fx_provider.go.tmpl` use `{{ if` instead of `{{- if` so gofmt-on-write does not merge third-party and project imports.
- Task 6: additional findings surfaced by the real lint and fixed: echo `Logging` cognitive complexity (extracted `logAccess`), chi `http.ResponseWriter` embedded-field spacing in `logging`/`recovery`, MySQL connector shadowed `err` then `noctx` (now `PingContext` under a 5 s `pingTimeout`), `int32(len(out))` in the memory repository, and an unused `nolint` on the Limit line.
- Task 7: `make test-all` help text says ~2 min warm cache (measured 97 s), not the ~9 min estimated before Elasticsearch was dropped from the bootstrap.

## Global Constraints

- `make lint` and `make test` (`go test -short ./...`) stay green after every task; `make test-all` (`go test -parallel 2 -timeout 30m ./...`) is run alone at the end of tasks 2, 3, 4, 6.
- Resource rule: one heavy toolchain command at a time, from the main thread; never `go clean -cache`.
- Templates receive `*config.ProjectConfig`; condition on `HasDatabase`, `HasSQL`, `HasRedis`, `HasHTTP`, `HasWorker`, `HasGRPC`, `UseWire`, `UseFx`.
- Changes to a framework-prefixed template must be applied to all four variants (fiber, gin, chi, echo).
- `templates/app/worker.go.tmpl` and `skel/worker/app_worker.go.tmpl` must stay byte-identical (`TestWorkerAppTemplatesIdentical`).
- Where a doc and a template disagree, fix the doc in the same change.
- Commit only via `/grimoire-core:commit` after the user confirms the message; never run `git commit`. Each task's final step stages files and asks the user to run `/commit`.
- Markdown edits must pass `npx --yes markdownlint-cli2 <file>` with the repo config.

---

### Task 1: gofmt every rendered Go file at write time

**Files:**

- Modify: `internal/generator/generator.go:702-734` (`renderFile`)
- Modify: `internal/generator/component_render.go:29-50` (`renderTemplates`)
- Test: `internal/generator/generator_test.go` (new `TestRenderedGoIsGofmtClean`)

**Interfaces:**

- Consumes: `renderProject(t, cfg) string` (existing helper, `generator_test.go:1175`), `httpMatrixConfig`, `workerMatrixConfig`.
- Produces: `formatGoSource(outPath string, src []byte) ([]byte, error)` in `internal/generator/gofmt.go`, used by both generators. Later tasks rely on rendered `.go` output being gofmt-clean.

- [x] **Step 1: Write the failing test**

Add to `internal/generator/generator_test.go` (add imports `bytes`, `go/format`, `io/fs`):

```go
// TestRenderedGoIsGofmtClean renders representative configs and asserts every
// .go file is already in gofmt form. Conditional template lines otherwise
// drift struct alignment and import order, and the generated project then
// fails its own `make lint` on the first commit.
func TestRenderedGoIsGofmtClean(t *testing.T) {
	t.Parallel()
	cfgs := []*config.ProjectConfig{
		httpMatrixConfig("fiber", "postgres", "wire"),
		httpMatrixConfig("gin", "mysql", "fx"),
		workerMatrixConfig("postgres", "kafka", "wire"),
		workerMatrixConfig("mysql", "rabbitmq", "fx"),
	}
	for _, cfg := range cfgs {
		dir := renderProject(t, cfg)
		walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			want, fmtErr := format.Source(src)
			if fmtErr != nil {
				return fmt.Errorf("%s: %w", path, fmtErr)
			}
			if !bytes.Equal(src, want) {
				rel, _ := filepath.Rel(dir, path)
				t.Errorf("%s/%s/%s/%s: %s is not gofmt-clean", cfg.Transport, cfg.HTTPFramework, cfg.Database, cfg.DI, rel)
			}
			return nil
		})
		if walkErr != nil {
			t.Fatal(walkErr)
		}
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test -run TestRenderedGoIsGofmtClean ./internal/generator`

Expected: FAIL listing at least `internal/usecase/user/service.go`, `internal/infrastructure/di/wire.go` (or `fx.go`, `fx_provider.go`), `internal/transport/http/health/checker.go`.

- [x] **Step 3: Add the shared formatter**

Create `internal/generator/gofmt.go`:

```go
package generator

import (
	"fmt"
	"go/format"
	"strings"
)

// formatGoSource gofmt's rendered Go so conditional template lines cannot
// leave misaligned fields or unsorted imports in the output. Non-Go files
// pass through untouched. A parse failure means the template produced
// invalid Go, which is a generator bug worth surfacing with the file name.
func formatGoSource(outPath string, src []byte) ([]byte, error) {
	if !strings.HasSuffix(outPath, ".go") {
		return src, nil
	}
	formatted, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("rendered %s is not valid Go: %w", outPath, err)
	}
	return formatted, nil
}
```

- [x] **Step 4: Use it in `renderFile`**

In `internal/generator/generator.go`, replace the write section of `renderFile` (currently `os.WriteFile(outPath, buf.Bytes(), outFileMode(outPath))`) with:

```go
	out, fmtErr := formatGoSource(outPath, buf.Bytes())
	if fmtErr != nil {
		return fmt.Errorf("template %s: %w", tmplPath, fmtErr)
	}

	// Write file
	if writeErr := os.WriteFile(outPath, out, outFileMode(outPath)); writeErr != nil {
		return fmt.Errorf("failed to write file %s: %w", outPath, writeErr)
	}
```

- [x] **Step 5: Use it in `renderTemplates`**

In `internal/generator/component_render.go`, inside the loop after `rendered, err := renderTemplateString(s.tmpl, data)`:

```go
		formatted, fmtErr := formatGoSource(s.outRel, []byte(rendered))
		if fmtErr != nil {
			return fmt.Errorf("template %s: %w", s.tmpl, fmtErr)
		}
		if wErr := writeFile(out, string(formatted)); wErr != nil {
			return wErr
		}
```

(replacing the existing `writeFile(out, rendered)` call).

- [x] **Step 6: Run the new test and the short suite**

Run: `go test -run TestRenderedGoIsGofmtClean ./internal/generator && go test -short ./...`

Expected: PASS. If a `component_test.go` or `generator_test.go` assertion fails because it matched pre-gofmt whitespace (for example a two-space alignment inside a `mustContainAll` string), update that assertion string to the gofmt form; do not weaken it.

- [x] **Step 7: Lint**

Run: `make lint`

Expected: `0 issues.`

- [ ] **Step 8: Commit**

Stage `internal/generator/gofmt.go internal/generator/generator.go internal/generator/component_render.go internal/generator/generator_test.go` and ask the user to run `/grimoire-core:commit`. Suggested message: `fix(generator): gofmt rendered Go so generated projects pass their own lint`.

---

### Task 2: Real-toolchain bootstrap test

**Files:**

- Test: `internal/generator/generator_test.go` (new `TestGeneratedProjectBootstrap`, `bootstrapProject`)

**Interfaces:**

- Consumes: `renderProject`, `httpMatrixConfig`, `workerMatrixConfig`.
- Produces: `bootstrapProject(t *testing.T, cfg *config.ProjectConfig)` which runs, in the rendered dir, `sqlc generate` (when `cfg.QueryGen == "sqlc"`), `go mod tidy`, `wire ./internal/infrastructure/di` (when `cfg.UseWire()`), `go build ./...`. Task 5 switches the first three to `make gen`; task 6 appends `golangci-lint run`.

- [x] **Step 1: Write the test**

```go
// TestGeneratedProjectBootstrap runs the real first-run sequence a user
// follows on representative projects. The vet matrix stubs dbgen and
// wire_gen, so only this test can catch engine-specific DDL that sqlc
// rejects or an injector wire cannot build. It is sequential on purpose
// (each step peaks near 1 GB) and skips when a tool is missing, so a CI
// box without sqlc/wire still passes.
func TestGeneratedProjectBootstrap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-toolchain bootstrap in -short mode")
	}
	for _, tool := range []string{"go", "sqlc", "wire"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not on PATH", tool)
		}
	}
	cases := []struct {
		name string
		cfg  *config.ProjectConfig
	}{
		{"http_fiber_postgres_wire", httpMatrixConfig("fiber", "postgres", "wire")},
		{"http_gin_mysql_wire", httpMatrixConfig("gin", "mysql", "wire")},
		{"worker_postgres_kafka_wire", workerMatrixConfig("postgres", "kafka", "wire")},
	}
	for _, tc := range cases {
		// No t.Parallel: each case compiles a full dependency tree.
		t.Run(tc.name, func(t *testing.T) { bootstrapProject(t, tc.cfg) })
	}
}

func bootstrapProject(t *testing.T, cfg *config.ProjectConfig) {
	t.Helper()
	dir := renderProject(t, cfg)
	run := func(sub, name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = filepath.Join(dir, sub)
		cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -p=2", "GOGC=50")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %s (in %s): %v\n%s", name, strings.Join(args, " "), sub, err, out)
		}
	}
	if cfg.QueryGen == "sqlc" {
		run("sqlc", "sqlc", "generate")
	}
	run("", "go", "mod", "tidy")
	if cfg.UseWire() {
		run("", "wire", "./internal/infrastructure/di")
	}
	run("", "go", "build", "./...")
}
```

- [x] **Step 2: Run it to see the MySQL failure**

Run: `go test -count=1 -run TestGeneratedProjectBootstrap ./internal/generator`

Expected: `http_fiber_postgres_wire` PASS, `worker_postgres_kafka_wire` PASS, `http_gin_mysql_wire` FAIL with `sqlc generate ... syntax error near "BIGSERIAL PRIMARY KEY,"`. This is the red for Task 3. (The test runs about 2 minutes; nothing else heavy may run alongside it.)

- [ ] **Step 3: Commit**

Stage `internal/generator/generator_test.go` and ask the user to run `/grimoire-core:commit`. Suggested message: `test(generator): bootstrap rendered projects with real sqlc, wire and go build`. State in the message that the MySQL case fails until the migration fix lands.

---

### Task 3: Per-engine `users` migration

**Files:**

- Modify: `internal/generator/templates/migrations/create_users_table.up.sql.tmpl` (whole file)
- Modify: `CLAUDE.md` (remove the "pre-existing bug" note in the "Component layout manifest" section)
- Test: `internal/generator/generator_test.go` (new `TestUsersMigrationPerEngine`)

**Interfaces:**

- Produces: MySQL table `users` with `id BIGINT AUTO_INCREMENT`, `created_at`/`updated_at DATETIME(6)`, matching the `time.Time` fields the mysql mapper and `mysqlDBGenStub` already assume.

- [x] **Step 1: Write the failing test**

```go
// TestUsersMigrationPerEngine pins the users DDL to the engine. sqlc derives
// the schema from this file, so Postgres-only syntax in a mysql project
// means dbgen is never generated and the project cannot build.
func TestUsersMigrationPerEngine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		db      string
		want    []string
		mustNot []string
	}{
		{"postgres", []string{"BIGSERIAL", "TIMESTAMP WITH TIME ZONE"}, []string{"AUTO_INCREMENT", "DATETIME"}},
		{"mysql", []string{"BIGINT AUTO_INCREMENT PRIMARY KEY", "DATETIME(6)"}, []string{"BIGSERIAL", "WITH TIME ZONE", "NOW()"}},
	}
	for _, tc := range cases {
		t.Run(tc.db, func(t *testing.T) {
			t.Parallel()
			dir := renderProject(t, httpMatrixConfig("fiber", tc.db, "wire"))
			matches, err := filepath.Glob(filepath.Join(dir, "sqlc/migrations/*_create_users_table.up.sql"))
			if err != nil || len(matches) != 1 {
				t.Fatalf("expected one users migration, got %v (%v)", matches, err)
			}
			src := readFile(t, matches[0])
			mustContainAll(t, src, tc.want...)
			mustNotContain(t, src, tc.mustNot...)
		})
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test -run TestUsersMigrationPerEngine ./internal/generator`

Expected: FAIL for `mysql` (missing `AUTO_INCREMENT`, contains `BIGSERIAL`).

- [x] **Step 3: Rewrite the migration template**

Replace the whole of `internal/generator/templates/migrations/create_users_table.up.sql.tmpl` with:

```sql
CREATE TABLE IF NOT EXISTS users (
{{- if eq .Database "postgres"}}
    id BIGSERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    password VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
{{- end}}
{{- if eq .Database "mysql"}}
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    password VARCHAR(255) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6)
{{- end}}
);
```

The old `CREATE INDEX idx_users_email` line is dropped: `UNIQUE` already indexes `email` on both engines.

- [x] **Step 4: Run the unit test, then the bootstrap test**

Run: `go test -run TestUsersMigrationPerEngine ./internal/generator`

Expected: PASS.

Run (alone): `go test -count=1 -run 'TestGeneratedProjectBootstrap/http_gin_mysql_wire' ./internal/generator`

Expected: PASS (sqlc accepts the DDL; `dbgen` emits `CreatedAt time.Time`, which the mysql mapper already expects).

- [x] **Step 5: Fix the CLAUDE.md note**

In `CLAUDE.md`, "Component layout manifest" paragraph, delete the sentence beginning `Note: nova new's create_*_table migration template is postgres-only DDL (BIGSERIAL) even for mysql projects — a pre-existing bug;` and keep `the sqlc repository generator emits correct per-engine DDL.` as `Both generators emit per-engine DDL.`

Run: `npx --yes markdownlint-cli2 CLAUDE.md`

Expected: `Summary: 0 issues`.

- [ ] **Step 6: Commit**

Stage `internal/generator/templates/migrations/create_users_table.up.sql.tmpl internal/generator/generator_test.go CLAUDE.md` and ask the user to run `/grimoire-core:commit`. Suggested message: `fix(templates): emit MySQL DDL for the users migration so sqlc generate succeeds`.

---

### Task 4: No-database HTTP projects compile and resolve a `UserRepository`

**Files:**

- Create: `internal/generator/templates/adapter/repository/memory/user_repository.go.tmpl`
- Modify: `internal/generator/generator.go:335-345` (`adapterFiles`, add the memory entry before the redis block)
- Modify: `internal/generator/templates/infrastructure/di/wire.go.tmpl:9-18` (imports) and `:146-167` (`adapterSet`)
- Modify: `internal/generator/templates/infrastructure/di/fx.go.tmpl:11-22` (imports) and `:161-179` (`adapterModule`)
- Modify: `internal/generator/templates/transport/http/health/checker.go.tmpl:10-27` (imports) and `:83-119` (`Readiness`)
- Test: `internal/generator/generator_test.go` (`noDBMatrixConfig`, new rows in `TestGenerateMatrix`, new bootstrap case)

**Interfaces:**

- Produces: `memory.NewUserRepository() *UserRepository` satisfying `domain.UserRepository` (`Create`, `GetByID`, `GetByEmail`, `Update`, `Delete`, `List`, `Count`, `ListAfter`) with the same error classification as the SQL adapters (`errors.ErrNotFound` on misses, `errors.ErrAlreadyExists` on duplicate email).
- Produces: `noDBMatrixConfig(framework, di string) *config.ProjectConfig`.

Decision: ship an in-memory adapter rather than gating the whole User slice on `HasDatabase`. The alternative threads a new conditional through router, registrar, DI, OpenAPI, and docs and leaves a hollow project; the adapter is one template plus two DI lines and keeps the CRUD demo runnable.

- [x] **Step 1: Add the no-DB matrix config and rows (failing)**

In `generator_test.go` add:

```go
// noDBMatrixConfig is an HTTP project with no database, cache, search, or
// broker: the leanest supported shape and the one most likely to leave an
// unconditional import or variable dangling.
func noDBMatrixConfig(framework, di string) *config.ProjectConfig {
	cfg := baseMatrixConfig("none", di)
	cfg.Transport = "http"
	cfg.HTTPFramework = framework
	cfg.DBDriver = ""
	cfg.QueryGen = ""
	cfg.Cache = "none"
	cfg.Search = "none"
	cfg.MessageQueue = "none"
	return cfg
}
```

In `TestGenerateMatrix`, inside the `for _, di := range dis` loop after the worker block, add:

```go
		for _, fw := range httpFrameworks {
			name := "http_" + di + "_" + fw + "_none"
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				renderAndVet(t, noDBMatrixConfig(fw, di), wireGenHTTPStub)
			})
		}
```

In `TestGeneratedProjectBootstrap` add the case `{"http_echo_none_fx", noDBMatrixConfig("echo", "fx")}`.

- [x] **Step 2: Run one new row to verify it fails**

Run: `go test -count=1 -run 'TestGenerateMatrix/http_fx_echo_none$' ./internal/generator`

Expected: FAIL with `declared and not used: probeCtx` and `"<module>/internal/domain" imported and not used`.

- [x] **Step 3: Create the in-memory repository template**

`internal/generator/templates/adapter/repository/memory/user_repository.go.tmpl`:

```go
// Package memory is the in-process UserRepository used when the project is
// generated without a database. It keeps the User CRUD runnable (demos,
// handler tests) with the same contract as the SQL adapters: ErrNotFound on
// misses, ErrAlreadyExists on a duplicate email, ids assigned on Create.
// Data lives for the lifetime of the process only.
package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"{{.ModuleName}}/internal/domain"
	"{{.ModuleName}}/internal/domain/entity"
	"{{.ModuleName}}/pkg/errors"
)

var _ domain.UserRepository = (*UserRepository)(nil)

// UserRepository is a mutex-guarded map keyed by id.
type UserRepository struct {
	mu     sync.RWMutex
	nextID int64
	users  map[int64]*entity.User
}

func NewUserRepository() *UserRepository {
	return &UserRepository{nextID: 1, users: map[int64]*entity.User{}}
}

func (r *UserRepository) Create(_ context.Context, user *entity.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if strings.EqualFold(u.Email, user.Email) {
			return errors.Wrapf(errors.ErrAlreadyExists, "user repo: insert email=%s", user.Email)
		}
	}
	now := time.Now()
	user.ID = r.nextID
	r.nextID++
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now
	stored := *user
	r.users[user.ID] = &stored
	return nil
}

func (r *UserRepository) GetByID(_ context.Context, id int64) (*entity.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.users[id]
	if !ok {
		return nil, errors.Wrapf(errors.ErrNotFound, "user repo: select id=%d", id)
	}
	out := *u
	return &out, nil
}

func (r *UserRepository) GetByEmail(_ context.Context, email string) (*entity.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, u := range r.users {
		if strings.EqualFold(u.Email, email) {
			out := *u
			return &out, nil
		}
	}
	return nil, errors.Wrapf(errors.ErrNotFound, "user repo: select email=%s", email)
}

func (r *UserRepository) Update(_ context.Context, user *entity.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.users[user.ID]; !ok {
		return errors.Wrapf(errors.ErrNotFound, "user repo: update id=%d", user.ID)
	}
	user.UpdatedAt = time.Now()
	stored := *user
	r.users[user.ID] = &stored
	return nil
}

func (r *UserRepository) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.users[id]; !ok {
		return errors.Wrapf(errors.ErrNotFound, "user repo: delete id=%d", id)
	}
	delete(r.users, id)
	return nil
}

func (r *UserRepository) List(_ context.Context, filter domain.UserFilter) ([]*entity.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := r.sortedMatching(filter.Email)
	start := int(filter.Offset)
	if start >= len(all) {
		return []*entity.User{}, nil
	}
	end := len(all)
	if filter.Limit > 0 && start+int(filter.Limit) < end {
		end = start + int(filter.Limit)
	}
	return all[start:end], nil
}

func (r *UserRepository) Count(_ context.Context, filter domain.UserFilter) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return int64(len(r.sortedMatching(filter.Email))), nil
}

func (r *UserRepository) ListAfter(_ context.Context, cursor int64, limit int32) ([]*entity.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*entity.User, 0, limit)
	for _, u := range r.sortedMatching("") {
		if u.ID <= cursor {
			continue
		}
		out = append(out, u)
		if int32(len(out)) == limit {
			break
		}
	}
	return out, nil
}

// sortedMatching returns copies of every user whose email contains filter
// (case-insensitive; empty matches all), ordered by id. Callers hold r.mu.
func (r *UserRepository) sortedMatching(emailFilter string) []*entity.User {
	needle := strings.ToLower(emailFilter)
	out := make([]*entity.User, 0, len(r.users))
	for _, u := range r.users {
		if needle != "" && !strings.Contains(strings.ToLower(u.Email), needle) {
			continue
		}
		cp := *u
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
```

- [x] **Step 4: Register the template in `adapterFiles`**

In `internal/generator/generator.go`, immediately before the `// Repository — cache` comment, add:

```go
		// Repository — in-memory. Keeps the User slice runnable when the project
		// has no database; the SQL adapters replace it otherwise.
		{
			"templates/adapter/repository/memory/user_repository.go.tmpl",
			"internal/adapter/repository/memory/user_repository.go",
			!cfg.HasDatabase(),
		},
```

- [x] **Step 5: Bind it in Wire**

In `internal/generator/templates/infrastructure/di/wire.go.tmpl`, after the `mysqlrepo` import block (line 17 `{{- end}}`) add:

```go
{{- if not .HasDatabase}}
	memoryrepo "{{.ModuleName}}/internal/adapter/repository/memory"
{{- end}}
```

In `adapterSet`, after the mysql block's closing `{{- end}}` (line 162) and before `{{- if .HasMessageQueue}}`, add:

```go
{{- if not .HasDatabase}}
	memoryrepo.NewUserRepository,
	wire.Bind(new(domain.UserRepository), new(*memoryrepo.UserRepository)),
{{- end}}
```

The `domain` import at line 18 is now used in every configuration, which removes the unused-import failure.

- [x] **Step 6: Bind it in fx**

In `internal/generator/templates/infrastructure/di/fx.go.tmpl`, after the `mysqlrepo` import block (line 19 `{{- end}}`) add:

```go
{{- if and (not .HasDatabase) (or .HasHTTP .HasWorker)}}
	memoryrepo "{{.ModuleName}}/internal/adapter/repository/memory"
{{- end}}
```

In `adapterModule()`, after the mysql block's closing `{{- end}}` (line 174) and before `{{- if .HasMessageQueue}}`, add:

```go
{{- if not .HasDatabase}}
		fx.Annotate(memoryrepo.NewUserRepository, fx.As(new(domain.UserRepository))),
{{- end}}
```

- [x] **Step 7: Gate the readiness probe body**

In `internal/generator/templates/transport/http/health/checker.go.tmpl`, change the import block so `time` is conditional:

```go
import (
	"context"
{{- if or .HasDatabase .HasRedis}}
	"time"
{{- end}}
{{- if .HasDatabase}}
{{- if eq .Database "postgres"}}

	"github.com/jackc/pgx/v5/pgxpool"
{{- end}}
{{- if eq .Database "mysql"}}

	"database/sql"
{{- end}}
{{- end}}
{{- if .HasRedis}}

	"{{.ModuleName}}/internal/infrastructure/cache"
{{- end}}
)
```

Replace everything from the `// Readiness pings every configured dependency` comment to the end of the file with:

```go
{{- if or .HasDatabase .HasRedis}}
// Readiness pings every configured dependency under a 1-second deadline.
// Returns OK=false if ANY check fails — the JSON body carries per-check
// detail so the failing dependency is visible without log archaeology.
func (c *Checker) Readiness(ctx context.Context) Status {
	probeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	details := map[string]string{}
	ok := true

{{- if and .HasDatabase (eq .Database "postgres")}}
	if err := c.pool.Ping(probeCtx); err != nil {
		details["database"] = err.Error()
		ok = false
	} else {
		details["database"] = "ok"
	}
{{- end}}
{{- if and .HasDatabase (eq .Database "mysql")}}
	if err := c.db.PingContext(probeCtx); err != nil {
		details["database"] = err.Error()
		ok = false
	} else {
		details["database"] = "ok"
	}
{{- end}}
{{- if .HasRedis}}
	if err := c.cache.Ping(probeCtx).Err(); err != nil {
		details["redis"] = err.Error()
		ok = false
	} else {
		details["redis"] = "ok"
	}
{{- end}}

	return Status{OK: ok, Details: details}
}
{{- else}}
// Readiness has no downstream dependency to probe in this configuration, so
// it reports the same process-up signal as Liveness.
func (*Checker) Readiness(context.Context) Status {
	return Status{OK: true}
}
{{- end}}
```

- [x] **Step 8: Run the no-DB rows, then the no-DB bootstrap case**

Run: `go test -count=1 -run 'TestGenerateMatrix/http_(wire|fx)_(fiber|gin|chi|echo)_none$' ./internal/generator`

Expected: PASS for all 8 rows.

Run (alone): `go test -count=1 -run 'TestGeneratedProjectBootstrap/http_echo_none_fx' ./internal/generator`

Expected: PASS (`go mod tidy` + `go build`).

- [x] **Step 9: Short suite, lint, then the full matrix alone**

Run: `go test -short ./... && make lint`

Expected: PASS, `0 issues.`

Run (alone, nothing else building): `make test-all`

Expected: PASS.

- [ ] **Step 10: Commit**

Stage `internal/generator/templates/adapter/repository/memory/user_repository.go.tmpl internal/generator/generator.go internal/generator/templates/infrastructure/di/wire.go.tmpl internal/generator/templates/infrastructure/di/fx.go.tmpl internal/generator/templates/transport/http/health/checker.go.tmpl internal/generator/generator_test.go` and ask the user to run `/grimoire-core:commit`. Suggested message: `fix(templates): make --database=none projects compile with an in-memory UserRepository`.

---

### Task 5: Bootstrap order that works, in `make gen`, the CLI, and the README

**Files:**

- Modify: `internal/generator/templates/Makefile.tmpl:3` (`WIRE_PATH`), `:50-70` (code generation section)
- Modify: `cmd/new.go:106-116` (next steps)
- Create: `cmd/new_test.go`
- Modify: `internal/generator/templates/README.md.tmpl:127-129`
- Modify: `internal/generator/generator_test.go` (`bootstrapProject` uses `make gen`)

**Interfaces:**

- Produces: `nextSteps(cfg *config.ProjectConfig) []string` in `cmd/new.go`.
- Produces: generated Makefile targets `tidy` and `gen` where `gen` depends on `sqlc-gen` (if sqlc), then `tidy`, then `wire-gen` (if wire), then `proto-gen` (if gRPC).

- [x] **Step 1: Write the failing CLI test**

Create `cmd/new_test.go`:

```go
package cmd

import (
	"strings"
	"testing"

	"github.com/quyennguyenvu/nova/internal/config"
)

// TestNextStepsPerTransport pins the post-generation checklist: the real
// file name, generators before anything that needs their output, and the
// subcommand that actually exists for the chosen transport.
func TestNextStepsPerTransport(t *testing.T) {
	t.Parallel()
	cases := []struct{ transport, want string }{
		{"http", "go run main.go api"},
		{"worker", "go run main.go worker"},
		{"grpc", "go run main.go grpc"},
	}
	for _, tc := range cases {
		t.Run(tc.transport, func(t *testing.T) {
			t.Parallel()
			cfg := config.DefaultConfig()
			cfg.ProjectName = "demo"
			cfg.Transport = tc.transport
			joined := strings.Join(nextSteps(cfg), "\n")
			for _, want := range []string{"cd demo", "cp .env.example .env", "make gen", tc.want} {
				if !strings.Contains(joined, want) {
					t.Errorf("missing %q in:\n%s", want, joined)
				}
			}
			for _, stale := range []string{"cp env.example", "go mod tidy", "docker-compose up"} {
				if strings.Contains(joined, stale) {
					t.Errorf("stale step %q in:\n%s", stale, joined)
				}
			}
			if strings.Index(joined, "make gen") > strings.Index(joined, "go run") {
				t.Errorf("make gen must precede go run:\n%s", joined)
			}
		})
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test -run TestNextStepsPerTransport ./cmd`

Expected: FAIL with `undefined: nextSteps`.

- [x] **Step 3: Implement `nextSteps` and use it**

In `cmd/new.go` replace the block from `fmt.Fprintln(os.Stdout, "Next steps:")` through the `if cfg.IncludeDocker { ... }` closing brace with:

```go
	fmt.Fprintln(os.Stdout, "Next steps:")
	for _, step := range nextSteps(cfg) {
		fmt.Fprintf(os.Stdout, "  %s\n", step)
	}
```

Add at the end of `cmd/new.go`:

```go
// nextSteps is the post-generation checklist in the order that actually
// works: code generators first (sqlc must emit dbgen before `go mod tidy`
// can resolve its import), then the transport's own subcommand.
func nextSteps(cfg *config.ProjectConfig) []string {
	steps := []string{
		"cd " + cfg.ProjectName,
		"cp .env.example .env   # fill in secrets (JWT keys, DB password)",
		"make gen               # code generators, then go mod tidy",
	}
	switch {
	case cfg.HasWorker():
		steps = append(steps, "go run main.go worker")
	case cfg.HasGRPC():
		steps = append(steps, "go run main.go grpc")
	default:
		steps = append(steps, "go run main.go api")
	}
	if cfg.IncludeDocker {
		steps = append(steps, "# or: docker compose up")
	}
	return steps
}
```

- [x] **Step 4: Run the CLI test**

Run: `go test -run TestNextStepsPerTransport ./cmd`

Expected: PASS.

- [x] **Step 5: Fix the generated Makefile**

In `internal/generator/templates/Makefile.tmpl` line 3 replace `WIRE_PATH = "$(GOPATH)/bin/wire"` with:

```makefile
WIRE_PATH ?= $(shell go env GOPATH)/bin/wire
```

Replace line 70 (`gen:{{if .UseWire}} wire-gen{{end}}{{if eq .QueryGen "sqlc"}} sqlc-gen{{end}}{{if .HasGRPC}} proto-gen{{end}}`) with:

```makefile
.PHONY: tidy
tidy:
	go mod tidy

# Order matters: sqlc must emit dbgen before tidy can resolve its import, and
# wire needs a tidied module to type-check the injector.
.PHONY: gen
gen:{{if eq .QueryGen "sqlc"}} sqlc-gen{{end}} tidy{{if .UseWire}} wire-gen{{end}}{{if .HasGRPC}} proto-gen{{end}}
```

If the template already declares a `tidy` target elsewhere, keep one definition only.

- [x] **Step 6: Fix the README quick start**

In `internal/generator/templates/README.md.tmpl` replace lines 127–129 (the `# Install dependencies...` comment, `go mod tidy`, and `make gen` lines) with:

```text
# Generate code and resolve dependencies. `make gen` runs{{if eq .QueryGen "sqlc"}} sqlc,{{end}} go mod tidy{{if .UseWire}}, wire{{end}}{{if .HasGRPC}}, protoc{{end}} in the order that works.
make gen
```

- [x] **Step 7: Make the bootstrap test exercise `make gen`**

In `bootstrapProject` (generator_test.go), replace the three tool invocations with the generated Makefile:

```go
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not on PATH")
	}
	run("", "make", "gen")
	run("", "go", "build", "./...")
```

(delete the `if cfg.QueryGen == "sqlc"`, `go mod tidy`, and `if cfg.UseWire()` blocks).

- [x] **Step 8: Run the bootstrap test alone**

Run: `go test -count=1 -run TestGeneratedProjectBootstrap ./internal/generator`

Expected: all four cases PASS through `make gen`.

- [x] **Step 9: Short suite, lint**

Run: `go test -short ./... && make lint`

Expected: PASS, `0 issues.`

- [ ] **Step 10: Commit**

Stage `cmd/new.go cmd/new_test.go internal/generator/templates/Makefile.tmpl internal/generator/templates/README.md.tmpl internal/generator/generator_test.go` and ask the user to run `/grimoire-core:commit`. Suggested message: `fix(templates): run sqlc before tidy and wire in make gen; correct next-steps text`.

---

### Task 6: Generated projects pass their own `make lint`

**Files:**

- Modify: `internal/generator/generator_test.go` (`bootstrapProject` appends lint)
- Modify: `internal/generator/templates/usecase/user/service.go.tmpl:3-8` (imports), `:156-163` (List offset)
- Modify: `internal/generator/templates/app/worker.go.tmpl:3-10`, `:34` and identically `internal/generator/skel/worker/app_worker.go.tmpl`
- Modify: `internal/generator/templates/infrastructure/jwt/service.go.tmpl:76-78`, `:176-180`
- Modify: `internal/generator/templates/infrastructure/pubsub/kafka.go.tmpl:3-10`, `:43`
- Modify: `internal/generator/templates/adapter/repository/redis/user_cache.go.tmpl:101`
- Modify: `internal/generator/templates/pkg/locale/locale_vi.go.tmpl:4`
- Modify: `internal/generator/templates/transport/http/v1/user/{fiber,gin,chi,echo}_registrar.go.tmpl` (blank line after `v1.Prefixed`)
- Modify: `internal/generator/templates/transport/http/middleware/{fiber,gin,chi,echo}_loginlimit.go.tmpl` (long `WriteError` line)
- Modify: `internal/generator/templates/infrastructure/config/config.go.tmpl` (TracingConfig tags), `internal/generator/templates/transport/worker/rabbitmq_consumer.go.tmpl`, `internal/generator/templates/adapter/pubsub/publisher.go.tmpl`, `internal/generator/templates/adapter/repository/mysql/user_repository.go.tmpl` (tool-reported lines)
- Modify: `internal/generator/templates/golangci.yaml.tmpl` (goimports `local-prefixes`)

**Interfaces:**

- Consumes: `bootstrapProject` from Task 5.
- Produces: generated `.golangci.yaml` with `local-prefixes: - {{.ModuleName}}`.

- [x] **Step 1: Make the bootstrap test lint (failing)**

Append to `bootstrapProject` after `go build`:

```go
	if _, err := exec.LookPath("golangci-lint"); err == nil {
		run("", "golangci-lint", "run")
	}
```

Run (alone): `go test -count=1 -run 'TestGeneratedProjectBootstrap/http_fiber_postgres_wire' ./internal/generator`

Expected: FAIL listing roughly ten issues: `health/checker.go` and `usecase/user/service.go` goimports are gone after Task 1; remaining are `config/config.go` (golines), `jwt/service.go:77` (golines) and `:180` (nonamedreturns), `middleware/loginlimit.go:57` (golines), `usecase/user/service.go:150` (gosec G115), `pkg/locale/locale_vi.go:4` (gosec G101), `pubsub/kafka.go:43` (perfsprint), `redis/user_cache.go:101` (staticcheck ST1020), `v1/user/registrar.go:20` (embeddedstructfieldcheck).

- [x] **Step 2: Bound the list offset (G115)**

In `templates/usecase/user/service.go.tmpl` add `"fmt"` and `"math"` to the standard-library import group, then replace

```go
	filter := domain.UserFilter{
		Email:  input.Email,
		Limit:  int32(perPage),
		Offset: int32((page - 1) * perPage),
	}
```

with

```go
	// Offsets are int32 at the repo boundary; refuse pages that cannot be
	// addressed instead of letting the conversion wrap negative.
	offset := int64(page-1) * int64(perPage)
	if offset > math.MaxInt32 {
		return nil, errors.L(locale.InvalidRequest).WithCause(fmt.Errorf("page %d out of range", page))
	}
	filter := domain.UserFilter{
		Email:  input.Email,
		Limit:  int32(perPage), //nolint:gosec // clamped to listMaxPerPage above
		Offset: int32(offset),  //nolint:gosec // bounded by the MaxInt32 check above
	}
```

- [x] **Step 3: Compare the worker's cancellation error correctly (errorlint)**

In both `templates/app/worker.go.tmpl` and `skel/worker/app_worker.go.tmpl` add `"errors"` to the standard-library imports and change

```go
		if startErr := worker.Start(ctx); startErr != nil && startErr != context.Canceled {
```

to

```go
		if startErr := worker.Start(ctx); startErr != nil && !errors.Is(startErr, context.Canceled) {
```

Run: `go test -run TestWorkerAppTemplatesIdentical ./internal/generator` — Expected: PASS.

- [x] **Step 4: JWT service (nonamedreturns, golines)**

In `templates/infrastructure/jwt/service.go.tmpl` change the `Sign` guard to

```go
	if s.privateKey == nil {
		return "", errors.L(locale.InternalError).
			WithCause(errors.New("no signing key configured (verify-only service)"))
	}
```

and change the `classifyParseError` doc comment and signature to

```go
// classifyParseError maps a golang-jwt v4 *ValidationError bitfield to a
// stable reason string, plus whether it is anomalous (worth a Warn). Anything
// that is not a recognised routine rejection — including a non-ValidationError,
// which shouldn't happen — is anomalous so it can never hide in the noise.
func classifyParseError(err error) (string, bool) {
```

(the body already returns both values explicitly).

- [x] **Step 5: Kafka constant error (perfsprint)**

In `templates/infrastructure/pubsub/kafka.go.tmpl` add `"errors"` to the standard-library imports (keep `"fmt"`, still used at lines 22 and 57) and change line 43 to

```go
		return nil, nil, errors.New("kafka: consumer_group_id is required for worker mode")
```

- [x] **Step 6: Redis cache doc comment (ST1020)**

In `templates/adapter/repository/redis/user_cache.go.tmpl` change the comment above `List` to

```go
// List passes straight through, as do Count and ListAfter: caching a page keyed
// by its filter invalidates on every write to the table, so the hit rate rarely
// pays for the staleness. Cache these per-query only if you can bound that.
```

- [x] **Step 7: Vietnamese locale (G101 false positive)**

In `templates/pkg/locale/locale_vi.go.tmpl` change line 4 to

```go
	translations[LangVi] = Mapping{ //nolint:gosec // G101: UI strings keyed by error name, not credentials
```

- [x] **Step 8: Registrars (embeddedstructfieldcheck), all four**

In each of `fiber_registrar.go.tmpl`, `gin_registrar.go.tmpl`, `chi_registrar.go.tmpl`, `echo_registrar.go.tmpl` insert a blank line directly after the `v1.Prefixed` field:

```go
type Registrar struct {
	v1.Prefixed

	handler    *Handler
```

- [x] **Step 9: Login limiter long line (golines), all four**

In `fiber_loginlimit.go.tmpl` change the `WriteError` return to

```go
		if !l.allow(c.IP()) {
			return httpwriter.WriteError(
				c,
				errors.L(locale.TooManyRequests).WithCause(errors.New("login rate limit exceeded")),
			)
		}
```

Apply the same three-line wrap in `gin_`, `chi_`, and `echo_loginlimit.go.tmpl` wherever the equivalent `WriteError(...)` call exceeds 120 columns (gin's and echo's take `c`, chi's takes `w, r`).

- [x] **Step 10: Tool-driven fixes for alignment and variant-specific hits**

Render fiber+postgres, gin+mysql, and worker+postgres+kafka into the scratchpad (one `nova new` each), run `make gen` in each, then in each run `golangci-lint fmt` followed by `git diff --no-index` against a pristine copy. Port each reported change into the owning template at the same line:

- `config/config.go.tmpl` TracingConfig struct: adopt the tag spacing golines emits (it realigns struct tags per struct; the template's hand alignment differs).
- `transport/worker/rabbitmq_consumer.go.tmpl` (gocognit 22 > 20 in `Subscribe`): move the per-delivery body (`dispatch`, the error log, `Ack`/`Nack`) into `func (c *rabbitConsumer) handle(ctx context.Context, d amqp.Delivery, dispatch DispatchFunc)` and call it from the `select`.
- `adapter/pubsub/publisher.go.tmpl` lines 26 and 41 (govet shadow): rename the inner `err` to `pubErr`.
- `adapter/repository/mysql/user_repository.go.tmpl` lines 75–76 (unconvert): delete the redundant `int32(...)`/`int64(...)` wrappers around values that already have that type.

- [x] **Step 11: Point goimports at the project module**

In `templates/golangci.yaml.tmpl` under `formatters.settings.goimports.local-prefixes` replace `- github.com/my/project` with:

```yaml
        - {{.ModuleName}}
```

Re-render one project and run `golangci-lint run`; if goimports now reports project imports grouped with third-party ones, move them into their own trailing import group in the owning template (the repository, DI, and transport templates already follow that layout).

- [x] **Step 12: Verify with the real tools, alone**

Run: `go test -count=1 -run TestGeneratedProjectBootstrap ./internal/generator`

Expected: all four cases PASS including `golangci-lint run`.

Run: `go test -short ./... && make lint`

Expected: PASS, `0 issues.`

Run (alone): `make test-all`

Expected: PASS.

- [ ] **Step 13: Commit**

Stage every template changed in this task plus `internal/generator/generator_test.go` and ask the user to run `/grimoire-core:commit`. Suggested message: `fix(templates): generated projects pass their own golangci-lint on first commit`.

---

### Task 7: Documentation sync for phase 1

**Files:**

- Modify: `CLAUDE.md` ("Working principles" item 4; "Rendering strategy" paragraph)
- Modify: `docs/01-cli-options.md` (database `none` row)
- Modify: `docs/02-project-layout.md` (add `internal/adapter/repository/memory/` and note `dbgen/`, `wire_gen.go` as `make gen` outputs)

- [x] **Step 1: CLAUDE.md**

In item 4 of "Working principles", directly after the link to `internal/generator/generator_test.go`, append this text:

```text
plus `TestGeneratedProjectBootstrap`, which runs the real `make gen` → `go build` → `golangci-lint run` sequence on four representative projects (sequential; skips when a tool is missing)
```

In "Rendering strategy", after the sentence that ends with the `toolingFiles` list, add this sentence:

```text
Every rendered `.go` file is passed through `go/format` before writing (`formatGoSource`), so conditional template lines cannot leave the output un-gofmt'd.
```

- [x] **Step 2: docs/01-cli-options.md**

Where the database options are described, state that `none` renders an in-memory `UserRepository` (`internal/adapter/repository/memory`) so the User CRUD still runs, and that data does not survive a restart.

- [x] **Step 3: docs/02-project-layout.md**

Add `internal/adapter/repository/memory/user_repository.go` with the note `only when --database=none`, and add a short paragraph that `internal/adapter/repository/<db>/dbgen/` and `internal/infrastructure/di/wire_gen.go` are `make gen` outputs, not rendered by nova.

- [x] **Step 4: Lint the Markdown**

Run: `npx --yes markdownlint-cli2 CLAUDE.md docs/01-cli-options.md docs/02-project-layout.md`

Expected: `Summary: 0 issues`.

- [ ] **Step 5: Commit**

Stage `CLAUDE.md docs/01-cli-options.md docs/02-project-layout.md` and ask the user to run `/grimoire-core:commit`. Suggested message: `docs: describe gofmt-on-write, the bootstrap test, and the in-memory repository`.

---

## Self-review notes

- Spec coverage: gofmt drift (Task 1), real-toolchain proof (Tasks 2, 5, 6), MySQL DDL (Task 3), no-DB compile and DI (Task 4), bootstrap order and CLI text and `WIRE_PATH` (Task 5), every lint hit observed in the review (Task 6), docs that the template changes contradict (Task 7). The matrix row for `database=none` lives in Task 4.
- Not in this phase, by design: option validation and `--docker=false` (phase 2), the `QueryGen` left at `sqlc` for no-DB projects by the CLI default (phase 2, `Validate()` normalizes it; `noDBMatrixConfig` sets it empty so the matrix is unaffected).
- Type consistency: `formatGoSource(outPath string, src []byte) ([]byte, error)` is used with the same signature in Tasks 1; `bootstrapProject(t, cfg)` keeps its signature across Tasks 2, 5, 6; `noDBMatrixConfig(framework, di string)` matches `httpMatrixConfig`'s parameter style.
