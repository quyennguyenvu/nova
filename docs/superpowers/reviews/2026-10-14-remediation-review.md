# Nova Remediation Review

Reviewed 2026-10-14, against the working tree with Phase 8 staged but not committed. Source plan: [2026-10-04-review-remediation-roadmap.md](../plans/2026-10-04-review-remediation-roadmap.md). Follow-up: the findings below are addressed by [Phase 9](../plans/2026-10-14-phase9-close-out.md), whose status block lists what was fixed and what stays deferred.

The eight-phase roadmap is mostly done but not finished: most original findings are fixed and all eight generated project shapes build, lint and pass their tests, yet five phases are marked done with exit criteria unmet, two headline findings are only partly fixed, and the review found four new High defects. Every High and most Medium items were re-checked live or against the dependency source, not taken from a reviewer's reading alone.

## Phase by phase

Four phases meet their exit criteria, one partly, and four do not, although the roadmap marks all of 1 to 7 as done.

| Phase                       | Original findings | Exit criteria | Main gap                                                                                                                                                                                            |
| --------------------------- | ----------------- | ------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0 Resource guardrails       | All fixed         | Met           | None                                                                                                                                                                                                |
| 1 Builds and lints          | All fixed         | Met           | The bootstrap silently skips lint, or the whole run, when a tool is missing. Matrix rows using wire never type-check `wire.go`.                                                                     |
| 2 CLI hardening             | Most fixed        | Not met       | Worker without a database renders a project that cannot compile. Two documented flag defaults fail. The Go-version and survey findings were dropped without a note. `internal/prompt` has no tests. |
| 3 Observability             | Most fixed        | Not met       | `TestLoggingFields` was never written. Emails still appear in error traces. No manual run per transport.                                                                                            |
| 4 HTTP parity and security  | Most fixed        | Not met       | Fiber trusts a forged `X-Forwarded-For` once trusted proxies are set, bypassing login throttling.                                                                                                   |
| 5 Worker reliability        | Most fixed        | Not met       | The RabbitMQ demo in the generated README still delivers nothing (verified live).                                                                                                                   |
| 6 Layers and domain         | All fixed         | Met           | The duplicate-email test fakes an already-translated error, not the driver error.                                                                                                                   |
| 7 Infrastructure and deploy | Most fixed        | Partly met    | Kafka projects cannot boot on the host. `make migrate-up` fails in the quick start.                                                                                                                 |
| 8 gRPC and docs honesty     | All listed fixed  | Met           | 14 other doc claims are false. gRPC has no login throttle or per-call deadline.                                                                                                                     |

## What was run live

The worker machinery is sound once names align; the defaults around it are what break, and two roadmap exit criteria that were never run by hand are now checked.

| Check                                                                                                                                                      | Result                                                                                                                                                            |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Hostile `nova new` inputs: path traversal, absolute paths, quotes in names, typos, `cron`, `toml`, `gorm`, `gitlab`, a bad module path, a non-empty target | All rejected before any file is written                                                                                                                           |
| Hostile `nova add` inputs: traversal names, spaces, leading digits, a second run, a mismatched `--type`                                                    | All rejected                                                                                                                                                      |
| README worker demo on RabbitMQ, run exactly as written (publisher project plus worker project, isolated compose stack)                                     | 0 audit rows: the publisher wrote to `demo-api.events`, the worker's queue is bound to `demo.events`, the broker dropped the message as unroutable                |
| Same demo with the exchange aligned and a fresh queue                                                                                                      | Delivered once; a redelivered duplicate was ignored; a malformed message was parked on the `.dlq` queue; the publisher's request id reached the worker's log line |
| Changing `RABBITMQ_EXCHANGE` on an existing queue                                                                                                          | Worker exits at start with `PRECONDITION_FAILED`, because the dead-letter exchange name is derived from the exchange and fixed in the queue's arguments           |
| Docker image plus compose for a fiber, Postgres, Redis, RabbitMQ, wire project                                                                             | Image built; container reported healthy via `/readyz`; login worked inside compose                                                                                |
| Quick start: `cp .env.example .env`, then `make migrate-up`                                                                                                | Runs `migrate -database '' up`, because the Makefile never reads `.env`                                                                                           |
| Full nova suite after Phase 8                                                                                                                              | `make test-all` green: 36 vet rows and 8 real-toolchain bootstraps in 3 min 33 s                                                                                  |

## High defects

Four defects break a supported, documented path; each was verified in rendered output, a live run or the dependency's source.

| Defect                                   | What happens                                                                                                                                                                                                       | Evidence                                                                     | Phase |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------- | ----- |
| Worker without a database cannot compile | `--transport=worker --database=none` passes validation, but the generated worker handler and `wire.go` import `usecase/useraudit`, which is only rendered with a SQL database                                      | Rendered project: package missing, two importers                             | 2     |
| RabbitMQ README demo cannot deliver      | Publisher and worker are always separate projects, and each defaults its exchange to `<project>.events`; the README never mentions `RABBITMQ_EXCHANGE`                                                             | Live run: 0 rows; unroutable message dropped                                 | 5     |
| Kafka projects cannot boot on the host   | `.env.example` and `base.yaml` use `localhost:9092`, but compose publishes and advertises the host listener on `29092`; the producer dials at startup                                                              | Rendered `.env.example` and `docker-compose.yaml`                            | 7     |
| Fiber trusts a forged client IP          | With `trusted_proxies` set, fiber's `c.IP()` returns the raw `X-Forwarded-For` value; a client rotates its login-limiter key and forges `client_ip` in logs; the key is also an unsafe string over a reused buffer | fiber v2.52.15 `ctx.go` `IP` and `extractIPFromHeader`; rendered `router.go` | 4     |

## Medium defects

Twelve Medium defects are config, docs or operational traps a new user hits in the first hour; all were verified except the last two, which are design gaps.

| Defect                                            | What happens                                                                                                                                                                           | Phase |
| ------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----- |
| Two documented flag defaults fail                 | `--database=mysql` without `--db-driver` and `--transport=http` without `--http-framework` are rejected, although the README lists defaults; interactive MySQL has the same root cause | 2     |
| Module path ignores the project name              | `nova new shop` with flags gets module `github.com/myorg/myproject`, because the default config pre-fills it                                                                           | 2     |
| Quick-start migration fails                       | `make migrate-up` after `cp .env.example .env` runs with an empty URL; the MySQL `.env` is also not shell-sourceable (unquoted `tcp(...)`)                                             | 7     |
| Empty `.env` keys wipe YAML values                | cleanenv treats a set-but-empty key as present, so `CORS_ALLOW_ORIGINS=` erases the development origins; list items are not trimmed, so `"a, b"` silently drops `b`                    | 7     |
| Kafka dead-letter failure loops                   | A failed `.dlq` write ends the whole consumer session, which rejoins immediately and repeats every few seconds                                                                         | 5     |
| Changing the RabbitMQ exchange crashes the worker | The dead-letter exchange is named after the work exchange and baked into the queue arguments                                                                                           | 5     |
| JWT key pair can leak                             | The README writes `jwt.key` to the project root; neither `.gitignore` nor `.dockerignore` excludes it                                                                                  | 7     |
| `make -C` deletes in the wrong place              | Generated `rm` lines use `$(PWD)`, which is the caller's directory under `make -C`; use `$(CURDIR)`                                                                                    | 1     |
| `nova new --force` duplicates migrations          | Timestamped names produce a second set; the worker's second audit migration fails on its unique index                                                                                  | 2     |
| `nova add worker` breaks a queue-less project     | Exits 0 and wires `wire.go` to `internal/infrastructure/pubsub`, which does not exist                                                                                                  | 2     |
| gRPC lacks HTTP's abuse controls                  | No login or registration throttle, no per-call deadline; auth is opt-in per method                                                                                                     | 8     |
| Public directory exposes emails                   | `GET /api/v1/public/users` returns up to 500 emails a page, unauthenticated and unthrottled                                                                                            | 4     |

## Low defects

The Low items are real but bounded; most come from reviewer reading, with the overflow and banner confirmed directly.

- **Page numbers can overflow.** A huge `page` wraps the offset negative past the range guard: a 500 on Postgres, a panic in the in-memory repository.
- **gRPC panics answer in English.** Recovery returns the literal "internal error" instead of the localized message, and stashes no error for the access log.
- **Client disconnects look like server faults.** `context.Canceled` classifies as 500 and logs at error level on HTTP and gRPC.
- **Unknown `/users/...` paths return 401 on fiber and echo.** Group-level auth runs before routing; gin and chi return 404 or 405.
- **Fiber's own 413 and 431 skip the middleware.** No `requestId`, no access line, and 431 maps to 500.
- **Fiber prints its banner into the JSON log stream.** `DisableStartupMessage` is not set.
- **gRPC health flips too late to matter.** NOT_SERVING is followed immediately by `GracefulStop`, so no balancer can observe it.
- **Transient outages park messages in bulk.** Anything not marked poison is parked after three quick attempts, so a database failover over about 2 s fills the dead-letter queue.
- **RabbitMQ parked messages lose the reason.** Kafka copies `x-dead-letter-reason`; RabbitMQ only gets `x-death: rejected`.
- **`nova add` misbehaves on some names.** Acronyms such as `HTTPServer` break the sqlc model name round trip, Go keywords pass validation, and multi-file writes are not atomic.
- **Emails still appear in error traces.** The Phase 3 finding was deferred and remains in the usecase wrap messages.
- **Fourteen doc claims are false.** Examples: docs/12 says every target is in `make help` (three are), docs/13 says only `/login` is throttled (registration is too), docs/12 gives `LIMIT_DB_MEMORY` as `512M` (compose uses `1G`).

## Tests and process

Most contract tests grep generated text instead of exercising behavior, which is why the High defects above all pass a green suite.

- **No rendered HTTP behavior tests.** No generated test sends a request, so `TestHTTPParity` passes on the string `EnableTrustedProxyCheck: true` while fiber trusts a forged IP.
- **`TestDocsClaims` mostly re-reads its own phrases.** None of the fourteen false claims would fail it.
- **`TestRenderedGoIsGofmtClean` cannot fail.** Every file is already formatted on write.
- **Wire matrix rows never type-check `wire.go`.** It is build-tagged and the injector is stubbed, so only the bootstraps prove a wire graph.
- **Tool skips look like passes.** The bootstrap skips lint, or the whole run, when a tool is missing.
- **Nothing runs nova's tests in CI.** The only workflow is the release job, so "green" is a local claim.
- **Good exceptions.** The worker policy tests, the gRPC end-to-end test over bufconn, and the `nova add` overwrite and path-escape tests do check real outcomes.

Each phase plan was followed and records its deviations, but four items disappeared without one: the Phase 3 `TestLoggingFields`, the Phase 3 manual run per transport, a Phase 2 item marked "deferred to Phase 5" that never arrived, and both Phase 2 dependency findings (Go version from nova's binary, unmaintained `survey`). The roadmap marks a phase done when its code lands, not when its exit criteria pass.

## Recommended next step

Run one close-out phase before calling the roadmap finished, in this order:

1. Fix the four High defects, each with a test that fails first.
2. Fix the Medium config and quick-start items, then re-run the README worker demo and a Kafka host boot by hand.
3. Replace grep-only parity checks with a rendered `httptest` table per framework, run in the bootstrap: spoofed `X-Forwarded-For` with and without trusted proxies, 404, 405, oversized body, expired deadline, and one captured access line.
4. Add a matrix or bootstrap row for every combination nova's own `gen-api`, `gen-worker` and `gen-grpc` targets produce, plus worker without a database.
5. Correct the fourteen false doc claims and turn `TestDocsClaims` into behavior checks where a claim is testable.
6. Add a CI workflow that runs `make test` and `make lint` on every push.
