# 7. Transport layer

Inbound delivery — HTTP, gRPC, worker. Converts wire formats into usecase calls and back. A sibling of `adapter/`, not a child ([04](04-placement-rationale.md#why-transport-is-a-sibling-of-adapter-not-inside-it)). ([index](README.md))

Transports are **mutually exclusive** in a generated project: one `--transport` flag, one of `transport/http/`, `transport/grpc/`, `transport/worker/`.

```bash
transport/
├── http/
│   ├── router.go        # framework instance + middleware order + Registrar iface
│   ├── health/          # /healthz + /readyz checker (framework-free)
│   ├── httpwriter/      # framework-bound bind + envelope writer
│   ├── middleware/      # framework-specific handlers
│   └── v1/
│       ├── v1.go        # the shared /api/v1 prefix
│       └── user/        # one self-contained package per feature
├── grpc/                # interceptor/ + service impls
└── worker/              # Worker orchestrator + Consumer port + v1/{feature}/
```

## internal/transport/http/router.go

Rendered from the `{framework}_router.go.tmpl` variant. It builds the framework instance and nothing else — port binding and graceful shutdown belong to `infrastructure/server` ([09](09-infrastructure-layer.md)).

```go
// Registrar attaches a feature's routes to a router subtree. The version
// package (transport/http/v1) provides a concrete Registrar implementing
// this interface. Prefix is mounted as a subgroup before Register is called.
// Multiple Registrars may share a Prefix; the slice order in NewRouter
// determines mount order.
type Registrar interface {
    Prefix() string
    Register(fiber.Router)
}

func NewRouter(
    cfg *config.Config,
    log observability.Logger,
    checker *health.Checker,
    registrars []Registrar,
) *fiber.App {
    app := fiber.New(fiber.Config{
        BodyLimit:         cfg.HTTP.BodyLimit,
        ReadBufferSize:    cfg.HTTP.ReadBufferSize,
        ReadTimeout:       cfg.HTTP.ReadTimeout,
        WriteTimeout:      cfg.HTTP.WriteTimeout,
        ProxyHeader:       fiber.HeaderXForwardedFor,
        EnablePrintRoutes: cfg.AppEnv == config.EnvLocal,
    })

    app.Use(middleware.RequestID())
    app.Use(middleware.Logging(log))
    app.Use(middleware.Recovery())
    app.Use(middleware.CORS(cfg.HTTP.CORS))
    app.Use(middleware.Locale())

    app.Get("/healthz", …)   // checker.Liveness()
    app.Get("/readyz",  …)   // checker.Readiness(ctx) → 200 or 503

    for _, r := range registrars {
        r.Register(app.Group(r.Prefix()))
    }
    return app
}
```

**Middleware order is load-bearing**, not alphabetical:

| Position | Middleware  | Why here                                                             |
| -------- | ----------- | -------------------------------------------------------------------- |
| 1        | `RequestID` | Every later layer (including the logger) must see the ID             |
| 2        | `Logging`   | Wraps everything below to measure duration and emit **one** log line |
| 3        | `Recovery`  | Inside `Logging`, so a panic still produces an access log            |
| 4        | `CORS`      | After the observability layers, before app logic                     |
| 5        | `Locale`    | Populates ctx language for `httpwriter.WriteError` to read back      |

`registrars` is a **slice, not a map**, so mount order is deterministic — map iteration order would make route shadowing nondeterministic between restarts.

`/healthz` and `/readyz` are mounted _inside_ the middleware stack so a panic during a readiness check still returns the envelope; `Logging` skips emitting a line for them (see below).

## internal/transport/http/v1/v1.go — the shared prefix

The prefix is declared once per API section and shared by embedding, not restated per feature:

```go
// APIPrefix is the URL subtree every v1 feature mounts under.
const APIPrefix = "/api/v1"

// Prefixed supplies Prefix() to a feature Registrar via embedding, so each
// feature satisfies the transport/http.Registrar interface without restating
// the prefix. A new API section (e.g. an unversioned /admin, or /api/v2) is a
// sibling package with its own APIPrefix + Prefixed base.
type Prefixed struct{}

func (Prefixed) Prefix() string { return APIPrefix }
```

## internal/transport/http/v1/user/ — the per-feature package

Each feature is a **self-contained package** with four files:

```bash
v1/user/
├── dto.go         # json + validate tags (this layer owns the wire format)
├── assembler.go   # usecase DTO → response projection
├── handler.go     # endpoint logic
└── registrar.go   # route registration + auth boundary
```

**Why per-feature?** A flat `v1/` with `request.go`, `response.go`, `user_handler.go`, `order_handler.go` becomes unmanageable at 10+ features. Per-feature keeps everything for one endpoint group in one package, and deleting a feature is deleting a directory.

### dto.go

```go
type CreateUserRequest struct {
    Email    string `json:"email"    validate:"required,email"`
    Name     string `json:"name"     validate:"required,min=2,max=100"`
    Password string `json:"password" validate:"required,min=8"`
}

// ListQuery is the pagination input for GET /api/v1/users (admin/page-based
// form), bound from the query string. Page is 1-based. It carries both `query:`
// (fiber, echo) and `form:` (gin) tags so this one shared DTO works with every
// framework's native query binder. There is no min/max here on purpose —
// page/perPage bounds are the usecase's policy (so gRPC/CLI inherit the same
// caps); this layer only parses.
type ListQuery struct {
    Email   string `query:"email"   form:"email"   validate:"omitempty,email"`
    Page    int    `query:"page"    form:"page"`
    PerPage int    `query:"perPage" form:"perPage"`
}
```

Two rules visible in that comment block:

- **One DTO, every framework.** `dto.go` and `assembler.go` are the only files in the feature package with no framework variant — the dual `query:`/`form:` tags are what buy that.
- **Validation here is shape, not policy.** Formats and enums (`email`, `min=8` on a password) belong to the wire contract. Magnitude (`per_page` caps) is usecase policy, so gRPC and CLI callers inherit the same limits ([06](06-usecase-layer.md#list--page-based-policy-clamped-in-the-usecase)).

`LoginResponse` follows the RFC 6749 token-endpoint shape (`accessToken`, `tokenType`, `expiresIn`) so OAuth-aware clients consume it directly. `PublicUserResponse` is email + name only — the public list is unauthenticated.

### assembler.go — the only place that maps

Six exported projections, each one direction only (usecase → wire):

| Function                   | From                        | To                      |
| -------------------------- | --------------------------- | ----------------------- |
| `ToUserResponse`           | `*usersvc.Output`           | `*UserResponse`         |
| `ToUserListResponse`       | `*usersvc.ListOutput`       | `[]*UserResponse`       |
| `ToUserListMeta`           | `*usersvc.ListOutput`       | `httputil.PageMeta`     |
| `ToPublicUserListResponse` | `*usersvc.PublicListOutput` | `[]*PublicUserResponse` |
| `ToPublicUserListMeta`     | `*usersvc.PublicListOutput` | `httputil.CursorMeta`   |
| `ToLoginResponse`          | `*usersvc.LoginOutput`      | `*LoginResponse`        |

Data and meta are projected by **separate** functions because the envelope keeps them in separate fields, and the two list shapes produce different meta types:

```go
// ToUserListMeta projects a use-case ListOutput's pagination into the admin
// page meta (page + perPage + totalPage + totalRecord) — the envelope's `meta`.
// NewPageMeta derives totalPage from totalRecord/perPage.
func ToUserListMeta(out *usersvc.ListOutput) httputil.PageMeta {
    return httputil.NewPageMeta(out.Page, out.PerPage, out.TotalRecord)
}

// ToPublicUserListMeta projects a public list's cursor seam into the user-facing
// cursor meta (hasMore + nextCursor) — the envelope's `meta`.
func ToPublicUserListMeta(out *usersvc.PublicListOutput) httputil.CursorMeta {
    return httputil.CursorMeta{HasMore: out.HasMore, NextCursor: out.NextCursor}
}
```

`tokenType` is hardcoded to `"Bearer"` in `ToLoginResponse` — a wire-format constant, so it belongs here rather than in the usecase's `LoginOutput`.

There is no request→input assembler function: the handler builds `usersvc.RegisterInput{…}` inline. Two-field mappings do not earn a named function.

### handler.go

```go
// Handler is the HTTP boundary for user operations. It never logs — the
// Logging middleware reads the AppError back from ctx via the WriteError
// stash and emits one access log per request. Request binding and path-param
// parsing are shared via httpwriter.Bind / httpwriter.ParseID.
type Handler struct {
    userService *usersvc.Service
    validate    *validator.Validate
}
```

Every method is the same five lines:

```go
func (h *Handler) Create(c *fiber.Ctx) error {
    req, appErr := httpwriter.Bind[CreateUserRequest](c, h.validate)
    if appErr != nil {
        return httpwriter.WriteError(c, appErr)
    }
    user, err := h.userService.Register(c.UserContext(), usersvc.RegisterInput{
        Email: req.Email, Name: req.Name, Password: req.Password,
    })
    if err != nil {
        return httpwriter.WriteError(c, errors.ToAppError(err))
    }
    return httpwriter.WriteJSON(c, fiber.StatusCreated, ToUserResponse(user))
}
```

Four invariants hold across all seven handlers (`Create`, `Login`, `GetByID`, `Update`, `Delete`, `List`, `PublicList`):

1. **No `if err != nil { log… }`.** The handler never logs. `WriteError` stashes the `AppError` in ctx and the `Logging` middleware picks it up — one line per request, not one per layer.
2. **`c.UserContext()`, never `c.Context()`.** Fiber's `c.Context()` is the fasthttp request context; the `context.Context` carrying request ID, logger, locale and principal is `UserContext()`. Passing the wrong one silently drops all of it.
3. **`errors.ToAppError(err)`** at the boundary — a usecase may return a wrapped non-`AppError` from a dependency, and this normalizes it (defaulting to a 500 `InternalError`).
4. **No status literals.** `fiber.StatusCreated`, and the error status comes from the `AppError`. `Delete` is the one exception to the envelope: `c.SendStatus(fiber.StatusNoContent)`, because 204 has no body to wrap.

### registrar.go — where the auth boundary lives

```go
type Registrar struct {
    v1.Prefixed                              // supplies Prefix() → /api/v1
    handler    *Handler
    tokenSvc   *jwtsec.TokenService
    loginLimit config.LoginRateLimitConfig
}

func (r *Registrar) Register(router fiber.Router) {
    router.Post("/login", middleware.LoginRateLimit(r.loginLimit), r.handler.Login)

    public := router.Group("/users")
    public.Post("/", r.handler.Create)

    pub := router.Group("/public")
    pub.Get("/users", r.handler.PublicList)

    protected := router.Group("/users", middleware.Auth(r.tokenSvc))
    protected.Get("/", r.handler.List)
    protected.Get("/:id", r.handler.GetByID)
    protected.Put("/:id", r.handler.Update)
    protected.Delete("/:id", r.handler.Delete)
}
```

**Auth is applied at the group, not per route.** One `Auth` middleware instance for the whole protected subtree makes the boundary visible in four lines of code, and a new protected route cannot forget it. The `loginLimit` config and `tokenSvc` arrive through the constructor rather than a package global, so the registrar is testable and the rate limiter's state is per-instance.

Shipped route table:

| Method   | Path                   | Auth | Notes                      |
| -------- | ---------------------- | ---- | -------------------------- |
| `POST`   | `/api/v1/login`        | —    | Per-IP rate limited        |
| `POST`   | `/api/v1/users`        | —    | Registration               |
| `GET`    | `/api/v1/public/users` | —    | Cursor-paginated directory |
| `GET`    | `/api/v1/users`        | ✔    | Page-based admin list      |
| `GET`    | `/api/v1/users/:id`    | ✔    |                            |
| `PUT`    | `/api/v1/users/:id`    | ✔    |                            |
| `DELETE` | `/api/v1/users/:id`    | ✔    | 204, no envelope           |

## internal/transport/http/httpwriter/writer.go

The framework-bound half of the response story. The envelope itself is framework-free in `pkg/httputil` ([10](10-shared-packages.md)); this file binds requests and serializes that envelope through the chosen framework. [04](04-placement-rationale.md#why-httpwriter-is-under-transporthttp-not-pkg) explains why it can't live in `pkg/`.

**Writers** — all three stamp the request ID onto the envelope from ctx:

```go
func WriteJSON(c *fiber.Ctx, status int, data any) error
func WriteListJSON(c *fiber.Ctx, status int, data, meta any) error
func WriteError(c *fiber.Ctx, app *errors.AppError) error
```

```go
// WriteError writes an error JSON response wrapped in the standard envelope.
// The HTTP status is taken from app.Code. The message is translated using the
// language carried in the request context (set by the Locale middleware) when
// app carries a Locale code; otherwise app.Message is used as-is.
//
// Also stashes app into the request ctx so the Logging middleware can include
// app_code/app_locale fields in the access log without a second mapping pass.
func WriteError(c *fiber.Ctx, app *errors.AppError) error {
    ctx := c.UserContext()
    errors.StashAppError(ctx, app)
    lang := locale.LanguageFromContext(ctx)
    resp := httputil.ErrorResponseFromApp(app, lang)
    resp.RequestID = httputil.RequestIDFrom(ctx)
    return c.Status(app.Code).JSON(resp)
}
```

The stash is the mechanism behind "handlers never log": `WriteError` is the single funnel every error passes through, so it is the one place that can hand the error to the logging middleware.

**Binders** — `Bind[T]` (body), `BindQuery[T]` (query string), `ParseID` (path param). All three collapse every failure to the same generic localized 400:

```go
// A parse failure and a validation failure both map to the same generic
// localized 400 — clients never see binder internals or field names — while the
// underlying cause rides along via WithCause so it surfaces in the access log's
// app_err/app_trace, traceable by request_id.
func Bind[T any](c *fiber.Ctx, validate *validator.Validate) (T, *errors.AppError) {
    var v T
    if err := c.BodyParser(&v); err != nil {
        return v, errors.L(locale.InvalidRequest).WithCause(err)
    }
    if err := validate.Struct(&v); err != nil {
        return v, errors.L(locale.InvalidRequest).WithCause(err)
    }
    return v, nil
}
```

Uniform messages mean a client can't probe struct field names or binder internals, and `WithCause` means the operator still sees exactly which field failed, correlated by request ID. If you want per-field validation errors in the response, that is a deliberate change to `ErrorResponseFromApp` — not something to bolt on here.

## internal/transport/http/middleware/

Seven middlewares, each rendered from a `{framework}_{name}.go.tmpl` variant.

### requestid.go

Honours an inbound `X-Request-ID`, generates a UUID otherwise, echoes it in the response header, and puts it in ctx via `httputil.WithRequestID` so the logger and the envelope both find it.

### logging.go — one line per request

Builds the request-scoped logger, installs the `AppError` stash slot, and emits exactly one access log at the end.

```go
scoped := base.
    With("request_id", reqID).
    With("trace_id", traceIDOr(sc)).
    With("method", c.Method()).
    With("path", c.Path())
ctx = logctx.With(ctx, scoped)
ctx = errors.PrepareAppErrorContext(ctx)
```

Four decisions in this file are worth keeping when you extend it:

- **`c.Path()`, never `c.OriginalURL()`.** `Path()` excludes the query string, so a secret in `?token=…` never reaches structured logs. There is a comment saying so; don't "improve" it.
- **`client_ip`/`user_agent` are on the access line only**, not on the scoped logger. They're entry-point facts; repeating them on every inner log line is pure volume.
- **`/healthz`, `/readyz` and `OPTIONS` emit no line.** Kubernetes probes every few seconds and preflights carry no app signal — both would drown real traffic. They still run through `RequestID` and `Recovery`, so the safety net is intact; only the log line is suppressed.
- **Level follows status**: `Info` < 400, `Warn` 4xx, `Error` 5xx or a returned error.

When the stash holds an `AppError` the line gains `app_code` (+ `app_locale`), and gains `app_err`/`app_trace` only when there is something to diagnose — a 5xx, or any error carrying an inner cause. A plain validation 400 stays lean, so client-driven noise doesn't move your error-rate alerts.

### recovery.go

Logs the panic with `debug.Stack()` through the request-scoped logger and returns `errors.L(locale.InternalError)` in the envelope. The framework variants genuinely differ here:

```go
// Fiber buffers the response via fasthttp until the handler chain returns;
// the double-write hazard that chi/gin/echo guard against does not apply
// here. Do NOT add a wroteHeader flag — fasthttp will silently overwrite
// the buffered partial response with our 500 envelope, which is the
// correct behaviour for this middleware.
```

The `net/http`-based variants (chi, gin, echo) _do_ track whether headers were written, because writing a 500 after a partial response corrupts it there.

### cors.go

```go
// CORS reads allowed origins/methods/headers from config — never default to
// "*" in scaffolded code; each deployment enumerates origins explicitly.
```

### locale.go

Parses `Accept-Language`, keeps the primary tag's language subtag, and stores it in ctx. It knows nothing about translation — it delegates entirely to `pkg/locale`:

```go
func Locale() fiber.Handler {
    return func(c *fiber.Ctx) error {
        lang := parseLanguage(c.Get("Accept-Language"))
        c.SetUserContext(locale.WithLanguage(c.UserContext(), lang))
        return c.Next()
    }
}
```

`parseLanguage` splits on `,` then `-` (so `vi-VN,en;q=0.9` → `vi`) and falls back to `locale.LangEn` for anything unrecognised. Unknown languages degrade to English rather than erroring — a bad `Accept-Language` header is not a client error.

### auth.go

Extracts the bearer token, verifies it, and attaches the principal:

```go
func Auth(tokenSvc *jwtsec.TokenService) fiber.Handler {
    return func(c *fiber.Ctx) error {
        token := bearerToken(c.Get("Authorization"))
        if token == "" {
            return httpwriter.WriteError(c, errors.L(locale.Unauthorized).
                WithCause(errors.New("missing bearer token")))
        }
        principal, err := tokenSvc.ParseAndVerifyAccessToken(c.UserContext(), token)
        if err != nil {
            return httpwriter.WriteError(c, errors.ToAppError(err))
        }
        c.SetUserContext(identity.WithUserPrincipal(c.UserContext(), principal))
        return c.Next()
    }
}
```

Handlers read the caller via `identity.UserPrincipalFromContext(ctx)` — they never see a token, and the usecase layer never learns that JWT exists.

### loginlimit.go

A per-source-IP token bucket (`golang.org/x/time/rate`) defaulting to 5/minute with burst 5, and returning `errors.L(locale.TooManyRequests)`. Two honest caveats live in the code:

```go
// ipLimiter holds a per-source-IP token bucket. The visitors map grows with
// distinct client IPs — acceptable for typical low-cardinality login traffic.
// Swap for an LRU/expiring cache if /login attracts traffic from many IPs
// (CDN egress, bot farms).
```

```go
// LoginRateLimit returns a per-IP token-bucket limiter intended for the
// /login route. Apply only to that route — global rate limiting belongs
// upstream (CDN, reverse proxy) where it can shed traffic before reaching
// the app.
```

The limiter is in-process, so N replicas means N× the limit. It raises the cost of credential stuffing; it is not a substitute for an edge WAF.

## The full locale flow

```bash
Request (Accept-Language: vi-VN)
    │
    ▼
┌──────────────────────────────────────────────────────────────┐
│ middleware.Locale()                                          │
│   • parseLanguage("vi-VN,…") → locale.LangVi                 │
│   • locale.WithLanguage(ctx, LangVi)                         │
└────────────────────────┬─────────────────────────────────────┘
                         ▼
┌──────────────────────────────────────────────────────────────┐
│ Handler — calls the usecase, on error:                       │
│   httpwriter.WriteError(c, errors.ToAppError(err))           │
└────────────────────────┬─────────────────────────────────────┘
                         ▼
┌──────────────────────────────────────────────────────────────┐
│ Usecase                                                      │
│   return errors.L(locale.UserNotFound)                       │
│   return errors.L(locale.UserEmailExists, input.Email)       │
└────────────────────────┬─────────────────────────────────────┘
                         │ (error bubbles up)
                         ▼
┌──────────────────────────────────────────────────────────────┐
│ httpwriter.WriteError()                                      │
│   1. errors.StashAppError(ctx, app)  → for the access log    │
│   2. locale.LanguageFromContext(ctx) → LangVi                │
│   3. httputil.ErrorResponseFromApp(app, LangVi)              │
│        translates app.Locale + app.LocaleArgs                │
│   4. c.Status(app.Code)   ← set by errors.L via              │
│                             localeHTTPStatus                 │
│                                                              │
│   HTTP 404                                                   │
│   {"success":false,                                          │
│    "error":{"code":-1100,"message":"Không tìm thấy…"},       │
│    "requestId":"…"}                                          │
└──────────────────────────────────────────────────────────────┘
```

The status is decided **when the error is created**, not at the boundary: `errors.L` looks the code up in `localeHTTPStatus` and stores it as `AppError.Code` ([10](10-shared-packages.md#pkgerrors)). That's what lets a gRPC or worker transport reuse the same classification without duplicating a status table.

**Where each piece lives:**

| Component                                   | Layer                | Why                                                       |
| ------------------------------------------- | -------------------- | --------------------------------------------------------- |
| `pkg/locale/` (codes, translations, ctx)    | **pkg/** (shared)    | Pure utility, no domain dependency                        |
| `pkg/errors/` (AppError, code→status table) | **pkg/** (shared)    | Framework-free, so every transport maps identically       |
| `pkg/httputil/response.go` (envelope)       | **pkg/** (shared)    | No framework, no `internal/` imports                      |
| `transport/http/middleware/locale.go`       | **transport/http**   | HTTP concern — reads headers, sets context                |
| `transport/http/httpwriter/writer.go`       | **transport/http**   | Serializes the envelope through the framework             |
| `errors.L(locale.UserNotFound)`             | **usecase** (caller) | Classify at the layer that knows what went wrong          |
| Domain entities/services                    | **domain**           | Never import locale — return plain errors or custom types |

> **Key rule**: the usecase layer returns `errors.L(locale.SomeCode, args…)`. It never imports a translation file and never learns the caller's language. The transport boundary translates.

## internal/transport/http/health/checker.go

Framework-free (no variant prefix), so all four framework routers share it.

```go
// Liveness is a no-op success — the process being able to answer the request
// is the signal. Use for orchestrator restart loops; do NOT couple it to DB
// or cache health (that's what Readiness is for).
func (*Checker) Liveness() Status { return Status{OK: true} }
```

`Readiness(ctx)` pings every configured dependency under a **1-second deadline** and returns `OK=false` if any fails, with a per-check `details` map so an operator sees _which_ dependency is down without log archaeology. The router maps `!OK` to 503.

The liveness/readiness split matters operationally: coupling liveness to the database means a database blip restarts every healthy pod, turning a degradation into an outage.

## internal/transport/grpc/

**Placeholder.** `--transport=grpc` emits a compiling stub, not a working service:

```go
// TODO: Implement gRPC handlers.
// This file is a placeholder. To use gRPC:
// 1. Define your .proto files in api/proto/
// 2. Generate Go code with protoc
// 3. Implement the generated service interfaces here
```

The server bootstrap, DI graph and entry point are real; only the service implementation is yours to write. `transport/grpc/interceptor/` is the gRPC counterpart to HTTP middleware — [04](04-placement-rationale.md#why-middleware-is-under-transporthttp-not-transport) explains why they can't share a package.

## internal/transport/worker/

The inbound transport for broker messages, deliberately shaped like `transport/http`:

```go
// Package worker is the inbound transport for messages received from a
// message broker. It mirrors transport/http in shape: a Worker owns the
// broker-specific consumer (Kafka group / RabbitMQ channel), routes each
// received message to a Handler keyed by topic, and exposes Start/Stop for
// lifecycle.
```

Two ports and one orchestrator:

```go
// Handler processes one event off the broker. Topic is the wire-level
// identifier (Kafka topic / RabbitMQ routing key). Handle returns nil to
// signal a successful ack; a non-nil error triggers a broker-specific
// nack/retry path.
type Handler interface {
    Topic() string
    Handle(ctx context.Context, payload []byte) error
}

// Subscribe blocks until ctx is cancelled or an unrecoverable error surfaces.
type Consumer interface {
    Subscribe(ctx context.Context, topics []string, dispatch DispatchFunc) error
    Close() error
}
```

`Worker` holds a topic-keyed handler map built from the injected `[]Handler`, derives `Topics()` from it, and `Start(ctx)` blocks on `consumer.Subscribe`. Adding a feature is: implement `Handler`, list it in `provideWorkerHandlers` ([11](11-di-and-entrypoints.md)). The consumer is selected at generation time — `kafka_consumer.go` (sarama consumer group) or `rabbitmq_consumer.go` — and the orchestrator never learns which.

Per-message dispatch attaches a topic-scoped logger to ctx, so the usecase and repository below emit correlated fields without any extra plumbing — the worker equivalent of the HTTP `Logging` middleware.

### The ack decision is the interesting part

`transport/worker/v1/user/handler.go` shows both outcomes:

```go
var msg UserCreatedMessage
if err := json.Unmarshal(payload, &msg); err != nil {
    // Poison message: malformed JSON cannot be retried. Log and ack so
    // it doesn't block the partition. Tune this to nack-to-DLQ in
    // production if your broker has one configured.
    log.Error("user.created: malformed payload", "err", err.Error())
    return nil
}
…
if err := h.audit.RecordUserCreated(ctx, input); err != nil {
    return errors.Wrapf(err, "handle user.created user_id=%d", msg.UserID)
}
```

**Malformed payload → ack** (returning `nil`), because retrying it will fail identically forever and, on Kafka, block the partition. **A failed downstream → nack** (returning the error), because that is transient. Getting this backwards is the classic worker outage: a poison message parked at the head of a partition.

The topic constant is duplicated on both sides on purpose, with a comment pointing at its twin:

```go
// userCreatedTopic is the wire-level event identifier this handler consumes.
// MUST match the publisher's topic constant (adapter/pubsub.userCreatedTopic).
const userCreatedTopic = "user.created"
```

A shared constant would couple the consumer's module to the producer's — fine in one repo, wrong the moment the worker is a separate service. See [08](08-adapter-layer.md) for the publisher side.

Kafka's ordering contract is documented where it can bite you:

```go
// Partition order is preserved within a single claim; cross-partition
// ordering is NOT — handlers must tolerate out-of-order delivery if your
// topic has more than one partition.
```

## internal/transport/cronjob/

**Not scaffolded.** Cron handlers are thin wrappers that call a usecase method. They belong in `transport/` because a schedule is a delivery mechanism, and they need no middleware.

```go
package cronjob

type ScanExpiredOrders struct {
    orderService *ordersvc.Service
}

func NewScanExpiredOrders(orderService *ordersvc.Service) *ScanExpiredOrders {
    return &ScanExpiredOrders{orderService: orderService}
}

func (s *ScanExpiredOrders) Run(ctx context.Context) {
    s.orderService.HandleExpiredOrders(ctx)
}
```

> A `templates/cmd/cron.go.tmpl` exists in the template tree but is not wired into `buildFileList`, so no project renders it today. Treat the snippet above as the intended shape.
