# 13. Cross-cutting features

The production concerns every generated project ships with, and where each one lives. ([index](README.md))

Templates should never omit these. **Adding a new HTTP framework variant means re-implementing this list for that framework, not removing items.**

## The list

| Feature                     | Where it lives                                                                                       |
| --------------------------- | ---------------------------------------------------------------------------------------------------- |
| Graceful shutdown           | `internal/app/*.go` + every constructor's cleanup ([11](11-di-and-entrypoints.md#how-cleanup-works)) |
| Context propagation         | `context.Context` first arg through every layer                                                      |
| Structured logging          | `pkg/observability.Logger` port → `infrastructure/logger/zerolog.go`                                 |
| Request-scoped logger       | `pkg/logctx` — `logctx.From(ctx)` in inner layers                                                    |
| Request ID                  | `middleware/requestid.go` + `pkg/httputil.WithRequestID`                                             |
| Health endpoints            | `transport/http/health/checker.go` — `/healthz`, `/readyz`                                           |
| OpenTelemetry tracing       | `infrastructure/tracing/otel.go`; `trace_id` on every log line                                       |
| Input validation            | `go-playground/validator` via `httpwriter.Bind` / `BindQuery`                                        |
| Error handling + i18n       | `pkg/errors` (AppError, status table) + `pkg/locale` (codes, translations)                           |
| API versioning              | URL path — `transport/http/v1/v1.go` (`/api/v1`)                                                     |
| CORS                        | `middleware/cors.go`, config-driven, never `*`                                                       |
| Login rate limiting         | `middleware/loginlimit.go`, applied to the auth route only                                           |
| Request/response access log | `middleware/logging.go` — exactly one line per request                                               |
| Panic recovery              | `middleware/recovery.go` — 500 in the standard envelope                                              |
| Auth                        | `middleware/auth.go` + `infrastructure/jwt` (RS256)                                                  |
| Transaction management      | `domain.TxManager` → `adapter/repository/{db}/tx_manager.go`                                         |

**Not shipped**, despite appearing in earlier drafts: a Prometheus `/metrics` endpoint, and a general-purpose (non-login) rate limiter.

## The four cross-cutting mechanisms

Most of the table above is implemented by four ideas that recur everywhere. If you understand these, the rest of the layout follows.

### 1. Everything rides on `context.Context`

Five distinct things travel through ctx, each with an unexported key type in the package that owns it:

| Value           | Set by                 | Read by                     | Package                   |
| --------------- | ---------------------- | --------------------------- | ------------------------- |
| Request ID      | `middleware.RequestID` | logger, response envelope   | `pkg/httputil`            |
| Scoped logger   | `middleware.Logging`   | any inner layer             | `pkg/logctx`              |
| Language        | `middleware.Locale`    | `httpwriter.WriteError`     | `pkg/locale`              |
| Principal       | `middleware.Auth`      | handlers                    | `domain/identity`         |
| `AppError` slot | `middleware.Logging`   | `middleware.Logging`, after | `pkg/errors`              |
| DB transaction  | `TxManager.WithinTx`   | `qx.Q(ctx)`                 | `adapter/repository/{db}` |

Two rules make this safe rather than a global-variable bag:

- **The key type is always an unexported `struct{}`** (`type langCtxKey struct{}`), so no other package can collide with it or read the value without going through the accessor.
- **The accessors live with the *value*, not with the setter.** `pkg/locale` owns `WithLanguage`/`LanguageFromContext` even though only middleware calls the setter — that's what lets `pkg/httputil` read the language without importing a transport package.

The one exception is `pkg/errors`'s `appErrSlot`, which is a *mutable* pointer in ctx. It exists because a value must flow back *up* through an immutable context ([10](10-shared-packages.md#the-apperror-context-stash)); it is the only place the codebase does this, deliberately.

**Fiber caveat:** the `context.Context` is `c.UserContext()`, not `c.Context()`. Every handler and middleware uses `SetUserContext`/`UserContext` — using the fasthttp context silently drops all of the above ([07](07-transport-layer.md#handlergo)).

### 2. Log once, at the boundary

Inner layers **wrap and return**; they do not log. The transport boundary emits exactly one line.

```bash
repository            usecase                 transport
─────────────────     ─────────────────       ──────────────────────────
errors.Wrapf(err,     errors.Wrapf(err,       httpwriter.WriteError()
  "user repo:           "register user:         → StashAppError(ctx, app)
   select id=1")         lookup")
      │                     │                 middleware.Logging (after c.Next())
      └─── frame ───────────┴── frame ──────▶   → AppErrorFromContext(ctx)
                                                → ONE line: status, duration_ms,
                                                  app_code, app_err, app_trace
```

Why this and not log-at-each-layer:

- **One line per failed request**, so error-rate alerting counts requests, not layers.
- **The frame trail replaces the stack**: `app_trace` shows `user/service.go:67 → v1/user/handler.go:36` — the path through *your* code, which is the part you read.
- **Correlation is free**: `request_id` and `trace_id` are on the scoped logger, so every line the request did emit joins up, and `requestId` in the response body lets a user hand you the key.

The three deliberate exceptions are all "the error is being swallowed, so it must be logged here": the best-effort event publish ([06](06-usecase-layer.md)), cache read/refill failures ([08](08-adapter-layer.md)), and panic recovery. A swallowed error that isn't logged is invisible.

For transports with no access-log middleware (the worker), `errors.LogFields(err)` emits the same field names so one log query works across both ([10](10-shared-packages.md#logfields--for-transports-without-middleware)).

### 3. Classify at the origin, translate at the edge

```bash
usecase                      pkg/errors                  transport
──────────────────────       ──────────────────────      ─────────────────────────
errors.L(locale                localeHTTPStatus[code]     locale.LanguageFromContext
  .UserEmailExists,     ──▶    → 409                ──▶   → LangVi
  input.Email)                 AppError{Code: 409,        Translate(vi, code, args…)
                                 Locale: -1101,           → "Email … đã tồn tại"
                                 Args: [email]}
```

- The **status** is decided where the error is created, from one table ([10](10-shared-packages.md#localehttpstatus--the-single-source-of-truth)) — so `Wrapf` at ten layers can't change a 409 into a 500.
- The **language** is decided at the boundary, so business code never names one.
- The **args** travel raw, so the same error renders in any language.

Because the status table lives in `pkg/errors` (framework-free), a gRPC or worker transport reuses the classification without a second mapping.

### 4. Each component owns its shutdown

Every infrastructure constructor returns `(instance, cleanup, error)`; DI composes the cleanups and runs them in reverse construction order ([11](11-di-and-entrypoints.md#how-cleanup-works)). No central teardown function, no `Close()` on a domain port.

The consequence to internalize: **a constructor that acquires a resource and doesn't return a cleanup leaks it, and nothing will tell you.** That's the cost of the pattern; the benefit is that adding a dependency can't break shutdown ordering.

## Security posture

Collected here because it's spread across layers by design:

| Concern                  | Measure                                                                 |
| ------------------------ | ----------------------------------------------------------------------- |
| Password storage         | bcrypt, cost from config, clamped to a valid range                      |
| Credential leakage       | `PasswordHash` never in a response DTO, never cached, never indexed     |
| Account enumeration      | Uniform 401 **and** uniform timing on login (dummy bcrypt verify)       |
| Token forgery            | RS256 — only the issuer holds the signing key                           |
| Token failure detail     | Generic 401 to the client; the reason is logged, triaged by severity    |
| Brute force              | Per-IP token bucket on `/login` only                                    |
| CORS                     | Explicit origin list; empty (reject-all) default, never `*`             |
| Validation error detail  | Generic localized 400; the field-level cause goes to the log            |
| Secrets in logs          | `c.Path()` not `OriginalURL()` — query strings never logged             |
| Secrets in git           | `.env` gitignored; JWT keys are env-only (no YAML tag)                  |
| SQL injection            | sqlc-generated prepared statements; optional filters as bound params    |
| DSN corruption           | `net/url` / `mysql.Config.FormatDSN`, never `fmt.Sprintf`               |
| Container privilege      | Non-root `USER app`, static binary                                      |

Each of these is documented at its source: [06](06-usecase-layer.md#login--uniform-failure-uniform-timing), [07](07-transport-layer.md#internaltransporthttpmiddleware), [08](08-adapter-layer.md#queries), [09](09-infrastructure-layer.md#jwt--rs256-signer-and-verifier), [12](12-build-and-deploy.md#dockerfile).

## Observability at a glance

```bash
Request
  │  X-Request-ID (or generated)          ─────▶ ctx + response header + body.requestId
  │  traceparent                          ─────▶ OTel span context → trace_id
  ▼
scoped logger = base.With(request_id, trace_id, method, path)
  │
  ├── inner layers: logctx.From(ctx).Warn(…)          ← correlated, no plumbing
  │
  ▼
one access line: status, duration_ms, client_ip, user_agent,
                 [app_code, app_locale, app_err, app_trace]
```

Three properties this buys:

- **Log → trace in one click**: `trace_id` is on every line, so a log search jumps to Jaeger/Tempo.
- **User report → full replay**: the `requestId` in the response body is the log search key.
- **Signal, not volume**: probes and `OPTIONS` emit no line; a plain validation 400 omits `app_err`/`app_trace`, so client-driven noise doesn't move error-rate alerts.

Spans are opened by hand in usecases/repositories where they're worth it:

```go
ctx, span := tracer.Start(ctx, "UserUsecase.Register")
defer span.End()
if err != nil {
    span.RecordError(err)
    span.SetStatus(codes.Error, err.Error())
    return err
}
```

`tracing.endpoint: ""` yields a no-op tracer that still propagates inbound trace headers, so running one hop without a collector doesn't break a distributed trace.

## What's deliberately absent

Being explicit about the gaps, so nobody assumes coverage that isn't there:

- **No `/metrics`.** Add `prometheus/client_golang` and a route; the port-vs-impl split for it would mirror `pkg/observability.Logger`.
- **No general rate limiter.** Only `/login` is throttled, and only per-process. Global shedding belongs upstream (CDN, ingress) where it can drop traffic before it costs you a goroutine.
- **No outbox.** Event publishing is best-effort-with-logging. If an event must not be lost, write it to a table in the same transaction as the aggregate and relay it separately.
- **No refresh tokens.** `AccessTokenTTL` only; `AccessTokenClaims` has a `SessionID` field to build on.
- **No authorization.** `middleware.Auth` establishes *who* the caller is; deciding *what* they may do is a domain concern with no scaffold.
- **gRPC is a stub.** Server, DI and entry point are real; the service implementation is yours ([07](07-transport-layer.md#internaltransportgrpc)).
- **`transport/cronjob/` and `cmd/cron.go` are not rendered.** The `cron.go.tmpl` template exists but is not wired into `buildFileList`.
- **Not scaffolded, documented as patterns**: `domain/valueobject/`, `domain/service/`, `domain/event/` as a package, `adapter/external/`.

---

All templates are embedded with `//go:embed all:templates` and rendered through `text/template`. See [../README.md](../README.md) for CLI usage and the flag reference, and [01](01-cli-options.md) for the prompt flow.
