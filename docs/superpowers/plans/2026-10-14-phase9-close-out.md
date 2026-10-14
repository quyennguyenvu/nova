# Phase 9: Close-out Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Heavy steps (`make test-all`, bootstraps, live compose runs) run alone from the main thread; see Global Constraints.

**Goal:** Close the roadmap: fix the four High and twelve Medium defects found by the [2026-10-14 remediation review](../reviews/2026-10-14-remediation-review.md), replace the grep-only HTTP parity proof with behavior tests that run in the rendered project, cover every combination nova's own `gen-*` targets produce, make the fourteen false doc claims true or remove them, and run nova's own tests in CI.

**Architecture:** Validation gains three normalizations (HTTP framework default, driver default, worker needs SQL) and the default config stops pre-filling the module path and driver. Fiber resolves the client IP the way chi already does, in a `ClientIP` middleware that walks `X-Forwarded-For` right to left past trusted proxies and stores a copied string. RabbitMQ publisher and worker meet on one default exchange (`events`) and the dead-letter exchange is named after the queue; Kafka's host listener moves to 9092 so `.env.example` is correct; a failed Kafka dead-letter write is retried inside the claim instead of ending the session. Config loading ignores empty `.env` values and trims list entries. The generated Makefile reads `MIGRATE_DATABASE_URL` from `.env` when the environment lacks it, documents every target for `make help`, and uses `$(CURDIR)`. gRPC becomes deny-by-default (`Public()` instead of `Protected()`), throttles credential RPCs per peer and bounds every call with a deadline. The public user directory stops exposing emails. Each HTTP framework gets a rendered `router_test.go` that drives the real router.

**Tech Stack:** Go 1.26 toolchain, `net/http/httptest` and fiber `App.Test`, `golang.org/x/time/rate`, `google.golang.org/grpc/peer`, GitHub Actions.

## Scope

In scope, from the review: every High and Medium defect, recommended steps 1 to 6, and the Low "gRPC panics answer in English" (it makes a doc claim true). Deferred, unchanged: the other Low items (page-offset overflow, `context.Canceled` as 500, fiber/echo 401 on unknown `/users/*`, fiber 413/431 outside middleware, fiber banner, health drain delay, bulk parking on transient outages, RabbitMQ dead-letter reason, `nova add` acronyms/keywords/atomicity, emails in error traces), plus the dropped Phase 2 dependency items (Go version source, `survey`) and the Phase 3 `TestLoggingFields` (task 6's access-line assertion covers the HTTP part).

**Status (2026-10-14):** all seven tasks implemented and verified; commit pending the user's `/grimoire-core:commit`. `make test` green, `make lint` 0 issues, markdownlint 0 issues on every edited file, `make test-all` green in 3 min 48 s (36 vet rows, now also vetting `wire.go` under `-tags wireinject`, and 8 bootstraps that now run the rendered HTTP router tests, the config test, and the `gen-api`, `gen-worker` and `gen-grpc` stacks). Each new behavior test was shown to fail against the old code: the fiber forwarded-IP test against the old proxy config, the config test against the old `.env` loader. Run by hand on isolated compose stacks: the RabbitMQ README worker demo as written (delivered once, request id correlated, `make migrate-up` straight from `.env`), and a Kafka-backed API booting on the host and publishing `user.created`.

**Deviations from the plan as written:**

- The live Kafka run found a defect the review missed: compose Kafka never started in any generated project, because Kafka 3.9 refuses an advertised `0.0.0.0` controller listener. Listeners now bind with empty hosts, and the healthcheck probes the in-network listener so a remapped host port stays healthy; both are pinned in `TestDockerComposeStack`.
- An explicit `--http-framework` on a non-HTTP transport is rejected rather than silently blanked.
- The generated README tells the RabbitMQ demo to start the worker first: the live run showed a message published before the queue is bound is dropped, as RabbitMQ topic exchanges do.
- `TestDocsClaims` grew helpers (`assertRejectedValues`, `assertCORSAndTraceOrder`, `assertNovaTargetsListed`) to stay under nova's gocognit limit.

## Global Constraints

- `make lint` and `make test` stay green after every task; `make test-all` runs alone at the end of tasks 6 and 7.
- Resource rule: one heavy toolchain command at a time from the main thread; never `go clean -cache`; live compose stacks use alternate host ports and never touch containers the project did not start.
- Every change to a `*_router`, `*_middleware`, `*_registrar` or `*_http` template applies to all four frameworks; `skel/worker` copies change together with their `templates/transport/worker` counterparts.
- Where a doc and a template disagree, the template wins and the doc is fixed in the same change.
- Markdown passes markdownlint with the repo config. Commits only via `/grimoire-core:commit` after confirmation.

---

### Task 1: Validation and defaults

**Files:** `internal/config/{config.go,options.go,options_test.go}`, `cmd/{new.go,new_test.go}`, `internal/generator/component.go`, `internal/generator/component_test.go`, `README.md`, `docs/01-cli-options.md`

- [x] `DefaultConfig` leaves `ModuleName` and `DBDriver` empty; `Validate` defaults `HTTPFramework` to `fiber` for HTTP and blanks it otherwise, and rejects `worker` without a SQL database (`transport worker needs --database postgres or mysql`).
- [x] `nova add worker` refuses a project whose broker resolves to neither kafka nor rabbitmq.
- [x] Tests: options table (mysql without driver, http without framework, grpc with a framework, worker without database); a `cmd` end-to-end test that runs `nova new shop --transport=http` in a temp dir and checks the module and framework; component tests set an explicit broker and one asserts the refusal.

### Task 2: Fiber client IP

**Files:** create `templates/transport/http/middleware/fiber_clientip.go.tmpl`; modify `fiber_router.go.tmpl`, `fiber_loginlimit.go.tmpl`, `fiber_logging.go.tmpl`, `generator.go` (clientip for chi and fiber), `generator_test.go` (`TestHTTPParity`)

- [x] `ClientIP(trusted []*net.IPNet) fiber.Handler` resolves the address with the chi algorithm and stores a copied string in `c.Locals`; `RemoteIP(c)` reads it back. The router drops `ProxyHeader`/`EnableTrustedProxyCheck`/`TrustedProxies` (so `c.IP()` is the TCP peer) and installs `ClientIP` first; the limiter and the access line use `RemoteIP`.

### Task 3: Broker defaults and dead letters

**Files:** `base.yaml.tmpl`, `docker-compose.yaml.tmpl`, `README.md.tmpl` (worker demo), `templates/transport/worker/{rabbitmq_consumer,kafka_consumer,kafka_consumer_test}.go.tmpl`, `skel/worker/{rabbitmq_consumer,kafka_consumer}.go.tmpl`, docs 07, 12, `generator_test.go`

- [x] RabbitMQ exchange default `events` in every project; dead-letter exchange `<queue>.dlx`.
- [x] Compose Kafka: in-network listener `kafka:29092`, host listener `localhost:9092` published on 9092; the app container gets `KAFKA_BROKERS: kafka:29092`.
- [x] Kafka `park(ctx, m, cause) bool` retries the dead-letter write with capped backoff (100 ms doubling to 30 s) until the session ends; `settle` never returns an error, so `ConsumeClaim` ends only on shutdown.
- [x] README demo: shared exchange, publisher generated with `--cache=none`, publisher `.env` pointed at the worker's database.
- [x] Tests: dead-letter failure then success marks the message; a cancelled session leaves it unmarked; compose and config assertions updated.

### Task 4: Config, quick start and generator hygiene

**Files:** `config.go.tmpl`, `env.example.tmpl`, `Makefile.tmpl`, `gitignore.tmpl`, `dockerignore.tmpl`, `generator.go` (migration names), `generator_test.go`

- [x] `loadDotEnv` sets only non-empty values not already in the environment; `Load` trims and drops empty list entries.
- [x] Makefile: `MIGRATE_URL` falls back to `.env`; every target carries `##`; `$(CURDIR)` replaces `$(PWD)`; the bootstrap drops its `PWD` workaround.
- [x] `.env.example` quotes the MySQL migrate URL and stops claiming sqlc reads it; `.gitignore` and `.dockerignore` exclude `*.key`, `*.pem`, `jwt.pub`.
- [x] `nova new --force` reuses an existing migration's timestamped name.
- [x] Tests: `.env.example` sources under `sh`; every Makefile target is documented; a second `Generate` into the same directory leaves one migration pair.

### Task 5: gRPC hardening and the public directory

**Files:** `templates/transport/grpc/{server,user,user_test}.go.tmpl`, `interceptor/{auth,interceptor}.go.tmpl`, create `interceptor/{deadline,throttle}.go.tmpl`, `config.go.tmpl`, `base.yaml.tmpl`, `di/*`, proto, usecase/HTTP public projection, OpenAPI, docs 06, 07, README

- [x] `Registrar` is `Register`, `Public() []string`, `Throttled() []string`; `Auth` protects every method outside the public set (health `Check` is always public).
- [x] `Deadline(timeout)` bounds every call (`grpc.request_timeout`, 10 s); `Throttle(cfg, methods)` applies a per-peer token bucket to `Login` and `CreateUser` (`grpc.login_rate_limit`).
- [x] Recovery answers with the localized internal-error message through `rpcstatus`.
- [x] The public directory returns names only (usecase, HTTP DTO, assembler, OpenAPI, proto, README, docs).
- [x] Tests: an RPC outside `Public()` needs a token; the third `Login` in a burst of two is `ResourceExhausted`; a panic answers with the translated message.

### Task 6: Behavior tests and coverage

**Files:** create `templates/transport/http/{fiber,gin,chi,echo}_router_test.go.tmpl`; `generator.go`, `generator_test.go`

- [x] Each rendered `router_test.go` drives the real `NewRouter`: 404 and 405 in the envelope with a `requestId`; an oversized body is 413; without trusted proxies a rotating `X-Forwarded-For` is still throttled at burst + 1; with the test peer trusted, two forwarded clients get separate buckets; the access line carries `request_id`, `route` and `status`.
- [x] Bootstrap runs `./internal/transport/http/...`; the fiber, worker and gRPC bootstrap configs match `gen-api` (minus Elasticsearch), `gen-worker` and `gen-grpc`; wire matrix rows also vet `wire.go` under `-tags wireinject`; a missing linter is logged, not silent.

### Task 7: Docs honesty, CI and close

**Files:** docs 07, 12, 13, `README.md.tmpl`, `CLAUDE.md.tmpl`, `internal/generator/docs_test.go`, create `.github/workflows/ci.yaml`, roadmap, `CLAUDE.md`

- [x] Fix the remaining false claims (docs/12 memory default, docs/13 throttled routes, docs/07 registrar snippet, generated README no-database tree, generated CLAUDE.md `docker-up`/cache/DB lines, health drain wording).
- [x] `TestDocsClaims` checks behavior where it can: Makefile targets all documented, compose `LIMIT_*` defaults equal docs/12's table, `.env.example` sources, registrars throttle exactly the routes docs/13 names.
- [x] nova CI runs `go test -short ./...` and golangci-lint v2.11.4 on push and pull request.
- [x] Verify: `make test`, `make lint`, markdownlint, `make test-all`, then by hand the README worker demo on RabbitMQ and a Kafka host boot against compose.
