# Phase 6: Layers and Domain Correctness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Heavy steps (`make test-all`) run alone from the main thread; see Global Constraints.

**Goal:** A `nova new` project obeys its own dependency rule (no `usecase` → `infrastructure` import), cannot return a typed-nil `*AppError`, answers a concurrent duplicate registration with 409 instead of 500, runs `Register` and `List` inside the transaction manager it already renders, enforces the user's invariants in the entity so every transport inherits them, reports the real `UpdatedAt` after an update, starts without Redis and caches with jittered TTLs, carries only identity claims in the principal, registers locale translations once, and uses `golang-jwt/jwt/v5`.

**Architecture:** A `security.TokenIssuer` port in `domain/security` replaces the concrete `*jwtsec.TokenService` in the usecase; DI binds the JWT service to it (wire `Bind`, an fx caster). `errors.Wrap`/`Wrapf` return `error` (nil in, nil out) while `L`/`FromStatus` keep returning `*AppError` because they never return nil; `ToAppError`/`As` still reach the concrete type. `domain.TxManager` is provided for every storage (a no-op for the in-memory repository) and injected into the user service, which hashes outside the transaction and runs the uniqueness check plus insert inside it, and runs page plus count inside one for `List`. Both SQL adapters translate the driver's unique-violation error to `errors.ErrAlreadyExists` through a `translate` helper, and the usecase maps that to `locale.UserEmailExists`. `entity.NewUser` and `User.Rename` own the email and name invariants and stamp timestamps; `UpdateUser` takes `updated_at` from the entity so the response is honest. `cache.New` logs and continues when Redis is unreachable, the decorator jitters its TTL and treats a failed invalidation as a warning, the principal and claims drop the device and session fields, `locale.NewMapping` is guarded by `sync.Once`, and the JWT service moves to v5's sentinel errors and parser options. Generated unit tests (errors, usecase with fakes, adapter translation) ship with the project and the bootstrap runs them; `TestDependencyRule` and `TestLayersContract` pin it all on the nova side.

**Tech Stack:** `github.com/golang-jwt/jwt/v5`, `github.com/jackc/pgx/v5/pgconn`, `github.com/go-sql-driver/mysql`, `net/mail`, `math/rand/v2`, `go/parser` (nova-side test), sqlc.

**Status (2026-10-14):** all five tasks implemented and verified (`make test` green; `make lint` 0 issues; markdownlint 0 issues on every edited file; `make test-all` green at ~2.5 min with 36 vet rows and 6 bootstraps, each bootstrap now running the rendered `pkg`, `usecase`, `adapter` and worker tests); commit steps pending the user's `/grimoire-core:commit`.

**Deviations from the plan as written:**

- `errors.L` and `errors.FromStatus` keep returning `*AppError` (the roadmap listed `L` among the functions to change): they never return nil, so there is no typed-nil hazard, and `.WithCause` chaining depends on the concrete type. Only `Wrap`/`Wrapf` return `error`.
- `adapterFiles` in `generator.go` crossed the `funlen` limit once the SQL adapters gained `errors.go`/`errors_test.go`; the two engines' repository entries moved into `sqlRepositoryFiles(engine)`, which derives both paths from the engine name. `assertLayersContract` was split into `assertUsecaseSeams`/`assertStorageSeams` for the same reason.
- `TestDependencyRule` matches an allowed package exactly or as a subpackage (a trailing-slash prefix check rejected the bare `internal/domain` import).
- The jwt v5 module (v5.3.1) was not in the module cache; the bootstrap's `go mod tidy` fetched it.
- Two generated test lines were wrapped or shortened to stay under golines' 120 columns.
- The skel (`nova add`) entity and usecase scaffolds are unchanged: they are stubs with no invariants to move, so the "nova add output bypasses them" finding is covered by the User entity alone.

## Global Constraints

- `make lint` and `make test` stay green after every task; `make test-all` runs alone at the end of tasks 3 and 5.
- Resource rule: one heavy toolchain command at a time from the main thread; never `go clean -cache`.
- `domain/` imports only the standard library and other `domain/` packages; `usecase/` imports `domain/` and `pkg/` only (`TestDependencyRule` enforces both).
- Transport may import `infrastructure/jwt` (verification is a middleware concern); only the usecase seam changes.
- `errors.L`, `errors.FromStatus` and `AppError.WithCause` keep returning `*AppError`; `errors.Wrap`/`Wrapf` return `error`. Nothing may assign a `Wrap` result to an `*AppError` variable.
- Generated test files pass the generated project's `golangci-lint run`: no named returns, `t.Context()`, comments end with a period, no `t.Parallel()`.
- Every change to a SQL query is mirrored in both engines and in both dbgen stubs in `generator_test.go`.
- Markdown edits pass markdownlint with the repo config. Commits only via `/grimoire-core:commit` after confirmation.

---

### Task 1: The usecase depends on a port, not on JWT

**Files:**

- Create: `internal/generator/templates/domain/security/token.go.tmpl`
- Modify: `internal/generator/templates/infrastructure/jwt/service.go.tmpl` (interface assertion), `internal/generator/templates/usecase/user/service.go.tmpl` (field, constructor, `Login`)
- Modify: `internal/generator/templates/infrastructure/di/wire.go.tmpl`, `fx.go.tmpl`, `fx_provider.go.tmpl`
- Modify: `internal/generator/generator.go` (`domainFiles`), `internal/generator/generator_test.go` (`TestDependencyRule`, `TestLayersContract` first assertions)
- Modify: `docs/03-architecture-rules.md`, `docs/04-placement-rationale.md`, `docs/06-usecase-layer.md`, `docs/02-project-layout.md`

**Interfaces:**

- Produces: `security.TokenIssuer` with `Sign(p *identity.UserPrincipal) (string, error)` and `AccessTokenTTL() time.Duration`; `usersvc.NewService(userRepo domain.UserRepository, hasher security.PasswordHasher, tokens security.TokenIssuer[, publisher domain.UserPublisher])` (the `domain.TxManager` parameter is added in task 3); `di.provideTokenIssuer(ts *jwtsec.TokenService) domainsec.TokenIssuer` (fx).

- [x] **Step 1: Write the failing tests**

Append to `internal/generator/generator_test.go`:

```go
// TestDependencyRule walks the rendered domain and usecase packages and fails
// on any import that points outward: domain may import only the standard
// library and other domain packages, usecase additionally pkg/. The vet matrix
// cannot catch this (an outward import compiles fine), so it is pinned here.
func TestDependencyRule(t *testing.T) {
	t.Parallel()
	cfg := httpMatrixConfig("fiber", "postgres", "wire")
	cfg.MessageQueue = "kafka"
	for _, c := range []*config.ProjectConfig{cfg, workerMatrixConfig("mysql", "rabbitmq", "fx")} {
		dir := renderProject(t, c)
		assertInwardImports(t, dir, "internal/domain", "internal/domain/")
		assertInwardImports(t, dir, "internal/usecase", "internal/domain/", "pkg/")
	}
}

// assertInwardImports checks every .go file under root: an import of the
// project's own module must start with one of the allowed prefixes.
func assertInwardImports(t *testing.T, dir, root string, allowed ...string) {
	t.Helper()
	module := "matrixtest/"
	walkErr := filepath.WalkDir(filepath.Join(dir, root), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(p, module) {
				continue
			}
			ok := false
			for _, a := range allowed {
				ok = ok || strings.HasPrefix(p, module+a)
			}
			if !ok {
				rel, _ := filepath.Rel(dir, path)
				t.Errorf("%s imports %s: %s may only import %v", rel, p, root, allowed)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
}

// TestLayersContract pins the phase 6 contract: the usecase depends on ports
// only, Wrap cannot return a typed nil, transactions and unique-violation
// translation guard Register, the entity owns its invariants, and the
// infrastructure degrades instead of blocking.
func TestLayersContract(t *testing.T) {
	t.Parallel()
	t.Run("postgres_wire", func(t *testing.T) {
		t.Parallel()
		cfg := httpMatrixConfig("fiber", "postgres", "wire")
		cfg.MessageQueue = "kafka"
		assertLayersContract(t, renderProject(t, cfg), "postgres")
	})
	t.Run("mysql_fx", func(t *testing.T) {
		t.Parallel()
		assertLayersContract(t, renderProject(t, httpMatrixConfig("gin", "mysql", "fx")), "mysql")
	})
	t.Run("memory", func(t *testing.T) {
		t.Parallel()
		dir := renderProject(t, noDBMatrixConfig("echo", "wire"))
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/adapter/repository/memory/tx_manager.go")),
			"func NewTxManager() *TxManager")
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/wire.go")),
			"memoryrepo.NewTxManager", "wire.Bind(new(domain.TxManager), new(*memoryrepo.TxManager))")
	})
}

func assertLayersContract(t *testing.T, dir, engine string) {
	t.Helper()
	svc := readFile(t, filepath.Join(dir, "internal/usecase/user/service.go"))
	mustContainAll(t, svc, "security.TokenIssuer", "domain.TxManager", "entity.NewUser(", "user.Rename(",
		"errors.ErrAlreadyExists", "s.tx.WithinTx(")
	mustNotContain(t, svc, "jwtsec", "infrastructure/")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/domain/security/token.go")), "type TokenIssuer interface")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/jwt/service.go")),
		"var _ security.TokenIssuer = (*TokenService)(nil)", "github.com/golang-jwt/jwt/v5", "jwt.WithIssuer(",
		"jwt.ErrTokenSignatureInvalid")
	mustNotContain(t, readFile(t, filepath.Join(dir, "internal/infrastructure/jwt/service.go")), "ValidationError")
	mustNotContain(t, readFile(t, filepath.Join(dir, "internal/domain/identity/principal.go")),
		"SessionID", "DeviceID", "Platform", "Phone")
	if engine == "postgres" {
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/wire.go")),
			"wire.Bind(new(domainsec.TokenIssuer), new(*jwtsec.TokenService))",
			"postgresrepo.NewTxManager", "wire.Bind(new(domain.TxManager), new(*postgresrepo.TxManager))")
	} else {
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/fx_provider.go")),
			"func provideTokenIssuer(ts *jwtsec.TokenService) domainsec.TokenIssuer")
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/di/fx.go")),
			"provideTokenIssuer,", "fx.Annotate(mysqlrepo.NewTxManager, fx.As(new(domain.TxManager)))")
	}
	errs := readFile(t, filepath.Join(dir, "pkg/errors/errors.go"))
	mustContainAll(t, errs, "func Wrap(err error, message string) error", "func Wrapf(err error, format string, args ...any) error")
	mustContainAll(t, readFile(t, filepath.Join(dir, "pkg/errors/errors_test.go")), "func TestWrapNilIsNil(")
	mustContainAll(t, readFile(t, filepath.Join(dir, "pkg/locale/locale.go")), "sync.Once")
	repo := readFile(t, filepath.Join(dir, "internal/adapter/repository/"+engine+"/user_repository.go"))
	mustContainAll(t, repo, "translate(err)")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/adapter/repository/"+engine+"/errors.go")),
		"func translate(err error) error", "errors.ErrAlreadyExists")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/adapter/repository/"+engine+"/errors_test.go")),
		"func TestTranslateUniqueViolation(")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/usecase/user/service_test.go")),
		"func TestRegisterDuplicateEmailIs409(", "func TestUpdateReportsNewUpdatedAt(")
	entity := readFile(t, filepath.Join(dir, "internal/domain/entity/user.go"))
	mustContainAll(t, entity, "func NewUser(email, name, passwordHash string) (*User, error)", "func (u *User) Rename(name string) error",
		"ErrEmailInvalid", "ErrNameInvalid")
	mustNotContain(t, readFile(t, filepath.Join(dir, "internal/domain/user.go")), "Name   string")
	query := readFile(t, filepath.Join(dir, "sqlc/query/user.sql"))
	if engine == "postgres" {
		mustContainAll(t, query, "UPDATE users SET name = $1, updated_at = $2")
	} else {
		mustContainAll(t, query, "UPDATE users SET name = ?, updated_at = ?")
	}
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/cache/redis.go")), "starting degraded")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/adapter/repository/redis/user_cache.go")),
		"userCacheJitter", "invalidate failed")
}
```

Add `"go/parser"` and `"go/token"` to the test file's imports (`fs` is already imported).

- [x] **Step 2: Run them to verify they fail**

Run: `go test -run 'TestDependencyRule|TestLayersContract' ./internal/generator/`
Expected: FAIL — `internal/usecase/user/service.go imports matrixtest/internal/infrastructure/jwt`.

- [x] **Step 3: The port**

`templates/domain/security/token.go.tmpl`:

```go
package security

import (
	"time"

	"{{.ModuleName}}/internal/domain/identity"
)

// TokenIssuer signs access tokens for an authenticated principal and reports
// their lifetime. Only the login flow needs it, so only signing is a port;
// verification is a transport (middleware) concern that calls the
// infrastructure/jwt service directly.
type TokenIssuer interface {
	Sign(p *identity.UserPrincipal) (string, error)
	AccessTokenTTL() time.Duration
}
```

`generator.go` `domainFiles`: add `{"templates/domain/security/token.go.tmpl", "internal/domain/security/token.go", true}` after the hasher entry.

`infrastructure/jwt/service.go.tmpl`: import `"{{.ModuleName}}/internal/domain/security"` and add `var _ security.TokenIssuer = (*TokenService)(nil)` after the `TokenService` struct. The package comment's last sentence becomes "the TokenService surface (Sign / ParseAndVerifyAccessToken) is what middleware depends on; the usecase sees only the security.TokenIssuer port."

- [x] **Step 4: The usecase**

In `usecase/user/service.go.tmpl`: drop the `jwtsec` import; the struct field becomes `tokens    security.TokenIssuer`; `NewService` takes `tokens security.TokenIssuer`; `Login` calls `s.tokens.Sign(principal)` and `s.tokens.AccessTokenTTL()`. The struct comment's second paragraph becomes:

```go
// Login lives on this service rather than a separate auth feature because
// authentication IS a user concern in the scaffold; it signs through the
// security.TokenIssuer port, so the usecase never imports infrastructure.
// Split it out when admin/service-account auth diverges from end-user auth.
```

- [x] **Step 5: DI**

`wire.go.tmpl` infraSet: after `jwtsec.NewTokenService,` add `wire.Bind(new(domainsec.TokenIssuer), new(*jwtsec.TokenService)),`. `fx_provider.go.tmpl`: import `jwtsec "{{.ModuleName}}/internal/infrastructure/jwt"` and `domainsec "{{.ModuleName}}/internal/domain/security"`, and add after `provideObservabilityLogger`:

```go
// provideTokenIssuer bridges the concrete signer to the usecase's port; the
// concrete type stays provided for the Auth middleware's verify path.
func provideTokenIssuer(ts *jwtsec.TokenService) domainsec.TokenIssuer { return ts }
```

`fx.go.tmpl` infraModule: `provideTokenIssuer,` after `jwtsec.NewTokenService,`.

- [x] **Step 6: Docs**

`docs/03`: the `infrastructure/jwt` paragraph: signing is reached through the `domain/security.TokenIssuer` port (bound in DI), verification needs no port. `docs/04`: the "Shipped variant" blockquote now says `nova new` ships `domain/security/token.go` with `TokenIssuer`, the user service holds the port, and `Login` is testable with a fake; the summary row "Token generator (login) | Yes | `domain/security/token.go` | `infrastructure/jwt/service.go`"; the `domain.TokenGenerator` snippets are renamed to the shipped shape. `docs/06`: line 3 drops "(plus `infrastructure/jwt` for the login flow)"; the service-struct paragraph and snippet show `tokens security.TokenIssuer` and `tx domain.TxManager`. `docs/02`: `security/` lists `token.go`, the usecase comment "Depends on: domain + pkg only".

- [x] **Step 7: Verify**

Run: `go test -run 'TestDependencyRule|TestRenderedGoIsGofmtClean|TestObservabilityContract' ./internal/generator/` then `make lint` and markdownlint on docs 02, 03, 04, 06.
Expected: `TestDependencyRule` PASS; the rest PASS; 0 issues.

- [ ] **Step 8: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): sign tokens through a domain port so the usecase imports no infrastructure`).

---

### Task 2: Wrap cannot return a typed nil; translations register once

**Files:**

- Modify: `internal/generator/templates/pkg/errors/errors.go.tmpl`, `internal/generator/templates/pkg/locale/locale.go.tmpl`
- Create: `internal/generator/templates/pkg/errors/errors_test.go.tmpl`
- Modify: `internal/generator/generator.go` (`pkgFiles`), `internal/generator/generator_test.go` (`bootstrapProject` runs the package tests)
- Modify: `docs/10-shared-packages.md`, `docs/06-usecase-layer.md` (error discipline table note)

**Interfaces:**

- Produces: `errors.Wrap(err error, message string) error`, `errors.Wrapf(err error, format string, args ...any) error` (nil for nil); `errors.L`, `errors.FromStatus`, `(*AppError).WithCause` unchanged.

- [x] **Step 1: The generated test**

`templates/pkg/errors/errors_test.go.tmpl`:

```go
package errors

import (
	"net/http"
	"testing"

	"{{.ModuleName}}/pkg/locale"
)

// TestWrapNilIsNil pins the reason Wrap and Wrapf return error rather than
// *AppError: a typed nil pointer in an error interface is non-nil, so
// `return errors.Wrap(err, "...")` on a nil err must yield a true nil.
func TestWrapNilIsNil(t *testing.T) {
	if err := Wrap(nil, "ctx"); err != nil {
		t.Fatalf("Wrap(nil) = %v, want nil", err)
	}
	if err := Wrapf(nil, "ctx %d", 1); err != nil {
		t.Fatalf("Wrapf(nil) = %v, want nil", err)
	}
}

func TestWrapKeepsClassification(t *testing.T) {
	err := Wrapf(L(locale.UserEmailExists, "a@b.co"), "register")
	app := ToAppError(err)
	if app.Code != http.StatusConflict || app.Locale != locale.UserEmailExists {
		t.Fatalf("got code=%d locale=%d, want 409/%d", app.Code, app.Locale, locale.UserEmailExists)
	}
	if len(app.Frames()) != 2 {
		t.Fatalf("frames = %d, want 2 (L + Wrapf)", len(app.Frames()))
	}
}

func TestSentinelsClassify(t *testing.T) {
	cases := map[error]int{
		ErrNotFound:      http.StatusNotFound,
		ErrAlreadyExists: http.StatusConflict,
		ErrInvalid:       http.StatusBadRequest,
		ErrUnauthorized:  http.StatusUnauthorized,
		ErrForbidden:     http.StatusForbidden,
	}
	for sentinel, want := range cases {
		if got := ToAppError(Wrap(sentinel, "op")).Code; got != want {
			t.Errorf("%v → %d, want %d", sentinel, got, want)
		}
	}
}
```

`generator.go` `pkgFiles`: add `{"templates/pkg/errors/errors_test.go.tmpl", "pkg/errors/errors_test.go", true}` after the errors entry.

- [x] **Step 2: Run the contract test to verify it fails**

Run: `go test -run TestLayersContract ./internal/generator/`
Expected: FAIL on `func Wrap(err error, message string) error`.

- [x] **Step 3: errors.go**

Replace the `Wrap`/`Wrapf` declarations:

```go
// Wrap attaches a caller frame (with an optional context message) to err.
// If err is already an *AppError, the frame is appended and Code/Message/
// Locale/Args are preserved so the originating layer's classification stays
// authoritative. If err is a plain or sentinel error, it is classified into
// HTTP terms. Returns nil for a nil err — and returns error, not *AppError,
// so that nil is a true nil and `return errors.Wrap(err, "...")` is safe
// unconditionally. Reach the *AppError with ToAppError or errors.As.
func Wrap(err error, message string) error {
	if err == nil {
		return nil
	}
	return wrapAt(err, message, 1)
}

// Wrapf is Wrap with fmt.Sprintf-style formatting for the context message.
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return wrapAt(err, fmt.Sprintf(format, args...), 1)
}
```

(`wrapAt` keeps returning `*AppError`.) The package comment's usecase example gains nothing; `L`'s comment gains "L never returns nil, so it keeps the concrete type for chaining WithCause."

- [x] **Step 4: locale.go**

```go
// mappingOnce guards the translations map: NewMapping is called from the DI
// provider and from tests, and a second registration must not race the
// first reader.
var mappingOnce sync.Once

// NewMapping registers all language translations. Safe to call more than
// once; only the first call does work.
func NewMapping() {
	mappingOnce.Do(func() {
		enMapping()
		viMapping()
	})
}
```

(import `"sync"`.)

- [x] **Step 5: The bootstrap runs package tests**

In `bootstrapProject`, replace the worker-only `go test` with:

```go
	// Rendered unit tests (errors, usecase with fakes, adapter translation,
	// worker policy and settle): run them so they are proven, not just compiled.
	pkgs := []string{"./pkg/...", "./internal/usecase/...", "./internal/adapter/..."}
	if cfg.HasWorker() {
		pkgs = append(pkgs, "./internal/transport/worker/...")
	}
	run("go", append([]string{"test"}, pkgs...)...)
```

- [x] **Step 6: Docs**

`docs/10`: the "Two constructors, two intents" section ends with: `Wrap`/`Wrapf` return `error`, not `*AppError`, because a nil `*AppError` stored in an `error` is not nil; `Wrap(nil, …)` returns a true nil so `return errors.Wrap(err, "…")` is safe, and `errors.ToAppError`/`errors.As` recover the concrete type at the boundary. The re-export snippet is unchanged. `docs/06` error table note: "`L` returns `*errors.AppError` (never nil, so `.WithCause` chains); `Wrapf` returns `error`."

- [x] **Step 7: Verify**

Run: `go test -run 'TestLayersContract|TestRenderedGoIsGofmtClean' ./internal/generator/`, `make lint`, markdownlint on docs 06 and 10.
Expected: `TestLayersContract` still fails only on task 3+ assertions; 0 issues.

- [ ] **Step 8: Commit** — stop and ask the user to run `/grimoire-core:commit` (`fix(generator): make Wrap return error so a nil cause is a true nil`).

---

### Task 3: Transactions and the duplicate-email 409

**Files:**

- Create: `internal/generator/templates/adapter/repository/postgres/errors.go.tmpl`, `errors_test.go.tmpl`; `internal/generator/templates/adapter/repository/mysql/errors.go.tmpl`, `errors_test.go.tmpl`; `internal/generator/templates/adapter/repository/memory/tx_manager.go.tmpl`; `internal/generator/templates/usecase/user/service_test.go.tmpl`
- Modify: `internal/generator/templates/adapter/repository/{postgres,mysql}/user_repository.go.tmpl` (`Create`), `internal/generator/templates/usecase/user/service.go.tmpl` (`tx` field, `Register`, `List`), DI templates, `internal/generator/generator.go`, `docs/08-adapter-layer.md`, `docs/06-usecase-layer.md`

**Interfaces:**

- Produces: `postgres.translate(err error) error` / `mysql.translate(err error) error` (unique violation → `fmt.Errorf("%w: %w", errors.ErrAlreadyExists, err)`); `memory.NewTxManager() *TxManager`; `usersvc.NewService(userRepo, hasher, tokens, tx domain.TxManager[, publisher])`.

- [x] **Step 1: The generated tests**

`templates/adapter/repository/postgres/errors_test.go.tmpl`:

```go
package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	apperrors "{{.ModuleName}}/pkg/errors"
)

func TestTranslateUniqueViolation(t *testing.T) {
	err := translate(&pgconn.PgError{Code: uniqueViolation, ConstraintName: "users_email_key"})
	if !errors.Is(err, apperrors.ErrAlreadyExists) {
		t.Fatalf("translate(23505) = %v, want ErrAlreadyExists", err)
	}
	other := &pgconn.PgError{Code: "42P01"}
	if got := translate(other); !errors.Is(got, other) {
		t.Fatalf("translate(42P01) = %v, want the driver error untouched", got)
	}
}
```

`templates/adapter/repository/mysql/errors_test.go.tmpl` is the same with `&mysql.MySQLError{Number: duplicateEntry}` and `&mysql.MySQLError{Number: 1146}` (import `"github.com/go-sql-driver/mysql"`).

`templates/usecase/user/service_test.go.tmpl`:

```go
package user

import (
	"context"
	"net/http"
	"testing"
	"time"

	"{{.ModuleName}}/internal/domain"
	"{{.ModuleName}}/internal/domain/entity"
	"{{.ModuleName}}/internal/domain/identity"
	"{{.ModuleName}}/pkg/errors"
	"{{.ModuleName}}/pkg/locale"
)

// fakeRepo is the smallest domain.UserRepository the service tests need: one
// stored user, a scripted Create error, and Update recording what it got.
type fakeRepo struct {
	stored    *entity.User
	createErr error
	updated   *entity.User
}

func (r *fakeRepo) Create(_ context.Context, u *entity.User) error {
	if r.createErr != nil {
		return r.createErr
	}
	u.ID = 1
	return nil
}

func (r *fakeRepo) GetByID(_ context.Context, id int64) (*entity.User, error) {
	if r.stored == nil || r.stored.ID != id {
		return nil, errors.Wrapf(errors.ErrNotFound, "fake: id=%d", id)
	}
	cp := *r.stored
	return &cp, nil
}

func (r *fakeRepo) GetByEmail(_ context.Context, email string) (*entity.User, error) {
	if r.stored == nil || r.stored.Email != email {
		return nil, errors.Wrapf(errors.ErrNotFound, "fake: email=%s", email)
	}
	cp := *r.stored
	return &cp, nil
}

func (r *fakeRepo) Update(_ context.Context, u *entity.User) error {
	cp := *u
	r.updated = &cp
	return nil
}

func (*fakeRepo) Delete(context.Context, int64) error                           { return nil }
func (*fakeRepo) List(context.Context, domain.UserFilter) ([]*entity.User, error) { return nil, nil }
func (*fakeRepo) Count(context.Context, domain.UserFilter) (int64, error)         { return 0, nil }
func (*fakeRepo) ListAfter(context.Context, int64, int32) ([]*entity.User, error) { return nil, nil }

type fakeHasher struct{}

func (fakeHasher) Hash(plain string) (string, error) { return "h:" + plain, nil }

func (fakeHasher) Verify(hash, plain string) error {
	if hash != "h:"+plain {
		return errors.ErrUnauthorized
	}
	return nil
}

type fakeIssuer struct{}

func (fakeIssuer) Sign(*identity.UserPrincipal) (string, error) { return "tok", nil }
func (fakeIssuer) AccessTokenTTL() time.Duration                { return time.Hour }

type noopTx struct{}

func (noopTx) WithinTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
{{- if .HasMessageQueue}}

type fakePublisher struct{}

func (fakePublisher) PublishUserCreated(context.Context, domain.UserCreated) error { return nil }
{{- end}}

func newTestService(repo *fakeRepo) *Service {
	return NewService(repo, fakeHasher{}, fakeIssuer{}, noopTx{}{{if .HasMessageQueue}}, fakePublisher{}{{end}})
}

// TestRegisterDuplicateEmailIs409 pins the race the uniqueness check cannot
// close: when the insert itself reports the duplicate (the adapter has
// translated the driver's unique violation to ErrAlreadyExists), the client
// still gets the 409 it would have got from the check.
func TestRegisterDuplicateEmailIs409(t *testing.T) {
	repo := &fakeRepo{createErr: errors.Wrap(errors.ErrAlreadyExists, "user repo: insert")}
	_, err := newTestService(repo).Register(t.Context(), RegisterInput{Email: "a@b.co", Name: "Ann", Password: "hunter2-pass"})
	app := errors.ToAppError(err)
	if app == nil || app.Code != http.StatusConflict || app.Locale != locale.UserEmailExists {
		t.Fatalf("Register on duplicate insert = %v, want 409 UserEmailExists", err)
	}
}

func TestRegisterRejectsInvalidInput(t *testing.T) {
	cases := map[string]RegisterInput{
		"bad email":  {Email: "not-an-email", Name: "Ann", Password: "hunter2-pass"},
		"short name": {Email: "a@b.co", Name: "A", Password: "hunter2-pass"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := newTestService(&fakeRepo{}).Register(t.Context(), in)
			if app := errors.ToAppError(err); app == nil || app.Code != http.StatusBadRequest {
				t.Fatalf("Register(%+v) = %v, want 400", in, err)
			}
		})
	}
}

func TestUpdateReportsNewUpdatedAt(t *testing.T) {
	before := time.Now().Add(-time.Hour)
	repo := &fakeRepo{stored: &entity.User{ID: 1, Email: "a@b.co", Name: "Ann", UpdatedAt: before}}
	ctx := identity.WithUserPrincipal(t.Context(), &identity.UserPrincipal{Principal: identity.Principal{UserID: 1}})
	out, err := newTestService(repo).Update(ctx, 1, UpdateInput{Name: "Anne"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !out.UpdatedAt.After(before) || repo.updated == nil || !repo.updated.UpdatedAt.Equal(out.UpdatedAt) {
		t.Fatalf("UpdatedAt = %v (persisted %+v), want a fresh stamp that was also persisted", out.UpdatedAt, repo.updated)
	}
}

func TestLoginSignsThroughThePort(t *testing.T) {
	repo := &fakeRepo{stored: &entity.User{ID: 1, Email: "a@b.co", Name: "Ann", PasswordHash: "h:hunter2-pass"}}
	out, err := newTestService(repo).Login(t.Context(), LoginInput{Email: "a@b.co", Password: "hunter2-pass"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if out.AccessToken != "tok" || out.ExpiresIn != 3600 {
		t.Fatalf("Login = %+v, want the port's token and TTL", out)
	}
}
```

- [x] **Step 2: Run the contract test to verify it fails**

Run: `go test -run TestLayersContract ./internal/generator/`
Expected: FAIL on `domain.TxManager` in service.go.

- [x] **Step 3: Adapters**

`templates/adapter/repository/postgres/errors.go.tmpl`:

```go
package postgres

import (
	stderrors "errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"{{.ModuleName}}/pkg/errors"
)

// uniqueViolation is the SQLSTATE a unique index raises; the only driver
// error the usecase has to act on (a duplicate email is a 409, not a 500).
const uniqueViolation = "23505"

// translate maps driver errors the usecase must distinguish to the pkg/errors
// sentinels, keeping the driver error in the chain; anything else passes
// through for Wrapf to classify as a 500.
func translate(err error) error {
	var pgErr *pgconn.PgError
	if stderrors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return fmt.Errorf("%w: %w", errors.ErrAlreadyExists, err)
	}
	return err
}
```

`mysql/errors.go.tmpl` is the same with `const duplicateEntry = 1062`, `var me *mysql.MySQLError` and `me.Number == duplicateEntry` (import `"github.com/go-sql-driver/mysql"`). Both `user_repository.go.tmpl` `Create` methods wrap `translate(err)` instead of `err`. `memory/tx_manager.go.tmpl`:

```go
package memory

import (
	"context"

	"{{.ModuleName}}/internal/domain"
)

var _ domain.TxManager = (*TxManager)(nil)

// TxManager is the no-op domain.TxManager for the in-memory repository: there
// is no transaction to open, so fn runs on ctx as it is. It exists so the
// usecase can depend on the port whichever storage is wired.
type TxManager struct{}

func NewTxManager() *TxManager { return &TxManager{} }

func (*TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}
```

`generator.go` `adapterFiles`: `errors.go` and `errors_test.go` for each SQL engine next to its repository entry (same condition); the memory `tx_manager.go` next to the memory repository (`!cfg.HasDatabase()`); `usecaseFiles`: `{"templates/usecase/user/service_test.go.tmpl", "internal/usecase/user/service_test.go", true}`.

- [x] **Step 4: The usecase**

Field `tx domain.TxManager`, constructor parameter `tx domain.TxManager` (after `tokens`). `Register` becomes:

```go
func (s *Service) Register(ctx context.Context, input RegisterInput) (*Output, error) {
	if len(input.Password) > security.MaxPasswordLen {
		return nil, errors.L(locale.InvalidRequest).
			WithCause(fmt.Errorf("password longer than %d bytes", security.MaxPasswordLen))
	}
	// Hash outside the transaction: bcrypt costs ~100ms and must not hold a
	// connection open.
	hash, err := s.hasher.Hash(input.Password)
	if err != nil {
		return nil, errors.Wrapf(err, "register user email=%s: hash", input.Email)
	}
	user, err := entity.NewUser(input.Email, input.Name, hash)
	if err != nil {
		return nil, errors.L(locale.InvalidRequest).WithCause(err)
	}

	txErr := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		// Treat ErrNotFound as the only "ok, proceed" outcome; any other error
		// from the lookup (DB outage, network) must propagate so we don't
		// race-create a duplicate row when the uniqueness check itself failed.
		existing, lookupErr := s.userRepo.GetByEmail(ctx, user.Email)
		switch {
		case lookupErr == nil && existing != nil:
			return errors.L(locale.UserEmailExists, user.Email).WithCause(errors.New("email already registered"))
		case lookupErr != nil && !errors.Is(lookupErr, errors.ErrNotFound):
			return errors.Wrapf(lookupErr, "register user email=%s: lookup", user.Email)
		}
		if createErr := s.userRepo.Create(ctx, user); createErr != nil {
			// Two registrations can pass the check together; the unique index
			// decides and the adapter reports it as ErrAlreadyExists.
			if errors.Is(createErr, errors.ErrAlreadyExists) {
				return errors.L(locale.UserEmailExists, user.Email).WithCause(createErr)
			}
			return errors.Wrapf(createErr, "register user email=%s", user.Email)
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	… (publish block unchanged, then return toOutput(user), nil)
}
```

`List` runs the two reads inside one transaction:

```go
	var (
		users []*entity.User
		total int64
	)
	// One transaction so the page and its count come from one snapshot where
	// the engine's isolation level provides one (MySQL's REPEATABLE READ does;
	// Postgres READ COMMITTED does not, raise it in TxManager if that matters).
	txErr := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if users, err = s.userRepo.List(ctx, filter); err != nil {
			return errors.Wrapf(err, "list users page=%d perPage=%d", page, perPage)
		}
		if total, err = s.userRepo.Count(ctx, filter); err != nil {
			return errors.Wrapf(err, "count users email=%s", input.Email)
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
```

(`entity.NewUser` is created in task 4; until then the tree does not compile, so execute tasks 3 and 4 back to back.)

- [x] **Step 5: DI**

wire `adapterSet`: postgres block adds `postgresrepo.NewTxManager,` + `wire.Bind(new(domain.TxManager), new(*postgresrepo.TxManager)),`; mysql likewise; memory block adds `memoryrepo.NewTxManager,` + `wire.Bind(new(domain.TxManager), new(*memoryrepo.TxManager)),`. fx `adapterModule`: `fx.Annotate(postgresrepo.NewTxManager, fx.As(new(domain.TxManager))),` and the mysql/memory counterparts.

- [x] **Step 6: Docs**

`docs/08`: after the `user_repository.go` section add "Translating the unique violation" (the `translate` helper, `23505`/`1062`, the chain keeps the driver error, the usecase maps `ErrAlreadyExists` to `UserEmailExists`); the tx paragraph "The interface lives in `domain/`, the implementation here: the usecase injects `domain.TxManager`…" now adds that `Register` and `List` use it and that the memory adapter ships a no-op. `docs/06` Register section: hash first, then check + insert inside `WithinTx`, duplicate insert → 409; List section: one transaction, isolation caveat.

- [x] **Step 7: Verify (after task 4)**

Run: `go test -run 'TestLayersContract|TestRenderedGoIsGofmtClean' ./internal/generator/`, `make lint`, markdownlint on docs 06 and 08, then alone `make test-all`.

- [ ] **Step 8: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): run Register and List in the transaction manager and answer a duplicate insert with 409`).

---

### Task 4: The entity owns its invariants and its timestamps

**Files:**

- Modify: `internal/generator/templates/domain/entity/user.go.tmpl`, `internal/generator/templates/domain/user.go.tmpl` (`UserFilter.Name` removed)
- Modify: `internal/generator/templates/usecase/user/service.go.tmpl` (`Update`)
- Modify: `internal/generator/templates/sqlc/query/{pg,mysql}_user.sql.tmpl`, `internal/generator/templates/adapter/repository/{postgres,mysql}/mapper/user.go.tmpl`, `internal/generator/generator_test.go` (dbgen stubs)
- Modify: `docs/05-domain-layer.md`, `docs/06-usecase-layer.md`

**Interfaces:**

- Produces: `entity.NewUser(email, name, passwordHash string) (*User, error)`; `(*User).Rename(name string) error`; `entity.ErrEmailInvalid`, `entity.ErrNameInvalid`; sqlc `UpdateUserParams{Name, UpdatedAt, ID}`.

- [x] **Step 1: Entity**

```go
package entity

import (
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// Invariant violations. The usecase maps them to a 400; every transport
// (HTTP, gRPC, worker, a future CLI) inherits the same rules because they
// live here, not in request-validation tags.
var (
	ErrEmailInvalid = errors.New("email is invalid")
	ErrNameInvalid  = errors.New("name must be 2 to 100 characters")
)

const (
	nameMinLen = 2
	nameMaxLen = 100
)

// User is a domain entity. NO framework tags — serialization is a transport
// concern. The stored credential is PasswordHash (bcrypt output); plaintext
// passwords never live on the entity.
type User struct {
	ID           int64
	Email        string
	Name         string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewUser builds a valid user or reports which invariant failed. The email
// must parse as an address and the name must be 2–100 characters; both are
// trimmed. passwordHash is the hasher's output, never the plaintext.
func NewUser(email, name, passwordHash string) (*User, error) {
	email = strings.TrimSpace(email)
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		return nil, ErrEmailInvalid
	}
	name = strings.TrimSpace(name)
	if !validName(name) {
		return nil, ErrNameInvalid
	}
	if passwordHash == "" {
		return nil, errors.New("password hash is empty")
	}
	now := time.Now()
	return &User{Email: email, Name: name, PasswordHash: passwordHash, CreatedAt: now, UpdatedAt: now}, nil
}

// Rename applies the name invariant and stamps UpdatedAt, so the row the
// repository writes and the response the caller sees agree on the time.
func (u *User) Rename(name string) error {
	name = strings.TrimSpace(name)
	if !validName(name) {
		return ErrNameInvalid
	}
	u.Name = name
	u.UpdatedAt = time.Now()
	return nil
}

func validName(name string) bool {
	n := utf8.RuneCountInString(name)
	return n >= nameMinLen && n <= nameMaxLen
}
```

`domain/user.go.tmpl`: delete `Name   string` from `UserFilter` (nothing read it).

- [x] **Step 2: Usecase Update**

```go
	if input.Name != "" {
		if renameErr := user.Rename(input.Name); renameErr != nil {
			return nil, errors.L(locale.InvalidRequest).WithCause(renameErr)
		}
	}
```

(The `Register` body from task 3 already calls `entity.NewUser`; remove the old `now := time.Now()` literal construction.)

- [x] **Step 3: Queries, mappers, stubs**

`pg_user.sql.tmpl`: `UPDATE users SET name = $1, updated_at = $2 WHERE id = $3;` `mysql_user.sql.tmpl`: `UPDATE users SET name = ?, updated_at = ? WHERE id = ?;` Mappers: `UserToUpdateParams` gains `UpdatedAt: pgtype.Timestamptz{Time: u.UpdatedAt, Valid: true}` (postgres) / `UpdatedAt: u.UpdatedAt` (mysql). Both dbgen stubs: `UpdateUserParams` gains `UpdatedAt pgtype.Timestamptz` / `UpdatedAt time.Time`.

- [x] **Step 4: Docs**

`docs/05`: replace "The scaffold has no `NewUser` constructor…" with the constructor and `Rename`, why the invariants live here (every transport inherits them), and the sentinel mapping to 400 in the usecase; drop `Name` from the `UserFilter` snippet. `docs/06`: `Update` loads, calls `Rename` (400 on violation), persists, and returns the stamped entity, so `updatedAt` in the response is the value written.

- [x] **Step 5: Verify** — with task 3: `go test -run 'TestLayersContract|TestRenderedGoIsGofmtClean' ./internal/generator/`, `make lint`, markdownlint on docs 05 and 06, then alone `make test-all`.

- [ ] **Step 6: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): enforce user invariants in the entity and report the real updated_at`).

---

### Task 5: Infrastructure degrades, the principal slims down, JWT moves to v5

**Files:**

- Modify: `internal/generator/templates/infrastructure/cache/redis.go.tmpl`, `internal/generator/templates/adapter/repository/redis/user_cache.go.tmpl`
- Modify: `internal/generator/templates/domain/identity/principal.go.tmpl`, `internal/generator/templates/infrastructure/jwt/claims.go.tmpl`, `service.go.tmpl`
- Modify: `docs/09-infrastructure-layer.md`, `docs/08-adapter-layer.md`, `docs/13-cross-cutting.md`, `docs/02-project-layout.md`, `docs/superpowers/plans/2026-10-04-review-remediation-roadmap.md`, `internal/generator/templates/README.md.tmpl` (if it names the removed fields)

- [x] **Step 1: Redis**

`cache.New`: replace the ping failure return with

```go
	if err := ping(uc, cfg.PingTimeout); err != nil {
		// Redis is a cache: reads fall through to the database and readiness
		// reports it as "fail" until it is back, so an unreachable cache must
		// not keep the process from booting.
		log.Warn("redis: ping failed, starting degraded", "mode", cfg.Mode, "err", err)
	}
```

Decorator: `userCacheJitter = 3 * time.Minute`; a `ttl()` helper returning `userCacheTTL + time.Duration(rand.Int64N(int64(userCacheJitter))) //nolint:gosec // jitter spreads expiry, not a secret` (import `math/rand/v2`); `GetByID` uses `ttl()`; `Update` and `Delete` log `log.Warn("user cache: invalidate failed", "key", key, "err", delErr)` and return nil, because the write succeeded and the entry expires within the TTL; the package comment says a cached entity carries no credentials and `Update` only ever persists the fields `UpdateUser` writes (name, updated_at), so a cached read can be passed back to `Update` safely — keep it that way if `UpdateUser` grows columns.

- [x] **Step 2: Principal and claims**

`identity.UserPrincipal` keeps `Principal` and `Name` only; its comment says to add claims here as the token grows. `AccessTokenClaims` keeps `UserID`, `Email`, `Name`; `Sign` and `ParseAndVerifyAccessToken` stop copying the removed fields.

- [x] **Step 3: jwt v5**

Imports become `"github.com/golang-jwt/jwt/v5"` in both files. `ParseAndVerifyAccessToken`:

```go
	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithExpirationRequired(),
	}
	// Issuer/audience are enforced by the parser so a token signed with our
	// key but minted for another environment fails like any other bad token.
	if s.issuer != "" {
		opts = append(opts, jwt.WithIssuer(s.issuer))
	}
	if s.audience != "" {
		opts = append(opts, jwt.WithAudience(s.audience))
	}
	tkn, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, errors.L(locale.Unauthorized).WithCause(errors.New("unexpected signing method"))
		}
		return s.publicKey, nil
	}, opts...)
	if err != nil {
		logVerifyFailure(ctx, err)
		return nil, errors.L(locale.Unauthorized).WithCause(err)
	}
	if tkn == nil || !tkn.Valid {
		return nil, errors.L(locale.Unauthorized).WithCause(errors.New("token invalid"))
	}
	return &identity.UserPrincipal{
		Principal: identity.Principal{UserID: claims.UserID, Email: claims.Email},
		Name:      claims.Name,
	}, nil
```

(the manual `VerifyIssuer`/`VerifyAudience` block and its `log` variable go away.) `classifyParseError`:

```go
// classifyParseError maps a golang-jwt v5 error to a stable reason string,
// plus whether it is anomalous (worth a Warn). Anything unrecognised is
// anomalous so it can never hide in the noise.
func classifyParseError(err error) (string, bool) {
	switch {
	case errors.Is(err, jwt.ErrTokenSignatureInvalid), errors.Is(err, jwt.ErrTokenUnverifiable):
		return "signature_invalid", true
	case errors.Is(err, jwt.ErrTokenInvalidIssuer):
		return "bad_issuer", true
	case errors.Is(err, jwt.ErrTokenInvalidAudience):
		return "bad_audience", true
	case errors.Is(err, jwt.ErrTokenMalformed):
		return "malformed", false
	case errors.Is(err, jwt.ErrTokenExpired):
		return "expired", false
	case errors.Is(err, jwt.ErrTokenNotValidYet), errors.Is(err, jwt.ErrTokenUsedBeforeIssued):
		return "not_yet_valid", false
	case errors.Is(err, jwt.ErrTokenInvalidClaims), errors.Is(err, jwt.ErrTokenRequiredClaimMissing):
		return "claims_invalid", false
	default:
		return "unknown", true
	}
}
```

- [x] **Step 4: Docs and roadmap**

`docs/09`: "constructors verify connectivity" gains "Redis is the exception: it logs and starts degraded, because a cache outage is survivable and readiness reports it"; the jwt section names v5, the parser options and the sentinel-based classification, and the claims paragraph drops "optional device/session fields". `docs/08` decorator: read and refill failures degrade, a failed `DEL` after a write logs a warning and relies on the jittered TTL (the write succeeded; failing the request would not make the cache fresher); the TTL carries up to 3 minutes of jitter so a cold start does not expire every key at once. `docs/13`: "No refresh tokens" line → "`AccessTokenTTL` only; add a session id claim to `AccessTokenClaims` to build on." `docs/02`: tree gains `errors.go`/`errors_test.go` under postgres, `service_test.go` under usecase/user, `errors_test.go` under pkg/errors, `tx_manager.go` under memory, `token.go` under security. Roadmap: Phase 5 header "(done 2026-10-13, commit `331c422`)", Phase 6 "Detailed plan:" link.

- [x] **Step 5: Verify**

Run: `make test`, `make lint`, `npx --no-install markdownlint-cli2 docs/*.md docs/superpowers/plans/*.md`, then alone `make test-all`.
Expected: all green; every bootstrap runs the rendered package tests.

- [ ] **Step 6: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): start without Redis, slim the principal, move to jwt v5`).
