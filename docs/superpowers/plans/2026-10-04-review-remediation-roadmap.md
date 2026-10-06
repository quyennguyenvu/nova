# Nova Review Remediation Roadmap

> **For agentic workers:** this is the master plan. Each phase below becomes its own executable plan in this directory (phase 1 exists: [2026-10-04-phase1-generated-output-builds-and-lints.md](2026-10-04-phase1-generated-output-builds-and-lints.md)). Execute phases in order; inside a phase use superpowers:subagent-driven-development or superpowers:executing-plans. Commits happen only through `/grimoire-core:commit` after the user confirms.

**Goal:** Turn the 2026-10-04 architecture review of nova into a sequenced set of fixes so every supported `nova new` output builds, lints, runs, and can be debugged from its logs, and so the CLI never silently produces a broken project or destroys user files.

**Source of findings:** the 2026-10-04 review (six parallel reviewers plus hands-on verification: seven variants rendered, three bootstrapped with real `sqlc`, `wire`, `go build`, `golangci-lint`). Every finding below was reproduced, not just read.

**Fix order and why:**

| Phase | Theme | Why this position |
| --- | --- | --- |
| 0 (done) | Resource guardrails | Verification must not freeze the 16 GB dev machine again |
| 1 | Generated output builds and lints, with a real-toolchain proof | Every later fix is verified through this harness; today the matrix stubs `sqlc` and `wire` and never renders `--database=none`, which is how a non-building MySQL output and a non-compiling no-DB output shipped |
| 2 | CLI hardening | Stops silent broken projects (`cron`, `cli`, `toml`, `env`, `raw`, `gorm`, typos), silent overwrites, and path traversal in `nova add`; small, self-contained, no template churn |
| 3 | Observability | The user's explicit lens. Today logged errors serialize to `{}`, `trace_id` is always empty, and the worker fails silently; additive changes with low regression risk that also make every later phase easier to debug |
| 4 | HTTP transport parity and security | Default path for most users: CORS `*` fallback, no body limits, no echo timeouts, spoofable rate limiter, 404s outside the envelope |
| 5 | Worker reliability and messaging | Zombie consumer on channel close, Kafka rebalance storm on one poison message, exchange-name mismatch that makes the README demo undeliverable; independent of phase 4, can run in parallel with it |
| 6 | Layers and domain correctness | Usecase depends on concrete JWT type, dead transaction manager, duplicate email returns 500, typed-nil `Wrap`, anemic entity; medium severity, touches shared code so it goes after the transport phases stabilize |
| 7 | Infrastructure and deploy hygiene | `.env` never loaded on host, unknown `APP_ENV` accepted, Dockerfile gaps, MySQL infra lags Postgres, RabbitMQ never closed |
| 8 | gRPC decision and docs honesty | Decide stub vs real transport, then the final docs sweep once behavior is settled |

Phases 4 and 5 are independent of each other. Phases 2 and 3 are independent of each other but both depend on phase 1's harness.

## Global Constraints

- Go 1.25.1+; module `github.com/quyennguyenvu/nova`; binary `bin/nova`.
- `make lint` (golangci-lint v2, strict golden config) and `make test` (`go test -short ./...`) stay green after every task. Generator or template changes are additionally proven by `make test-all` (`go test -parallel 2 -timeout 30m ./...`), run alone.
- Resource rule (CLAUDE.md "Resource limits"): heavy toolchain commands run one at a time from the main thread, never inside parallel subagents; never `go clean -cache`.
- Templates receive `*config.ProjectConfig`; condition on its `Has*`/`Use*` helpers; add a helper to `internal/config/config.go` before using it in templates or `buildFileList()`.
- Every change to a `*_handler`, `*_router`, `*_middleware`, `*_registrar`, or `*_http` template must be applied to all four framework variants (fiber, gin, chi, echo).
- `templates/app/worker.go.tmpl` and `skel/worker/app_worker.go.tmpl` must stay byte-identical (`TestWorkerAppTemplatesIdentical`); likewise `cmd/worker.go.tmpl` and `skel/worker/cmd_worker.go.tmpl`.
- Where a doc in `docs/` and a template disagree, the template wins and the doc is fixed in the same change.
- Markdown edits must pass markdownlint with the repo `.markdownlint.json`; no hard-wrapped prose.
- Commits only via `/grimoire-core:commit` after explicit confirmation; no `git commit`, no `--amend`, no Claude co-author trailer.

---

## Phase 0 (done 2026-10-04): resource guardrails

Applied and verified in the working tree (uncommitted): `make test` is `-short`; `make test-all` is `-parallel 2 -timeout 30m`; the matrix harness passes `GOFLAGS=-mod=mod -p=2` to child `go` commands; `run.concurrency: 4` in both golangci configs; `GOGC=50` on both lint targets; "Resource limits" section in CLAUDE.md; "Machine resources" section in `~/.claude/CLAUDE.md`. Measured: full matrix 58 s and 0.45 GB peak with a warm cache.

## Phase 1: generated output builds and lints

Detailed plan: [2026-10-04-phase1-generated-output-builds-and-lints.md](2026-10-04-phase1-generated-output-builds-and-lints.md).

Findings fixed:

- Rendered Go is not gofmt-clean in 21 files across variants (conditional template lines break struct alignment and import order), so a fresh project fails its own `make lint`, pre-commit hook, and CI lint job. Fix: run `go/format` on every rendered `.go` file at write time, in both generators.
- MySQL projects cannot build: `templates/migrations/create_users_table.up.sql.tmpl` is Postgres DDL (`BIGSERIAL`, `TIMESTAMP WITH TIME ZONE`), `sqlc generate` rejects it, `dbgen` never exists. Hidden by `writeDBGenStub` in the matrix.
- No-database HTTP projects do not compile: `health/checker.go.tmpl:87` declares `probeCtx` unconditionally; `wire.go.tmpl:18` and `fx.go.tmpl:21` import `domain` unconditionally; `usecaseSet`/`usecaseModule` require a `domain.UserRepository` no adapter provides. Fix: in-memory `UserRepository` adapter bound when `!HasDatabase()`, gated probe body, no-DB matrix rows.
- Documented first run fails: CLI, README, and `make gen` order `go mod tidy` before `sqlc`; `tidy` cannot resolve the missing `dbgen` import. Also `cp env.example .env` names the wrong file, `go run main.go api` is wrong for worker and gRPC, and `WIRE_PATH="$(GOPATH)/bin/wire"` is empty when `GOPATH` is not exported.
- Real lint hits in templates: `usecase/user/service.go.tmpl` int→int32 overflow of `Offset` from an unbounded `page` (G115); `app/worker.go.tmpl` compares `context.Canceled` with `!=` (errorlint); `jwt/service.go.tmpl` named returns and a long line; `pubsub/kafka.go.tmpl` `fmt.Errorf` without verbs; `redis/user_cache.go.tmpl` misformed doc comment; `locale_vi.go.tmpl` gosec G101 false positive; four `*_registrar.go.tmpl` embedded-field spacing; `fiber_loginlimit.go.tmpl` long line; `config.go.tmpl` tag alignment; `rabbitmq_consumer.go.tmpl` gocognit; `pubsub/publisher.go.tmpl` shadowed `err`; `mysql/user_repository.go.tmpl` redundant conversions; goimports `local-prefixes` still says `github.com/my/project`.
- Matrix gaps: no `database=none` rows; no real `sqlc`/`wire`/`go build`/`golangci-lint` run anywhere in nova's tests.

Exit criteria: `make test-all` includes `TestGeneratedProjectBootstrap` running the real `make gen` → `go build` → `golangci-lint run` sequence on fiber+postgres+wire, gin+mysql+wire, worker+postgres+kafka+wire, and echo+none+fx, all green; `TestRenderedGoIsGofmtClean` green; no-DB rows in `TestGenerateMatrix` green.

## Phase 2: CLI hardening (nova itself)

Findings:

- Three sources of truth for options (help text in `cmd/new.go`, option lists in `internal/prompt/prompt.go`, whitelists in `generator.New`). Result, verified by rendering: `--transport=cron` emits `cmd/root.go` calling a `cronCommand()` that does not exist; `--transport=cli` and any typo such as `htpp` emit no `cmd/` at all; `--config=toml` and `--config=env` leave `//go:embed *.toml` / `*.env` with no matching files; `--query=raw`/`gorm` drop `sqlc/` but the repository still imports `dbgen`; `--transport=worker --queue=none` renders no consumer and wire fails; `bigcache`, `sqlx`, `--grpc-gateway`, `--type`, `--ci=gitlab` are silently ignored; `--docker=false` is a no-op because `applyFlags` assigns only when true and `DefaultConfig` sets it true.
- `nova new` writes anywhere: `nova new ../escaped` and absolute paths are accepted; a project name with a quote renders non-parsing Go (`Use: "bad name"x"`); generating into an existing directory silently overwrote `internal/domain/user.go`.
- `nova new` with flags but no name silently creates `./myproject`.
- `nova add` path traversal: `nova add entity ../../../escape3` wrote a file above the project root; names are never validated (`Bad Name`, `123abc` produce non-parsing Go with exit 0); re-running overwrites user edits; with no `go.mod` it emits imports beginning with a bare slash instead of failing; `--type=mysql` in a postgres project writes a repository that cannot compile and drops MySQL DDL into the Postgres migrations dir; a second `nova add worker <Feature>` is never wired into `provideWorkerHandlers` and the printed hint names the wrong file.
- Two snake-case implementations disagree (`manifest.snake("HTTPServer")` → `h_t_t_p_server`, `generator.snakeCase` → `http_server`), so file name and table name diverge; `plural` appends `s` (`categorys`); `toTitle` duplicates `manifest.title`.
- No tests for `cmd/` or `internal/prompt/`, which is how the flag bugs survived.
- `GoVersion` comes from `runtime.Version()` of the nova binary, not the user's toolchain; `survey/v2` last released December 2022.
- Code layout: `internal/generator` mixes two generators, Go AST merge utilities, and a string-built codegen; generators print to `os.Stdout` directly.

Plan shape: `ProjectConfig.Validate()` driven by one option registry consumed by help text, prompts, and `generator.New` (reject unimplemented values with a clear message; normalize `QueryGen`/`DBDriver` to empty when no database); `Flags().Changed` for booleans; project-name and module-path validation; refuse non-empty target directory without `--force`; `nova add` name regex `^[A-Za-z][A-Za-z0-9]*$`, confine every output path under the root via `filepath.Rel`, refuse overwrite without `--force`, error when `Module` is empty, export one acronym-aware `Snake` from `manifest` and use it everywhere, irregular plural rules; table tests for `applyFlags` and `validateComponent`; inject an `io.Writer` into both generators; move `mergeGoDecls`/`injectImports` into `internal/gosrc`. Update `docs/01-cli-options.md` to match the rejected set.

Exit criteria: every value the CLI advertises either renders a project that passes `TestGeneratedProjectBootstrap` or is rejected with a one-line error before any file is written; adversarial names and paths are rejected; `cmd` and `prompt` have tests.

## Phase 3: observability

Findings (generated project):

- `infrastructure/logger/zerolog.go.tmpl` passes every kv value through `Interface`; an `error` JSON-marshals to `{}`. Eleven call sites pass raw errors, including every "stopped with error" and cache-degradation line.
- Tracing is dead: `tracing/otel.go.tmpl` builds a provider but no middleware or interceptor starts a span, so `trace_id` is empty on every line in every variant; docs/13 promises the opposite.
- `production.yaml.tmpl` sets `log.level: warn`, erasing all access and lifecycle lines.
- No `service`/`version`/`env` fields on log lines; no redacted config summary at boot.
- `user_id` never reaches the access log (auth runs after logging; chi needs a mutable slot).
- Worker scope lacks partition/offset/message id; no correlation header is propagated producer→consumer; no per-message duration, panic recovery, or timeout.
- `AppError.Error()` drops wrap messages; `errors.LogFields` has zero callers, so consumers and boot failures lose the frame trail.
- RFC3339 second-precision timestamps; `err` vs `error` key drift; readiness failures log nothing; shutdown outcomes swallowed (`_ = fxApp.Stop`, `_ = shutdown`); emails in `app_trace`.
- gRPC server has no interceptors at all.

Plan shape: logger `emit` type-switches `error` to `AnErr` (or add `Err(error)` to the `observability.Logger` port); `logger.New` takes app name/version/env and `With()`s them; `TimeFieldFormat = RFC3339Nano`; one `Info("config loaded", ...)` with secrets masked; prod level `info` plus an optional 2xx sampling knob; access log reads the principal after `Next()` and emits `route` + `path`; inbound `X-Request-ID` capped at 128 chars and charset-validated; otel middleware per framework (otelfiber/otelgin/otelchi/otelecho) and `otelgrpc`, with `otelpgx`/`redisotel` optional; worker `DispatchFunc` carries message metadata, publisher injects request/trace ids as headers, consumer scopes `message_id`/`partition`/`offset` and logs one outcome line with `duration_ms`; `errors.LogFields` used in consumers and runners; readiness `Warn` on failure; `shutdown complete` with duration; single `err` key.

Exit criteria: a debuggability scorecard (where/when/what/who/why × HTTP/worker/gRPC) is "ok" everywhere except gRPC "who", verified by asserting field presence in the matrix (`TestLoggingFields`) and by one manual run per transport.

## Phase 4: HTTP transport parity and security

Findings (all four frameworks unless noted):

- fiber and echo CORS fall back to `*` when `allow_origins` is empty (the production default), contradicting the template comment, `base.yaml`, and docs/13.
- echo server never applies `ReadTimeout`/`WriteTimeout` (`echo_http.go.tmpl` uses `e.Start`).
- gin, chi, echo have no request body limit; only fiber applies `cfg.HTTP.BodyLimit`.
- Login rate limiter keyed on spoofable `X-Forwarded-For` in all four; per-IP map never evicts.
- fiber handlers use `c.UserContext()` (never cancels on disconnect); no per-request deadline anywhere.
- gin registers `/api/v1/users/` and redirects the documented path (307/301).
- Framework-level 404/405 bypass the envelope; fiber's default error handler leaks `err.Error()` as text; fiber and echo log 404s as `status=200` at ERROR.
- No ownership check on `/users/{id}` (documented gap, but it is the pattern users copy).
- `/readyz` returns raw dial errors to anonymous callers.
- OpenAPI drift: documents `limit`/`offset`, code binds `page`/`perPage`; `/public/users` missing; envelopes lack `requestId`/`meta`.
- Bearer scheme compared case-sensitively; no `WWW-Authenticate`/`Retry-After`; gin/chi hand-rolled CORS omit `Vary: Origin`; chi logs `RemoteAddr` with port; registration endpoint unthrottled; bcrypt 72-byte limit unguarded (`max=72` missing); `primeDummyHash` fallback is an empty string.

Plan shape: a `trusted_proxies` CIDR list in config honored per framework; pass-through middleware when the origin list is empty; `http.MaxBytesReader` middleware for gin/chi and `echomw.BodyLimit`; `e.Server` timeouts; timeout middleware in all four; framework error handlers routed through `httpwriter.WriteError`; `""` route in gin; readiness returns `"fail"`; limiter TTL eviction or Redis when `HasRedis`; OpenAPI regenerated from the handlers; `TestHTTPParity` asserting header names, status codes, and timeouts across the four rendered variants.

Exit criteria: the parity table from the review has no "no"/"not set"/"missing" cells; `TestHTTPParity` green across fiber/gin/chi/echo.

## Phase 5: worker reliability and messaging

Findings:

- RabbitMQ publisher hardcodes exchange `events`; consumer binds `cfg.Exchange` (`<project>.events`); `nova add worker` uses a third name. The README demo can never deliver.
- RabbitMQ consumer returns `nil` when the delivery channel closes → `Start` returns nil → `RunWorker` blocks forever as a zombie; no `NotifyClose`, no reconnect; no `Qos`; publishes are transient (`DeliveryMode` unset); no confirms; connection and publisher channel never closed.
- Kafka `ConsumeClaim` returns the handler error → sarama ends the session → immediate rejoin → rebalance storm on one poison message; `Return.Errors=true` but `group.Errors()` never drained; no backoff, attempt counter, or DLQ; no key on produced messages (no per-user ordering); no headers.
- No panic recovery or per-message timeout in `Worker.dispatch`; shutdown does not wait for the consume goroutine before `Stop()`/`cleanup()`, so final offset commits can be lost.
- No event id or correlation id in `UserCreatedMessage`; duplicate deliveries insert duplicate audit rows; `KafkaConfig.UserCreatedTopic`/`RabbitMQConfig.UserCreatedQueue` are dead config that desync producer and consumer if overridden.

Plan shape: exchange/topic/queue names flow from config into both publisher and consumer (and the `nova add worker` skel); consumer returns a sentinel on channel close and `RunWorker` treats `Start()==nil` before `ctx.Done()` as fatal; `conn.NotifyClose` → exit non-zero; `ch.Qos`; `amqp.Persistent`; bounded in-claim retries with backoff then park (DLQ topic / `x-dead-letter-exchange`); drain `Errors()` into the logger; `wg.Wait()` before `Stop()`; `recover()` + `context.WithTimeout` per message; `EventID`/`CorrelationID` in the DTO with `UNIQUE(event_id)` + `ON CONFLICT DO NOTHING`; `Key: sarama.StringEncoder(userID)`; delete dead config fields.

Exit criteria: a broker-less unit test suite for the worker (fake consumer) proves ack/nack/retry/park semantics are identical for kafka and rabbitmq; a manual run against docker-compose shows the README demo delivering to `user_audit_log`.

## Phase 6: layers and domain correctness

Findings:

- `usecase/user/service.go.tmpl:14` imports the concrete `*jwtsec.TokenService` from infrastructure (the sole dependency-rule break; docs/03 and docs/04 contradict each other about it).
- `TxManager`/`qx` are rendered but no caller uses them; `Register` is check-then-insert with no transaction; `List` runs page and count in separate snapshots; docs/08 claims the usecase injects `TxManager`.
- Duplicate-email race returns 500: no adapter translates `23505`/`1062` to `ErrAlreadyExists`.
- `Wrap`/`Wrapf`/`L` return `*AppError`; docs/10 recommends `return errors.Wrap(err, …)` unconditionally, a typed-nil panic waiting to happen.
- Anemic entity: invariants exist only as transport validate tags, so gRPC, worker, and `nova add` output bypass them; `UserFilter.Name` is never read.
- Redis is a hard startup dependency for a cache decorator nothing wires; `Update` would wipe credentials if the decorator were enabled and `UpdateUser` ever wrote `password`; no TTL jitter; a failed `Del` after a successful write returns an error to the client.
- `Update` echoes the pre-update `UpdatedAt`; `identity.UserPrincipal` carries session/device fields; `pkg/locale` global written without `sync.Once`; `golang-jwt/jwt/v4` superseded by v5.

Plan shape: `security.TokenIssuer` port in `domain/security`, bound in DI; `TxManager` provided and used around `Register` and `List`; unique-violation translation in both SQL adapters; `Wrap`/`Wrapf`/`L` return `error` (keep `*AppError` reachable via `As`) and docs/10 fixed; `entity.NewUser(email, name, hash) (*User, error)`; health check degrades instead of blocking construction when Redis is down; `RETURNING updated_at` / set `UpdatedAt` before persist; `sync.Once` in locale; jwt v5 migration.

Exit criteria: dependency-rule grep (`TestDependencyRule`) shows zero outward imports from `usecase`; `TestRegisterDuplicateEmailIs409` with a fake repo returning the driver error; `TestWrapNilIsNil`.

## Phase 7: infrastructure and deploy hygiene

Findings:

- Nothing loads `.env` for host runs (`cleanenv.ReadEnv` only); the README's `cp .env.example .env` does nothing and the OS user leaks in as the DB user; cobra dumps usage on boot failure (`SilenceUsage` unset); `.env.example` placeholders are not shell-sourceable.
- Unknown `APP_ENV` silently runs base defaults with `ssl_mode: disable`.
- `database/mysql.go.tmpl`: no TLS option, no `SetConnMaxLifetime/IdleTime`, `Ping` without timeout.
- Dockerfile: `COPY . .` with no `.dockerignore`; `alpine:3.19` is end-of-life; dead `COPY .../config`; `HEALTHCHECK` hardcodes 8080.
- fx `stopTimeout = 15s` can skip `OnStop` hooks and the error is swallowed.
- `golangci-lint-action@v6` with `version: latest` against a v2 config (verify the action major before changing); `wire`/`sqlc`/`migrate`/`sqlfluff` unpinned; `migrate-down` with no count; CI boots Postgres/Redis the hermetic tests never use.
- HTTP startup coupled to Kafka producer dial; `redis.Addrs[0]` panics on empty list; `openapi.yaml` not served; no `-trimpath`.
- Linting a project that imports the typed `go-elasticsearch` client takes minutes (golangci-lint's analyzers crawl its thousands of generated types), observed 2026-10-04 at 6–8 minutes per project versus 19 seconds without it. Consider the low-level `esapi` client only, or document the cost next to `--search=elasticsearch`.

Plan shape: `cleanenv.ReadConfig(".env", &cfg)` when present; `SilenceUsage: true`; `APP_ENV` whitelist; MySQL config surface mirrors Postgres; `.dockerignore` template, `alpine:3.20`, port from config; fx stop timeout from config and logged; `go tool` directives for wire/sqlc (Go 1.24+ tool deps); action pins; `migrate-down` takes `n=1` default.

Exit criteria: fresh project `cp .env.example .env && go run main.go api` reaches "listening" with only the compose dependencies up; Dockerfile builds and `docker compose up` passes healthchecks (manual, documented in the plan).

## Phase 8: gRPC decision and docs honesty

Findings:

- gRPC transport is a `Ping()` placeholder never registered; no interceptors, health service, or reflection; `make gen` fails on a missing `api/proto`; `RunGRPCServer` exits 0 on bind failure and uses `Run` not `RunE`; docs/07 says placeholder, the generated README and CLAUDE.md do not.
- docs/01: `toml`/`raw`/`gorm` listed as "silently ignored" but they break the build; `env` format missing; `cron` described as "no cmd/" but it renders a dangling `cmd/root.go`; `worker+queue=none`, `--docker=false`, `--type` not covered.
- `templates/README.md.tmpl:145` says `go run main.go api` for gRPC; `CLAUDE.md.tmpl:63,89` render `repository/none` and `none dialect` for no-DB projects.
- Root README/CLAUDE.md omit the `{cache}` manifest placeholder and the `gen-grpc`/`run`/`clean`/`help` targets; docs/02 never mentions `dbgen/` or `wire_gen.go`; docs/03 vs docs/04 disagree on the jwt import; docs/08 `TxManager` claim; docs/13 CORS and trace claims.

Plan shape: decide between (a) real proto + `buf.gen.yaml` + health + reflection + recovery/logging interceptors + `RunE`, or (b) label gRPC experimental in CLI help, README, and CLAUDE.md and fix `make gen`; then a single docs sweep after phases 2–7 so the docs describe final behavior, verified by a `TestDocsClaims` that greps the ten checkable claims against rendered output.

Exit criteria: every doc claim listed in the review is confirmed against rendered code; markdownlint clean.
