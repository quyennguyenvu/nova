# 7. Transport layer

Inbound delivery — HTTP, gRPC, worker, cron. Converts wire formats into usecase calls and back. A sibling of `adapter/`, not a child ([04](04-placement-rationale.md#why-transport-is-a-sibling-of-adapter-not-inside-it)). ([index](README.md))

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
├── grpc/                # server factory + rpcstatus/ + interceptor/ + service impls
└── worker/              # Worker orchestrator + Consumer port + v1/{feature}/
```

## internal/transport/http/router.go

Rendered from the `{framework}_router.go.tmpl` variant. It builds the framework instance and nothing else — port binding and graceful shutdown belong to `infrastructure/server` ([09](09-infrastructure-layer.md)).

```go
// Registrar attaches a feature's routes to a router subtree. Each feature
// package (transport/http/v1/<feature>) provides a concrete Registrar; its
// Prefix() (the embedded v1.Prefixed) is mounted as a subgroup before
// Register is called. Multiple Registrars may share a Prefix; the slice
// order in NewRouter determines mount order.
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
        ErrorHandler:      httpwriter.HandleError,          // framework errors answer in the envelope
        EnablePrintRoutes: cfg.AppEnv == config.EnvLocal,   // no proxy header: c.IP() is the TCP peer
    })

    trusted, _ := cfg.HTTP.TrustedProxyNets()
    app.Use(middleware.ClientIP(trusted))                 // X-Forwarded-For only from trusted proxies
    app.Use(otelfiber.Middleware(…))                      // server span; probes skipped
    app.Use(middleware.RequestID())
    app.Use(middleware.Logging(log))
    app.Use(middleware.Recovery())
    app.Use(middleware.Timeout(cfg.HTTP.RequestTimeout))  // deadline on every request ctx
    app.Use(middleware.CORS(cfg.HTTP.CORS))
    app.Use(middleware.Locale())

    app.Get("/healthz", …)   // checker.Liveness()
    app.Get("/readyz",  …)   // checker.Readiness(ctx) → 200 or 503; the error is logged, the body says "fail"

    for _, r := range registrars {
        r.Register(app.Group(r.Prefix()))
    }
    return app
}
```

**Middleware order is load-bearing**, not alphabetical:

| Position | Middleware  | Why here                                                                                   |
| -------- | ----------- | ------------------------------------------------------------------------------------------ |
| 0        | OTel        | Opens the server span so `RequestID` and `Logging` see `trace_id`; probes are skipped      |
| 1        | `RequestID` | Every later layer (including the logger) must see the ID                                   |
| 2        | `Logging`   | Wraps everything below to measure duration and emit **one** log line                       |
| 3        | `Recovery`  | Inside `Logging`, so a panic still produces an access log                                  |
| 4        | `Timeout`   | Inside `Recovery`, so a deadline panic is still caught; puts `http.request_timeout` on ctx |
| 5        | `BodyLimit` | gin, chi and echo only — fiber caps the body in `fiber.Config`; over the cap is 413        |
| 6        | `CORS`      | After the observability layers, before app logic                                           |
| 7        | `Locale`    | Populates ctx language for `httpwriter.WriteError` to read back                            |

fiber and chi install one more middleware before even the OTel one: `ClientIP`, which resolves the client address through `http.trusted_proxies` (the right-most `X-Forwarded-For` hop that is not itself a trusted proxy) so the access line and the login limiter read the same value; chi rewrites `RemoteAddr`, fiber stores a copied string read back with `middleware.RemoteIP`. fiber needs it because its own `c.IP()` returns the header verbatim, client-controlled left-most entry included, once any proxy is trusted. gin and echo resolve the same way natively (`SetTrustedProxies`, `IPExtractor`); with no trusted proxies every framework ignores `X-Forwarded-For`.

Framework-raised errors take one path per framework into the same envelope: fiber's `ErrorHandler` and echo's `HTTPErrorHandler` are `httpwriter.HandleError`, gin's `NoRoute`/`NoMethod` (with redirects off, so `/api/v1/users/` gets the envelope 404, not a redirect) and chi's `NotFound`/`MethodNotAllowed` call `httpwriter.WriteError` with `errors.FromStatus(code)`. A client never sees a framework's plain-text 404.

`registrars` is a **slice, not a map**, so mount order is deterministic — map iteration order would make route shadowing nondeterministic between restarts.

`/healthz` and `/readyz` are mounted _inside_ the middleware stack so a panic during a readiness check still returns the envelope; `Logging` skips emitting a line for them (see below). `GET /openapi.yaml` sits next to them and serves the document embedded by `api/openapi` (`openapi.Spec`), so the contract is fetched from the binary that implements it.

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
- **Validation here is shape, not policy.** Formats and enums (`email`, `min=8` on a password) belong to the wire contract. Magnitude (`perPage` caps) is usecase policy, so gRPC and CLI callers inherit the same limits ([06](06-usecase-layer.md#list--page-based-policy-clamped-in-the-usecase)).

`LoginResponse` carries the RFC 6749 token-endpoint fields in the API's camelCase (`accessToken`, `tokenType`, `expiresIn`), plus `user`. `PublicUserResponse` is the name only — the public list is unauthenticated, so it exposes no email or id.

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

There is no request→input assembler function: the handler builds `usersvc.RegisterInput{…}` inline. Small mappings do not earn a named function.

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
    public.Post("/", middleware.LoginRateLimit(r.loginLimit), r.handler.Create)

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

| Method   | Path                   | Auth | Notes                             |
| -------- | ---------------------- | ---- | --------------------------------- |
| `POST`   | `/api/v1/login`        | —    | Per-IP rate limited               |
| `POST`   | `/api/v1/users`        | —    | Registration, per-IP rate limited |
| `GET`    | `/api/v1/public/users` | —    | Cursor-paginated directory        |
| `GET`    | `/api/v1/users`        | ✔    | Page-based admin list             |
| `GET`    | `/api/v1/users/:id`    | ✔    |                                   |
| `PUT`    | `/api/v1/users/:id`    | ✔    | Own id only (403 otherwise)       |
| `DELETE` | `/api/v1/users/:id`    | ✔    | Own id only; 204, no envelope     |

## internal/transport/http/httpwriter/writer.go

The framework-bound half of the response story. The envelope itself is framework-free in `pkg/httputil` ([10](10-shared-packages.md)); this file binds requests and serializes that envelope through the chosen framework. [04](04-placement-rationale.md#why-httpwriter-is-under-transporthttp-not-pkg) explains why it can't live in `pkg/`.

**Writers** — all three stamp the request ID onto the envelope from ctx (chi's `WriteJSON`/`WriteListJSON` read the `X-Request-ID` response header instead):

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
    resp.RequestID = requestid.From(ctx)
    return c.Status(app.Code).JSON(resp)
}
```

The stash is the mechanism behind "handlers never log": `WriteError` is the single funnel every error passes through, so it is the one place that can hand the error to the logging middleware.

**Binders** — `Bind[T]` (body), `BindQuery[T]` (query string; chi has no struct binder, so its handlers use `QueryInt` + `ValidateQuery`), `ParseID` (path param). Every parse or validation failure becomes the same generic localized 400; on gin, chi and echo an over-limit body is 413:

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

Eight middlewares are rendered for every framework from `{framework}_{name}.go.tmpl` variants (`requestid`, `logging`, `recovery`, `timeout`, `cors`, `locale`, `auth`, `loginlimit`), plus `bodylimit` for gin, chi and echo (fiber caps the body in `fiber.Config`), `clientip` for fiber and chi (gin and echo resolve it natively), and the framework-free `principal.go`.

### requestid.go

Reuses an inbound `X-Request-ID` only when `requestid.Valid` accepts it (1–128 characters of `[A-Za-z0-9._-]`), generates a UUID otherwise, echoes it in the response header, and puts it in ctx via `requestid.With` so the logger and the envelope both find it. The header name is the only HTTP-specific part (`httputil.RequestIDHeader`); the gRPC interceptor and the worker set the same ctx value from their own carriers.

### logging.go — one line per request

Builds the request-scoped logger, installs the `AppError` stash slot and the principal slot (`principal.go`, filled by `Auth`), and emits exactly one access log at the end.

```go
scoped := base.
    With("request_id", reqID).
    With("trace_id", traceIDOr(sc)).
    With("method", c.Method()).
    With("path", c.Path())
ctx = logctx.With(ctx, scoped)
ctx = errors.PrepareAppErrorContext(ctx)
ctx = prepareAuthSlot(ctx)
```

Six decisions in this file are worth keeping when you extend it:

- **`c.Path()`, never `c.OriginalURL()`.** `Path()` excludes the query string, so a secret in `?token=…` never reaches structured logs. There is a comment saying so; don't "improve" it.
- **`client_ip`/`user_agent` are on the access line only**, not on the scoped logger. They're entry-point facts; repeating them on every inner log line is pure volume. `client_ip` is the address the framework resolved through the trusted-proxy list (chi: `RemoteAddr` after `ClientIP`), without a port.
- **`/healthz`, `/readyz` and `OPTIONS` emit no line.** Kubernetes probes every few seconds and preflights carry no app signal — both would drown real traffic. They still run through `RequestID` and `Recovery`, so the safety net is intact; only the log line is suppressed.
- **Level follows status**: `Info` < 400, `Warn` 4xx, `Error` 5xx. fiber and echo resolve a returned error inside the middleware (`c.App().ErrorHandler`, `c.Error`) before reading the status, so a router 404 logs as a 404 at `Warn` rather than a 200 at `Error`.
- **`route` and `path` are both on the access line.** `path` is the raw request path (also on the scoped logger); `route` is the matched pattern (`/api/v1/users/:id`; chi `/api/v1/users/{id}`) for low-cardinality dashboards.
- **`user_id` comes from `Auth` through a slot.** `Auth` runs inside `Logging` and derives a child context only inner handlers see, so the only way the access line can say who called is the mutable slot `Logging` installs and `Auth` fills (`rememberPrincipal`).

When the stash holds an `AppError` the line gains `app_code` (+ `app_locale`), and gains `app_err`/`app_trace` only when there is something to diagnose — a 5xx, or any error carrying an inner cause (binder 400s do, so the failing field is logged). A 4xx raised without a cause stays lean, and every 4xx logs at `Warn`, so client mistakes never reach 5xx alerts.

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
// CORS reads allowed origins/methods/headers from config; each deployment
// enumerates its origins explicitly. An empty origin list means no
// cross-origin access at all, so nothing is installed: fiber's cors
// middleware would otherwise treat the empty list as "*".
```

The empty-list guard is what keeps the `base.yaml` default honest: fiber and echo both read an empty origin list as `*`, so the middleware is simply not installed. gin and chi, whose CORS is hand-rolled, add `Vary: Origin` on every response so a shared cache never serves one origin's answer to another.

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
        token := bearerToken(c.Get(fiber.HeaderAuthorization))
        if token == "" {
            c.Set(fiber.HeaderWWWAuthenticate, challengeMissing)
            return httpwriter.WriteError(c, errors.L(locale.Unauthorized).
                WithCause(errors.New("missing bearer token")))
        }
        principal, err := tokenSvc.ParseAndVerifyAccessToken(c.UserContext(), token)
        if err != nil {
            app := errors.ToAppError(err)
            if app.Code == fiber.StatusUnauthorized {
                c.Set(fiber.HeaderWWWAuthenticate, challengeInvalid)
            }
            return httpwriter.WriteError(c, app)
        }
        rememberPrincipal(c.UserContext(), principal)
        c.SetUserContext(identity.WithUserPrincipal(c.UserContext(), principal))
        return c.Next()
    }
}
```

`bearerToken` compares the scheme case-insensitively (RFC 9110), and every 401 carries an RFC 6750 challenge: `Bearer` when the credential is missing, `Bearer error="invalid_token"` when it was rejected.

The usecase reads the caller via `identity.UserPrincipalFromContext(ctx)` (`requireSelf`); handlers never touch it, and the usecase never learns that JWT exists.

### loginlimit.go

A per-source-IP token bucket (`golang.org/x/time/rate`) defaulting to 5/minute with burst 5. A rejected request answers `429` with `errors.L(locale.TooManyRequests)` and a `Retry-After` header (whole seconds until the bucket refills). Buckets idle for ten minutes are evicted on the first call after each one-minute sweep, so the map is bounded by recent clients rather than by the life of the process. The registrar applies a separate instance to `POST /login` and `POST /users`, so registration cannot be used to probe or flood either. The client IP is whatever the framework resolved through the trusted-proxy list, never a raw header.

```go
// ipLimiter holds one token bucket per client IP. Buckets idle for longer
// than visitorTTL are dropped by the first call after each sweepEvery
// interval, so memory is bounded by the clients seen recently rather than
// over the life of the process. It is per-process: N replicas allow N× the
// limit; global rate limiting belongs upstream (CDN, reverse proxy).
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
│        translates app.Locale + app.Args                      │
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

Errors the framework raises itself take a parallel path: `errors.FromStatus(code)` picks the canonical locale code for a 404, 405 or 413 so the response uses the same envelope and translation, and an unmapped status becomes 500 rather than leaking the framework's own message. A request deadline that expires (`middleware.Timeout`) surfaces as `context.DeadlineExceeded`, which `classify` maps to 504.

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

`Readiness(ctx)` pings the database and Redis, when configured (search and broker clients are not probed), under one shared **1-second deadline** and returns `OK=false` if any fails, with a per-check `details` map so an operator sees _which_ dependency is down without log archaeology. The router maps `!OK` to 503.

The liveness/readiness split matters operationally: coupling liveness to the database means a database blip restarts every healthy pod, turning a degradation into an outage.

## internal/transport/grpc/

`--transport=grpc` renders a working service shaped like `transport/http`: a server factory in place of the router, interceptors in place of middleware, `rpcstatus` in place of `httpwriter`, and the user feature implemented against generated stubs.

```bash
transport/grpc/
├── server.go            # New(): interceptor chain + Registrars + health + reflection
├── user.go              # UserHandler — user.v1.UserService over the api/gen stubs
├── user_test.go         # end to end over bufconn: codes, auth, health, locale
├── rpcstatus/status.go  # FromError: AppError → gRPC status, stashed for the access log
└── interceptor/         # RequestID, Locale, Logging, Recovery, Deadline, Throttle, Auth
```

The contract is `api/proto/user/v1/user.proto` (package `user.v1`, one RPC per HTTP endpoint: `CreateUser`, `Login`, `GetUser`, `UpdateUser`, `DeleteUser`, `ListUsers`, `ListPublicUsers`). `make gen` runs `buf generate`, which writes `api/gen/user/v1` through version-pinned `go run` plugins ([12](12-build-and-deploy.md#makefile)); the output is committed like `dbgen`.

**`New` is the router.** `transport/grpc.New(cfg, log, tokenSvc, services)` builds the `*grpc.Server`, chains the unary interceptors in the HTTP middleware order, except that `Locale` runs second (`RequestID → Locale → Logging → Recovery → Deadline → Throttle → Auth`), registers every `Registrar`, then the standard `grpc.health.v1` service and, when `grpc.reflection` is on (base `true`, production `false`), server reflection so `grpcurl` needs no proto files. `infrastructure/server.NewGRPCServer` wraps it for the listener and the graceful stop, and flips health to NOT_SERVING as shutdown starts, immediately before the drain ([09](09-infrastructure-layer.md)); a balancer that polls health only sees it if the platform delays the stop signal (a Kubernetes `preStop` sleep). Health reports process liveness, not dependency readiness: there is no gRPC `/readyz`.

**A feature is a `Registrar`.** Three methods: `Register(*grpc.Server)` mounts the generated service, `Public() []string` names the full methods callable without a bearer token, and `Throttled() []string` names the credential methods limited per peer. Auth is deny-by-default: an RPC added to the proto needs a token until it is listed in `Public()`, which `TestEveryRPCIsProtectedUnlessPublic` checks against the generated service descriptor. `UserHandler` lists `CreateUser`, `Login` and `ListPublicUsers` as public, the same split as the HTTP routes, and throttles `CreateUser` and `Login` like the HTTP login and registration routes. DI collects the registrars in `provideGRPCServices` ([11](11-di-and-entrypoints.md)).

**Errors take one path.** A handler never builds a status itself: it returns `rpcstatus.FromError(ctx, err)`, which runs `errors.ToAppError`, stashes the `AppError` for the access log, translates the message into the call's language (`accept-language` metadata, read by the `Locale` interceptor) and maps the HTTP status `pkg/errors` assigned to the nearest gRPC code (400/415/422 → `InvalidArgument`, 401 → `Unauthenticated`, 403 → `PermissionDenied`, 404 → `NotFound`, 409 → `AlreadyExists`, 408/504 → `DeadlineExceeded`, 413/429 → `ResourceExhausted`, 405/501 → `Unimplemented`, 502/503 → `Unavailable`, anything else `Internal`). A wrapped internal error therefore reaches the client as the generic localized message, never its text. Wire-shape validation reuses the HTTP DTO rules through small tagged structs and the shared `*validator.Validate`; the entity still owns its invariants ([05](05-domain-layer.md)).

**Interceptors mirror middleware** ([04](04-placement-rationale.md#why-middleware-is-under-transporthttp-not-transport) explains why they cannot share a package). `RequestID` adopts a valid `x-request-id` or mints one and echoes it. `Locale` sets the language. `Logging` scopes the logger with `request_id`, `trace_id` and `method`, installs the `AppError` stash and emits one `rpc` line per call with `code` and `duration_ms`, using the stashed `AppError` (cause and frame trail) when there is one. `Recovery` turns a panic into `codes.Internal` with the localized internal-error message, through `rpcstatus` like any other failure. `Deadline` bounds every call by `grpc.request_timeout` (a sooner client deadline wins). `Throttle` applies a per-peer token bucket (`grpc.login_rate_limit`) to the throttled methods and answers `ResourceExhausted` with a `retry-after` header; it keys on the TCP peer, since gRPC has no `X-Forwarded-For` convention to trust. `Auth` verifies the bearer token from the `authorization` metadata on every method outside the public set (health `Check` is always public) and attaches the `identity.UserPrincipal`, so `requireSelf` in the usecase works unchanged. Only unary interceptors are installed; the first streaming RPC needs their stream counterparts.

`user_test.go` runs the whole stack over `bufconn` with a real RS256 `TokenService` and an in-memory repository: a duplicate email is `AlreadyExists`, a bad email `InvalidArgument`, a missing token `Unauthenticated`, another user's id `PermissionDenied`, and health flips to NOT_SERVING on `Shutdown`. nova's bootstrap runs it in `grpc_postgres_wire` and `grpc_none_fx`.

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
// identifier (Kafka topic / RabbitMQ routing key). Handle returns nil on
// success. A plain error is retried under the worker Policy, then parked;
// wrap input that can never succeed in Poison to park it at once.
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

`Worker` holds a topic-keyed handler map built from the injected `[]Handler`, derives `Topics()` from it, and `Start(ctx)` blocks on `consumer.Subscribe`. Adding a feature is: implement `Handler`, list it in `provideWorkerHandlers` ([11](11-di-and-entrypoints.md)). The consumer is selected at generation time — `consumer.go`, rendered from `kafka_consumer.go.tmpl` (sarama consumer group) or `rabbitmq_consumer.go.tmpl` — and the orchestrator never learns which.

Consumers hand `dispatch` a broker-neutral `Message` (topic, payload, key, correlation headers, Kafka partition/offset or RabbitMQ delivery tag/message id). `Worker.dispatch` adopts the producer's `traceparent` and `x-request-id` headers, scopes the logger with the topic, the position and those ids, runs the handler under the policy below, and emits one `worker: message handled|parked|abandoned` line with `attempts` and `duration_ms` (and `errors.LogFields` on failure) — the worker equivalent of the HTTP access log. Inner layers still log through `logctx.From(ctx)` with the same correlation fields; handlers do not log success themselves.

### One policy, three outcomes

Every message is handled under `worker.Policy`, projected from the `worker` config section by `provideWorkerPolicy` ([11](11-di-and-entrypoints.md)); the zero value takes the defaults:

```go
type Policy struct {
    HandlerTimeout time.Duration // per attempt; the handler's ctx expires at it (30s)
    MaxAttempts    int           // tries before the message is parked (3)
    RetryBackoff   time.Duration // wait before the second attempt, doubling after (500ms)
}
```

`Worker.run` gives each attempt its own deadline and panic guard (a panic becomes `ErrHandlerPanic` and counts as a failed attempt), retries a failed attempt after the backoff, and gives up in two cases: the handler returned `worker.Poison(err)`, or the budget is spent. Both come back wrapped in `ErrParked`. A ctx cancelled mid-message (shutdown) returns `ctx.Err()` instead, after at most the attempt in flight. So `dispatch` has exactly three outcomes (a topic with no handler is logged and acked before any of them), and the handler rule is short:

```go
if err := json.Unmarshal(payload, &msg); err != nil {
    // Malformed JSON can never succeed: park it on the dead-letter
    // topic/queue instead of retrying it or silently dropping it.
    return worker.Poison(errors.Wrap(err, "user.created: malformed payload"))
}
…
if err := h.audit.RecordUserCreated(ctx, input); err != nil {
    return errors.Wrapf(err, "handle user.created user_id=%d", msg.UserID)
}
```

**Input that can never succeed → `worker.Poison`**, so it is parked at once instead of retried forever or, worse, acked and lost. **Anything transient → a plain error**, so it is retried and then parked. Never swallow an error to "keep the partition moving": the policy does that, with the message preserved on the dead-letter side. Getting this wrong is the classic worker outage: a poison message at the head of a partition, or a bug silently dropping real events.

### Settling a delivery

The consumers map the three outcomes the same way; `consumer_test.go` pins it for each broker with a fake session or acknowledger, no broker needed:

| Outcome | Kafka (`settle`) | RabbitMQ (`handle`) |
| --- | --- | --- |
| handled (`nil`) | `MarkMessage` | `Ack` |
| parked (`ErrParked`) | copy to `<topic>.dlq` with `x-dead-letter-reason` and `x-dead-letter-source` headers, then `MarkMessage` | `Nack(requeue=false)`; the queue's `x-dead-letter-exchange` routes it to `<queue>.dlx`, whose queue is `<queue>.dlq` |
| abandoned (`ctx.Err()`) | left unmarked; the next session redelivers | `Nack(requeue=true)` |

`ConsumeClaim` therefore never returns because a handler failed — that used to end the session and rebalance the group on every poison message. It never returns because a dead-letter write failed either: `park` retries the copy with capped backoff (100 ms doubling to 30 s) until it lands or the session ends; only then does the message stay unmarked and come back. The RabbitMQ consumer declares the whole topology on start (exchange, dead-letter exchange and queue, work queue), sets `Qos(prefetch)` so a slow handler does not hold the entire backlog unacked, and watches `NotifyClose`. Both `Subscribe` loops return `ctx.Err()` on shutdown and `ErrConsumerClosed` when the broker side ends the subscription while ctx is live, which `RunWorker` turns into a non-zero exit ([11](11-di-and-entrypoints.md)) — a worker that lost its subscription must not idle as a zombie.

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
