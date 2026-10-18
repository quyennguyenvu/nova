# Cron Transport Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `nova new --transport cron` render a run-once cron service (`myapp cron <job>`) that reaches the database, cache and broker through the usual layers, with a reference `user-scan` job.

**Architecture:** A cron project gets a `cron` parent command with one cobra command per job (`cmd/cron/`), a shared lifecycle in `internal/app/cron.go`, a `transport/cronjob` package (the `Job` interface plus a `Runner` that applies timeout, span, run id, panic guard and one outcome line), and one wire injector and one fx injector per job returning a shared `CronApp`. The reference job walks users in keyset pages through a narrow `usecase/userscan` that depends only on `domain.UserRepository`, so the binary never needs JWT keys. The spec is [docs/superpowers/specs/2026-10-18-cron-transport-design.md](../specs/2026-10-18-cron-transport-design.md).

**Tech Stack:** Go `text/template` generator (nova), cobra, Google Wire, Uber fx, OpenTelemetry trace, `github.com/google/uuid`, zerolog behind `observability.Logger`.

## Global Constraints

- Transport value `cron`; CLI shape `myapp cron <job>`; the reference job is `user-scan` with one flag `--after-id` (int64).
- Package names: `cmd/cron` is package `cron`; `internal/transport/cronjob` is package `cronjob`; `internal/transport/cronjob/user` is package `user`, imported as `usercron`; `internal/usecase/userscan` is package `userscan`, imported as `userscansvc`.
- `batchSize` is `500`; `cronjob` default timeout is `30 * time.Minute`; config key `cron.timeout` (env `CRON_TIMEOUT`) defaults to `30m` in `base.yaml`; `cron.spec` / `CRON_SPEC` are removed.
- Outcome log lines, exactly: `cron: job finished` (Info), `cron: job failed` (Error), `cron: job interrupted` (Warn).
- Exit code 0 when the job completed, 1 otherwise; nothing in the cron path calls `panic` or `os.Exit` itself.
- No in-process scheduler, lock or retry.
- Heavy toolchain commands (`make test-all`, `golangci-lint run`, `go build`/`go vet`/`wire` on a rendered project) run one at a time from the main thread, never in parallel subagents and never while another is running (repo `CLAUDE.md`, "Resource limits").
- No agent commits. Each task ends with a suggested Conventional Commit message; the user runs `/grimoire-core:commit` when they choose. Never add a `Co-Authored-By` trailer.
- Every Markdown file touched passes `npx --yes markdownlint-cli2 <file>` with the repo's `.markdownlint.json`; never hard-wrap prose.
- Code comments explain why, not what; match the surrounding file's density.
- Rendered Go must pass the generated project's strict `.golangci.yaml`: no always-nil error returns (`unparam`), no shadowed `err`/`ctx` (`govet shadow strict`), `t.Context()` in tests (`usetesting`), lines ≤ 120.

### Render check (RC)

Several tasks verify templates by rendering a throwaway project. Run this as one command from the repo root; `DI` defaults to `fx`, `DB` to `none` (prefix `DI=wire` or `DB=postgres` to change them):

```bash
make build && NOVA="$PWD/bin/nova" && CHECK="${TMPDIR:-/tmp}/nova-cron-check" && rm -rf "$CHECK" && mkdir -p "$CHECK" && (cd "$CHECK" && "$NOVA" new crontest --module=crontest --transport=cron --database="${DB:-none}" --cache=none --search=none --queue=none --di="${DI:-fx}" --docker=false --ci=none) && cd "$CHECK/crontest" && make gen
```

The project lands in `${TMPDIR:-/tmp}/nova-cron-check/crontest`. Its `make gen` runs the generators in the order that works (sqlc for a SQL `DB`, then `go mod tidy`, then wire when `DI=wire`).

---

### Task 1: Accept `cron` as a transport

**Files:**

- Modify: `internal/config/options.go:18`
- Modify: `internal/config/options_test.go:20,128,134`
- Modify: `cmd/new.go:131-147` (`nextSteps`)
- Modify: `cmd/new_test.go:17-21`
- Modify: `internal/generator/docs_test.go:150-166` (`assertRejectedValues`)
- Modify: `docs/01-cli-options.md:13,33,35,36`
- Modify: `README.md:54,77,91`

**Interfaces:**

- Consumes: nothing.
- Produces: `config.Transports` contains `"cron"`; `(*ProjectConfig).HasCron()` (already exists) is now reachable through `Validate`. `nextSteps` ends with `go run main.go cron user-scan` for cron.

- [ ] **Step 1: Write the failing tests**

In `internal/config/options_test.go`, delete this line from `TestValidateRejectsUnimplemented`:

```go
		"transport cron":       func(c *ProjectConfig) { c.Transport = "cron" },
```

In `TestValidateErrorNamesEveryProblem`, change `cfg.Transport = "cron"` to `cfg.Transport = "cli"` and the wanted substring `` `transport "cron"` `` to `` `transport "cli"` ``.

Append to the same file:

```go
// TestValidateAcceptsCron pins the cron transport's open shape: unlike the
// worker it needs no broker or SQL engine, and it takes no HTTP framework.
func TestValidateAcceptsCron(t *testing.T) {
	t.Parallel()
	for _, db := range []string{"postgres", "mysql", "none"} {
		t.Run(db, func(t *testing.T) {
			t.Parallel()
			cfg := DefaultConfig()
			cfg.Transport = "cron"
			cfg.Database = db
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate cron/%s: %v", db, err)
			}
			if cfg.HTTPFramework != "" {
				t.Errorf("cron kept http-framework %q", cfg.HTTPFramework)
			}
		})
	}
	withFramework := DefaultConfig()
	withFramework.Transport = "cron"
	withFramework.HTTPFramework = "fiber"
	if err := withFramework.Validate(); err == nil {
		t.Error("Validate accepted --http-framework on cron")
	}
}
```

In `cmd/new_test.go`, add a row to the `TestNextStepsPerTransport` table:

```go
		{"cron", "go run main.go cron user-scan"},
```

In `internal/generator/docs_test.go` `assertRejectedValues`, change the first table row from

```go
		{"Transport", "cron"}, {"Transport", "cli"}, {"ConfigFormat", "toml"}, {"ConfigFormat", "env"},
```

to

```go
		{"Transport", "cli"}, {"ConfigFormat", "toml"}, {"ConfigFormat", "env"},
```

and in the `mustContainAll` call below it replace `` "`cron` and `cli` transports" `` with `` "the `cli` transport" ``.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/ ./cmd/ -run 'TestValidate|TestNextSteps' -v`
Expected: FAIL. `TestValidateAcceptsCron/*` reports `transport "cron" is not supported`; `TestNextStepsPerTransport/cron` reports `missing "go run main.go cron user-scan"`.

Run: `go test -short -run TestDocsClaims ./internal/generator/`
Expected: FAIL. docs/01 lacks "the `cli` transport".

- [ ] **Step 3: Implement**

`internal/config/options.go`:

```go
	Transports     = []string{"http", "grpc", "worker", "cron"}
```

`cmd/new.go`, in `nextSteps`, add a case before `default`:

```go
	case cfg.HasCron():
		steps = append(steps, "go run main.go cron user-scan")
```

`docs/01-cli-options.md`:

- Line 13, replace the row with (27 spaces after `` `cron` `` keep the column):

```text
| Transport layer       | `--transport`      | `http`, `grpc`, `worker`, `cron`                           |
```

- Line 33: replace `` `transport "cron" is not supported (valid: http, grpc, worker)` `` with `` `transport "cli" is not supported (valid: http, grpc, worker, cron)` ``.
- Line 35: replace `` the `cron` and `cli` transports, `` with `` the `cli` transport, ``.
- Line 36: after `(its audit trail is stored there).` insert `` A `cron` service needs neither: any `--database` (including `none`), cache and queue work. ``

`README.md`:

- Line 54: replace `HTTP / gRPC / worker handlers + middleware` with `HTTP / gRPC / worker / cron handlers + middleware`.
- Line 77, replace the row with:

```text
| `--transport`      | `http`, `grpc`, `worker`, `cron`         | _(none — must be set)_    |
```

- Line 91: replace `` (`cron`/`cli` transports, `` with `` (the `cli` transport, ``.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/ ./cmd/ -run 'TestValidate|TestNextSteps' -v`
Expected: PASS.

Run: `go test -short -run 'TestDocsClaims|TestDocsLinksResolve' ./internal/generator/`
Expected: PASS.

Run: `npx --yes markdownlint-cli2 README.md docs/01-cli-options.md`
Expected: `Summary: 0 issues in 0 files`.

- [ ] **Step 5: Checkpoint**

Run: `make test`
Expected: PASS. Suggested commit: `feat(config): accept cron as a transport`.

---

### Task 2: `userscan` usecase

**Files:**

- Create: `internal/generator/templates/usecase/userscan/dto.go.tmpl`
- Create: `internal/generator/templates/usecase/userscan/service.go.tmpl`
- Create: `internal/generator/templates/usecase/userscan/service_test.go.tmpl`
- Modify: `internal/generator/generator.go` (`usecaseFiles`)
- Test: `internal/generator/generator_test.go` (new `cronMatrixConfig`, new `TestCronTransportFiles`)

**Interfaces:**

- Consumes: `domain.UserRepository.ListAfter(ctx context.Context, cursor int64, limit int32) ([]*entity.User, error)` (exists).
- Produces: package `userscan` with `type ScanInput struct{ AfterID int64 }`, `type ScanResult struct{ Scanned, LastID int64 }`, `func NewService(users domain.UserRepository) *Service`, `func (s *Service) Scan(ctx context.Context, in ScanInput) (*ScanResult, error)`. Test helper `cronMatrixConfig(database, di string) *config.ProjectConfig` and test `TestCronTransportFiles` with a `cronOnly` slice later tasks extend.

- [ ] **Step 1: Write the failing generator test**

Add next to `grpcMatrixConfig` in `internal/generator/generator_test.go`:

```go
// cronMatrixConfig is the run-once cron transport with no cache, search or
// broker, so a row stays cheap; Validate blanks the SQL fields for "none".
func cronMatrixConfig(database, di string) *config.ProjectConfig {
	cfg := baseMatrixConfig(database, di)
	cfg.Transport = "cron"
	cfg.Cache = "none"
	cfg.Search = "none"
	cfg.MessageQueue = "none"
	return cfg
}
```

Add the test:

```go
// TestCronTransportFiles pins what a cron render emits, and that neither the
// cron files nor another transport's files leak across renders.
func TestCronTransportFiles(t *testing.T) {
	t.Parallel()
	cronOnly := []string{
		"internal/usecase/userscan/service.go",
		"internal/usecase/userscan/dto.go",
		"internal/usecase/userscan/service_test.go",
	}
	cronDir := renderProject(t, cronMatrixConfig("postgres", "wire"))
	for _, rel := range cronOnly {
		if _, err := os.Stat(filepath.Join(cronDir, rel)); err != nil {
			t.Errorf("cron render lacks %s", rel)
		}
	}
	for _, rel := range []string{
		"internal/transport/http", "internal/transport/grpc", "internal/transport/worker",
		"internal/usecase/useraudit", "internal/infrastructure/server",
	} {
		if _, err := os.Stat(filepath.Join(cronDir, rel)); !os.IsNotExist(err) {
			t.Errorf("cron render has %s", rel)
		}
	}
	for name, cfg := range map[string]*config.ProjectConfig{
		"http":   httpMatrixConfig("fiber", "postgres", "wire"),
		"grpc":   grpcMatrixConfig("wire"),
		"worker": workerMatrixConfig("postgres", "kafka", "wire"),
	} {
		dir := renderProject(t, cfg)
		for _, rel := range cronOnly {
			if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
				t.Errorf("%s render has %s", name, rel)
			}
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -short -run TestCronTransportFiles ./internal/generator/`
Expected: FAIL with `cron render lacks internal/usecase/userscan/service.go` (and the other two).

- [ ] **Step 3: Write the generated test first**

Create `internal/generator/templates/usecase/userscan/service_test.go.tmpl`:

```go
package userscan

import (
	"context"
	"errors"
	"slices"
	"testing"

	"{{.ModuleName}}/internal/domain"
	"{{.ModuleName}}/internal/domain/entity"
)

var errDB = errors.New("db down")

// pagedRepo serves users 1..total through ListAfter and records each cursor.
type pagedRepo struct {
	domain.UserRepository // only ListAfter is called

	total   int64
	failOn  int                // 1-based ListAfter call that fails; 0 never fails
	cancel  context.CancelFunc // called after the first page: a shutdown mid-scan
	cursors []int64
}

func (r *pagedRepo) ListAfter(_ context.Context, cursor int64, limit int32) ([]*entity.User, error) {
	r.cursors = append(r.cursors, cursor)
	if len(r.cursors) == r.failOn {
		return nil, errDB
	}
	var out []*entity.User
	for id := cursor + 1; id <= r.total && len(out) < int(limit); id++ {
		out = append(out, &entity.User{ID: id})
	}
	if r.cancel != nil && len(r.cursors) == 1 {
		r.cancel()
	}
	return out, nil
}

func TestScanWalksEveryPage(t *testing.T) {
	repo := &pagedRepo{total: 2*batchSize + 1}
	res, err := NewService(repo).Scan(t.Context(), ScanInput{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if res.Scanned != repo.total || res.LastID != repo.total {
		t.Errorf("result = %+v, want Scanned and LastID %d", res, repo.total)
	}
	if want := []int64{0, batchSize, 2 * batchSize}; !slices.Equal(repo.cursors, want) {
		t.Errorf("cursors = %v, want %v", repo.cursors, want)
	}
}

func TestScanResumesAfterID(t *testing.T) {
	repo := &pagedRepo{total: 10}
	res, err := NewService(repo).Scan(t.Context(), ScanInput{AfterID: 7})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if res.Scanned != 3 || res.LastID != 10 {
		t.Errorf("result = %+v, want Scanned 3 and LastID 10", res)
	}
}

func TestScanErrorKeepsResumePoint(t *testing.T) {
	repo := &pagedRepo{total: 2 * batchSize, failOn: 2}
	res, err := NewService(repo).Scan(t.Context(), ScanInput{})
	if !errors.Is(err, errDB) {
		t.Fatalf("Scan error = %v, want errDB", err)
	}
	if res.Scanned != batchSize || res.LastID != batchSize {
		t.Errorf("result = %+v, want the first page as the resume point", res)
	}
}

func TestScanStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	repo := &pagedRepo{total: 2 * batchSize, cancel: cancel}
	res, err := NewService(repo).Scan(ctx, ScanInput{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan error = %v, want context.Canceled", err)
	}
	if len(repo.cursors) != 1 || res.LastID != batchSize {
		t.Errorf("cursors = %v, LastID = %d; want one page then stop", repo.cursors, res.LastID)
	}
}
```

Add to `usecaseFiles` in `internal/generator/generator.go`, after the `useraudit` entries:

```go
		// userscan usecase — cron-only. The reference job's keyset batch walk.
		{"templates/usecase/userscan/dto.go.tmpl", "internal/usecase/userscan/dto.go", cfg.HasCron()},
		{"templates/usecase/userscan/service.go.tmpl", "internal/usecase/userscan/service.go", cfg.HasCron()},
		{
			"templates/usecase/userscan/service_test.go.tmpl",
			"internal/usecase/userscan/service_test.go",
			cfg.HasCron(),
		},
```

Create `dto.go.tmpl` and `service.go.tmpl` as empty-but-valid stubs so the render succeeds and the generated test fails to compile:

```go
package userscan
```

- [ ] **Step 4: Run the generated test to verify it fails**

Run: the [Render check](#render-check-rc), then `go test ./internal/usecase/userscan/...`
Expected: FAIL to compile: `undefined: batchSize`, `undefined: NewService`, `undefined: ScanInput`.

- [ ] **Step 5: Write the implementation**

Replace `internal/generator/templates/usecase/userscan/dto.go.tmpl` with:

```go
package userscan

// ScanInput is where a scan starts. AfterID 0 starts from the first user; a
// failed run's ScanResult.LastID resumes it.
type ScanInput struct {
	AfterID int64
}

// ScanResult reports how far a scan got. LastID is the last user id read, so
// it is the resume point even when Scan returns an error.
type ScanResult struct {
	Scanned int64
	LastID  int64
}
```

Replace `internal/generator/templates/usecase/userscan/service.go.tmpl` with:

```go
// Package userscan is the cron transport's reference usecase: it walks every
// user in keyset pages. Copy its loop for any job that must touch many rows
// without one long transaction or the whole table in memory. It depends only
// on domain.UserRepository, so a cron binary never builds the user CRUD
// service or its JWT signer.
package userscan

import (
	"context"

	"{{.ModuleName}}/internal/domain"
	"{{.ModuleName}}/pkg/errors"
	"{{.ModuleName}}/pkg/logctx"
)

// batchSize bounds each page; one page is one round-trip.
const batchSize = 500

type Service struct {
	users domain.UserRepository
}

func NewService(users domain.UserRepository) *Service {
	return &Service{users: users}
}

// Scan walks users in id order, batchSize at a time, starting after
// in.AfterID, and stops at the first short page. It checks ctx between pages
// so a shutdown or deadline ends it cleanly. On error it still returns the
// progress so far: LastID is where a rerun resumes.
func (s *Service) Scan(ctx context.Context, in ScanInput) (*ScanResult, error) {
	res := &ScanResult{LastID: in.AfterID}
	for {
		if err := ctx.Err(); err != nil {
			return res, errors.Wrapf(err, "scan users after id=%d", res.LastID)
		}
		batch, err := s.users.ListAfter(ctx, res.LastID, batchSize)
		if err != nil {
			return res, errors.Wrapf(err, "scan users after id=%d", res.LastID)
		}
		// A real job's per-batch business goes here (update, publish, notify).
		// Keep it idempotent: a resumed run may see a page again.
		res.Scanned += int64(len(batch))
		if len(batch) > 0 {
			res.LastID = batch[len(batch)-1].ID
		}
		logctx.From(ctx).Debug("user scan: page done", "last_id", res.LastID, "scanned", res.Scanned)
		if len(batch) < batchSize {
			return res, nil
		}
	}
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: the [Render check](#render-check-rc), then `go test ./internal/usecase/userscan/...`
Expected: `ok  crontest/internal/usecase/userscan`.

Run (from the repo root): `go test -short -run TestCronTransportFiles ./internal/generator/`
Expected: PASS.

- [ ] **Step 7: Checkpoint**

Run: `make test`
Expected: PASS. Suggested commit: `feat(generator): add the userscan usecase for the cron transport`.

---

### Task 3: `transport/cronjob` — Job, Runner and the `user-scan` job

**Files:**

- Create: `internal/generator/templates/transport/cronjob/job.go.tmpl`
- Create: `internal/generator/templates/transport/cronjob/runner.go.tmpl`
- Create: `internal/generator/templates/transport/cronjob/runner_test.go.tmpl`
- Create: `internal/generator/templates/transport/cronjob/user/scan.go.tmpl`
- Modify: `internal/generator/generator.go` (new `cronFiles`, `buildFileList`)
- Test: `internal/generator/generator_test.go` (`TestCronTransportFiles`, `TestTransportLoggerPortDecoupling`)

**Interfaces:**

- Consumes: `userscansvc.Service.Scan`, `userscansvc.ScanInput` (Task 2); `logctx.With/From`, `requestid.With`, `observability.Logger`, `errors.Wrapf/LogFields/New` (exist).
- Produces, package `cronjob`: `type Job interface { Name() string; Run(ctx context.Context) error }`; `type Policy struct{ Timeout time.Duration }`; `var ErrJobPanic`; `func NewRunner(log observability.Logger, tracer trace.Tracer, policy Policy) *Runner`; `func (r *Runner) Run(ctx context.Context, job Job) error`. Package `usercron`: `type ScanFlags struct{ AfterID int64 }`; `func NewScanJob(scan *userscansvc.Service, flags ScanFlags) *ScanJob`; `(*ScanJob).Name() == "user-scan"`.

- [ ] **Step 1: Write the failing generator tests**

In `TestCronTransportFiles`, extend `cronOnly`:

```go
		"internal/transport/cronjob/job.go",
		"internal/transport/cronjob/runner.go",
		"internal/transport/cronjob/runner_test.go",
		"internal/transport/cronjob/user/scan.go",
```

In `TestTransportLoggerPortDecoupling`, add after `workerFiles`:

```go
	cronFiles := []string{"internal/transport/cronjob/runner.go"}
```

and a subtest after `fx_worker`:

```go
	t.Run("wire_cron", func(t *testing.T) {
		t.Parallel()
		dir := render(t, cronMatrixConfig("postgres", "wire"))
		assertDecoupled(t, dir, cronFiles...)
	})
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -short -run 'TestCronTransportFiles|TestTransportLoggerPortDecoupling' ./internal/generator/`
Expected: FAIL with `cron render lacks internal/transport/cronjob/job.go` and `read .../runner.go: no such file`.

- [ ] **Step 3: Write the generated tests first**

Create `internal/generator/templates/transport/cronjob/runner_test.go.tmpl`:

```go
package cronjob

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"{{.ModuleName}}/pkg/observability"
	"{{.ModuleName}}/pkg/requestid"
)

var errJob = errors.New("job failed")

// funcJob adapts a function to Job.
type funcJob func(ctx context.Context) error

func (funcJob) Name() string { return "test-job" }

func (f funcJob) Run(ctx context.Context) error { return f(ctx) }

// recLogger records each line as "<level> <msg>".
type recLogger struct{ lines *[]string }

func (l recLogger) Debug(msg string, _ ...any)             { l.add("debug", msg) }
func (l recLogger) Info(msg string, _ ...any)              { l.add("info", msg) }
func (l recLogger) Warn(msg string, _ ...any)              { l.add("warn", msg) }
func (l recLogger) Error(msg string, _ ...any)             { l.add("error", msg) }
func (l recLogger) With(string, any) observability.Logger { return l }
func (l recLogger) add(level, msg string)                  { *l.lines = append(*l.lines, level+" "+msg) }

func newTestRunner(log observability.Logger) *Runner {
	return NewRunner(log, noop.NewTracerProvider().Tracer("test"), Policy{Timeout: 50 * time.Millisecond})
}

func TestRunOutcomes(t *testing.T) {
	cases := []struct {
		name     string
		job      funcJob
		wantErr  error // nil for success
		wantLine string
	}{
		{"success", func(context.Context) error { return nil }, nil, "info cron: job finished"},
		{"job error passes through", func(context.Context) error { return errJob }, errJob, "error cron: job failed"},
		{
			"timeout ends the run",
			func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
			context.DeadlineExceeded,
			"error cron: job failed",
		},
		{
			"panic becomes an error",
			func(context.Context) error { panic("boom") },
			ErrJobPanic,
			"error cron: job failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var lines []string
			err := newTestRunner(recLogger{&lines}).Run(t.Context(), tc.job)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Run error = %v, want %v", err, tc.wantErr)
			}
			if !slices.Equal(lines, []string{tc.wantLine}) {
				t.Errorf("log lines = %q, want [%q]", lines, tc.wantLine)
			}
		})
	}
}

// TestRunInterrupted: a parent ctx cancelled mid-run (SIGTERM) is reported as
// interrupted, not failed.
func TestRunInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	job := funcJob(func(runCtx context.Context) error {
		cancel()
		<-runCtx.Done()
		return runCtx.Err()
	})
	var lines []string
	err := newTestRunner(recLogger{&lines}).Run(ctx, job)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
	if !slices.Equal(lines, []string{"warn cron: job interrupted"}) {
		t.Errorf("log lines = %q, want the interrupted line", lines)
	}
}

// TestRunScopesTheRun: the job's ctx carries the run id that correlates its
// logs and outbound calls.
func TestRunScopesTheRun(t *testing.T) {
	var runID string
	job := funcJob(func(ctx context.Context) error {
		runID = requestid.From(ctx)
		return nil
	})
	if err := newTestRunner(observability.Nop()).Run(t.Context(), job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if runID == "" {
		t.Error("job ctx carries no run id")
	}
}

func TestPolicyDefaults(t *testing.T) {
	r := NewRunner(observability.Nop(), noop.NewTracerProvider().Tracer("test"), Policy{})
	if r.policy.Timeout != defaultTimeout {
		t.Errorf("zero Policy timeout = %v, want %v", r.policy.Timeout, defaultTimeout)
	}
}
```

Add to `internal/generator/generator.go`, after `workerFiles`:

```go
// cronFiles registers the run-once cron transport: the Job contract, the
// Runner that applies the run policy, and the reference user-scan job.
func (g *Generator) cronFiles() []templateFile {
	if !g.cfg.HasCron() {
		return nil
	}
	return []templateFile{
		{"templates/transport/cronjob/job.go.tmpl", "internal/transport/cronjob/job.go", true},
		{"templates/transport/cronjob/runner.go.tmpl", "internal/transport/cronjob/runner.go", true},
		// Fake-job tests of the run policy; the bootstrap runs them.
		{"templates/transport/cronjob/runner_test.go.tmpl", "internal/transport/cronjob/runner_test.go", true},
		{"templates/transport/cronjob/user/scan.go.tmpl", "internal/transport/cronjob/user/scan.go", true},
	}
}
```

and in `buildFileList`, after `files = append(files, g.workerFiles()...)`:

```go
	files = append(files, g.cronFiles()...)
```

Create `job.go.tmpl`, `runner.go.tmpl` and `user/scan.go.tmpl` as one-line stubs (`package cronjob`, `package cronjob`, `package user`).

- [ ] **Step 4: Run the generated tests to verify they fail**

Run: the [Render check](#render-check-rc), then `go test ./internal/transport/cronjob/...`
Expected: FAIL to compile: `undefined: Runner`, `undefined: NewRunner`, `undefined: ErrJobPanic`.

- [ ] **Step 5: Write the implementation**

`internal/generator/templates/transport/cronjob/job.go.tmpl`:

```go
// Package cronjob is the run-once cron transport: `cron <job>` boots the DI
// graph for one Job, Runner applies the run policy, and the process exits 0
// when the job completed or 1 when it did not. The schedule lives in the
// deploy layer (a Kubernetes CronJob or crontab), not in this binary.
package cronjob

import "context"

// Job is one unit of scheduled work. Run must be idempotent (the scheduler
// may retry a failed run or start a second one) and must return once ctx is
// done.
type Job interface {
	// Name is the job's log and span name, matching its cron subcommand.
	Name() string
	Run(ctx context.Context) error
}
```

`internal/generator/templates/transport/cronjob/runner.go.tmpl`:

```go
package cronjob

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"{{.ModuleName}}/pkg/errors"
	"{{.ModuleName}}/pkg/logctx"
	"{{.ModuleName}}/pkg/observability"
	"{{.ModuleName}}/pkg/requestid"
)

// defaultTimeout bounds a run when Policy.Timeout is unset.
const defaultTimeout = 30 * time.Minute

// ErrJobPanic wraps a recovered job panic, so DI cleanup still runs and the
// process exits non-zero instead of crashing.
var ErrJobPanic = errors.New("job panic")

// Policy bounds one run. The zero value takes the defaults.
type Policy struct {
	// Timeout is the run's deadline; the job's ctx expires at it.
	Timeout time.Duration
}

func (p Policy) withDefaults() Policy {
	if p.Timeout <= 0 {
		p.Timeout = defaultTimeout
	}
	return p
}

// Runner applies the Policy to one run: it scopes a run id and a logger into
// ctx, opens a root span, enforces the deadline, turns a panic into an error,
// and writes exactly one outcome line — the cron counterpart of the worker's
// per-message policy.
type Runner struct {
	log    observability.Logger
	tracer trace.Tracer
	policy Policy
}

func NewRunner(log observability.Logger, tracer trace.Tracer, policy Policy) *Runner {
	return &Runner{log: log, tracer: tracer, policy: policy.withDefaults()}
}

// Run runs job once and returns its error. A cancelled parent ctx (SIGTERM)
// is logged as interrupted rather than failed: the scheduler stopped the run,
// the job did not break.
func (r *Runner) Run(ctx context.Context, job Job) error {
	runID := uuid.New().String()
	ctx, span := r.tracer.Start(ctx, "cron "+job.Name())
	defer span.End()

	scoped := r.log.
		With("job", job.Name()).
		With("run_id", runID).
		With("trace_id", traceIDOr(span.SpanContext()))
	ctx = logctx.With(requestid.With(ctx, runID), scoped)

	start := time.Now()
	err := r.run(ctx, job)
	fields := []any{"duration_ms", time.Since(start).Milliseconds()}
	if err == nil {
		scoped.Info("cron: job finished", fields...)
		return nil
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	fields = append(fields, errors.LogFields(err)...)
	if ctx.Err() != nil {
		scoped.Warn("cron: job interrupted", fields...)
	} else {
		scoped.Error("cron: job failed", fields...)
	}
	return err
}

// run applies the deadline and the panic guard to one call of the job.
func (r *Runner) run(ctx context.Context, job Job) error {
	runCtx, cancel := context.WithTimeout(ctx, r.policy.Timeout)
	defer cancel()
	var err error
	func() {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("%w: %v", ErrJobPanic, p)
			}
		}()
		err = job.Run(runCtx)
	}()
	return err
}

func traceIDOr(sc trace.SpanContext) string {
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
```

`internal/generator/templates/transport/cronjob/user/scan.go.tmpl`:

```go
// Package user holds the cron jobs over the user aggregate.
package user

import (
	"context"

	userscansvc "{{.ModuleName}}/internal/usecase/userscan"
	"{{.ModuleName}}/pkg/errors"
	"{{.ModuleName}}/pkg/logctx"
)

// ScanFlags is the user-scan command's input. It is a named type so the DI
// graph can inject it into NewScanJob.
type ScanFlags struct {
	// AfterID resumes a scan after this user id; 0 starts from the first.
	AfterID int64
}

// ScanJob walks every user through userscansvc.Service.Scan.
type ScanJob struct {
	scan  *userscansvc.Service
	flags ScanFlags
}

func NewScanJob(scan *userscansvc.Service, flags ScanFlags) *ScanJob {
	return &ScanJob{scan: scan, flags: flags}
}

func (*ScanJob) Name() string { return "user-scan" }

// Run scans once. A failed run names the --after-id that resumes it.
func (j *ScanJob) Run(ctx context.Context) error {
	res, err := j.scan.Scan(ctx, userscansvc.ScanInput{AfterID: j.flags.AfterID})
	if err != nil {
		return errors.Wrapf(err, "user-scan: resume with --after-id=%d", res.LastID)
	}
	logctx.From(ctx).Info("user-scan: done", "scanned", res.Scanned, "last_id", res.LastID)
	return nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: the [Render check](#render-check-rc), then `go test ./internal/transport/cronjob/... ./internal/usecase/userscan/...`
Expected: both packages `ok`.

Run (from the repo root): `go test -short -run 'TestCronTransportFiles|TestTransportLoggerPortDecoupling' ./internal/generator/`
Expected: PASS.

- [ ] **Step 7: Checkpoint**

Run: `make test`
Expected: PASS. Suggested commit: `feat(generator): add the cronjob transport runner and user-scan job`.

---

### Task 4: Config and DI wiring

**Files:**

- Modify: `internal/generator/templates/infrastructure/config/config.go.tmpl` (time import, `CronConfig`, `Summary`)
- Modify: `internal/generator/templates/infrastructure/config/base.yaml.tmpl` (`cron:` block)
- Modify: `internal/generator/templates/infrastructure/di/app.go.tmpl`
- Modify: `internal/generator/templates/infrastructure/di/wire.go.tmpl`
- Modify: `internal/generator/templates/infrastructure/di/wire_provider.go.tmpl`
- Modify: `internal/generator/templates/infrastructure/di/fx.go.tmpl`
- Modify: `internal/generator/generator.go` (`infrastructureFiles` gates)
- Test: `internal/generator/generator_test.go` (`TestFxDISelection`, `TestTransportLoggerPortDecoupling`, new `TestCronWiring`)

**Interfaces:**

- Consumes: `cronjob.NewRunner`, `cronjob.Policy`, `cronjob.Job`, `usercron.NewScanJob`, `usercron.ScanFlags`, `userscansvc.NewService` (Tasks 2–3).
- Produces: `config.CronConfig{Timeout time.Duration}`; `di.CronApp{Logger *logger.Logger; Tracer trace.Tracer; Runner *cronjob.Runner; Job cronjob.Job}`; `func provideCronPolicy(cfg *config.Config) cronjob.Policy`; `func di.InitializeUserScanJob(ctx context.Context, flags usercron.ScanFlags) (*CronApp, func(), error)` under both wire and fx.

- [ ] **Step 1: Write the failing generator tests**

In `TestFxDISelection`, add a case:

```go
		{"cron", cronMatrixConfig("postgres", "fx"), "func InitializeUserScanJob("},
```

In `TestTransportLoggerPortDecoupling`, add after `wire_cron`:

```go
	t.Run("fx_cron", func(t *testing.T) {
		t.Parallel()
		dir := render(t, cronMatrixConfig("none", "fx"))
		assertDecoupled(t, dir, cronFiles...)
		assertFxCaster(t, dir)
	})
```

Add:

```go
// TestCronWiring pins the cron DI seams vet cannot see: wire.go is
// build-tagged and fx resolves its graph only at Start.
func TestCronWiring(t *testing.T) {
	t.Parallel()
	t.Run("wire", func(t *testing.T) {
		t.Parallel()
		dir := renderProject(t, cronMatrixConfig("postgres", "wire"))
		diDir := filepath.Join(dir, "internal/infrastructure/di")
		mustContainAll(t, readFile(t, filepath.Join(diDir, "wire.go")),
			"func InitializeUserScanJob(ctx context.Context, flags usercron.ScanFlags) (*CronApp, func(), error)",
			"wire.Bind(new(cronjob.Job), new(*usercron.ScanJob))",
			"cronInfraSet,", "userscansvc.NewService,")
		mustContainAll(t, readFile(t, filepath.Join(diDir, "app.go")),
			"type CronApp struct", "*cronjob.Runner", "cronjob.Job")
		mustContainAll(t, readFile(t, filepath.Join(diDir, "provider.go")),
			"func provideCronPolicy(cfg *config.Config) cronjob.Policy")
		cfgDir := filepath.Join(dir, "internal/infrastructure/config")
		base := readFile(t, filepath.Join(cfgDir, "base.yaml"))
		mustContainAll(t, base, "\ncron:\n", "timeout: 30m")
		mustNotContain(t, base, "\n  spec:")
		mustContainAll(t, readFile(t, filepath.Join(cfgDir, "config.go")),
			"type CronConfig struct", `env:"CRON_TIMEOUT"`, `"cron_timeout"`)
	})
	t.Run("fx", func(t *testing.T) {
		t.Parallel()
		dir := renderProject(t, cronMatrixConfig("none", "fx"))
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/fx.go")),
			"func initializeCron(", "fx.Supply(flags)",
			"fx.Annotate(usercron.NewScanJob, fx.As(new(cronjob.Job)))",
			"cronModule(),", "userscansvc.NewService,")
	})
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -short -run 'TestFxDISelection|TestTransportLoggerPortDecoupling|TestCronWiring' ./internal/generator/`
Expected: FAIL (`fx.go` absent for cron, `wire.go missing ...InitializeUserScanJob`, `base.yaml` still has `spec:`).

- [ ] **Step 3: Config**

`config.go.tmpl`, the `time` import condition — replace

```text
{{- if or .HasHTTP .HasGRPC .HasDatabase .HasRedis .HasElasticsearch .HasMessageQueue}}
	"time"
```

with

```text
{{- if or .HasHTTP .HasGRPC .HasCron .HasDatabase .HasRedis .HasElasticsearch .HasMessageQueue}}
	"time"
```

Replace

```go
// CronConfig holds cron job configuration.
type CronConfig struct {
	Spec string `{{.ConfigFormat}}:"spec" env:"CRON_SPEC"`
}
```

with

```go
// CronConfig bounds one cron run; see transport/cronjob.Policy. The schedule
// lives in the deploy layer (Kubernetes CronJob, crontab), not here.
type CronConfig struct {
	Timeout time.Duration `{{.ConfigFormat}}:"timeout" env:"CRON_TIMEOUT"`
}
```

In `Summary`, after the `{{- if .HasWorker}} … {{- end}}` block and before `return fields`, insert:

```text
{{- if .HasCron}}
	fields = append(fields, "cron_timeout", c.Cron.Timeout)
{{- end}}
```

`base.yaml.tmpl`, replace

```text
{{- if .HasCron}}

cron:
  spec: "0 * * * *" # every hour
{{- end}}
```

with

```text
{{- if .HasCron}}

cron:
  # Deadline for one run; the job's ctx expires at it. Keep the scheduler's
  # own limit (Kubernetes activeDeadlineSeconds) a little above it.
  timeout: 30m
{{- end}}
```

- [ ] **Step 4: `app.go.tmpl` and `wire_provider.go.tmpl`**

`app.go.tmpl`, in the import block after the `{{- if .HasWorker}} … {{- end}}` import, insert:

```text
{{- if .HasCron}}
	"{{.ModuleName}}/internal/transport/cronjob"
{{- end}}
```

Append at the end of the file:

```text
{{- if .HasCron}}

// CronApp is what every cron job injector returns: the shared run policy plus
// the one job this process runs. Tracer is held so DI builds the tracing
// provider first and, cleaning up in reverse, flushes the run's spans last.
type CronApp struct {
	Logger *logger.Logger
	Tracer trace.Tracer
	Runner *cronjob.Runner
	Job    cronjob.Job
}
{{- end}}
```

`wire_provider.go.tmpl`, in the import block after the worker imports' `{{- end}}`, insert:

```text
{{- if .HasCron}}
	"{{.ModuleName}}/internal/transport/cronjob"
{{- end}}
```

Append at the end of the file:

```text
{{- if .HasCron}}

// provideCronPolicy projects the cron config onto the transport's Policy so
// transport/cronjob stays free of the config package.
func provideCronPolicy(cfg *config.Config) cronjob.Policy {
	return cronjob.Policy{Timeout: cfg.Cron.Timeout}
}
{{- end}}
```

- [ ] **Step 5: `wire.go.tmpl`**

In the import block, after

```text
{{- if .HasWorker}}
	"{{.ModuleName}}/internal/transport/worker"
	workeruser "{{.ModuleName}}/internal/transport/worker/v1/user"
	userauditsvc "{{.ModuleName}}/internal/usecase/useraudit"
{{- end}}
```

insert:

```text
{{- if .HasCron}}
	"{{.ModuleName}}/internal/transport/cronjob"
	usercron "{{.ModuleName}}/internal/transport/cronjob/user"
	userscansvc "{{.ModuleName}}/internal/usecase/userscan"
{{- end}}
```

In the `infraSet` doc comment, replace `so a worker never builds TokenService` with `so a worker or cron job never builds TokenService`, and `// grpcInfraSet / workerInfraSet.` with `// grpcInfraSet / workerInfraSet / cronInfraSet.`

After the `workerInfraSet` block (it ends `provideWorkerPolicy,` / `)` / `{{- end}}`) and before `var adapterSet`, insert:

```text
{{- if .HasCron}}

// cronInfraSet adds the run policy and the Runner every cron injector shares.
var cronInfraSet = wire.NewSet(
	provideCronPolicy,
	cronjob.NewRunner,
)
{{- end}}
```

In `usecaseSet`, after the `userauditsvc.NewService` block, insert:

```text
{{- if .HasCron}}
	userscansvc.NewService,
{{- end}}
```

Append at the end of the file:

```text
{{- if .HasCron}}

// InitializeUserScanJob builds the user-scan job and only what it reaches:
// one injector per job keeps a job off dependencies it never uses. flags
// carries the command's parsed flags into the graph.
func InitializeUserScanJob(ctx context.Context, flags usercron.ScanFlags) (*CronApp, func(), error) {
	panic(wire.Build(
		infraSet,
		cronInfraSet,
		adapterSet,
		usecaseSet,
		usercron.NewScanJob,
		wire.Bind(new(cronjob.Job), new(*usercron.ScanJob)),
		wire.Struct(new(CronApp), "*"),
	))
}
{{- end}}
```

- [ ] **Step 6: `fx.go.tmpl`**

Replace all 7 occurrences of `.HasHTTP .HasGRPC .HasWorker` with `.HasHTTP .HasGRPC .HasWorker .HasCron` (Edit with `replace_all`; `grep -c '.HasHTTP .HasGRPC .HasWorker .HasCron' internal/generator/templates/infrastructure/di/fx.go.tmpl` must print `7`).

In the import block, after the `{{- if .HasWorker}}` import group (the same three worker lines as wire.go), insert the same cron import group as Step 5. In the `--- modules ---` comment, replace `httpInfraSet / grpcInfraSet / workerInfraSet` with `httpInfraSet / grpcInfraSet / workerInfraSet / cronInfraSet`.

Before the line `{{- if or .HasHTTP .HasGRPC .HasWorker .HasCron}}` that precedes `func adapterModule() fx.Option {`, insert:

```text
{{- if .HasCron}}

func cronModule() fx.Option {
	return fx.Provide(
		provideCronPolicy,
		cronjob.NewRunner,
	)
}
{{- end}}

```

In `usecaseModule`, after the `userauditsvc.NewService` block, insert:

```text
{{- if .HasCron}}
		userscansvc.NewService,
{{- end}}
```

Append at the end of the file:

```text
{{- if .HasCron}}

// initializeCron builds one cron job's graph with fx: the shared modules plus
// job, the options that provide that job as cronjob.Job. Each job's
// Initialize<Job>Job is one call to it — the fx counterpart of one wire
// injector per job.
func initializeCron(ctx context.Context, job ...fx.Option) (*CronApp, func(), error) {
	var (
		log    *logger.Logger
		tracer trace.Tracer
		runner *cronjob.Runner
		j      cronjob.Job
		cfg    *config.Config
	)
	fxApp := fx.New(
		fx.NopLogger,
		fx.Provide(func() context.Context { return ctx }),
		infraModule(),
		cronModule(),
		adapterModule(),
		usecaseModule(),
		fx.Options(job...),
		fx.Populate(&log, &tracer, &runner, &j, &cfg),
	)
	if err := fxApp.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("fx: start cron app: %w", err)
	}
	cleanup := func() {
		// The budget is config so an operator can widen it when a dependency
		// close is slow; a stop that overruns it is logged with the budget so
		// the skipped hooks are visible, not silently dropped.
		stopCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if stopErr := fxApp.Stop(stopCtx); stopErr != nil {
			log.Error("fx: stop", "err", stopErr, "timeout_ms", cfg.ShutdownTimeout.Milliseconds())
		}
	}
	return &CronApp{Logger: log, Tracer: tracer, Runner: runner, Job: j}, cleanup, nil
}

// InitializeUserScanJob builds the user-scan job graph; flags carries the
// command's parsed flags into it.
func InitializeUserScanJob(ctx context.Context, flags usercron.ScanFlags) (*CronApp, func(), error) {
	return initializeCron(ctx,
		fx.Supply(flags),
		fx.Provide(fx.Annotate(usercron.NewScanJob, fx.As(new(cronjob.Job)))),
	)
}
{{- end}}
```

- [ ] **Step 7: Generator gates**

In `internal/generator/generator.go` `infrastructureFiles`, replace all 4 occurrences of `cfg.HasHTTP() || cfg.HasGRPC() || cfg.HasWorker()` with `cfg.HasHTTP() || cfg.HasGRPC() || cfg.HasWorker() || cfg.HasCron()` (the `fx.go`, `fx_provider.go`, `provider.go` and `app.go` entries).

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test -short -run 'TestFxDISelection|TestTransportLoggerPortDecoupling|TestCronWiring|TestCronTransportFiles' ./internal/generator/`
Expected: PASS.

Run: the [Render check](#render-check-rc) (fx, no DB), then `go build ./internal/infrastructure/di/`
Expected: no output, exit 0. (`go build ./...` still fails on the missing `cronCommand` until Task 5.)

Run: `DI=wire` + the [Render check](#render-check-rc) (its `make gen` runs wire), then `go build ./internal/infrastructure/di/`
Expected: the render check prints `wire: crontest/internal/infrastructure/di: wrote .../wire_gen.go`; the build is clean. This is the first real wire run over the per-job injector, its `flags` argument and the `Bind`.

- [ ] **Step 9: Checkpoint**

Run: `make test`
Expected: PASS. Suggested commit: `feat(generator): wire the cron transport through config, wire and fx`.

---

### Task 5: Entry points — `cron` command, `user-scan` command, `app/cron.go`

**Files:**

- Modify (rewrite): `internal/generator/templates/cmd/cron.go.tmpl`
- Create: `internal/generator/templates/cmd/cron/user_scan.go.tmpl`
- Create: `internal/generator/templates/app/cron.go.tmpl`
- Modify: `internal/generator/generator.go` (`entryPointFiles`)
- Test: `internal/generator/generator_test.go` (`TestCronTransportFiles`, new `TestCronEntryPoints`)

**Interfaces:**

- Consumes: `di.InitializeUserScanJob`, `di.CronApp` (Task 4); `usercron.ScanFlags` (Task 3). `cmd/root.go.tmpl` already registers `cronCommand()` under `HasCron`.
- Produces: `func cronCommand() *cobra.Command` (package `cmd`); `func cron.UserScan() *cobra.Command`; `func app.RunUserScan(afterID int64) error`.

- [ ] **Step 1: Write the failing generator tests**

In `TestCronTransportFiles`, extend `cronOnly`:

```go
		"cmd/cron.go",
		"cmd/cron/user_scan.go",
		"internal/app/cron.go",
```

Add:

```go
// TestCronEntryPoints pins the run-once lifecycle: signals cancel the run,
// DI boots one job, the Runner's error reaches cobra, and nothing panics.
func TestCronEntryPoints(t *testing.T) {
	t.Parallel()
	dir := renderProject(t, cronMatrixConfig("postgres", "wire"))
	mustContainAll(t, readFile(t, filepath.Join(dir, "cmd/root.go")), "cronCommand(),")
	mustContainAll(t, readFile(t, filepath.Join(dir, "cmd/cron.go")), `Use:   "cron"`, "cron.UserScan(),")
	mustContainAll(t, readFile(t, filepath.Join(dir, "cmd/cron/user_scan.go")),
		`Use:   "user-scan"`, `"after-id"`, "cobra.NoArgs", "app.RunUserScan(afterID)")
	appSrc := readFile(t, filepath.Join(dir, "internal/app/cron.go"))
	mustContainAll(t, appSrc, "signal.NotifyContext(",
		"di.InitializeUserScanJob(ctx, usercron.ScanFlags{AfterID: afterID})",
		"app.Runner.Run(ctx, app.Job)", "shutdown complete")
	mustNotContain(t, appSrc, "panic(", "os.Exit")
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -short -run 'TestCronTransportFiles|TestCronEntryPoints' ./internal/generator/`
Expected: FAIL (`cron render lacks cmd/cron.go`, `read .../cmd/cron/user_scan.go: no such file`).

- [ ] **Step 3: Write the templates**

Replace `internal/generator/templates/cmd/cron.go.tmpl` with:

```go
package cmd

import (
	"github.com/spf13/cobra"

	"{{.ModuleName}}/cmd/cron"
)

// cronCommand groups the run-once jobs: `cron <job>` runs one and exits, so
// the schedule lives in the deploy layer (Kubernetes CronJob, crontab).
func cronCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "Run one scheduled job and exit",
	}
	cmd.AddCommand(
		cron.UserScan(),
	)
	return cmd
}
```

Create `internal/generator/templates/cmd/cron/user_scan.go.tmpl`:

```go
// Package cron holds one cobra command per cron job; cmd/cron.go mounts them
// under `cron`.
package cron

import (
	"github.com/spf13/cobra"

	"{{.ModuleName}}/internal/app"
)

// UserScan is `cron user-scan`: walk every user once, a page at a time.
func UserScan() *cobra.Command {
	var afterID int64
	cmd := &cobra.Command{
		Use:   "user-scan",
		Short: "Walk every user in id order, one page at a time",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return app.RunUserScan(afterID)
		},
	}
	cmd.Flags().Int64Var(&afterID, "after-id", 0, "Resume after this user id (a failed run logs it)")
	return cmd
}
```

Create `internal/generator/templates/app/cron.go.tmpl`:

```go
package app

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"{{.ModuleName}}/internal/infrastructure/di"
	usercron "{{.ModuleName}}/internal/transport/cronjob/user"
)

// RunUserScan runs the user-scan job once.
func RunUserScan(afterID int64) error {
	return runCron(func(ctx context.Context) (*di.CronApp, func(), error) {
		return di.InitializeUserScanJob(ctx, usercron.ScanFlags{AfterID: afterID})
	})
}

// runCron boots one job's DI graph, runs the job once through the Runner, and
// returns its error so cobra exits non-zero. SIGINT/SIGTERM cancel the run's
// ctx: the job returns, DI cleanup still runs (pool closed, spans flushed),
// and the Runner reports the run as interrupted.
func runCron(initialize func(context.Context) (*di.CronApp, func(), error)) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	app, cleanup, err := initialize(ctx)
	if err != nil {
		return fmt.Errorf("initialize cron job: %w", err)
	}
	defer func() {
		start := time.Now()
		cleanup()
		app.Logger.Info("shutdown complete", "duration_ms", time.Since(start).Milliseconds())
	}()

	return app.Runner.Run(ctx, app.Job)
}
```

In `internal/generator/generator.go` `entryPointFiles`, after the `cmd/worker.go` entry add:

```go
		{"templates/cmd/cron.go.tmpl", "cmd/cron.go", cfg.HasCron()},
		{"templates/cmd/cron/user_scan.go.tmpl", "cmd/cron/user_scan.go", cfg.HasCron()},
```

and after the `internal/app/worker.go` entry add:

```go
		{"templates/app/cron.go.tmpl", "internal/app/cron.go", cfg.HasCron()},
```

- [ ] **Step 4: Run the generator tests to verify they pass**

Run: `go test -short -run 'TestCronTransportFiles|TestCronEntryPoints' ./internal/generator/`
Expected: PASS.

- [ ] **Step 5: Prove the rendered project runs (fx, no DB)**

Run: the [Render check](#render-check-rc), then:

```bash
go vet ./... && go build -o crontest-bin . && ./crontest-bin cron --help && ./crontest-bin cron user-scan; echo "exit=$?"
```

Expected: `cron --help` lists `user-scan`; the run prints JSON lines including `"message":"config loaded"`, `"job":"user-scan"`, `"message":"user-scan: done"`, `"message":"cron: job finished"` and `"message":"shutdown complete"`, then `exit=0`.

Then, in the same directory and alone (heavy): `golangci-lint run`
Expected: `0 issues.`

- [ ] **Step 6: Prove the wire variant builds**

Run: `DI=wire` + the [Render check](#render-check-rc) (its `make gen` runs wire), then `go build ./...`
Expected: the render check writes `wire_gen.go`; the build is clean.

- [ ] **Step 7: Checkpoint**

Run: `make test`
Expected: PASS. Suggested commit: `feat(generator): add the cron and user-scan commands and the run-once lifecycle`.

---

### Task 6: Generated deploy files and docs

**Files:**

- Modify: `internal/generator/templates/Dockerfile.tmpl:65`
- Modify: `internal/generator/templates/docker-compose.yaml.tmpl:28-29`
- Modify: `internal/generator/templates/env.example.tmpl:18`
- Modify: `internal/generator/templates/README.md.tmpl` (title, tree, JWT setup, run section, new "Cron jobs" section)
- Modify: `internal/generator/templates/CLAUDE.md.tmpl` (stack, reference feature, run command, transport line, DI hint, feature recipe)
- Test: `internal/generator/generator_test.go` (new `TestCronDeployFiles`, `TestClaudeMDGenerated` subtest)

**Interfaces:**

- Consumes: everything a cron render produces after Task 5.
- Produces: rendered `Dockerfile` with `CMD ["cron", "user-scan"]`; `.env.example` without `JWT_`; README "Cron jobs" section.

- [ ] **Step 1: Write the failing tests**

Add to `internal/generator/generator_test.go`:

```go
// TestCronDeployFiles pins how a run-once service ships: the image runs the
// sample job, nothing is exposed or health-checked, no JWT keys are asked
// for, and the README documents scheduling instead of a server.
func TestCronDeployFiles(t *testing.T) {
	t.Parallel()
	dir := renderProject(t, cronMatrixConfig("postgres", "wire"))
	docker := readFile(t, filepath.Join(dir, "Dockerfile"))
	mustContainAll(t, docker, `CMD ["cron", "user-scan"]`)
	mustNotContain(t, docker, "EXPOSE", "HEALTHCHECK")
	mustNotContain(t, readFile(t, filepath.Join(dir, ".env.example")), "JWT_")
	compose := readFile(t, filepath.Join(dir, "docker-compose.yaml"))
	mustContainAll(t, compose, "docker compose run --rm app cron <job>")
	mustNotContain(t, compose, "JWT keys")
	readme := readFile(t, filepath.Join(dir, "README.md"))
	mustContainAll(t, readme, "cron service", "### Cron jobs", "go run main.go cron user-scan",
		"concurrencyPolicy: Forbid", "activeDeadlineSeconds", "flock -n", "--after-id", "userscan/", "cronjob/")
	mustNotContain(t, readme, "go run main.go api", "JWT_PRIVATE_KEY")
	if ports := loadCompose(t, cronMatrixConfig("postgres", "wire")).Services["app"].Ports; len(ports) != 0 {
		t.Errorf("cron app publishes ports %v, want none", ports)
	}
}
```

In `TestClaudeMDGenerated`, add after `worker_fx`:

```go
	t.Run("cron_fx", func(t *testing.T) {
		t.Parallel()
		src := renderClaudeMD(t, cronMatrixConfig("postgres", "fx"))
		mustContainAll(t, src,
			"cron service",                  // project type line
			"Cron transport: run-once jobs", // stack line
			"`user-scan` cron job",          // reference feature
			"go run main.go cron user-scan", // run command
			"`initializeCron`",              // fx-specific job DI guidance
		)
		mustNotContain(t, src, "provideRegistrars", "provideWorkerHandlers")
	})
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -short -run 'TestCronDeployFiles|TestClaudeMDGenerated' ./internal/generator/`
Expected: FAIL (`CMD ["api"]`, `JWT_PUBLIC_KEY` present, no `### Cron jobs`; CLAUDE.md lacks the cron stack line and says `go run main.go api`).

- [ ] **Step 3: Dockerfile, compose, env**

`Dockerfile.tmpl` line 65, replace with:

```text
CMD [{{if .HasWorker}}"worker"{{else if .HasGRPC}}"grpc"{{else if .HasCron}}"cron", "user-scan"{{else}}"api"{{end}}]
```

`docker-compose.yaml.tmpl` lines 28–29, replace with:

```text
    # The subcommand comes from the image's CMD ({{if .HasWorker}}worker{{else if .HasGRPC}}grpc{{else if .HasCron}}cron user-scan, which runs once and exits; run another job with `docker compose run --rm app cron <job>`{{else}}api{{end}}) — see Dockerfile.
    # Secrets and per-deploy values ({{if .HasDatabase}}DB_USER/DB_PASSWORD, {{end}}{{if or .HasHTTP .HasGRPC}}JWT keys, {{end}}APP_ENV).
```

`env.example.tmpl` line 18, replace `{{- if not .HasWorker}}` with `{{- if or .HasHTTP .HasGRPC}}`.

- [ ] **Step 4: README.md.tmpl**

Line 3: replace `{{else if .HasWorker}}consumer worker{{else}}service{{end}}` with `{{else if .HasWorker}}consumer worker{{else if .HasCron}}cron service{{else}}service{{end}}`.

Replace the usecase tree lines

```text
│   {{if .HasWorker}}├──{{else}}└──{{end}} user/               # Service (Register, Login, Get, Update, Delete, List)
{{- if .HasWorker}}
│   └── useraudit/          # Audit-trail service the worker dispatches into
{{- end}}
```

with

```text
│   {{if or .HasWorker .HasCron}}├──{{else}}└──{{end}} user/               # Service (Register, Login, Get, Update, Delete, List)
{{- if .HasWorker}}
│   └── useraudit/          # Audit-trail service the worker dispatches into
{{- end}}
{{- if .HasCron}}
│   └── userscan/           # Keyset batch walk the user-scan job runs
{{- end}}
```

After the transport-tree worker block (`│       └── v1/user/        # Per-feature message handler + DTO` / `{{- end}}`) insert:

```text
{{- if .HasCron}}
│   └── cronjob/            # Job contract + Runner (deadline, span, outcome line)
│       └── user/           # user-scan job
{{- end}}
```

In the `app/` tree line, replace `{{else if .HasGRPC}}RunGRPCServer{{else}}RunWorker{{end}}` with `{{else if .HasGRPC}}RunGRPCServer{{else if .HasCron}}RunUserScan; one Run<Job> per cron job{{else}}RunWorker{{end}}`.

In the Setup block, replace the `{{- if not .HasWorker}}` that opens the JWT keypair commands with `{{- if or .HasHTTP .HasGRPC}}`.

Replace the run lines

```text
# Run the server
{{- if .HasWorker}}
go run main.go worker
{{- else if .HasGRPC}}
go run main.go grpc
{{- else}}
go run main.go api
{{- end}}
```

with

```text
# Run {{if .HasCron}}the sample job once{{else}}the server{{end}}
{{- if .HasWorker}}
go run main.go worker
{{- else if .HasGRPC}}
go run main.go grpc
{{- else if .HasCron}}
go run main.go cron user-scan
{{- else}}
go run main.go api
{{- end}}
```

Immediately before the `{{- if .HasHTTP}}` line that opens `### API Endpoints`, insert:

````text
{{- if .HasCron}}

### Cron jobs

This binary runs one job per invocation and exits: `go run main.go cron <job>`; `go run main.go cron --help` lists the jobs. The schedule lives in the deploy layer, not here, so the scheduler supplies retries, overlap control and failure alerts.

| Job | Command | What it does |
| --- | --- | --- |
| `user-scan` | `cron user-scan [--after-id N]` | Walks every user in id order, 500 per page (`usecase/userscan.Scan`) |

Every job follows one contract:

- **Idempotent.** The scheduler may retry a failed run or, without overlap control, start two at once.
- **Resumable.** A long job reports how far it got: a failed `user-scan` names the `--after-id` that resumes it.
- **Returns when its ctx is done.** A run gets `cron.timeout` (default `30m`) and SIGTERM cancels it; both reach the job as ctx cancellation.

Exit code `0` means the job completed; `1` means anything else (job error, panic, timeout, SIGTERM, failed boot). Each run logs one outcome line — `cron: job finished`, `cron: job failed` or `cron: job interrupted` — with `job`, `run_id`, `trace_id` and `duration_ms`.

A Kubernetes `CronJob` is the recommended trigger:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: {{.ProjectName}}-user-scan
spec:
  schedule: "0 2 * * *"           # daily at 02:00; set spec.timeZone to pin the zone
  concurrencyPolicy: Forbid       # never two runs at once
  startingDeadlineSeconds: 3600   # still start a run missed by up to an hour
  jobTemplate:
    spec:
      backoffLimit: 2             # retry a failed run twice
      activeDeadlineSeconds: 1900 # a little above cron.timeout (30m)
      template:
        spec:
          restartPolicy: Never
          containers:
            - name: {{.ProjectName}}
              image: {{.ProjectName}}:latest
              args: ["cron", "user-scan"]
              envFrom:
                - secretRef:
                    name: {{.ProjectName}}-env
```

On a single server, crontab works; `flock -n` stands in for `concurrencyPolicy: Forbid`, and the `cd` lets the binary find its `.env`:

```bash
0 2 * * * cd /srv/{{.ProjectName}} && flock -n /tmp/{{.ProjectName}}-user-scan.lock ./{{.ProjectName}} cron user-scan >> /var/log/{{.ProjectName}}-user-scan.log 2>&1
```

To add a job:

1. Add the usecase method that does the work (see `usecase/userscan`).
2. Add `transport/cronjob/<feature>/<job>.go`: a type with `Name()` and `Run(ctx) error` that calls it.
3. Add `cmd/cron/<job>.go` (the cobra command and its flags) and mount it in `cmd/cron.go`.
4. Add `Run<Job>` to `internal/app/cron.go`.
5. Add an `Initialize<Job>Job` injector to {{if .UseWire}}`internal/infrastructure/di/wire.go` and run `make gen`{{else}}`internal/infrastructure/di/fx.go` (one `initializeCron` call){{end}}.
{{- end}}

````

- [ ] **Step 5: CLAUDE.md.tmpl**

Make six replacements; each old text occurs once.

Stack list, replace

```text
{{- if .HasWorker}}- Worker transport: consumes from {{.MessageQueue}}
{{end}}
```

with

```text
{{- if .HasWorker}}- Worker transport: consumes from {{.MessageQueue}}
{{end}}{{- if .HasCron}}- Cron transport: run-once jobs (`cron <job>`) scheduled by the deploy layer
{{end}}
```

Reference feature, replace

```text
copy that slice when adding a new event.{{else}}
```

with

```text
copy that slice when adding a new event.{{else if .HasCron}}Ships the User aggregate (entity, usecase, repository) plus a `user-scan` cron job over a keyset batch walk (`usecase/userscan`) as the reference feature — copy that slice when adding a new job.{{else}}
```

Run command, replace

```text
{{else if .HasGRPC}}grpc{{else}}api{{end}}`. Run a single test
```

with

```text
{{else if .HasGRPC}}grpc{{else if .HasCron}}cron user-scan{{else}}api{{end}}`. Run a single test
```

Transport description, replace

```text
{{else if .HasWorker}}worker consumer + handlers{{else if .HasGRPC}}
```

with

```text
{{else if .HasWorker}}worker consumer + handlers{{else if .HasCron}}cron jobs + the Runner that applies the run policy{{else if .HasGRPC}}
```

DI hint, replace

```text
{{if .HasWorker}} A new worker message handler adds itself to `provideWorkerHandlers`.{{end}}
```

with

```text
{{if .HasWorker}} A new worker message handler adds itself to `provideWorkerHandlers`.{{end}}{{if .HasCron}} A new cron job adds an `Initialize<Job>Job` injector ({{if .UseWire}}in `wire.go`{{else}}one `initializeCron` call in `fx.go`{{end}}), an `app.Run<Job>` and a `cmd/cron/<job>.go` mounted in `cmd/cron.go`.{{end}}
```

Feature recipe, replace

```text
{{else if .HasWorker}} → worker handler + DTO{{end}}
```

with

```text
{{else if .HasWorker}} → worker handler + DTO{{else if .HasCron}} → cron job + command{{end}}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test -short -run 'TestCronDeployFiles|TestClaudeMDGenerated|TestDockerComposeStack|TestDeployContract' ./internal/generator/`
Expected: PASS.

Run: the [Render check](#render-check-rc), then from the rendered project: `npx --yes markdownlint-cli2 --config /Users/lap02445/workspace/gh_leo/nova/.markdownlint.json README.md CLAUDE.md`
Expected: `Summary: 0 issues`. If issues appear only on lines outside the cron additions, they predate this work: list them for the user, don't fix them here.

- [ ] **Step 7: Checkpoint**

Run: `make test`
Expected: PASS. Suggested commit: `feat(generator): ship the cron service image, env and scheduling docs`.

---

### Task 7: Nova docs

**Files:**

- Modify: `docs/02-project-layout.md:13,36-45,64-70,129-141`
- Modify: `docs/03-architecture-rules.md:64,67`
- Modify: `docs/07-transport-layer.md:630-650`
- Modify: `docs/13-cross-cutting.md:184`
- Modify: `docs/README.md:33`
- Test: `internal/generator/docs_test.go` (new `TestDocsClaims` subtest)

**Interfaces:**

- Consumes: the rendered cron project (Tasks 2–6). Quote code only from a render.
- Produces: docs that describe the cron transport as scaffolded.

- [ ] **Step 1: Write the failing doc test**

In `TestDocsClaims`, add a subtest:

```go
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
		mustContainAll(t, doc, "`cron: job finished`", "`cron: job failed`", "`cron: job interrupted`", "`cron.timeout`")
		mustNotContain(t, doc, "**Not scaffolded.** Cron", "not wired into `buildFileList`")
		mustNotContain(t, docFile(t, "13-cross-cutting.md"), "`cmd/cron.go` are not rendered")
		mustNotContain(t, docFile(t, "03-architecture-rules.md"), "`transport/cronjob/` are patterns to grow into")
		mustNotContain(t, docFile(t, "README.md"), "- `transport/cronjob/`")
		mustContainAll(t, docFile(t, "02-project-layout.md"), "[cron]", "cronjob/", "userscan/", "cmd/cron.go")
	})
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -short -run TestDocsClaims ./internal/generator/`
Expected: FAIL (docs/07 still says "Not scaffolded").

- [ ] **Step 3: docs/07**

Render a project to quote from: the [Render check](#render-check-rc) (the job files do not vary by database or DI). Replace the whole `## internal/transport/cronjob/` section of `docs/07-transport-layer.md` (from the heading through the `> A templates/cmd/cron.go.tmpl exists …` blockquote) with the text below, after confirming each Go line matches the rendered `internal/transport/cronjob/job.go` and `user/scan.go` byte for byte:

````markdown
## internal/transport/cronjob/

Rendered by `--transport=cron`. A cron service is run-once: `myapp cron <job>` boots the DI graph for one job, runs it, and exits `0` when it completed or `1` when it did not (job error, panic, timeout, SIGTERM, failed boot). The schedule lives in the deploy layer — a Kubernetes `CronJob` with `concurrencyPolicy: Forbid`, or crontab behind `flock -n` — which owns retries, missed runs and overlap; the generated README shows both.

A job implements `Job` (`job.go`):

```go
type Job interface {
	// Name is the job's log and span name, matching its cron subcommand.
	Name() string
	Run(ctx context.Context) error
}
```

`Runner.Run` (`runner.go`) applies the run policy once, the counterpart of the worker's per-message policy: it mints a `run_id` into `requestid` and scopes a logger (`job`, `run_id`, `trace_id`) into `logctx`, opens a `cron <name>` span, bounds the run with `cron.timeout` (default `30m`), turns a panic into `ErrJobPanic`, and writes one outcome line — `cron: job finished`, `cron: job failed`, or `cron: job interrupted` when SIGTERM cancelled the run. There are no in-process retries; the scheduler retries.

Jobs are thin, like handlers: one package per feature, each job calling one usecase method. The reference job walks every user through the keyset `usecase/userscan.Scan` and names the resume point when it fails:

```go
func (j *ScanJob) Run(ctx context.Context) error {
	res, err := j.scan.Scan(ctx, userscansvc.ScanInput{AfterID: j.flags.AfterID})
	if err != nil {
		return errors.Wrapf(err, "user-scan: resume with --after-id=%d", res.LastID)
	}
	logctx.From(ctx).Info("user-scan: done", "scanned", res.Scanned, "last_id", res.LastID)
	return nil
}
```

Each job has its own cobra command in `cmd/cron/` with its own flags (`user-scan` takes `--after-id`), and its own DI injector, `Initialize<Job>Job`, which takes the parsed flags as an argument. One injector per job means a job never builds a dependency it doesn't use, so an outage in one job's dependency cannot fail another. Every job must be idempotent, resumable, and return when its ctx is done.
````

- [ ] **Step 4: docs/02, 03, 13 and docs/README**

`docs/02-project-layout.md` line 13: replace `` all of `cmd/api.go`, `cmd/grpc.go` and `cmd/worker.go` — lines marked `[HTTP]`, `[gRPC]` and `[worker]` `` with `` all of `cmd/api.go`, `cmd/grpc.go`, `cmd/worker.go` and `cmd/cron.go` — lines marked `[HTTP]`, `[gRPC]`, `[worker]` and `[cron]` ``.

Replace line 38 (`│   └── worker.go … # [worker] starts the queue consumer`) with:

```text
│   ├── worker.go                           # [worker] starts the queue consumer
│   ├── cron.go                             # [cron]   `cron` parent; mounts one subcommand per job
│   └── cron/                               # [cron]   one cobra command per job (user_scan.go)
```

Replace line 44 (`│   │   └── worker.go … # [worker] RunWorker()`) with:

```text
│   │   ├── worker.go                       # [worker] RunWorker()
│   │   └── cron.go                         # [cron]   runCron() lifecycle + one Run<Job>() per job
```

Replace lines 67–69 (the `useraudit/` block) with:

```text
│   │   ├── useraudit/                      # [worker] audit-trail feature
│   │   │   ├── service.go
│   │   │   └── dto.go
│   │   └── userscan/                       # [cron] keyset batch walk the user-scan job runs
│   │       ├── service.go
│   │       ├── dto.go
│   │       └── service_test.go             # Paging, resume, error and cancel with a fake repository
```

Replace lines 131–140 (the `worker/` block through its `dto.go`) with:

```text
│   │   ├── worker/                         # [worker]
│   │   │   ├── worker.go                   # Orchestrator: consumer → handler dispatch
│   │   │   ├── policy.go                   # Per-message deadline, retries, parking (ErrPoison/ErrParked)
│   │   │   ├── worker_test.go              # Broker-less tests of the policy
│   │   │   ├── handler.go                  # Handler interface
│   │   │   ├── consumer.go                 # Kafka/RabbitMQ consumer (settles ack / dead-letter / redeliver)
│   │   │   ├── consumer_test.go            # Settle semantics with a fake session / acknowledger
│   │   │   └── v1/user/                    # One package per feature
│   │   │       ├── handler.go
│   │   │       └── dto.go
│   │   └── cronjob/                        # [cron]
│   │       ├── job.go                      # Job interface: Name + Run(ctx) error
│   │       ├── runner.go                   # Run policy: run_id, span, deadline, panic guard, outcome line
│   │       ├── runner_test.go              # Outcomes with a fake job and a recording logger
│   │       └── user/scan.go                # user-scan job → usecase/userscan; one package per feature
```

`docs/03-architecture-rules.md` line 64: replace `# Cron job handlers (no middleware)` with `# [--transport=cron] Job contract, Runner, one package per feature`. Line 67: replace `` `adapter/external/` and `transport/cronjob/` are patterns to grow into — `nova new` doesn't scaffold them. `` with `` `adapter/external/` is a pattern to grow into — `nova new` doesn't scaffold it. `transport/cronjob/` is rendered by `--transport=cron` ([07](07-transport-layer.md#internaltransportcronjob)). ``

`docs/13-cross-cutting.md`: delete the line `` - **`transport/cronjob/` and `cmd/cron.go` are not rendered.** The `cron.go.tmpl` template exists but is not wired into `buildFileList`. ``

`docs/README.md`: delete the line `` - `transport/cronjob/` `` from the not-scaffolded list.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -short -run 'TestDocsClaims|TestDocsLinksResolve' ./internal/generator/`
Expected: PASS (the new docs/03 link must resolve to the docs/07 heading anchor).

Run: `npx --yes markdownlint-cli2 docs/02-project-layout.md docs/03-architecture-rules.md docs/07-transport-layer.md docs/13-cross-cutting.md docs/README.md`
Expected: `Summary: 0 issues in 0 files`.

- [ ] **Step 6: Checkpoint**

Run: `make test`
Expected: PASS. Suggested commit: `docs: describe the scaffolded cron transport`.

---

### Task 8: Vet matrix, bootstrap, counts and final verification

**Files:**

- Modify: `internal/generator/generator_test.go` (`wireGenCronStub`, `TestGenerateMatrix`, `TestGeneratedProjectBootstrap`, `bootstrapProject`, new `smokeRun`, `assertTracerBuiltFirst`)
- Modify: `CLAUDE.md:12,36`
- Modify: `README.md:191`
- Modify: `Makefile:102`

**Interfaces:**

- Consumes: `cronMatrixConfig` (Task 2); everything rendered by Tasks 2–6.
- Produces: 40 vet-matrix cases and 10 bootstraps; the counts in docs match.

- [ ] **Step 1: Add the cron rows**

Next to `wireGenGRPCStub`, add:

```go
// wireGenCronStub stands in for the per-job cron injector; the module path is
// baseMatrixConfig's.
const wireGenCronStub = `// Code generated by Wire. DO NOT EDIT.
//go:build !wireinject
// +build !wireinject

package di

import (
	"context"

	usercron "matrixtest/internal/transport/cronjob/user"
)

func InitializeUserScanJob(_ context.Context, _ usercron.ScanFlags) (*CronApp, func(), error) {
	return &CronApp{}, func() {}, nil
}
`
```

In `TestGenerateMatrix`, inside `for _, di := range dis`, after the gRPC row:

```go
		for _, db := range []string{"postgres", "none"} {
			t.Run("cron_"+di+"_"+db, func(t *testing.T) {
				t.Parallel()
				renderAndVet(t, cronMatrixConfig(db, di), wireGenCronStub)
			})
		}
```

Update the `TestGenerateMatrix` doc comment's first sentence to `every transport × database (× broker for the worker case, postgres/none for cron)`.

In `TestGeneratedProjectBootstrap`, append to `cases`:

```go
		// cron: real wire over sqlc for the per-job injector, and the fx graph
		// run for real against the in-memory repository (smokeRun executes
		// user-scan and checks its exit and outcome line).
		{"cron_postgres_wire", with(cronMatrixConfig("postgres", "wire"), "redis", "none")},
		{"cron_none_fx", cronMatrixConfig("none", "fx")},
```

In `bootstrapProject`, after the `cfg.HasHTTP()` package block:

```go
	if cfg.HasCron() {
		pkgs = append(pkgs, "./internal/transport/cronjob/...")
	}
```

and after the `smokeBoot` call block:

```go
	if !cfg.HasDatabase() && cfg.HasCron() {
		smokeRun(t, dir)
	}
```

Add next to `smokeBoot`:

```go
// smokeRun runs the sample cron job for real — the DI graph, the Runner and
// the exit path — against the in-memory repository.
func smokeRun(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "run", ".", "cron", "user-scan")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod -p=2", "APP_ENV=local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cron user-scan: %v\n%s", err, out)
	}
	mustContainAll(t, string(out), "cron: job finished", `"job":"user-scan"`, "shutdown complete")
}
```

In `assertTracerBuiltFirst`, add `"cronjob.NewRunner("` to the constructor list, and in its doc comment change `before the server or worker` to `before the server, worker or cron runner`.

- [ ] **Step 2: Fast suite and lint**

Run: `make lint`
Expected: `0 issues.`

Run: `make test`
Expected: PASS.

- [ ] **Step 3: Full suite (alone, nothing else building)**

Run: `time make test-all`
Expected: PASS, including `TestGenerateMatrix/cron_wire_postgres`, `cron_wire_none`, `cron_fx_postgres`, `cron_fx_none`, `TestGeneratedProjectBootstrap/cron_postgres_wire` and `cron_none_fx`. Note the wall time.

- [ ] **Step 4: Update the counts**

With `N` = the measured wall time rounded up to whole minutes:

- `Makefile:102` → `test-all: ## Full suite: 40-case vet matrix (2 at a time) + 10 real-toolchain bootstraps (~N min warm cache; <2.5 GB)`
- `CLAUDE.md:36` → `make test-all         # full suite: 40-case vet matrix (2 at a time) + 10 real-toolchain bootstraps (~N min warm cache; <2.5 GB)`
- `CLAUDE.md:12` → replace `eight representative projects` with `ten representative projects`.
- `README.md:191` → `make test-all         # full suite: 40-case vet matrix + 10 real-toolchain bootstraps (~N min; run it alone)`

- [ ] **Step 5: Stale-reference sweep**

Run:

```bash
grep -rnE 'cron\.spec|CRON_SPEC|RunCronJobScheduler|Not scaffolded\.\*\* Cron|`cmd/cron\.go` are not rendered|transport "cron" is not supported|`cron`/`cli` transports|36-case|8 real-toolchain|eight representative' --include='*.md' --include='*.go' --include='*.tmpl' --include=Makefile . | grep -v -e docs/superpowers -e '_test.go'
```

Expected: no output.

Run: `npx --yes markdownlint-cli2 README.md CLAUDE.md`
Expected: `Summary: 0 issues in 0 files`.

Run: `make test`
Expected: PASS.

- [ ] **Step 6: Checkpoint**

Suggested commit: `test(generator): cover the cron transport in the vet matrix and bootstraps`.
