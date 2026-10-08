# Phase 2: CLI Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Heavy steps (`make test-all`) run alone from the main thread; see Global Constraints.

**Goal:** `nova new` and `nova add` never silently produce a broken project, never destroy user files, and never write outside the target directory; every advertised option is either implemented or rejected before a file is written.

**Architecture:** One option registry in `internal/config/options.go` (exported value slices plus `ProjectConfig.Validate()`) feeds the flag help in `cmd/new.go`, the prompt choices in `internal/prompt/prompt.go`, and the gate in `generator.New`. Safety checks live where the writes happen: `cmd/new.go` guards the target directory, `ComponentGenerator` confines every output path under the project root and refuses to overwrite feature files without `--force`. Shared string helpers (`Snake`, `Title`) move to `internal/manifest`; the Go AST merge helpers move to `internal/gosrc`; both generators print through an injectable `io.Writer`.

**Tech Stack:** Go 1.25+, cobra/pflag, text/template, go/ast + go/format.

**Status (2026-10-07):** all eight tasks implemented and verified in the working tree; commit steps are pending the user's `/grimoire-core:commit`. Final verification: `make test-all` green (about 100 s), `make lint` 0 issues, and a smoke run of the built binary rejects `--transport=cron`, a quoted project name, `../escaped`, a non-empty target directory, `nova add` with a traversal or non-identifier name, a repeat `nova add` without `--force`, a foreign `--type`, `nova add` outside a module, and `worker` without a queue, while `--docker=false --ci=none` now really omits Docker and CI.

**Deviations from the plan as written:**

- Task 1: the `GRPCGateway` field was removed from `ProjectConfig` along with the flag and prompt (nothing read it), so `Validate` has no gateway check.
- Task 1: `gochecknoglobals` whitelists `regexp.MustCompile`, so the regex vars carry no `nolint` directive.
- Task 3: the `runNew` guard uses `dirErr` to avoid shadowing the earlier `err`.
- One `make test-all` run saw `http_gin_mysql_wire` fail inside `TestGeneratedProjectBootstrap` while it passed alone and in every later run; treat a recurrence as a concurrency issue between the matrix and the bootstrap, not as a template regression.

## Global Constraints

- `make lint` and `make test` (`go test -short ./...`) stay green after every task; `make test-all` is run alone at the end of tasks 1, 3, 4 and 7.
- Resource rule: one heavy toolchain command at a time, from the main thread; never `go clean -cache`.
- Surgical changes: match surrounding style; remove only what a change orphans.
- Markdown edits must pass `npx --yes markdownlint-cli2 <file>` with the repo config.
- Commit only via `/grimoire-core:commit` after the user confirms the message; never run `git commit`. Each task's final step lists the files to stage.

---

### Task 1: One option registry, one validator

**Files:**

- Create: `internal/config/options.go`, `internal/config/options_test.go`
- Modify: `internal/config/config.go` (drop `GRPCGateway`)
- Modify: `internal/generator/generator.go:39-99` (`supportedFrameworks`, `supportedDatabases`, `supportedDI`, `New`)
- Modify: `internal/generator/component.go:95-101` (`GenerateHandler` framework check)
- Modify: `internal/generator/generator_test.go` (`TestNewRejectsUnsupportedDI`)
- Modify: `internal/prompt/prompt.go` (option lists, drop gateway/driver/query/config prompts)
- Modify: `cmd/new.go` (flags, `applyFlags`, `runNew`)
- Modify: `cmd/new_test.go` (new `TestApplyFlags`)
- Modify: `cmd/root.go` (`SilenceUsage`)

**Interfaces:**

- Produces: `config.Transports`, `config.HTTPFrameworks`, `config.Databases`, `config.Caches`, `config.Searches`, `config.MessageQueues`, `config.ConfigFormats`, `config.DIs`, `config.CIs` (`[]string`), and `func (c *ProjectConfig) Validate() error`, which also normalizes `Database`/`Cache`/`Search`/`MessageQueue` empty → `none`, `DBDriver` → the engine's driver, `QueryGen` → `sqlc`, and blanks `DBDriver`/`QueryGen` when there is no database.
- Produces: `applyFlags(cmd *cobra.Command, cfg *config.ProjectConfig) error`.

- [x] **Step 1: Write the failing validator test**

Create `internal/config/options_test.go`:

```go
package config

import (
	"strings"
	"testing"
)

func validHTTP() *ProjectConfig {
	cfg := DefaultConfig()
	cfg.Transport = "http"
	cfg.HTTPFramework = "fiber"
	return cfg
}

// TestValidateRejectsUnimplemented pins the one gate every advertised option
// goes through: anything outside the registry fails before a file is written.
func TestValidateRejectsUnimplemented(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*ProjectConfig){
		"transport cron":      func(c *ProjectConfig) { c.Transport = "cron" },
		"transport cli":       func(c *ProjectConfig) { c.Transport = "cli" },
		"transport typo":      func(c *ProjectConfig) { c.Transport = "htpp" },
		"transport empty":     func(c *ProjectConfig) { c.Transport = "" },
		"framework nethttp":   func(c *ProjectConfig) { c.HTTPFramework = "nethttp" },
		"database sqlite":     func(c *ProjectConfig) { c.Database = "sqlite" },
		"driver sqlx":         func(c *ProjectConfig) { c.DBDriver = "sqlx" },
		"driver mismatch":     func(c *ProjectConfig) { c.Database = "mysql"; c.DBDriver = "pgx" },
		"query raw":           func(c *ProjectConfig) { c.QueryGen = "raw" },
		"cache bigcache":      func(c *ProjectConfig) { c.Cache = "bigcache" },
		"queue nats":          func(c *ProjectConfig) { c.MessageQueue = "nats" },
		"worker without queue": func(c *ProjectConfig) { c.Transport = "worker"; c.MessageQueue = "none" },
		"config toml":         func(c *ProjectConfig) { c.ConfigFormat = "toml" },
		"di manual":           func(c *ProjectConfig) { c.DI = "manual" },
		"name with space":     func(c *ProjectConfig) { c.ProjectName = "bad name" },
		"name traversal":      func(c *ProjectConfig) { c.ProjectName = "../escaped" },
		"name quote":          func(c *ProjectConfig) { c.ProjectName = `bad"x` },
		"module with space":   func(c *ProjectConfig) { c.ModuleName = "foo bar" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := validHTTP()
			mutate(cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate accepted %s", name)
			}
		})
	}
}

// TestValidateNormalizes pins the canonical shape templates rely on.
func TestValidateNormalizes(t *testing.T) {
	t.Parallel()
	cfg := validHTTP()
	cfg.Database = "none"
	cfg.Cache = ""
	cfg.Search = ""
	cfg.MessageQueue = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.DBDriver != "" || cfg.QueryGen != "" {
		t.Errorf("no database must blank driver/query, got %q/%q", cfg.DBDriver, cfg.QueryGen)
	}
	for field, got := range map[string]string{"cache": cfg.Cache, "search": cfg.Search, "queue": cfg.MessageQueue} {
		if got != "none" {
			t.Errorf("%s = %q, want none", field, got)
		}
	}

	mysql := validHTTP()
	mysql.Database = "mysql"
	mysql.DBDriver = ""
	mysql.QueryGen = ""
	if err := mysql.Validate(); err != nil {
		t.Fatalf("Validate mysql: %v", err)
	}
	if mysql.DBDriver != "database/sql" || mysql.QueryGen != "sqlc" {
		t.Errorf("mysql defaults = %q/%q, want database/sql/sqlc", mysql.DBDriver, mysql.QueryGen)
	}

	worker := DefaultConfig()
	worker.Transport = "worker"
	worker.MessageQueue = "kafka"
	if err := worker.Validate(); err != nil {
		t.Fatalf("Validate worker: %v", err)
	}
}

func TestValidateErrorNamesEveryProblem(t *testing.T) {
	t.Parallel()
	cfg := validHTTP()
	cfg.Transport = "cron"
	cfg.Cache = "bigcache"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{`transport "cron"`, `cache "bigcache"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test -run 'TestValidate' ./internal/config`

Expected: FAIL to compile with `cfg.Validate undefined`.

- [x] **Step 3: Create the registry and validator**

Create `internal/config/options.go`:

```go
package config

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Supported option values. These slices are the single source of truth for
// the `nova new` flag help, the interactive prompts and Validate; the
// generator trusts a validated config. Add a value here only together with
// its templates.
//
//nolint:gochecknoglobals // immutable option sets; treated as consts.
var (
	Transports     = []string{"http", "grpc", "worker"}
	HTTPFrameworks = []string{"fiber", "gin", "chi", "echo"}
	Databases      = []string{"postgres", "mysql", noneStr}
	Caches         = []string{"redis", noneStr}
	Searches       = []string{"elasticsearch", noneStr}
	MessageQueues  = []string{"kafka", "rabbitmq", noneStr}
	ConfigFormats  = []string{"yaml"}
	DIs            = []string{"wire", "fx"}
	CIs            = []string{"github", noneStr}
)

// dbDrivers maps each SQL engine to the one driver its templates use.
//
//nolint:gochecknoglobals // immutable; treated as a const.
var dbDrivers = map[string]string{"postgres": "pgx", "mysql": "database/sql"}

const queryGenSQLC = "sqlc"

//nolint:gochecknoglobals // compiled once; treated as consts.
var (
	projectNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	modulePathRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~/-]*$`)
)

// Validate rejects unsupported or inconsistent choices before anything is
// rendered and normalizes the dependent fields so templates see one
// canonical shape per database. It is the only place option values are
// checked; the CLI, the prompts and generator.New all rely on it. Every
// problem is reported, not just the first.
func (c *ProjectConfig) Validate() error {
	for _, f := range []*string{&c.Database, &c.Cache, &c.Search, &c.MessageQueue} {
		if *f == "" {
			*f = noneStr
		}
	}

	var errs []error
	if c.ProjectName == "." || c.ProjectName == ".." || !projectNameRE.MatchString(c.ProjectName) {
		errs = append(errs, fmt.Errorf(
			"project name %q: use letters, digits, '.', '_' or '-' only (no spaces, quotes or path separators)",
			c.ProjectName,
		))
	}
	if c.ModuleName != "" && !modulePathRE.MatchString(c.ModuleName) {
		errs = append(errs, fmt.Errorf("module %q is not a valid Go module path", c.ModuleName))
	}
	errs = append(errs, oneOf("transport", c.Transport, Transports))
	if c.HasHTTP() {
		errs = append(errs, oneOf("http-framework", c.HTTPFramework, HTTPFrameworks))
	}
	errs = append(errs, oneOf("database", c.Database, Databases))
	errs = append(errs, c.validateSQL()...)
	errs = append(errs, oneOf("cache", c.Cache, Caches))
	errs = append(errs, oneOf("search", c.Search, Searches))
	errs = append(errs, oneOf("queue", c.MessageQueue, MessageQueues))
	if c.HasWorker() && !c.HasMessageQueue() {
		errs = append(errs, errors.New("transport worker needs --queue kafka or rabbitmq"))
	}
	errs = append(errs, oneOf("config", c.ConfigFormat, ConfigFormats))
	errs = append(errs, oneOf("di", c.DI, DIs))
	return errors.Join(errs...)
}

// validateSQL checks the engine-dependent fields and fills their defaults.
// Without a database they are meaningless, so they are blanked; otherwise the
// Makefile, nova.yaml and CLAUDE.md templates would still mention sqlc.
func (c *ProjectConfig) validateSQL() []error {
	if !c.HasDatabase() {
		c.DBDriver = ""
		c.QueryGen = ""
		return nil
	}
	want, known := dbDrivers[c.Database]
	if !known {
		return nil // the database check already reported the engine
	}
	var errs []error
	switch c.DBDriver {
	case "", want:
		c.DBDriver = want
	default:
		errs = append(errs, fmt.Errorf("db-driver %q is not supported for %s (templates use %s)", c.DBDriver, c.Database, want))
	}
	switch c.QueryGen {
	case "", queryGenSQLC:
		c.QueryGen = queryGenSQLC
	default:
		errs = append(errs, fmt.Errorf("query %q is not supported (only sqlc)", c.QueryGen))
	}
	return errs
}

func oneOf(flag, got string, allowed []string) error {
	if slices.Contains(allowed, got) {
		return nil
	}
	return fmt.Errorf("%s %q is not supported (valid: %s)", flag, got, strings.Join(allowed, ", "))
}
```

Then in `internal/config/config.go` delete the `GRPCGateway bool \`json:"grpc_gateway"\`` field (no template reads it; the gateway was never implemented).

- [x] **Step 4: Run the validator tests**

Run: `go test -run 'TestValidate' ./internal/config`

Expected: PASS.

- [x] **Step 5: Make `generator.New` and `GenerateHandler` use the registry**

In `internal/generator/generator.go` delete the three `supported*` maps with their comments (lines 39–77) and replace the body of `New` up to `funcMap` with:

```go
// New creates a new Generator. It validates cfg first so a typo (e.g. "echo2")
// or an unimplemented option fails here instead of producing a half-rendered
// project with cryptic "template not found in embedded FS" errors.
func New(cfg *config.ProjectConfig) (*Generator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("generator: %w", err)
	}
```

In `internal/generator/component.go`, `GenerateHandler`: replace `if !supportedFrameworks[fw] {` with `if !slices.Contains(config.HTTPFrameworks, fw) {`, and add `"slices"` and `"github.com/quyennguyenvu/nova/internal/config"` to the imports.

In `internal/generator/generator_test.go`, `TestNewRejectsUnsupportedDI`: replace both `baseMatrixConfig("postgres", di)` calls with `httpMatrixConfig("fiber", "postgres", di)` (the base config has no transport, which Validate now requires), and rename the doc comment's "guards the supportedDI whitelist" to "guards the DI option set".

- [x] **Step 6: Derive the prompts from the registry**

In `internal/prompt/prompt.go`:

- Add `"slices"` to the imports.
- In `RunInteractive` delete the three blocks that call `promptGRPCGateway`, `promptSQL` and `promptConfigFormat`, and delete those three functions (the gateway was never implemented; driver, query and config format each have one valid value, which `Validate` fills in).
- Replace the literal option slices: `promptTransport` uses `Options: config.Transports`; `promptHTTPFramework` uses `Options: config.HTTPFrameworks`; `promptDatabase` uses `Options: config.Databases`; `promptCache` uses `Options: config.Caches`; `promptSearch` uses `Options: config.Searches`; `promptDI` uses `Options: config.DIs`.
- Replace `promptMessageQueue` with:

```go
func promptMessageQueue(cfg *config.ProjectConfig) error {
	opts := slices.Clone(config.MessageQueues)
	defaultMQ := "none"
	if cfg.HasWorker() {
		// A worker is a consumer; "none" would fail Validate anyway.
		opts = slices.DeleteFunc(opts, func(s string) bool { return s == "none" })
		defaultMQ = "kafka"
	}
	if err := survey.AskOne(&survey.Select{
		Message: "Message queue:",
		Options: opts,
		Default: defaultMQ,
	}, &cfg.MessageQueue); err != nil {
		return err
	}
	return nil
}
```

- [x] **Step 7: Write the failing flag test**

Append to `cmd/new_test.go` (add `"github.com/spf13/cobra"` is not needed; `newCommand()` returns one):

```go
// parseNewFlags builds the `new` command and parses args as a user would type
// them, so applyFlags sees real pflag state (Changed, defaults, values).
func parseNewFlags(t *testing.T, args ...string) *config.ProjectConfig {
	t.Helper()
	cmd := newCommand()
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	cfg := config.DefaultConfig()
	if err := applyFlags(cmd, cfg); err != nil {
		t.Fatalf("applyFlags %v: %v", args, err)
	}
	return cfg
}

// TestApplyFlags pins the booleans that used to be no-ops: --docker=false and
// --ci=none must switch the feature off, and an unknown --ci value must fail.
func TestApplyFlags(t *testing.T) {
	t.Parallel()
	if cfg := parseNewFlags(t, "--transport=http", "--docker=false", "--ci=none"); cfg.IncludeDocker || cfg.IncludeCI {
		t.Errorf("docker=%v ci=%v, want both false", cfg.IncludeDocker, cfg.IncludeCI)
	}
	if cfg := parseNewFlags(t, "--transport=http"); !cfg.IncludeDocker || !cfg.IncludeCI {
		t.Errorf("defaults: docker=%v ci=%v, want both true", cfg.IncludeDocker, cfg.IncludeCI)
	}
	cmd := newCommand()
	if err := cmd.ParseFlags([]string{"--ci=gitlab"}); err != nil {
		t.Fatal(err)
	}
	if err := applyFlags(cmd, config.DefaultConfig()); err == nil {
		t.Error("--ci=gitlab accepted")
	}
	if cmd.Flags().Lookup("type") != nil || cmd.Flags().Lookup("grpc-gateway") != nil {
		t.Error("dead flags --type / --grpc-gateway still registered")
	}
}
```

Run: `go test -run TestApplyFlags ./cmd`

Expected: FAIL (`applyFlags` returns no value; dead flags present).

- [x] **Step 8: Rewrite the flags and `applyFlags`**

In `cmd/new.go` add `"errors"` and `"strings"` to the imports, replace the flag block with:

```go
	f := newCmd.Flags()
	f.String("module", "", "Go module path (e.g. github.com/myorg/myproject)")
	f.String("transport", "", "Transport layer: "+strings.Join(config.Transports, ", "))
	f.String("http-framework", "", "HTTP framework: "+strings.Join(config.HTTPFrameworks, ", "))
	f.String("database", "", "Database: "+strings.Join(config.Databases, ", "))
	f.String("db-driver", "", "Database driver (pgx for postgres, database/sql for mysql; defaults per engine)")
	f.String("query", "", "Query generation: sqlc (default for SQL engines)")
	f.String("cache", "", "Cache: "+strings.Join(config.Caches, ", "))
	f.String("search", "", "Search engine: "+strings.Join(config.Searches, ", "))
	f.String("queue", "", "Message queue: "+strings.Join(config.MessageQueues, ", "))
	f.String("config", "", "Configuration format: "+strings.Join(config.ConfigFormats, ", "))
	f.String("di", "", "Dependency injection: "+strings.Join(config.DIs, ", "))
	f.Bool("docker", true, "Include Docker setup (--docker=false to skip)")
	f.String("ci", "github", "CI/CD: "+strings.Join(config.CIs, ", "))
	f.Bool("force", false, "Generate into a non-empty directory, overwriting files that already exist")
```

Replace `applyFlags` with:

```go
func applyFlags(cmd *cobra.Command, cfg *config.ProjectConfig) error {
	flags := cmd.Flags()
	for flag, field := range map[string]*string{
		"module":         &cfg.ModuleName,
		"transport":      &cfg.Transport,
		"http-framework": &cfg.HTTPFramework,
		"database":       &cfg.Database,
		"db-driver":      &cfg.DBDriver,
		"query":          &cfg.QueryGen,
		"cache":          &cfg.Cache,
		"search":         &cfg.Search,
		"queue":          &cfg.MessageQueue,
		"config":         &cfg.ConfigFormat,
		"di":             &cfg.DI,
	} {
		if v, _ := flags.GetString(flag); v != "" {
			*field = v
		}
	}
	// Booleans must honour an explicit false, so test Changed rather than value.
	if flags.Changed("docker") {
		cfg.IncludeDocker, _ = flags.GetBool("docker")
	}
	if flags.Changed("ci") {
		switch v, _ := flags.GetString("ci"); v {
		case "github":
			cfg.IncludeCI = true
		case "none":
			cfg.IncludeCI = false
		default:
			return fmt.Errorf("ci %q is not supported (valid: %s)", v, strings.Join(config.CIs, ", "))
		}
	}
	return nil
}
```

In `runNew`, change the flags branch to `if err := applyFlags(cmd, cfg); err != nil { return err }`, delete the `cfg.GRPCGateway` reference (gone with the field), and after the module default add:

```go
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}
```

Update the `Long` examples to drop `--db-driver=pgx` (now implied). The `"errors"` import is used in Task 2; if lint flags it as unused after this task, add it in Task 2 instead.

In `cmd/root.go` add `SilenceUsage: true,` to the root `cobra.Command` literal, so a validation error prints the error without the full usage dump.

- [x] **Step 9: Run tests and lint**

Run: `go test -short ./... && make lint`

Expected: PASS, `0 issues.`

Run (alone): `make test-all`

Expected: PASS.

- [ ] **Step 10: Commit**

Stage `internal/config/options.go internal/config/options_test.go internal/config/config.go internal/generator/generator.go internal/generator/component.go internal/generator/generator_test.go internal/prompt/prompt.go cmd/new.go cmd/new_test.go cmd/root.go` and ask the user to run `/grimoire-core:commit`. Suggested message: `feat(cli): validate every option against one registry before rendering`.

---

### Task 2: `nova new` refuses to clobber a non-empty directory

**Files:**

- Modify: `cmd/new.go` (`runNew`, new `checkTargetDir`)
- Modify: `cmd/new_test.go` (new `TestCheckTargetDir`)

**Interfaces:**

- Produces: `checkTargetDir(dir string, force bool) error`.

- [x] **Step 1: Write the failing test**

Append to `cmd/new_test.go` (add `"os"` and `"path/filepath"` to its imports):

```go
// TestCheckTargetDir pins the overwrite guard: Generate writes file by file
// and would silently clobber edits in an existing project.
func TestCheckTargetDir(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	missing := filepath.Join(root, "missing")
	empty := filepath.Join(root, "empty")
	full := filepath.Join(root, "full")
	for _, d := range []string{empty, full} {
		if err := os.Mkdir(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(full, "keep.go"), []byte("package keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkTargetDir(missing, false); err != nil {
		t.Errorf("missing dir: %v", err)
	}
	if err := checkTargetDir(empty, false); err != nil {
		t.Errorf("empty dir: %v", err)
	}
	if err := checkTargetDir(full, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("non-empty dir without --force: want error mentioning --force, got %v", err)
	}
	if err := checkTargetDir(full, true); err != nil {
		t.Errorf("non-empty dir with --force: %v", err)
	}
}
```

Run: `go test -run TestCheckTargetDir ./cmd` — Expected: FAIL with `undefined: checkTargetDir`.

- [x] **Step 2: Implement and wire the guard**

Add to `cmd/new.go` (imports gain `"errors"` and `"io/fs"`):

```go
// checkTargetDir refuses to render into a directory that already has content
// unless --force was given: Generate overwrites file by file and would
// silently clobber edits.
func checkTargetDir(dir string, force bool) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("inspect %s: %w", dir, err)
	case len(entries) == 0 || force:
		return nil
	default:
		return fmt.Errorf("%s already exists and is not empty; re-run with --force to overwrite", dir)
	}
}
```

In `runNew`, right after `outputDir := cfg.ProjectName`:

```go
	force, _ := cmd.Flags().GetBool("force")
	if err := checkTargetDir(outputDir, force); err != nil {
		return err
	}
```

- [x] **Step 3: Run tests and lint**

Run: `go test -short ./... && make lint` — Expected: PASS, `0 issues.`

- [ ] **Step 4: Commit**

Stage `cmd/new.go cmd/new_test.go`. Suggested message: `fix(cli): refuse to generate into a non-empty directory without --force`.

---

### Task 3: `nova add` validates names, requires a module, confines output, and never overwrites without `--force`

**Files:**

- Modify: `cmd/add.go` (`--force` flag, `validateComponent(c, m)`)
- Create: `cmd/add_test.go`
- Modify: `internal/generator/component.go` (`ComponentGenerator.Force`, `outPath`, `guardOverwrite`, `mergeOrWriteDI`)
- Modify: `internal/generator/component_render.go` (`renderTemplates`)
- Modify: `internal/generator/repository_sqlc.go` (write loop, migration skip)
- Modify: `internal/generator/component_test.go` (new tests)

**Interfaces:**

- Produces: `validateComponent(c *prompt.Component, m *manifest.Manifest) error`; `ComponentGenerator.Force bool`; `func (g *ComponentGenerator) outPath(rel string) (string, error)`; `func (g *ComponentGenerator) guardOverwrite(rel, abs string) error`.

- [x] **Step 1: Write the failing CLI test**

Create `cmd/add_test.go`:

```go
package cmd

import (
	"testing"

	"github.com/quyennguyenvu/nova/internal/manifest"
	"github.com/quyennguyenvu/nova/internal/prompt"
)

// TestValidateComponentNames pins the identifier rule: names flow raw into
// file paths, package names and Go identifiers, so anything that is not a Go
// identifier must stop before a file is written.
func TestValidateComponentNames(t *testing.T) {
	t.Parallel()
	m := manifest.Default()
	m.Module = "example.com/app"
	for _, bad := range []string{"../../escape", "Bad Name", "123abc", "order-item", "a.b", `x"y`} {
		c := prompt.Component{Type: "entity", Name: bad}
		if err := validateComponent(&c, m); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
	for _, good := range []string{"Order", "order", "OrderItem", "HTTPServer", "V2Thing"} {
		c := prompt.Component{Type: "entity", Name: good}
		if err := validateComponent(&c, m); err != nil {
			t.Errorf("name %q rejected: %v", good, err)
		}
	}
	c := prompt.Component{Type: "entity", Name: "Order"}
	if err := validateComponent(&c, manifest.Default()); err == nil {
		t.Error("empty module path accepted; generated imports would start with a bare slash")
	}
}
```

Run: `go test -run TestValidateComponentNames ./cmd` — Expected: FAIL (signature mismatch).

- [x] **Step 2: Validate names and the module in `cmd/add.go`**

Add `"regexp"` to the imports and, after `const addMaxArgs = 2`:

```go
// componentNameRE: the name becomes a Go identifier, a package name and a
// file name, so only letters and digits starting with a letter are safe.
//
//nolint:gochecknoglobals // compiled once; treated as a const.
var componentNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
```

Replace `validateComponent` with:

```go
func validateComponent(c *prompt.Component, m *manifest.Manifest) error {
	c.Type = strings.ToLower(c.Type)
	if !slices.Contains(prompt.SupportedComponents, c.Type) {
		return fmt.Errorf(
			"unknown component %q (supported: %s)",
			c.Type, strings.Join(prompt.SupportedComponents, ", "),
		)
	}
	if c.Name == "" {
		return errors.New("component name is required")
	}
	if !componentNameRE.MatchString(c.Name) {
		return fmt.Errorf("component name %q must be a Go identifier (letters and digits, starting with a letter)", c.Name)
	}
	if m.Module == "" {
		return errors.New("cannot determine the module path: run inside a Go module (go.mod) or set `module:` in nova.yaml")
	}
	if c.DB == "" {
		c.DB = m.Stack.Database
	}
	return nil
}
```

In `runAdd`: call `validateComponent(&c, m)`, register `addCmd.Flags().Bool("force", false, "Overwrite feature files that already exist")`, and after constructing `gen` set `gen.Force, _ = cmd.Flags().GetBool("force")`.

- [x] **Step 3: Write the failing generator tests**

Append to `internal/generator/component_test.go`:

```go
// TestGenerateEntityRefusesOverwriteWithoutForce pins that feature files are
// the user's to edit after generation: a re-run must stop, and --force must
// be the only way through.
func TestGenerateEntityRefusesOverwriteWithoutForce(t *testing.T) {
	gen, dir := newComponentGen(t, manifest.Default())
	if err := gen.GenerateEntity("Order"); err != nil {
		t.Fatalf("GenerateEntity: %v", err)
	}
	path := filepath.Join(dir, "internal/domain/entity/order.go")
	if err := os.WriteFile(path, []byte("package entity\n\n// USER EDIT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := gen.GenerateEntity("Order"); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("second GenerateEntity: want --force error, got %v", err)
	}
	assertFileContains(t, path, "USER EDIT")
	gen.Force = true
	if err := gen.GenerateEntity("Order"); err != nil {
		t.Fatalf("GenerateEntity --force: %v", err)
	}
	assertFileContains(t, path, "type Order struct")
}

// TestOutPathRefusesEscape pins the root confinement: a layout (or name) that
// resolves above the project root must fail instead of writing there.
func TestOutPathRefusesEscape(t *testing.T) {
	m := manifest.Default()
	m.Layout["entity"] = manifest.Target{Dir: "../outside", File: "{snake}.go", Package: "entity"}
	gen, dir := newComponentGen(t, m)
	if err := gen.GenerateEntity("Order"); err == nil || !strings.Contains(err.Error(), "outside the project root") {
		t.Fatalf("want escape error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "outside")); err == nil {
		t.Error("a file was written outside the project root")
	}
}

// TestGenerateRepositorySkipsExistingMigration pins that re-running the sqlc
// repository with --force refreshes impl/mapper/query but does not add a
// second create-table migration.
func TestGenerateRepositorySkipsExistingMigration(t *testing.T) {
	gen, dir := newComponentGen(t, manifest.Default())
	if err := gen.GenerateEntity("Order"); err != nil {
		t.Fatal(err)
	}
	if err := gen.GenerateRepository("Order", "postgres"); err != nil {
		t.Fatal(err)
	}
	gen.Force = true
	if err := gen.GenerateRepository("Order", "postgres"); err != nil {
		t.Fatalf("second GenerateRepository --force: %v", err)
	}
	ups, _ := filepath.Glob(filepath.Join(dir, "sqlc/migrations/*_create_orders_table.up.sql"))
	if len(ups) != 1 {
		t.Errorf("want exactly one orders migration, got %d: %v", len(ups), ups)
	}
}
```

Run: `go test -short -run 'TestGenerateEntityRefusesOverwriteWithoutForce|TestOutPathRefusesEscape|TestGenerateRepositorySkipsExistingMigration' ./internal/generator` — Expected: FAIL (`gen.Force` undefined).

- [x] **Step 4: Add `Force`, `outPath`, `guardOverwrite`**

In `internal/generator/component.go`:

```go
// ComponentGenerator generates individual components in an existing project.
// Output paths and package names come from the manifest, so it can target
// projects with non-standard layouts (see internal/manifest). Force lets
// feature files overwrite existing ones; shared files are always left alone.
type ComponentGenerator struct {
	baseDir  string
	manifest *manifest.Manifest
	Force    bool
}
```

Add after `path`:

```go
// outPath resolves rel under baseDir and refuses anything that escapes it, so
// a hostile name or nova.yaml layout cannot write outside the project.
func (g *ComponentGenerator) outPath(rel string) (string, error) {
	abs := filepath.Join(g.baseDir, rel)
	inside, err := filepath.Rel(g.baseDir, abs)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to write outside the project root: %s", rel)
	}
	return abs, nil
}

// guardOverwrite refuses to replace an existing feature file unless Force is
// set: these files are the user's to edit after generation.
func (g *ComponentGenerator) guardOverwrite(rel, abs string) error {
	if g.Force || !fileExists(abs) {
		return nil
	}
	return fmt.Errorf("%s already exists; re-run with --force to overwrite", rel)
}
```

In `mergeOrWriteDI`, replace `writeFile(filepath.Join(g.baseDir, fallbackRel), src)` with:

```go
		fallbackAbs, pErr := g.outPath(fallbackRel)
		if pErr != nil {
			return pErr
		}
		if wErr := writeFile(fallbackAbs, src); wErr != nil {
			return wErr
		}
```

In `internal/generator/component_render.go`, `renderTemplates`, replace the loop head through the `skipIfExists` check with:

```go
	for _, s := range specs {
		out, pErr := g.outPath(s.outRel)
		if pErr != nil {
			return pErr
		}
		if s.skipIfExists {
			if fileExists(out) {
				fmt.Fprintf(os.Stdout, "   ↩︎  exists, skipped %s\n", s.outRel)
				continue
			}
		} else if gErr := g.guardOverwrite(s.outRel, out); gErr != nil {
			return gErr
		}
```

In `internal/generator/repository_sqlc.go`, `generateSQLCRepository`: before building `files`, detect an existing migration and build the list conditionally:

```go
	ts := time.Now().Format("20060102150405")
	var files []genFile
	existing, _ := filepath.Glob(filepath.Join(g.baseDir, migRes.Dir, "*_create_"+table+"_table.up.sql"))
	if len(existing) > 0 {
		fmt.Fprintf(os.Stdout, "   ↩︎  migration for %s exists, skipped\n", table)
	} else {
		files = append(files,
			genFile{
				filepath.Join(migRes.Dir, fmt.Sprintf("%s_create_%s_table.up.sql", ts, table)),
				genMigrationUp(engine, table, insertCols),
				false,
			},
			genFile{
				filepath.Join(migRes.Dir, fmt.Sprintf("%s_create_%s_table.down.sql", ts, table)),
				genMigrationDown(table),
				false,
			},
		)
	}
	files = append(files,
		genFile{relOf(queryRes, ""), genQuery(engine, title, table, insertCols, updateCols), false},
		genFile{relOf(repoRes, ""), genRepoImpl(ic), true},
		genFile{
			filepath.Join(repoRes.Dir, "mapper", snakeCase(name)+".go"),
			genMapper(ic, cols, insertCols, updateCols),
			true,
		},
	)
```

and change the write loop head to:

```go
	for _, f := range files {
		abs, pErr := g.outPath(f.rel)
		if pErr != nil {
			return pErr
		}
		if gErr := g.guardOverwrite(f.rel, abs); gErr != nil {
			return gErr
		}
		content := f.content
		...
		if wErr := writeFile(abs, content); wErr != nil {
```

- [x] **Step 5: Run the new tests, the short suite, lint, then the full suite alone**

Run: `go test -short ./... && make lint` — Expected: PASS, `0 issues.`

Run (alone): `make test-all` — Expected: PASS (the matrix is unaffected; the bootstrap renders fresh dirs).

- [ ] **Step 6: Commit**

Stage `cmd/add.go cmd/add_test.go internal/generator/component.go internal/generator/component_render.go internal/generator/repository_sqlc.go internal/generator/component_test.go`. Suggested message: `fix(add): validate names, require a module, confine output and guard overwrites`.

---

### Task 4: One `Snake`, one `Title`, a real `plural`

**Files:**

- Modify: `internal/manifest/manifest.go:218-249` (`expand`, `title`, `snake`)
- Modify: `internal/manifest/manifest_test.go` (`TestResolve` case, new `TestSnake`)
- Modify: `internal/generator/repository_sqlc.go` (`plural`, delete `snakeCase`, use `manifest.Snake`)
- Modify: `internal/generator/component.go` (delete `toTitle`, use `manifest.Title`)
- Modify: `internal/generator/component_test.go` (move `TestSnakeCaseAcronyms`, add `TestPlural`)

**Interfaces:**

- Produces: `manifest.Snake(s string) string` (acronym-aware), `manifest.Title(s string) string`.

- [x] **Step 1: Write the failing tests**

In `internal/manifest/manifest_test.go` add a `TestResolve` row `{"entity acronym", "entity", "HTTPServer", "", "internal/domain/entity", "http_server.go", "entity"},` and append:

```go
// TestSnake pins the acronym-aware rule shared with the sqlc generator so
// the entity file, table and mapper names agree.
func TestSnake(t *testing.T) {
	cases := map[string]string{
		"ID": "id", "UserID": "user_id", "CreatedAt": "created_at",
		"HTTPServer": "http_server", "Name": "name", "OrderItem": "order_item",
	}
	for in, want := range cases {
		if got := Snake(in); got != want {
			t.Errorf("Snake(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Title("order"); got != "Order" {
		t.Errorf("Title = %q", got)
	}
}
```

In `internal/generator/component_test.go` delete `TestSnakeCaseAcronyms` and append:

```go
func TestPlural(t *testing.T) {
	cases := map[string]string{
		"order": "orders", "category": "categories", "box": "boxes",
		"status": "statuses", "day": "days", "batch": "batches",
	}
	for in, want := range cases {
		if got := plural(in); got != want {
			t.Errorf("plural(%q) = %q, want %q", in, got, want)
		}
	}
}
```

Run: `go test ./internal/manifest && go test -short -run TestPlural ./internal/generator` — Expected: FAIL (`Snake` undefined; `categorys`).

- [x] **Step 2: Implement**

In `internal/manifest/manifest.go` replace `title` and `snake` with:

```go
// Title upper-cases the first rune: order -> Order.
func Title(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// Snake converts a Go identifier to snake_case, keeping acronyms intact
// (ID -> id, UserID -> user_id, HTTPServer -> http_server). It is the one
// rule for the {snake} placeholder, the sqlc table and column names and the
// mapper file name, so they always agree.
func Snake(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			prevLower := i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]))
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if i > 0 && (prevLower || nextLower) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
```

and in `expand` use `Snake(name)` and `Title(name)`.

In `internal/generator/repository_sqlc.go` delete `snakeCase` (and its comment), replace every `snakeCase(` with `manifest.Snake(`, and replace `plural` with:

```go
// plural forms the table name from a snake_case entity (category -> categories,
// box -> boxes, order -> orders). Good enough for scaffolding; rename the table
// in the migration if English disagrees.
func plural(s string) string {
	switch {
	case strings.HasSuffix(s, "y") && len(s) > 1 && !strings.ContainsRune("aeiou", rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "z"),
		strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	default:
		return s + "s"
	}
}
```

In `internal/generator/component.go` delete `toTitle` and replace every `toTitle(` with `manifest.Title(`; in `repository_sqlc.go` do the same (`title := manifest.Title(name)`). Remove the `unicode` import from `component.go` if nothing else uses it.

- [x] **Step 3: Tests and lint**

Run: `go test -short ./... && make lint` — Expected: PASS, `0 issues.`

Run (alone): `make test-all` — Expected: PASS.

- [ ] **Step 4: Commit**

Stage `internal/manifest/manifest.go internal/manifest/manifest_test.go internal/generator/repository_sqlc.go internal/generator/component.go internal/generator/component_test.go`. Suggested message: `fix(add): share one acronym-aware Snake/Title and pluralize table names properly`.

---

### Task 5: `--type` must match the project's engine; correct worker hint

**Files:**

- Modify: `internal/generator/component.go` (`GenerateRepository`, worker hint line)
- Modify: `internal/generator/component_test.go` (new test)

- [x] **Step 1: Write the failing test**

```go
// TestGenerateRepositoryRejectsForeignEngine pins that --type cannot silently
// drop a mysql repository (and mysql DDL) into a postgres project.
func TestGenerateRepositoryRejectsForeignEngine(t *testing.T) {
	gen, _ := newComponentGen(t, manifest.Default()) // stack: postgres
	if err := gen.GenerateEntity("Order"); err != nil {
		t.Fatal(err)
	}
	err := gen.GenerateRepository("Order", "mysql")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("want engine mismatch error, got %v", err)
	}
}
```

Run: `go test -short -run TestGenerateRepositoryRejectsForeignEngine ./internal/generator` — Expected: FAIL (no error returned).

- [x] **Step 2: Implement**

In `GenerateRepository`, after `requireEntity`:

```go
	if repoType != g.manifest.Stack.Database {
		res, _ := g.manifest.Resolve("repository", name, repoType)
		if !fileExists(filepath.Join(g.baseDir, res.Dir)) {
			return fmt.Errorf(
				"--type=%s does not match the project's database (%s) and %s does not exist; "+
					"use the project's engine or add that adapter package first",
				repoType, g.manifest.Stack.Database, res.Dir,
			)
		}
	}
```

Change the worker hint line to:

```go
	fmt.Fprintf(os.Stdout, "   ▶ add more feature handlers to provideWorkerHandlers in internal/infrastructure/di/provider.go\n")
```

- [x] **Step 3: Tests and lint**

Run: `go test -short ./... && make lint` — Expected: PASS, `0 issues.`

- [ ] **Step 4: Commit**

Stage `internal/generator/component.go internal/generator/component_test.go`. Suggested message: `fix(add): reject --type that does not match the project engine`.

---

### Task 6: Generators print through an injectable writer

**Files:**

- Modify: `internal/generator/generator.go` (`Generator.Out`, `New`, `renderFile`)
- Modify: `internal/generator/component.go`, `component_render.go`, `repository_sqlc.go` (every `fmt.Fprintf(os.Stdout`)
- Modify: `internal/generator/generator_test.go` (`renderProject`, `renderAndVet`), `internal/generator/component_test.go` (`newComponentGen`)

**Interfaces:**

- Produces: `Generator.Out io.Writer` and `ComponentGenerator.Out io.Writer`, both defaulting to `os.Stdout` in the constructors.

- [x] **Step 1: Implement**

In `generator.go`: add `Out io.Writer // progress output; os.Stdout by default` to `Generator`, set `Out: os.Stdout` in `New`, and print with `fmt.Fprintf(g.Out, ...)` in `renderFile`. Add `"io"` to the imports.

In `component.go`: add `Out io.Writer` to `ComponentGenerator`, set `Out: os.Stdout` in `NewComponentGenerator`, and replace every `fmt.Fprintf(os.Stdout,` in `component.go`, `component_render.go` and `repository_sqlc.go` with `fmt.Fprintf(g.Out,` (all call sites are methods on `g`). Add `"io"` imports where needed.

In the tests: `newComponentGen` sets `gen.Out = io.Discard` before returning; `renderProject` and `renderAndVet` set `gen.Out = io.Discard` after `New`. Test output loses the per-file `📄` lines.

- [x] **Step 2: Tests and lint**

Run: `go test -short ./... && make lint` — Expected: PASS, `0 issues.`

- [ ] **Step 3: Commit**

Stage the five Go files. Suggested message: `refactor(generator): print progress through an injectable io.Writer`.

---

### Task 7: Move the Go AST merge helpers to `internal/gosrc`

**Files:**

- Create: `internal/gosrc/merge.go`, `internal/gosrc/merge_test.go`
- Modify: `internal/generator/component.go` (delete `mergeGoDecls`, `splitGoScaffold`, `declDoc`, `injectImports`, `importGroup`, `importPath`; call `gosrc.MergeDecls`)

**Interfaces:**

- Produces: `gosrc.MergeDecls(absPath, scaffoldSrc string) error` with the semantics of today's `mergeGoDecls`.

- [x] **Step 1: Write the failing test**

Create `internal/gosrc/merge_test.go`:

```go
package gosrc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMergeDeclsInjectsImportsAndDecls pins the merge contract: every
// non-import declaration of the scaffold is appended, imports the target
// lacks are added (creating a block when the file has none), and the result
// is gofmt-clean.
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
	for _, want := range []string{"import \"context\"", "type HTTPApp struct{}", "// WorkerApp bundles the worker.", "type WorkerApp struct"} {
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
```

Run: `go test ./internal/gosrc` — Expected: FAIL (package missing).

- [x] **Step 2: Move the code**

Create `internal/gosrc/merge.go` with package doc `// Package gosrc edits Go source files structurally: merging scaffolded declarations into an existing file while keeping its imports and formatting intact.` Move `mergeGoDecls` (renamed `MergeDecls`, exported doc comment kept), `splitGoScaffold`, `declDoc`, `injectImports`, `importGroup`, `importPath` verbatim from `component.go` into it, with imports `fmt`, `go/ast`, `go/format`, `go/parser`, `go/token`, `os`, `strings`. In `component.go` replace `mergeGoDecls(targetAbs, src)` with `gosrc.MergeDecls(targetAbs, src)`, add the import, and drop the orphaned `go/ast`, `go/parser`, `go/token` imports (keep `go/format` only if `registerWorkerCommand` still uses it, which it does).

Note: gofmt may collapse `import (\n\t"context"\n)` to `import "context"` only when it rewrites; if the first assertion fails on that exact string, assert on `"context"` alone.

- [x] **Step 3: Tests and lint, then full suite alone**

Run: `go test -short ./... && make lint` — Expected: PASS, `0 issues.`

Run (alone): `make test-all` — Expected: PASS.

- [ ] **Step 4: Commit**

Stage `internal/gosrc/merge.go internal/gosrc/merge_test.go internal/generator/component.go`. Suggested message: `refactor: move Go declaration merging into internal/gosrc`.

---

### Task 8: Docs describe the validated CLI

**Files:**

- Modify: `docs/01-cli-options.md` (table + failure-modes section)
- Modify: `README.md` (flags table, warning paragraph, `nova add` flags, placeholders)
- Modify: `CLAUDE.md` (whitelist sentence, placeholders, `--force`)
- Modify: `docs/superpowers/plans/2026-10-04-review-remediation-roadmap.md` (phase 1 done, phase 2 link)

- [x] **Step 1: docs/01**

Replace the table's "Options (default first)" and "Implemented today" columns with a single "Values (default first)" column listing exactly the registry values; drop the gRPC-Gateway, driver, query and config-format rows (no longer prompted; the flags remain and accept only the registry value). Replace the section "Unimplemented options fail in three different ways" with "Validation", stating: every value is checked by `ProjectConfig.Validate()` before a file is written; the error lists every problem; `cron`, `cli`, `nethttp`, `sqlite`, `mongodb`, `sqlx`, `gorm`, `raw`, `bigcache`, `nats`, `toml`, `env` and `--grpc-gateway` are rejected; `--transport` is required when any flag is given; `--database=none` renders the in-memory repository (keep that bullet); `--force` is required to render into a non-empty directory.

- [x] **Step 2: README.md and CLAUDE.md**

README: in the flags table set `--docker` default `true` with the note `--docker=false to skip`, add `--force`; rewrite the paragraph beginning "Because any flag skips the prompts" to say nova validates every value and refuses unimplemented ones before writing files; in the `nova add` section mention `--force` and that names must be Go identifiers; in the `nova.yaml` placeholder list add `{cache}`.

CLAUDE.md: change "extend the whitelist in `generator.New()`" to "add the value to the option sets in `internal/config/options.go` (`Validate` is the single gate)"; add `{cache}` to the placeholder list; mention `--force` for both commands and that `nova add` confines all output under the project root.

Roadmap: mark Phase 1 "(done 2026-10-05, commit `635595b`)" and add `Detailed plan: [2026-10-07-phase2-cli-hardening.md](2026-10-07-phase2-cli-hardening.md).` under Phase 2.

- [x] **Step 3: Lint**

Run: `npx --yes markdownlint-cli2 docs/01-cli-options.md README.md CLAUDE.md "docs/superpowers/plans/*.md"` — Expected: `Summary: 0 issues`.

- [ ] **Step 4: Commit**

Stage the four Markdown files. Suggested message: `docs: describe option validation, --force, and the shared name helpers`.

---

## Self-review notes

- Spec coverage against the roadmap's phase 2 list: option registry + `Validate()` + normalization (Task 1); `Flags().Changed` booleans and `--ci` validation (Task 1); dead `--type`/`--grpc-gateway` removed (Task 1); project-name and module validation (Task 1); non-empty target dir + `--force` (Task 2); `nova add` name regex, root confinement, overwrite guard, module requirement, migration de-dup (Task 3); one `Snake`/`Title`, real `plural` (Task 4); `--type` engine guard and corrected worker hint (Task 5); `io.Writer` injection (Task 6); AST merge helpers moved (Task 7); docs/01 honesty (Task 8). Deferred to phase 5 with a roadmap note: wiring a second `nova add worker` feature into `provideWorkerHandlers` automatically.
- Deliberately kept: `nova new` with flags but no name still renders `./myproject`; it is the documented Quick Start default and is now guarded by the non-empty-directory check.
- Type consistency: `Validate() error` (Task 1) is what `generator.New` calls; `applyFlags` returns `error` in Task 1 and the test in the same task; `ComponentGenerator.Force`/`Out` are plain exported fields set after construction; `manifest.Snake`/`manifest.Title` replace `snakeCase`/`toTitle` everywhere in Task 4; `gosrc.MergeDecls(absPath, scaffoldSrc string) error` mirrors `mergeGoDecls`.
