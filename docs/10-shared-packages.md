# 10. Shared packages (pkg/)

Cross-cutting utilities with **no layer affiliation**. Nothing here imports `internal/`, and
nothing here imports a framework. ([index](README.md))

```bash
pkg/
├── errors/           # AppError + caller trace + locale→status table
├── locale/           # error codes, translations, language ctx
├── httputil/         # framework-free response envelope + request-ID ctx
├── logctx/           # request-scoped logger through context
└── observability/     # Logger port
```

That "no `internal/`, no framework" rule is what makes `pkg/` legitimate rather than a dumping
ground — see [04](04-placement-rationale.md#pkg-vs-internalshared--do-you-actually-need-pkg) for
whether you want `internal/shared/` instead.

## pkg/errors

The design brief, from the package doc:

```go
// Package errors provides HTTP-focused application errors with a lightweight
// caller-info trace, designed for one-line logging at the transport boundary.
//
// Each Wrap captures the call site (file:line, func, optional context message).
// Frames accumulate innermost-first as the error travels up the layers, so the
// outermost layer can log a single line that points to where the error came
// from and the path it took to get out — without dumping a full stack.
```

Three deliberate non-goals: **no stack dumps** (a wrap trail is cheaper and more readable), **no
status codes passed to `Wrap`** (classification happens where the error originates), and **no
language in business code** (translation happens at the boundary).

### The type

```go
type AppError struct {
    Code    int           `json:"code"`
    Message string        `json:"message"`
    Locale  locale.Locale `json:"locale,omitempty"`
    Args    []any         `json:"-"`
    Err     error         `json:"-"`
    frames  []Frame
}
```

`Args` and `Err` are `json:"-"` — arguments may contain user data and the cause is diagnostic.
The client sees a translated message; the operator sees the cause in the log.

### Sentinels and the re-exports

```go
var (
    ErrNotFound      = stderrors.New("not found")
    ErrAlreadyExists = stderrors.New("already exists")
    ErrInvalid       = stderrors.New("invalid")
    ErrUnauthorized  = stderrors.New("unauthorized")
    ErrForbidden     = stderrors.New("forbidden")
    ErrConflict      = stderrors.New("conflict")
    ErrInternal      = stderrors.New("internal")
)

// Re-export std helpers so callers only import this package.
var (
    New = stderrors.New; Is = stderrors.Is; As = stderrors.As
    Unwrap = stderrors.Unwrap; Join = stderrors.Join
)
```

The re-exports mean a file needs one `errors` import, not two — which is why the repository files
alias the standard library as `stderrors` when they need it for a driver sentinel
([08](08-adapter-layer.md#user_repositorygo)).

Sentinels are the adapter→usecase vocabulary: the repository maps `pgx.ErrNoRows` to
`ErrNotFound`, and the usecase checks `errors.Is(err, errors.ErrNotFound)` without knowing the
driver.

### Two constructors, two intents

```go
// usecase — a rule the caller violated
return errors.L(locale.UserEmailExists, input.Email).
    WithCause(errors.New("email already registered"))

// anywhere — a dependency failed
return errors.Wrapf(err, "register user email=%s: lookup", input.Email)
```

`L(code, args…)` derives the HTTP status from the code (below) and captures one frame.
`Wrap`/`Wrapf` append a frame and **preserve the existing classification**:

```go
// If err is already an *AppError, the frame is appended and Code/Message/
// Locale/Args are preserved so the originating layer's classification stays
// authoritative.
```

That is the rule the whole design rests on: **classify once, at the layer that knows what went
wrong.** Twelve layers of `Wrapf` on a 404 still produce a 404. And `Wrap(nil, …)` returns nil, so
`return errors.Wrap(err, "…")` is safe unconditionally.

`WithCause` attaches a diagnostic cause **without** changing the classification:

```go
// WithCause attaches an inner diagnostic cause to e without altering its
// client-facing classification (Code/Locale/Message). The cause is never
// rendered to the client — it surfaces only in the access log's
// app_err/app_trace, so an otherwise-opaque 4xx (a body-parse or validation
// failure mapped to a generic locale message) stays traceable by request_id.
```

This is what lets `httpwriter.Bind` return an identical generic 400 to every client while the
operator still sees which field failed ([07](07-transport-layer.md#internaltransporthttphttpwriterwritergo)).

### The frame trail

```go
// Trace returns a compact one-line trail "file:line(msg) → file:line(msg)"
// suitable for a single structured log field at the transport boundary.
```

`Frames()` (innermost first), `Origin()` (the innermost frame), `Trace()` (the joined line).
`shortFile` renders `user/service.go` rather than an absolute build path — deliberately matching
the logger's `caller` format, so a trace entry and a log caller field read the same way.

A frame is `runtime.Caller` only — no `runtime.Callers` walk, no symbolization of a full stack.
That's the cost/benefit trade: you get the path the error took through *your* layers, which is the
part you actually read.

### localeHTTPStatus — the single source of truth

```go
// localeHTTPStatus is the single source of truth mapping every HTTP-facing
// locale code to its response status. Both error-construction paths read it via
// httpStatusForLocale: L() (the usecase-facing constructor) and classify() (the
// bare locale.Locale path that reaches the transport boundary). Holding the
// mapping in one table — instead of duplicating it across per-status
// constructors and a parallel switch — means a code has exactly one status, so
// the two paths cannot drift. A code absent from the table resolves to 500; add
// new codes here as you introduce them.
```

| Status | Locale codes                                        |
| ------ | --------------------------------------------------- |
| 400    | `InvalidRequest`                                    |
| 401    | `Unauthorized`, `InvalidCredentials`                |
| 403    | `Forbidden`                                         |
| 404    | `RecordNotFound`, `UserNotFound`                    |
| 405    | `MethodNotAllowed`                                  |
| 408    | `RequestTimeout`                                    |
| 409    | `UserEmailExists`, `Conflict`                       |
| 413    | `PayloadTooLarge`                                   |
| 415    | `UnsupportedMediaType`                              |
| 422    | `ValidationFailed`                                  |
| 429    | `TooManyRequests`                                   |
| 500    | `InternalError`                                     |
| 501    | `Unimplemented`                                     |
| 502    | `BadGateway`                                        |
| 503    | `ServiceUnavailable`                                |
| 504    | `GatewayTimeout`                                    |

Because the table is framework-free, a gRPC or worker transport maps the same code to the same
meaning without a second table. **Adding a locale code means adding a row here** — omitting it
silently yields 500.

### classify and ToAppError

`classify` turns a non-`AppError` into one, and is **always locale-coded**:

```go
// classify maps a non-AppError into an *AppError, always locale-coded so the
// transport boundary can translate the client-facing message — no bare strings
// leak out.
```

`*locale.LocaleError` → its own code; a package sentinel → its canonical code; anything else →
`InternalError` (500). So an unclassified error from a third-party library can never leak its raw
text to a client.

`ToAppError(err)` is the boundary normalizer: already an `AppError` → returned as-is with frames
intact; otherwise classify and attach the cause.

### LogFields — for transports without middleware

```go
// LogFields renders err as structured log key/values for a single diagnostic
// log line, using the SAME field names the HTTP access log emits (app_code,
// app_err, app_trace, app_locale) so an error reads identically whether it
// surfaced from an HTTP request or a worker.
//
//	log.Error("kafka: handle message", errors.LogFields(err)...)
```

The worker has no access-log middleware to fold cause and trace in for it, so it calls this. One
query shape works across HTTP and worker logs.

### The AppError context stash

```go
// The Logging middleware needs to log the AppError that the handler emitted,
// but the response is written after the middleware's outbound ctx has
// already been observed — and contexts are immutable, so we can't replumb
// ctx upward. PrepareAppErrorContext installs a mutable slot in ctx; a
// handler or error writer calls StashAppError into that slot; the
// middleware reads it via AppErrorFromContext once the handler returns.

type appErrSlot struct{ err *AppError }
type appErrSlotKey struct{}
```

A pointer-to-struct in ctx is a mutable channel *upward* through an immutable value. It's the one
place the codebase does this, and it's what buys "handlers never log":
`Logging` installs the slot, `httpwriter.WriteError` fills it, `Logging` reads it after
`c.Next()`. `StashAppError` no-ops when the slot is absent, so tests and background contexts are
safe.

## pkg/locale

```go
// Package locale is the source of truth for application error codes and their
// per-language translations. A Locale is an int identifier; translations are
// registered at startup via NewMapping() and resolved at the transport
// boundary via Translate(). HTTP-status mapping lives in pkg/errors.
```

Note the division of labour: **codes and text here, status in `pkg/errors`.** Keeping the status
table out of this package is what lets a non-HTTP transport use the same codes.

### Codes are negative ints, banded by feature

```go
// Error code ranges — organize by feature:
//
//	General:  -1000 to -1099
//	User:     -1100 to -1199
const (
    InternalError        Locale = -1000
    InvalidRequest       Locale = -1001
    Unauthorized         Locale = -1002
    RecordNotFound       Locale = -1003
    Forbidden            Locale = -1004
    Conflict             Locale = -1005
    TooManyRequests      Locale = -1006
    Unimplemented        Locale = -1007
    MethodNotAllowed     Locale = -1008
    RequestTimeout       Locale = -1009
    PayloadTooLarge      Locale = -1010
    UnsupportedMediaType Locale = -1011
    ValidationFailed     Locale = -1012
    BadGateway           Locale = -1013
    ServiceUnavailable   Locale = -1014
    GatewayTimeout       Locale = -1015

    UserNotFound       Locale = -1100
    UserEmailExists    Locale = -1101
    InvalidCredentials Locale = -1102
)
```

Negative so they can't be confused with an HTTP status in a response body, and banded so a new
feature claims a range rather than appending to a flat list. A new aggregate takes `-1200`.

### LocaleError carries args, not text

```go
// LocaleError carries a Locale code and the format arguments to be substituted
// into the translated message at the transport boundary. Args are stored raw —
// translation happens in Translate, not at error-creation time.
type LocaleError struct {
    Code Locale
    Args []any
}
```

This is the mechanism behind "business code never names a language". `errors.L(locale.UserEmailExists,
"a@b.com")` stores the email; only `Translate` decides whether the template is
`"Email %s already exists"` or `"Email %s đã tồn tại"`.

`Locale.Err()` / `Locale.ErrFormat(args…)` build a bare `LocaleError`, and `AsLocaleError` unwraps
one. In practice usecase code goes through `errors.L` instead, which classifies the status at the
same time — the bare constructors exist for code that doesn't want to depend on `pkg/errors`.

### Translations register explicitly

```go
// translations is populated by NewMapping() — no init() side effects.
var translations = map[Language]Mapping{}

// NewMapping registers all language translations.
// Call this ONCE at app startup (e.g. in main.go or DI provider).
func NewMapping() { enMapping(); viMapping() }
```

No `init()`, so registration order is explicit and testable. `cmd/root.go` calls it
([11](11-di-and-entrypoints.md)) — a generated project that forgets to would serve
`"unknown error: -1100"`.

```go
// Translate resolves a Locale code to the user's language.
// Falls back to English if language or code is not found.
```

Two fallbacks, in order: unknown language → English mapping; code missing from that mapping →
English entry; still missing → `"unknown error: %d"`. A half-translated new language degrades to
English per-string rather than blanking the message.

### Language in context

```go
// The chosen language travels from transport middleware to the response writer
// through request context. Keeping the key + accessors here (rather than in a
// per-framework middleware package) means any layer that wants the language —
// including pkg/httputil and pkg/errors — can read it without importing
// transport-specific code.
```

`WithLanguage(ctx, lang)` / `LanguageFromContext(ctx)` (defaulting to `LangEn`). The middleware
only parses the header; the ctx plumbing lives here so `pkg/` never depends on `internal/`.

## pkg/httputil

The **framework-free** half of the HTTP response story. Its framework-bound counterpart is
`transport/http/httpwriter` ([07](07-transport-layer.md)).

```go
// All success and error responses share a single shape:
//
//	{
//	  "success":   true|false,
//	  "data":      <payload>,                             // present on success
//	  "error":     { "code": int, "message": string },    // present on error
//	  "meta":      <CursorMeta|PageMeta>,                 // optional pagination
//	  "requestId": string                                 // mirrors X-Request-ID
//	}
```

```go
// requestId is the same value as the X-Request-ID response header (see
// RequestIDHeader). Surfacing it in the body lets a client quote one ID when
// reporting an issue; grepping the logs for that request_id replays the whole
// request — every log line the request produced carries it, with caller
// file:line — so you can see what ran and where it errored.
```

That paragraph is the operational payoff of the whole logging design: one envelope field turns a
user's screenshot into a complete server-side replay.

### Request ID lives here, not in transport

```go
// RequestIDFrom returns the request ID attached to ctx, or "" if none. Safe to
// call from any layer (usecase, repository) that needs to log or propagate the
// ID to downstream services — it lives here, not in transport, so inner layers
// can reach it without importing a transport package.
```

### Two pagination metas, two audiences

```go
// CursorMeta is the pagination metadata for the user-facing, cursor (keyset)
// form. … Cursor paging is stable under concurrent inserts and stays
// cheap at depth (no OFFSET scan), so it's the default for end-user feeds. It
// deliberately exposes no totals — counting every match defeats the point.
type CursorMeta struct {
    HasMore    bool   `json:"hasMore"`
    NextCursor string `json:"nextCursor,omitempty"`
}

// PageMeta is the pagination metadata for the admin-facing, offset/page form.
// … TotalPage/TotalRecord require a COUNT query, so reserve this for
// back-office lists rather than hot user paths.
type PageMeta struct {
    Page        int   `json:"page"`
    PerPage     int   `json:"perPage"`
    TotalPage   int   `json:"totalPage"`
    TotalRecord int64 `json:"totalRecord"`
}
```

`NewPageMeta` derives `TotalPage` by ceil division and returns 0 rather than dividing by zero on a
non-positive `perPage`. The two shapes are why the assembler has separate meta projections
([07](07-transport-layer.md#assemblergo--the-only-place-that-maps)).

### ErrorResponseFromApp — the locale code becomes the client code

```go
// When app.Locale is non-zero the response uses the locale code as the
// machine-readable identifier (more specific than the HTTP status) and the
// message is the translated template with Args substituted. Otherwise the
// envelope falls back to app.Code + app.Message as-is.
//
// A nil app is treated as a generic 500 in the requested language.
```

So the HTTP status says *category* (404) and `error.code` says *reason* (-1100 `UserNotFound` vs
-1003 `RecordNotFound`) — a client can branch on the specific code without string matching. The
nil-app path means a bug in the caller still produces a valid, translated envelope.

## pkg/observability — the Logger port

```go
type Logger interface {
    Debug(msg string, kv ...any)
    Info(msg string, kv ...any)
    Warn(msg string, kv ...any)
    Error(msg string, kv ...any)
    With(key string, value any) Logger
}
```

The port lives in `pkg/`, the implementation in `internal/infrastructure/logger`
([09](09-infrastructure-layer.md)) — that inversion is what lets a usecase log without importing
infrastructure.

```go
// The human-readable text is the msg argument — never also pass it as a
// "message" kv key, and avoid the reserved field names the implementation adds
// automatically ("time", "level", "caller"); reusing any of them emits a
// duplicate field in the output.
```

**Reserved keys: `time`, `level`, `caller`, plus `message`.** Passing one produces a duplicate JSON
field, which most log pipelines resolve unpredictably.

Tracing and metrics deliberately have no port here:

```go
// Tracing and metrics are intentionally not modelled as ports here: the
// OpenTelemetry package-level APIs (otel.Tracer / otel.Meter) and ctx-threaded
// span context already serve as the seam. Add per-need ports when a layer
// actually needs to be tested without OTel — not before.
```

`Nop()` returns a discarding logger, so `logctx.From` can always return something non-nil.

## pkg/logctx — the request-scoped logger

```go
// Package logctx carries a request-scoped logger through context.Context so any
// inner layer (usecase, adapter, repository) can log with the request's
// correlation fields (request_id, method, path, …) already attached — without
// taking the logger as a constructor dependency or re-deriving the IDs on every
// call.
```

Two functions:

```go
func With(ctx context.Context, log observability.Logger) context.Context

// From returns the request-scoped logger attached to ctx, or a no-op logger when
// none is present (background goroutines, tests, or code paths that skip the
// access-log middleware). It never returns nil, so callers can chain directly:
//
//	logctx.From(ctx).Warn("cache miss", "key", key)
func From(ctx context.Context) observability.Logger
```

**`From` never returns nil.** That's why `logctx.From(ctx).Error(…)` appears inline throughout the
adapters and usecases with no guard — a test or background goroutine gets the nop logger rather
than a panic.

Note what this package does *not* do: it doesn't create loggers, and it doesn't depend on a
concrete one. It depends only on the port, so the dependency direction stays
`usecase → logctx → observability`, with `infrastructure/logger` plugged in at the bottom by DI.
