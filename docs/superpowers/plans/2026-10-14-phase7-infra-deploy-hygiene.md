# Phase 7: Infrastructure and Deploy Hygiene Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Heavy steps (`make test-all`, the one-off `docker build`) run alone from the main thread; see Global Constraints.

**Goal:** A fresh `nova new` project boots on the host from `cp .env.example .env`, rejects a misspelled `APP_ENV`, configures MySQL as fully as Postgres, builds a reproducible image on a supported Alpine whose port and healthcheck follow one knob, ships a CI that is pinned and hermetic, lets fx shutdown be tuned and observed, serves its own OpenAPI document, and documents the costs it cannot remove.

**Architecture:** `config.Load` loads `./.env` through `godotenv` (real environment wins, a missing file is fine), whitelists `APP_ENV`, and requires `DB_USER` so the OS user can never leak in; the cobra root sets `SilenceUsage`. `DatabaseConfig` for MySQL gains `tls`, `max_conn_lifetime`, `max_conn_idle_time` and `connect_timeout`, applied in `NewMySQLDB`; `cache.New` refuses an empty address list. The Dockerfile moves to `alpine:3.22`, builds with `-trimpath`, drops the dead config copy, takes `SERVER_PORT`/`GRPC_PORT` as build args that drive `ENV`, `EXPOSE` and the healthcheck, and gets a `.dockerignore`. CI pins `golangci-lint-action@v8` to golangci-lint `v2.11.4`, drops the service containers and env the hermetic tests never use, and builds with `-trimpath`. fx's stop budget comes from a new top-level `shutdown_timeout` and a failed stop is logged with that budget. `wire` becomes a `go tool` dependency, `migrate-down` rolls back `n` (default 1), and the README pins the external tool versions. The routers serve `GET /openapi.yaml` from an embedded copy. A new bootstrap smoke test builds the no-database project, starts it with generated RSA keys, waits for `/healthz`, and sends SIGTERM, proving the host boot path end to end.

**Tech Stack:** `github.com/joho/godotenv` (already transitive via cleanenv), `go-sql-driver/mysql` TLS names, Docker build args, `go tool` directives (Go 1.24+), `crypto/rsa` in the nova-side smoke test.

**Status (2026-10-14):** all five tasks implemented and verified (`make test` green; `make lint` 0 issues; markdownlint 0 issues on every edited file; `make test-all` green at ~3.5 min with the no-database bootstrap now ending in the host boot smoke test; a one-off `docker build` of a rendered fiber/postgres/wire project succeeded and the image exposes `8080`, sets `SERVER_PORT`, probes `/readyz` and runs as `app`); commit steps pending the user's `/grimoire-core:commit`. `docker compose up` against live dependencies was not run.

**Deviations from the plan as written:**

- `fx.Populate` now also populates `*config.Config` for the shutdown budget, so the `nova add worker` fx assertion in `component_test.go` was updated to the new arity.
- The MySQL `MIGRATE_DATABASE_URL` example gained the `mysql://` scheme golang-migrate expects; the previous placeholder was not a valid migrate URL.
- The Kafka producer still dials at startup (documented as deliberate in docs 09 rather than made lazy); the Elasticsearch lint cost is documented in docs 01 rather than switching clients.

## Global Constraints

- `make lint` and `make test` stay green after every task; `make test-all` runs alone at the end of tasks 3 and 5.
- Resource rule: one heavy toolchain command at a time from the main thread; never `go clean -cache`.
- `.env` is read only when present; real environment variables always win over it; containers keep injecting env and never ship a `.env`.
- The Docker image, compose and CI stay consistent with the README's quick start: `cp .env.example .env`, generate keys, `make gen`, `docker compose up -d <deps>`, `make migrate-up`, `go run main.go api`.
- Pinned versions: `golangci-lint v2.11.4`, `golangci/golangci-lint-action@v8`, `sqlc v1.30.0`, `migrate v4.19.0`, `sqlfluff 4.2.2`, `alpine:3.22`; wire through `go tool`.
- Markdown edits pass markdownlint with the repo config. Commits only via `/grimoire-core:commit` after confirmation.

---

### Task 1: Host runs work out of the box

**Files:**

- Modify: `internal/generator/templates/infrastructure/config/config.go.tmpl` (`loadDotEnv`, `APP_ENV` whitelist, `DB_USER` required, `ShutdownTimeout`), `base.yaml.tmpl`, `env.example.tmpl`
- Modify: `internal/generator/templates/cmd/root.go.tmpl` (`SilenceUsage`), `internal/generator/templates/infrastructure/cache/redis.go.tmpl` (empty `Addrs`)
- Modify: `internal/generator/generator_test.go` (`TestDeployContract` first assertions, `smokeBoot`), `docs/09-infrastructure-layer.md`, `docs/12-build-and-deploy.md`

**Interfaces:**

- Produces: `config.Config.ShutdownTimeout time.Duration` (`shutdown_timeout`, `APP_SHUTDOWN_TIMEOUT`, default 20s); `config.Load` errors `unknown APP_ENV %q (valid: local, development, production)` and `config database.user (DB_USER) is required`; `cache.New` errors `redis: addrs is empty`.

- [x] **Step 1: Write the failing tests**

Append to `internal/generator/generator_test.go`:

```go
// TestDeployContract pins the phase 7 hygiene: host runs load .env, APP_ENV is
// whitelisted, MySQL is configured like Postgres, the image is reproducible
// and port-driven, CI is pinned and hermetic, shutdown budgets are config,
// and the OpenAPI document is served.
func TestDeployContract(t *testing.T) {
	t.Parallel()
	t.Run("postgres_wire", func(t *testing.T) {
		t.Parallel()
		cfg := httpMatrixConfig("fiber", "postgres", "wire")
		cfg.MessageQueue = "kafka"
		assertDeployContract(t, renderProject(t, cfg), "postgres")
	})
	t.Run("mysql_fx", func(t *testing.T) {
		t.Parallel()
		assertDeployContract(t, renderProject(t, httpMatrixConfig("gin", "mysql", "fx")), "mysql")
	})
}

func assertDeployContract(t *testing.T, dir, engine string) {
	t.Helper()
	cfgSrc := readFile(t, filepath.Join(dir, "internal/infrastructure/config/config.go"))
	mustContainAll(t, cfgSrc, "godotenv.Load(", "unknown APP_ENV", "DB_USER) is required", `"shutdown_timeout"`)
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/config/base.yaml")), "shutdown_timeout: 20s")
	mustContainAll(t, readFile(t, filepath.Join(dir, "cmd/root.go")), "SilenceUsage: true")
	env := readFile(t, filepath.Join(dir, ".env.example"))
	mustNotContain(t, env, "<set-me", "<user>", "<password>")
	mustContainAll(t, env, "DB_USER=matrixtest", "JWT_PUBLIC_KEY=")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/cache/redis.go")), "addrs is empty")
	if engine == "mysql" {
		mustContainAll(t, cfgSrc, `"tls"`, `"max_conn_lifetime"`, `"max_conn_idle_time"`, `"connect_timeout"`)
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/database/mysql.go")),
			"TLSConfig", "SetConnMaxLifetime(", "SetConnMaxIdleTime(", "cfg.ConnectTimeout")
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/config/production.yaml")),
			`tls: "skip-verify"`)
	}
	docker := readFile(t, filepath.Join(dir, "Dockerfile"))
	mustContainAll(t, docker, "alpine:3.22", "-trimpath", "ARG SERVER_PORT=8080", "${SERVER_PORT}/readyz")
	mustNotContain(t, docker, "/app/config", "alpine:3.19")
	mustContainAll(t, readFile(t, filepath.Join(dir, ".dockerignore")), ".env", ".git", "bin/")
	ci := readFile(t, filepath.Join(dir, ".github/workflows/ci.yaml"))
	mustContainAll(t, ci, "golangci/golangci-lint-action@v8", "version: v2.11.4", "-trimpath")
	mustNotContain(t, ci, "services:", "JWT_PUBLIC_KEY", "DB_HOST")
	mk := readFile(t, filepath.Join(dir, "Makefile"))
	mustContainAll(t, mk, "down $(n)", "n ?= 1")
	if engine == "postgres" { // wire
		mustContainAll(t, mk, "go tool wire ./internal/infrastructure/di")
		mustContainAll(t, readFile(t, filepath.Join(dir, "go.mod")), "tool github.com/google/wire/cmd/wire")
	} else { // fx
		fxSrc := readFile(t, filepath.Join(dir, "internal/infrastructure/di/fx.go"))
		mustContainAll(t, fxSrc, "cfg.ShutdownTimeout", `"timeout_ms"`)
		mustNotContain(t, fxSrc, "stopTimeout")
	}
	mustContainAll(t, readFile(t, filepath.Join(dir, "api/openapi/openapi.go")), "//go:embed openapi.yaml", "var Spec []byte")
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/http/router.go")), `"/openapi.yaml"`, "openapi.Spec")
	mustContainAll(t, readFile(t, filepath.Join(dir, "README.md")), "sqlc v1.30.0", "migrate v4.19.0", "sqlfluff 4.2.2")
}
```

- [x] **Step 2: Run it to verify it fails**

Run: `go test -run TestDeployContract ./internal/generator/`
Expected: FAIL on `godotenv.Load(`.

- [x] **Step 3: Config**

In `config.go.tmpl`: import `"io/fs"` and `"github.com/joho/godotenv"`; `Config` gains, after `AppEnv`:

```go
	// ShutdownTimeout bounds the whole cleanup chain (fx's Stop budget); keep
	// it above the server's shutdown_timeout plus the dependency closes.
	ShutdownTimeout time.Duration `{{.ConfigFormat}}:"shutdown_timeout" env:"APP_SHUTDOWN_TIMEOUT"`
```

`Load` becomes:

```go
func Load() (*Config, error) {
	if err := loadDotEnv(); err != nil {
		return nil, errors.Wrap(err, "config .env")
	}

	var cfg Config
	if err := readEmbedded("base.{{.ConfigFormat}}", &cfg); err != nil {
		return nil, errors.Wrap(err, "config base")
	}

	switch env := os.Getenv("APP_ENV"); env {
	case "", EnvLocal:
	case EnvDev, EnvProd:
		envFile := env + ".{{.ConfigFormat}}"
		if err := readEmbedded(envFile, &cfg); err != nil {
			return nil, errors.Wrapf(err, "config env %s", envFile)
		}
	default:
		// A typo must not silently run with base defaults (ssl_mode: disable,
		// debug-friendly limits) in what the operator believes is production.
		return nil, fmt.Errorf("unknown APP_ENV %q (valid: %s, %s, %s)", env, EnvLocal, EnvDev, EnvProd)
	}

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, errors.Wrap(err, "config env vars")
	}
{{- if .HasDatabase}}
	// Without DB_USER the driver falls back to the OS user, which looks like a
	// permissions bug on the database; fail with the real cause instead.
	if cfg.Database.User == "" {
		return nil, errors.New("config database.user (DB_USER) is required")
	}
{{- end}}
{{- if .HasHTTP}}
	if _, err := cfg.HTTP.TrustedProxyNets(); err != nil {
		return nil, errors.Wrap(err, "config http.trusted_proxies")
	}
{{- end}}

	return &cfg, nil
}

// loadDotEnv reads ./.env into the environment for host runs, so
// `cp .env.example .env && go run main.go api` works. Variables already set
// win (godotenv.Load never overrides), and a missing file is not an error:
// containers inject their environment and ship no .env.
func loadDotEnv() error {
	err := godotenv.Load()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
```

`base.yaml.tmpl`: after `app_version` add

```yaml
# Budget for the whole shutdown chain (server drain, then dependency closes);
# the fx engine stops its hooks within it. Keep above http/grpc shutdown_timeout.
shutdown_timeout: 20s
```

`cmd/root.go.tmpl`: `SilenceUsage: true,` in the root command with the comment "a boot failure prints the error, not the usage text". `cache/redis.go.tmpl` `New`: first line `if len(cfg.Addrs) == 0 { return nil, nil, errors.New("redis: addrs is empty") }` (import `"errors"`).

`env.example.tmpl`: every placeholder becomes a sourceable value — `JWT_PUBLIC_KEY=` and `JWT_PRIVATE_KEY=` empty with the comment "required; generate with the commands above (empty fails at startup)"; `DB_USER={{.ProjectName}}` and `DB_PASSWORD={{.ProjectName}}` with "dev defaults matching docker-compose; change in any shared environment"; `MIGRATE_DATABASE_URL` uses the same values; `RABBITMQ_URL=amqp://guest:guest@localhost:5672/`. The header gains "`.env` is loaded automatically by the binary on host runs; environment variables always win."

- [x] **Step 4: Docs** — `docs/09` config section: `.env` loading and precedence (`.env` < `<APP_ENV>.yaml`? no: `base.yaml` → `<APP_ENV>.yaml` → `.env` → real environment), the `APP_ENV` whitelist (now an error, fixing the sentence that claimed it already was), `DB_USER` required, `shutdown_timeout`; `docs/12` `.env.example` section: sourceable values, loaded on host.

- [x] **Step 5: Verify** — `go test -run 'TestDeployContract|TestRenderedGoIsGofmtClean' ./internal/generator/` (fails only on later-task assertions), `make lint`, markdownlint on docs 09 and 12.

- [ ] **Step 6: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): load .env on host runs, whitelist APP_ENV, require DB_USER`).

---

### Task 2: MySQL configured like Postgres

**Files:**

- Modify: `internal/generator/templates/infrastructure/config/config.go.tmpl` (MySQL `DatabaseConfig`, `Summary`), `base.yaml.tmpl`, `production.yaml.tmpl`, `env.example.tmpl`
- Modify: `internal/generator/templates/infrastructure/database/mysql.go.tmpl`, `docs/09-infrastructure-layer.md`

- [x] **Step 1: Config**

MySQL block of `DatabaseConfig`:

```go
{{- if eq .Database "mysql"}}
	Host     string `{{.ConfigFormat}}:"host" env:"DB_HOST"`
	Port     int    `{{.ConfigFormat}}:"port" env:"DB_PORT"`
	Name     string `{{.ConfigFormat}}:"name" env:"DB_NAME"`
	User     string `env:"DB_USER"`
	Password string `env:"DB_PASSWORD"`
	// TLS is the driver's tls mode: false | true | skip-verify | preferred.
	// "skip-verify" encrypts without authenticating the server (the
	// counterpart of Postgres sslmode=require); register a tls.Config with
	// the CA for "true" in production.
	TLS string `{{.ConfigFormat}}:"tls" env:"DB_TLS"`

	// Pool / connection tuning.
	MaxOpenConns    int           `{{.ConfigFormat}}:"max_open_conns"     env:"DB_MAX_OPEN_CONNS"`
	MaxIdleConns    int           `{{.ConfigFormat}}:"max_idle_conns"     env:"DB_MAX_IDLE_CONNS"`
	MaxConnLifetime time.Duration `{{.ConfigFormat}}:"max_conn_lifetime"  env:"DB_MAX_CONN_LIFETIME"`
	MaxConnIdleTime time.Duration `{{.ConfigFormat}}:"max_conn_idle_time" env:"DB_MAX_CONN_IDLE_TIME"`
	ConnectTimeout  time.Duration `{{.ConfigFormat}}:"connect_timeout"    env:"DB_CONNECT_TIMEOUT"`
{{- end}}
```

`Summary` for mysql: `fields = append(fields, "db_tls", c.Database.TLS)`. `base.yaml` mysql: `tls: "false"` (comment as for ssl_mode), `max_conn_lifetime: 5m`, `max_conn_idle_time: 1m`, `connect_timeout: 10s`; `production.yaml` mysql: `tls: "skip-verify" # encrypt in transit; use "true" with a registered CA to also authenticate the server`, `max_conn_lifetime: 30m`, `max_conn_idle_time: 5m`. `env.example` mysql comment mentions `DB_TLS`.

- [x] **Step 2: Factory**

```go
// NewMySQLDB creates a new MySQL database connection pool.
func NewMySQLDB(cfg config.DatabaseConfig, log *logger.Logger) (*sql.DB, func(), error) {
	mysqlCfg := mysql.NewConfig()
	mysqlCfg.User = cfg.User
	mysqlCfg.Passwd = cfg.Password
	mysqlCfg.Net = "tcp"
	mysqlCfg.Addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	mysqlCfg.DBName = cfg.Name
	mysqlCfg.ParseTime = true
	mysqlCfg.Timeout = cfg.ConnectTimeout
	if cfg.TLS != "" {
		mysqlCfg.TLSConfig = cfg.TLS
	}

	db, err := sql.Open("mysql", mysqlCfg.FormatDSN())
	…
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.MaxConnLifetime)
	db.SetConnMaxIdleTime(cfg.MaxConnIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	…
```

(the `pingTimeout` const goes away; the `time` import stays only if still used.)

- [x] **Step 3: Docs** — `docs/09` database section: the MySQL surface mirrors Postgres (`tls` ↔ `ssl_mode`, lifetimes, `connect_timeout` bounds both the dial and the startup ping).

- [x] **Step 4: Verify** — `go test -run 'TestDeployContract|TestRenderedGoIsGofmtClean' ./internal/generator/`, `make lint`, markdownlint on docs 09.

- [ ] **Step 5: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): give MySQL the same TLS, lifetime and timeout knobs as Postgres`).

---

### Task 3: A reproducible, port-driven image

**Files:**

- Create: `internal/generator/templates/dockerignore.tmpl`
- Modify: `internal/generator/templates/Dockerfile.tmpl`, `docker-compose.yaml.tmpl` (app port), `internal/generator/generator.go` (`toolingFiles`), `docs/12-build-and-deploy.md`

- [x] **Step 1: .dockerignore**

```text
# Keep the build context to what the image needs: no secrets, no local
# state, no git history.
.git
.env
.env.*
bin/
tmp/
*.test
coverage.*
.idea/
.vscode/
docker-compose.yaml
Dockerfile
.dockerignore
```

`generator.go` `toolingFiles`: `{"templates/dockerignore.tmpl", ".dockerignore", cfg.IncludeDocker}` after the Dockerfile entry.

- [x] **Step 2: Dockerfile**

```dockerfile
# syntax=docker/dockerfile:1.7

# (header unchanged)

# ── Build stage ──────────────────────────────────────────────────────────
FROM golang:{{.GoVersion}}-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# -trimpath drops the build host's paths from the binary (reproducible,
# no leaked directories in panics); -w -s strips DWARF and symbols.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /out/api .

# ── Runtime stage ────────────────────────────────────────────────────────
FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=builder /out/api /app/api
{{- if .HasSQL}}
{{- if eq .QueryGen "sqlc"}}
COPY --from=builder /src/sqlc/migrations /app/migrations
{{- else}}
COPY --from=builder /src/migrations /app/migrations
{{- end}}
{{- end}}

USER app
{{- if .HasHTTP}}

# One knob for the port: the build arg sets the env var the app reads
# (SERVER_PORT), the EXPOSE metadata and the healthcheck target.
ARG SERVER_PORT=8080
ENV SERVER_PORT=${SERVER_PORT}
EXPOSE ${SERVER_PORT}

# Probes the readiness endpoint, not the liveness one, so the container is
# only "healthy" once DB/cache are reachable. start-period=10s gives the app
# time to dial dependencies on cold boot.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:${SERVER_PORT}/readyz >/dev/null 2>&1 || exit 1
{{- end}}
{{- if .HasGRPC}}

ARG GRPC_PORT=9090
ENV GRPC_PORT=${GRPC_PORT}
EXPOSE ${GRPC_PORT}
{{- end}}

ENTRYPOINT ["/app/api"]
CMD ["{{if .HasWorker}}worker{{else if .HasGRPC}}grpc{{else}}api{{end}}"]
```

(The dead `COPY …/config /app/config` is gone: the YAML is embedded in the binary.) Compose app ports: `- "${APP_HOST_PORT:-8080}:${SERVER_PORT:-8080}"` and the gRPC line `${GRPC_PORT:-9090}`.

- [x] **Step 3: Docs** — `docs/12` Dockerfile section: `alpine:3.22`, `-trimpath` (sixth choice), the port arg and the healthcheck following it, the removed config copy ("the YAML is embedded; nothing to copy"), the `.dockerignore` and why `.env` is in it.

- [x] **Step 4: Verify** — `go test -run 'TestDeployContract|TestDockerCompose' ./internal/generator/` (name per the existing compose test), `make lint`, markdownlint on docs 12, then alone `make test-all`.

- [ ] **Step 5: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): build a trimmed image on alpine 3.22 with one port knob and a dockerignore`).

---

### Task 4: Pinned, hermetic tooling and a tunable shutdown

**Files:**

- Modify: `internal/generator/templates/ci/github-ci.yaml.tmpl`, `Makefile.tmpl`, `gomod.tmpl`, `README.md.tmpl` (prerequisites), `infrastructure/di/fx.go.tmpl`, `internal/generator/skel/worker/di_worker_fx.go.tmpl`
- Modify: `docs/11-di-and-entrypoints.md`, `docs/12-build-and-deploy.md`

- [x] **Step 1: CI**

The lint job uses `golangci/golangci-lint-action@v8` with `version: v2.11.4` (the golden config is a v2 config; the v6 action only drove v1). The test job loses the `services:` block and the whole `env:` block: the rendered tests are hermetic (fakes, driver errors built by hand), and the comment says so. The build job runs `go build -trimpath -ldflags="-w -s" -o bin/api .`.

- [x] **Step 2: Makefile and go.mod**

`gomod.tmpl` (wire projects only):

```text
module {{.ModuleName}}

go {{.GoVersion}}
{{- if .UseWire}}

tool github.com/google/wire/cmd/wire
{{- end}}
```

`Makefile.tmpl`: drop `WIRE_PATH`; `wire-gen` runs `rm -f $(PWD)/internal/infrastructure/di/wire_gen.go; go tool wire ./internal/infrastructure/di`; `migrate-down` becomes

```make
# Roll back n migrations (default 1); `make migrate-down n=3`.
n ?= 1
migrate-down:
	migrate -path {{$migrationsDir}} -database '${MIGRATE_DATABASE_URL}' down $(n)
```

README prerequisites gain the tool list with pinned install commands: `golangci-lint v2.11.4`, `sqlc v1.30.0` (`go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0`), `migrate v4.19.0` (`go install -tags 'postgres mysql' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.19.0`), `sqlfluff 4.2.2` (`pipx install sqlfluff==4.2.2`); wire needs no install (`go tool`).

- [x] **Step 3: fx stop budget**

`fx.go.tmpl`: delete the `stopTimeout` const and its comment; every `Initialize*` populates `cfg *config.Config` too (`fx.Populate(&log, &srv, &tracer, &cfg)`) and its cleanup is

```go
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
```

(`time` import removed if unused.) `skel/worker/di_worker_fx.go.tmpl` gets the same shape (populate `cfg`, import config).

- [x] **Step 4: Docs** — `docs/11`: the fx cleanup snippet and the `stopTimeout` quote are replaced by the config-driven budget; `docs/12`: CI table (pinned action and version, hermetic test job, `-trimpath`), `migrate-down n`, `go tool wire`, the tool version list.

- [x] **Step 5: Verify** — `go test -run 'TestDeployContract|TestFxDISelection|TestGenerateWorker' ./internal/generator/`, `make lint`, markdownlint on docs 11 and 12.

- [ ] **Step 6: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): pin CI and tools, make migrate-down bounded and fx shutdown tunable`).

---

### Task 5: Serve the OpenAPI document, document the costs, prove the boot

**Files:**

- Create: `internal/generator/templates/api/openapi/openapi.go.tmpl`
- Modify: the four `internal/generator/templates/transport/http/{fiber,gin,chi,echo}_router.go.tmpl`, `internal/generator/generator.go` (`toolingFiles`)
- Modify: `internal/generator/generator_test.go` (`smokeBoot` in the no-DB bootstrap), `docs/01-cli-options.md`, `docs/07-transport-layer.md`, `docs/09-infrastructure-layer.md`, `docs/12-build-and-deploy.md`, roadmap

- [x] **Step 1: Embedded spec and route**

`api/openapi/openapi.go.tmpl`:

```go
// Package openapi embeds the API description so the running service can
// serve it (GET /openapi.yaml) and tooling can fetch the contract from the
// same binary that implements it.
package openapi

import _ "embed"

// Spec is the OpenAPI 3 document for this service.
//
//go:embed openapi.yaml
var Spec []byte
```

`generator.go` `toolingFiles`: `{"templates/api/openapi/openapi.go.tmpl", "api/openapi/openapi.go", cfg.HasHTTP()}`. Each router imports `"{{.ModuleName}}/api/openapi"` and registers, after `/readyz`:

- fiber: `app.Get("/openapi.yaml", func(c *fiber.Ctx) error { c.Set(fiber.HeaderContentType, "application/yaml"); return c.Send(openapi.Spec) })`
- gin: `engine.GET("/openapi.yaml", func(c *gin.Context) { c.Data(http.StatusOK, "application/yaml", openapi.Spec) })`
- chi: `router.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Content-Type", "application/yaml"); _, _ = w.Write(openapi.Spec) })`
- echo: `e.GET("/openapi.yaml", func(c echo.Context) error { return c.Blob(http.StatusOK, "application/yaml", openapi.Spec) })`

- [x] **Step 2: The boot smoke test**

In `generator_test.go`, `bootstrapProject` ends with `if !cfg.HasDatabase() && cfg.HasHTTP() { smokeBoot(t, dir) }`:

```go
// smokeBoot proves the host boot path on the no-dependency project: build the
// binary, start it with a generated keypair and a free port the way the
// README does (.env in cwd), wait for /healthz, then SIGTERM it and expect a
// clean exit. It runs only for the no-database bootstrap, so no compose
// dependency is needed.
func smokeBoot(t *testing.T, dir string) {
	t.Helper()
	bin := filepath.Join(dir, "smoke-bin")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod -p=2")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	pub, priv := testKeyPair(t)
	port := freePort(t)
	envFile := "APP_ENV=local\nSERVER_PORT=" + port + "\nJWT_PUBLIC_KEY=" + pub + "\nJWT_PRIVATE_KEY=" + priv + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envFile), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "api")
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	healthy := false
	for i := 0; i < 50 && !healthy; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get("http://127.0.0.1:" + port + "/healthz")
		if err == nil {
			healthy = resp.StatusCode == http.StatusOK
			resp.Body.Close()
		}
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	waitErr := cmd.Wait()
	if !healthy {
		t.Fatalf("/healthz never answered 200\n%s", output.String())
	}
	if waitErr != nil {
		t.Fatalf("exit after SIGTERM: %v\n%s", waitErr, output.String())
	}
	mustContainAll(t, output.String(), "starting HTTP server", "shutdown complete")
}

func testKeyPair(t *testing.T) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return base64.StdEncoding.EncodeToString(pubPEM), base64.StdEncoding.EncodeToString(privPEM)
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port
}
```

(imports: `bytes` exists; add `crypto/rand`, `crypto/rsa`, `crypto/x509`, `encoding/base64`, `encoding/pem`, `net`, `net/http`, `syscall`, `time`.) The server's startup log says `starting HTTP server` and the entry point logs `shutdown complete`, both already present.

- [x] **Step 3: Docs and roadmap** — `docs/07`: `/openapi.yaml` next to the probes; `docs/01`: a note under the options table that `--search=elasticsearch` makes `golangci-lint run` take minutes (the typed client's generated types; measured 6–8 min vs 19 s) and that the bootstrap keeps Search off for that reason; `docs/09` pubsub: the Kafka producer dials at startup on purpose (a publisher that cannot reach its broker is misconfiguration; compose waits on the broker's healthcheck), unlike Redis; `docs/12`: the boot smoke test and the one-off `docker build` check; roadmap: Phase 6 header "(done 2026-10-14, commit `5bf2c7a`)", Phase 7 "Detailed plan:" link.

- [x] **Step 4: Verify** — `make test`, `make lint`, `npx --no-install markdownlint-cli2 docs/*.md docs/superpowers/plans/*.md`, then alone `make test-all` (the echo/none/fx bootstrap now ends with the smoke boot). Then a one-off, alone: render fiber/postgres/wire with docker on, `make gen`, `docker build -t nova-smoke .` and confirm it succeeds; remove the image afterwards. `docker compose up` against live dependencies stays manual.

- [ ] **Step 5: Commit** — stop and ask the user to run `/grimoire-core:commit` (`feat(generator): serve /openapi.yaml and prove the host boot path`).
