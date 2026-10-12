# Phase 3: Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Heavy steps (`make test-all`) run alone from the main thread; see Global Constraints.

**Goal:** From the logs alone, an on-call engineer can answer where (service, version, route, caller), when (sub-second timestamps, durations), what (route, message topic and position, config in effect), who (request id, trace id, user id, producer-side correlation) and why (the error chain) for every HTTP request, worker message and gRPC call the generated projects handle.

**Architecture:** The zerolog adapter renders `error` values properly, stamps service identity on every line and logs a redacted config summary once. The four HTTP middleware chains gain a tracing middleware, request-id validation, a `route` field, and a `user_id` field fed back from Auth through a slot in the middleware package. The worker transport gets a broker-neutral `Message` carrying key, position and correlation headers, so `Worker.dispatch` emits one outcome line per message with duration and the producer's request/trace ids, which the publisher now injects. Runners log shutdown duration and the full error chain; the gRPC server gets request-id, logging and recovery interceptors plus the OpenTelemetry stats handler. Everything lands in `internal/generator/templates` (and the mirrored `skel/worker` files), proven by a contract test over rendered output plus the existing real-toolchain bootstrap.

**Tech Stack:** zerolog, OpenTelemetry (otelfiber v2, otelgin, otelchi, otelecho, otelgrpc, propagation), sarama, amqp091-go, google.golang.org/grpc.

**Status (2026-10-08):** all six tasks implemented and verified (`make test-all` green at ~115 s with two new gRPC vet rows and the real bootstraps compiling the OpenTelemetry middleware; `make lint` 0 issues; docs/07, 09, 10, 13 updated); commit steps pending the user's `/grimoire-core:commit`.

**Deviations from the plan as written:**

- Task 3: echo keeps the contrib `otelecho` package with `//nolint:staticcheck // SA1019: echo v4`; the maintained `labstack/echo-opentelemetry` module targets echo v5 and does not compile against the generated project's echo v4.
- Task 4: the request-id context plumbing (`With`/`From`/`Valid`) moved out of `pkg/httputil` into a new transport-neutral `pkg/requestid`, rendered for every project; `pkg/httputil` stays HTTP-only (envelope + `RequestIDHeader`) and is again rendered only when `HasHTTP`. An earlier cut had rendered `pkg/httputil` for every project so the worker and publishers could reach the helpers; that leaked an HTTP package into worker and gRPC builds and was reverted. The user handler's `user.created: recorded` line was removed because the worker's outcome line is the single boundary log. `kafkaSessionHandler` lost its logger field (the worker logs failures).
- Task 5: `tracing.Provider` and `provideTracer` take an `observability.Logger`; the cleanup variable is `shutdownErr` to avoid shadowing.

## Global Constraints

- `make lint` and `make test` stay green after every task; `make test-all` runs alone at the end of tasks 3, 4, 5 and 6.
- Resource rule: one heavy toolchain command at a time from the main thread; never `go clean -cache`.
- Framework-prefixed templates change in all four variants (fiber, gin, chi, echo).
- `templates/app/worker.go.tmpl` and `skel/worker/app_worker.go.tmpl` stay byte-identical (`TestWorkerAppTemplatesIdentical`); the skel worker transport (`skel/worker/{worker,kafka_consumer,rabbitmq_consumer}.go.tmpl`) mirrors the `nova new` worker changes.
- Transport templates depend on `pkg/observability.Logger`, never on `infrastructure/logger` (`TestTransportLoggerPortDecoupling`).
- Log field names: `err` for errors (never `error`), `request_id`, `trace_id`, `user_id`, `route`, `path`, `duration_ms`.
- Markdown edits pass markdownlint with the repo config. Commits only via `/grimoire-core:commit` after confirmation.

---

### Task 1: Logger renders errors, carries service identity, logs the config once

**Files:**

- Modify: `internal/generator/templates/infrastructure/logger/zerolog.go.tmpl`
- Modify: `internal/generator/templates/infrastructure/config/config.go.tmpl` (new `Summary`)
- Modify: `internal/generator/templates/infrastructure/config/production.yaml.tmpl` (`level: "info"`)
- Modify: `internal/generator/templates/infrastructure/di/wire.go.tmpl:66` (drop `"Log"` from `FieldsOf`)
- Modify: `internal/generator/templates/infrastructure/di/fx.go.tmpl` (drop `provideLogConfig,`), `fx_provider.go.tmpl` (delete `provideLogConfig`)
- Modify: `"error",` → `"err",` in `templates/app/api.go.tmpl`, `templates/app/worker.go.tmpl`, `skel/worker/app_worker.go.tmpl`, `templates/app/grpc.go.tmpl`, `templates/infrastructure/cache/redis.go.tmpl`, `templates/infrastructure/pubsub/{kafka,rabbitmq}.go.tmpl`, `templates/usecase/user/service.go.tmpl`
- Test: `internal/generator/generator_test.go` (`TestObservabilityContract`, logger part; the full test is written in Task 6 and this task adds its first assertions)

**Interfaces:**

- Produces: `logger.New(cfg *config.Config) *Logger`, `logger.NewWithWriter(cfg *config.Config, w io.Writer) *Logger`, `(*config.Config).Summary() []any`.

- [x] **Step 1: Write the failing contract test (logger part)**

Append to `internal/generator/generator_test.go`:

```go
// TestObservabilityContract pins the fields an on-call engineer relies on:
// every HTTP access line can answer who (user_id), where (route) and how
// long; inbound request ids are validated; tracing middleware is installed;
// readiness failures are logged; the logger renders errors and carries the
// service identity; the worker scopes broker position and correlation ids.
// Later tasks extend the per-framework assertions.
func TestObservabilityContract(t *testing.T) {
	t.Parallel()
	dir := renderProject(t, httpMatrixConfig("fiber", "postgres", "wire"))
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/logger/zerolog.go")),
		"AnErr(", "RFC3339Nano", `"service"`, `"version"`, `"env"`, `"config loaded"`)
	mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/config/production.yaml")), `level: "info"`)
	mustNotContain(t, readFile(t, filepath.Join(dir, "internal/app/api.go")), `"error",`)
}
```

Run: `go test -short -count=1 -run TestObservabilityContract ./internal/generator` — Expected: FAIL (missing `AnErr(`, `RFC3339Nano`, …).

- [x] **Step 2: Rewrite the logger adapter**

In `templates/infrastructure/logger/zerolog.go.tmpl` add `"time"` to the imports and replace `New`, `NewWithWriter`, `With` and `emit` with:

```go
// New builds a Logger from the app config and writes to stdout. Every line
// carries service/version/env so aggregated logs identify the emitting
// process, and the first line is a redacted summary of the loaded config.
func New(cfg *config.Config) *Logger {
	l := NewWithWriter(cfg, os.Stdout)
	l.Info("config loaded", cfg.Summary()...)
	return l
}

// NewWithWriter is useful for tests.
func NewWithWriter(cfg *config.Config, w io.Writer) *Logger {
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	zerolog.CallerMarshalFunc = shortCaller
	// Sub-second precision: lines within one second must stay orderable, and
	// duration_ms is finer than a second-precision clock.
	zerolog.TimeFieldFormat = time.RFC3339Nano

	writer := w
	if strings.EqualFold(cfg.Log.Format, "console") {
		writer = zerolog.ConsoleWriter{Out: w}
	}

	zl := zerolog.New(writer).
		Level(parseLevel(cfg.Log.Level)).
		With().
		Timestamp().
		Str("service", cfg.AppName).
		Str("version", cfg.AppVersion).
		Str("env", cfg.AppEnv).
		Logger()

	return &Logger{zl: zl}
}
```

```go
// With returns a child logger with the key/value attached to every event.
// Returns observability.Logger (not *Logger) so the type satisfies the
// interface — inner layers fork via logctx.From(ctx).With(...) without
// importing this package.
func (l *Logger) With(key string, value any) observability.Logger {
	if err, ok := value.(error); ok {
		return &Logger{zl: l.zl.With().AnErr(key, err).Logger()}
	}
	return &Logger{zl: l.zl.With().Interface(key, value).Logger()}
}
```

```go
// emit writes (key, value) pairs as structured fields. Odd-length input is
// ignored silently. Callers must not pass a key zerolog populates itself —
// "message" (the msg arg), "time", "level", or "caller" — doing so emits a
// duplicate field in the JSON line.
func emit(ev *zerolog.Event, msg string, kv []any) {
	ev = ev.Caller(callerSkip)
	if len(kv)%2 != 0 {
		kv = kv[:len(kv)-1]
	}
	for i := 0; i < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			continue
		}
		// An error would JSON-marshal to {} through Interface; AnErr renders
		// its message (and stack, when the error carries one).
		if err, isErr := kv[i+1].(error); isErr {
			ev = ev.AnErr(key, err)
			continue
		}
		ev = ev.Interface(key, kv[i+1])
	}
	ev.Msg(msg)
}
```

- [x] **Step 3: Add the redacted config summary**

Append to `templates/infrastructure/config/config.go.tmpl`:

```go
// Summary returns the loaded configuration as log fields with every secret
// omitted, so the first log line of a process shows what it actually runs
// with. Only fields that exist in this build are included.
func (c *Config) Summary() []any {
	fields := []any{
		"log_level", c.Log.Level,
		"log_format", c.Log.Format,
		"tracing_endpoint", c.Tracing.Endpoint,
		"tracing_sample_ratio", c.Tracing.SampleRatio,
	}
{{- if .HasHTTP}}
	fields = append(fields, "http_port", c.HTTP.Port)
{{- end}}
{{- if .HasGRPC}}
	fields = append(fields, "grpc_port", c.GRPC.Port)
{{- end}}
{{- if .HasDatabase}}
	fields = append(fields, "db_host", c.Database.Host, "db_port", c.Database.Port, "db_name", c.Database.Name)
{{- end}}
{{- if eq .Database "postgres"}}
	fields = append(fields, "db_ssl_mode", c.Database.SSLMode)
{{- end}}
{{- if .HasRedis}}
	fields = append(fields, "redis_mode", c.Redis.Mode, "redis_addrs", c.Redis.Addrs)
{{- end}}
{{- if eq .MessageQueue "kafka"}}
	fields = append(fields, "kafka_brokers", c.Kafka.Brokers, "kafka_consumer_group", c.Kafka.ConsumerGroupID)
{{- end}}
{{- if eq .MessageQueue "rabbitmq"}}
	// The RabbitMQ URL carries credentials and is never logged.
	fields = append(fields, "rabbitmq_exchange", c.RabbitMQ.Exchange)
{{- end}}
	return fields
}
```

- [x] **Step 4: DI, production level, error keys**

- `wire.go.tmpl`: `wire.FieldsOf(new(*config.Config), "Log", "JWT", "Security"),` → `wire.FieldsOf(new(*config.Config), "JWT", "Security"),`.
- `fx.go.tmpl`: delete the line `provideLogConfig,`; `fx_provider.go.tmpl`: delete the line `func provideLogConfig(c *config.Config) config.LogConfig           { return c.Log }` (keep the two neighbours; gofmt realigns them).
- `production.yaml.tmpl`: `level: "warn"` → `level: "info" # access and lifecycle lines stay visible; raise only if volume forces it`.
- Replace every `"error",` key with `"err",` in the eight files listed above (`grep -rn '"error", ' internal/generator/templates internal/generator/skel` must return nothing afterwards).

- [x] **Step 5: Verify**

Run: `go test -short -count=1 ./... && make lint` — Expected: PASS (contract test green, `TestWorkerAppTemplatesIdentical` green), `0 issues.`

- [ ] **Step 6: Commit**

Stage the listed templates and `generator_test.go`. Suggested message: `fix(templates): render logged errors, stamp service identity, log config summary`.

---

### Task 2: Access lines answer who and where; request ids are validated; readiness failures are logged

**Files:**

- Create: `internal/generator/templates/transport/http/middleware/principal.go.tmpl`
- Modify: `internal/generator/generator.go` (`transportFiles`: add `principal.go`)
- Modify: `templates/pkg/httputil/response.go.tmpl` (`ValidRequestID`)
- Modify: `templates/transport/http/middleware/{fiber,gin,chi,echo}_requestid.go.tmpl`
- Modify: `templates/transport/http/middleware/{fiber,gin,chi,echo}_auth.go.tmpl`
- Modify: `templates/transport/http/middleware/{fiber,gin,chi,echo}_logging.go.tmpl`
- Modify: `templates/transport/http/{fiber,gin,chi,echo}_router.go.tmpl` (readiness warn)
- Test: `generator_test.go` (`TestObservabilityContract`, per-framework part)

**Interfaces:**

- Produces: `httputil.ValidRequestID(id string) bool`; middleware-package helpers `prepareAuthSlot(ctx) context.Context`, `rememberPrincipal(ctx, *identity.UserPrincipal)`, `principalFromSlot(ctx) *identity.UserPrincipal`.

- [x] **Step 1: Extend the contract test (failing)**

Replace the body of `TestObservabilityContract` with:

```go
	t.Parallel()
	for _, fw := range []string{"fiber", "gin", "chi", "echo"} {
		t.Run(fw, func(t *testing.T) {
			t.Parallel()
			dir := renderProject(t, httpMatrixConfig(fw, "postgres", "wire"))
			mw := filepath.Join(dir, "internal/transport/http/middleware")
			mustContainAll(t, readFile(t, filepath.Join(mw, "logging.go")),
				`"request_id"`, `"trace_id"`, `"route"`, `"path"`, `"user_id"`, `"duration_ms"`, `"client_ip"`, "prepareAuthSlot(")
			mustContainAll(t, readFile(t, filepath.Join(mw, "auth.go")), "rememberPrincipal(")
			mustContainAll(t, readFile(t, filepath.Join(mw, "requestid.go")), "httputil.ValidRequestID(")
			mustContainAll(t, readFile(t, filepath.Join(mw, "principal.go")), "func principalFromSlot(")
			mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/http/router.go")), "readiness check failed")
			mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/logger/zerolog.go")),
				"AnErr(", "RFC3339Nano", `"service"`, `"version"`, `"env"`, `"config loaded"`)
			mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/config/production.yaml")), `level: "info"`)
			mustNotContain(t, readFile(t, filepath.Join(dir, "internal/app/api.go")), `"error",`)
		})
	}
```

Run: `go test -short -count=1 -run TestObservabilityContract ./internal/generator` — Expected: FAIL on `"route"`, `"user_id"`, `principal.go`.

- [x] **Step 2: Request-id validation**

Append to `templates/pkg/httputil/response.go.tmpl` after `RequestIDFrom`:

```go
// requestIDMaxLen bounds an inbound X-Request-ID so a hostile client cannot
// bloat log lines or smuggle delimiters into dashboards.
const requestIDMaxLen = 128

// ValidRequestID reports whether an inbound request ID may be reused as-is:
// 1–128 characters from [A-Za-z0-9._-]. Anything else is replaced by a
// freshly generated ID at the transport boundary.
func ValidRequestID(id string) bool {
	if id == "" || len(id) > requestIDMaxLen {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}
```

In all four `*_requestid.go.tmpl` replace `if id == "" {` with `if !httputil.ValidRequestID(id) {`, and delete the dead local stores `c.Locals("request_id", id)` (fiber) and `c.Set("request_id", id)` (gin, echo) after confirming with `grep -rn 'Locals("request_id")\|GetString("request_id")\|Get("request_id")' internal/generator/templates` that nothing reads them.

- [x] **Step 3: The principal slot**

Create `templates/transport/http/middleware/principal.go.tmpl`:

```go
package middleware

import (
	"context"

	"{{.ModuleName}}/internal/domain/identity"
)

// principalSlot lets the Auth middleware hand the authenticated principal back
// up to the Logging middleware, which runs outside it. identity.WithUserPrincipal
// derives a child context that only inner handlers see, so the access log
// needs this mutable slot to learn who made the request.
type principalSlot struct {
	p *identity.UserPrincipal
}

type principalSlotKey struct{}

// prepareAuthSlot installs an empty slot; Logging calls it before next().
func prepareAuthSlot(ctx context.Context) context.Context {
	return context.WithValue(ctx, principalSlotKey{}, &principalSlot{})
}

// rememberPrincipal records p for the access log. No-op when Logging did not
// run (a route mounted outside the middleware chain).
func rememberPrincipal(ctx context.Context, p *identity.UserPrincipal) {
	if slot, ok := ctx.Value(principalSlotKey{}).(*principalSlot); ok {
		slot.p = p
	}
}

// principalFromSlot returns the principal Auth recorded, or nil.
func principalFromSlot(ctx context.Context) *identity.UserPrincipal {
	if slot, ok := ctx.Value(principalSlotKey{}).(*principalSlot); ok {
		return slot.p
	}
	return nil
}
```

Register it in `generator.go` `transportFiles`, next to the middleware entries:

```go
		// Principal slot shared by Auth and Logging — framework-agnostic.
		{
			"templates/transport/http/middleware/principal.go.tmpl",
			"internal/transport/http/middleware/principal.go",
			cfg.HasHTTP(),
		},
```

Auth, one line each, right after the principal is verified:

- fiber: before `c.SetUserContext(...)` add `rememberPrincipal(c.UserContext(), principal)`.
- gin: before `c.Request = c.Request.WithContext(...)` add `rememberPrincipal(c.Request.Context(), principal)`.
- chi: before `next.ServeHTTP(...)` add `rememberPrincipal(r.Context(), principal)`.
- echo: before `req := c.Request()` add `rememberPrincipal(c.Request().Context(), principal)`.

- [x] **Step 4: Logging emits route and user_id**

In every `*_logging.go.tmpl`, directly after `ctx = errors.PrepareAppErrorContext(ctx)` add `ctx = prepareAuthSlot(ctx)`.

Then add two fields to the access line and, for echo, make the scoped `path` the raw path:

- fiber (`fiber_logging.go.tmpl`): in `fields` add `"route", c.Route().Path,` after `"status", status,`; after the `fields := []any{...}` literal add:

```go
		if p := principalFromSlot(ctx); p != nil {
			fields = append(fields, "user_id", p.UserID)
		}
```

- gin: add `"route", c.FullPath(),` and the same `principalFromSlot` block.
- chi: add `"route", chi.RouteContext(r.Context()).RoutePattern(),` and the same block; add the import `"github.com/go-chi/chi/v5"`.
- echo: in the scoped logger use `With("path", req.URL.Path)` instead of `With("path", c.Path())`; in `logAccess` add `"route", c.Path(),` and the same block (it already receives `ctx`).

- [x] **Step 5: Readiness failures are logged**

In every `*_router.go.tmpl` `/readyz` handler, right after `status := checker.Readiness(...)`:

```go
		if !status.OK {
			log.Warn("readiness check failed", "details", status.Details)
		}
```

(Logging skips probes, so this is the only line a failing dependency produces.)

- [x] **Step 6: Verify**

Run: `go test -short -count=1 ./... && make lint` — Expected: PASS, `0 issues.`

- [ ] **Step 7: Commit**

Suggested message: `feat(templates): access logs carry route and user_id; validate inbound request ids; log readiness failures`.

---

### Task 3: Tracing middleware is installed, so trace_id is real

**Files:**

- Modify: `templates/transport/http/{fiber,gin,chi,echo}_router.go.tmpl`
- Test: `generator_test.go` (`TestObservabilityContract` router assertion)

- [x] **Step 1: Extend the test (failing)**

Add before the loop in `TestObservabilityContract`:

```go
	otelMiddleware := map[string]string{
		"fiber": "otelfiber.Middleware(", "gin": "otelgin.Middleware(",
		"chi": "otelchi.Middleware(", "echo": "otelecho.Middleware(",
	}
```

and change the router assertion to `mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/http/router.go")), "readiness check failed", otelMiddleware[fw], "func isProbe(")`.

Run the test — Expected: FAIL on the otel identifiers.

- [x] **Step 2: Install the middleware first in each router**

Add to every router file:

```go
// isProbe reports whether path is a health probe. Probes stay out of traces
// (and access logs) because they carry no application signal.
func isProbe(path string) bool { return path == "/healthz" || path == "/readyz" }
```

fiber (`fiber_router.go.tmpl`): import `"github.com/gofiber/contrib/otelfiber/v2"`; before `app.Use(middleware.RequestID())` add:

```go
	// Tracing first so RequestID and Logging see the server span (trace_id).
	app.Use(otelfiber.Middleware(otelfiber.WithNext(func(c *fiber.Ctx) bool { return isProbe(c.Path()) })))
```

Also replace the stray comment block (`/healthz and /readyz are mounted BEFORE the middleware stack-... actually no — ...`) with `// /healthz and /readyz sit inside the middleware stack so a panic during a readiness check still flows through Recovery.`

gin: import `"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"`; before `engine.Use(middleware.RequestID())`:

```go
	// Tracing first so RequestID and Logging see the server span (trace_id).
	engine.Use(otelgin.Middleware(cfg.AppName, otelgin.WithFilter(func(r *http.Request) bool { return !isProbe(r.URL.Path) })))
```

chi: import `"github.com/riandyrn/otelchi"`; before `router.Use(middleware.RequestID)`:

```go
	// Tracing first so RequestID and Logging see the server span (trace_id).
	router.Use(otelchi.Middleware(
		cfg.AppName,
		otelchi.WithChiRoutes(router),
		otelchi.WithFilter(func(r *http.Request) bool { return !isProbe(r.URL.Path) }),
	))
```

echo: import `"go.opentelemetry.io/contrib/instrumentation/github.com/labstack/echo/otelecho"` with `//nolint:staticcheck // SA1019: echo v4` on the import line (the replacement `labstack/echo-opentelemetry` module targets echo v5, which the generated project does not use yet); before `e.Use(middleware.RequestID())`:

```go
	// Tracing first so RequestID and Logging see the server span (trace_id).
	e.Use(otelecho.Middleware(
		cfg.AppName,
		otelecho.WithSkipper(func(c echo.Context) bool { return isProbe(c.Request().URL.Path) }),
	))
```

- [x] **Step 3: Verify, including the real bootstrap (new dependencies resolve)**

Run: `go test -short -count=1 ./... && make lint` — Expected: PASS, `0 issues.`

Run (alone): `make test-all` — Expected: PASS; the bootstrap cases download the otel contrib modules through `go mod tidy` and build.

- [ ] **Step 4: Commit**

Suggested message: `feat(templates): wire OpenTelemetry server spans so trace_id is populated`.

---

### Task 4: Worker messages carry position and correlation; one outcome line per message

**Files:**

- Modify: `templates/transport/worker/worker.go.tmpl`, `kafka_consumer.go.tmpl`, `rabbitmq_consumer.go.tmpl`, `v1/user/handler.go.tmpl`
- Modify: `skel/worker/worker.go.tmpl`, `skel/worker/kafka_consumer.go.tmpl`, `skel/worker/rabbitmq_consumer.go.tmpl`
- Modify: `templates/adapter/pubsub/publisher.go.tmpl`, `user_publisher.go.tmpl`
- Test: `generator_test.go` (`TestObservabilityContract` worker + publisher subtests)

**Interfaces:**

- Produces: `worker.Message{Topic, Payload, Key string, Headers map[string]string, Partition int32, Offset int64, DeliveryTag uint64, MessageID string}`, `type DispatchFunc func(ctx context.Context, msg Message) error`, `const HeaderRequestID = "x-request-id"`; publisher `publish(ctx, topic, key string, payload any)` and `correlationHeaders(ctx) map[string]string`.

- [x] **Step 1: Extend the test (failing)**

Append two subtests inside `TestObservabilityContract` after the loop:

```go
	t.Run("worker", func(t *testing.T) {
		t.Parallel()
		dir := renderProject(t, workerMatrixConfig("postgres", "kafka", "wire"))
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/worker/worker.go")),
			"type Message struct", `"partition"`, `"offset"`, `"request_id"`, `"trace_id"`, `"duration_ms"`, "errors.LogFields(")
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/worker/consumer.go")), "group.Errors()")
	})
	t.Run("publisher", func(t *testing.T) {
		t.Parallel()
		cfg := httpMatrixConfig("fiber", "postgres", "wire")
		cfg.MessageQueue = "kafka"
		dir := renderProject(t, cfg)
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/adapter/pubsub/publisher.go")),
			"correlationHeaders(", "propagation.MapCarrier", "sarama.StringEncoder(key)")
	})
```

Run the test — Expected: FAIL.

- [x] **Step 2: Rewrite `templates/transport/worker/worker.go.tmpl`**

```go
package worker

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"{{.ModuleName}}/pkg/errors"
	"{{.ModuleName}}/pkg/httputil"
	"{{.ModuleName}}/pkg/logctx"
	"{{.ModuleName}}/pkg/observability"
)

// Consumer is the broker-side abstraction. Implementations live in
// kafka_consumer.go / rabbitmq_consumer.go (one is rendered per project per
// MessageQueue choice). Subscribe blocks until ctx is cancelled or an
// unrecoverable error surfaces; dispatch is invoked per received message.
// Return nil from dispatch to ack; a non-nil error nacks (broker-specific:
// Kafka surfaces back to sarama for retry, RabbitMQ nacks with requeue).
type Consumer interface {
	Subscribe(ctx context.Context, topics []string, dispatch DispatchFunc) error
	Close() error
}

// Message is the broker-neutral view of one delivery. Consumers fill the
// fields their broker knows (Kafka: Key/Partition/Offset; RabbitMQ:
// DeliveryTag/MessageID); Headers carry the producer's correlation ids.
type Message struct {
	Topic       string
	Payload     []byte
	Key         string
	Headers     map[string]string
	Partition   int32  // Kafka only; -1 when unknown
	Offset      int64  // Kafka only
	DeliveryTag uint64 // RabbitMQ only
	MessageID   string // RabbitMQ MessageId when the producer set one
}

// DispatchFunc is the per-message callback the Consumer invokes after
// receipt. Workers use a single bound dispatch closure that routes by topic.
type DispatchFunc func(ctx context.Context, msg Message) error

// HeaderRequestID is the message header carrying the producer-side request
// id, so a consumer log line joins the HTTP request that caused it. The
// publisher (adapter/pubsub) sets it; W3C trace context travels alongside.
const HeaderRequestID = "x-request-id"

// Worker owns one Consumer and a topic-keyed Handler set. The lifecycle is
// driven by Start(ctx) (blocks) / Stop() (closes the consumer).
type Worker struct {
	consumer Consumer
	handlers map[string]Handler
	log      observability.Logger
}

func NewWorker(c Consumer, log observability.Logger, handlers []Handler) *Worker {
	m := make(map[string]Handler, len(handlers))
	for _, h := range handlers {
		m[h.Topic()] = h
	}
	return &Worker{consumer: c, handlers: m, log: log}
}

// Topics returns the set of topics this worker subscribes to — read by the
// Consumer impl to know which subscriptions to create.
func (w *Worker) Topics() []string {
	out := make([]string, 0, len(w.handlers))
	for t := range w.handlers {
		out = append(out, t)
	}
	return out
}

// Start blocks until ctx is cancelled. Per-message dispatch attaches a
// scoped logger to ctx so inner layers (usecase, repo) emit topic-aware
// structured fields without re-plumbing.
func (w *Worker) Start(ctx context.Context) error {
	w.log.Info("worker: starting", "topics", w.Topics())
	return w.consumer.Subscribe(ctx, w.Topics(), w.dispatch)
}

// Stop closes the consumer. Idempotent at the broker layer (sarama / amqp
// both tolerate double-close).
func (w *Worker) Stop() error {
	return w.consumer.Close()
}

// dispatch routes one message to its handler and emits exactly one outcome
// line carrying the broker position, the producer's correlation ids and the
// handler duration — the worker-side counterpart of the HTTP access log.
func (w *Worker) dispatch(ctx context.Context, msg Message) error {
	h, ok := w.handlers[msg.Topic]
	if !ok {
		// Unknown topic — log and ack so we don't poison-pill the partition.
		// Tighten by returning an error if your broker has a DLQ configured.
		w.log.Warn("worker: dropping message, no handler for topic", "topic", msg.Topic, "size", len(msg.Payload))
		return nil
	}

	// Adopt the producer's trace context and request id so this line joins
	// the upstream request; mint an id when the producer sent none.
	ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(msg.Headers))
	reqID := msg.Headers[HeaderRequestID]
	if !httputil.ValidRequestID(reqID) {
		reqID = uuid.New().String()
	}
	ctx = httputil.WithRequestID(ctx, reqID)

	scoped := w.log.
		With("topic", msg.Topic).
		With("request_id", reqID).
		With("trace_id", traceIDOr(trace.SpanContextFromContext(ctx))).
		With("size", len(msg.Payload))
	if msg.Key != "" {
		scoped = scoped.With("key", msg.Key)
	}
	if msg.Partition >= 0 {
		scoped = scoped.With("partition", msg.Partition).With("offset", msg.Offset)
	}
	if msg.DeliveryTag > 0 {
		scoped = scoped.With("delivery_tag", msg.DeliveryTag)
	}
	if msg.MessageID != "" {
		scoped = scoped.With("message_id", msg.MessageID)
	}
	ctx = logctx.With(ctx, scoped)

	start := time.Now()
	err := h.Handle(ctx, msg.Payload)
	fields := []any{"duration_ms", time.Since(start).Milliseconds()}
	if err != nil {
		scoped.Error("worker: message failed", append(fields, errors.LogFields(err)...)...)
		return err
	}
	scoped.Info("worker: message handled", fields...)
	return nil
}

func traceIDOr(sc trace.SpanContext) string {
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
```

- [x] **Step 3: Consumers build the Message and stop double-logging**

`templates/transport/worker/kafka_consumer.go.tmpl`: in `Subscribe`, before the `for` loop, drain the error channel:

```go
	// sarama reports partition/offset/broker errors here only because
	// Return.Errors is set (infrastructure/pubsub); undrained they are lost
	// and the channel eventually blocks. The goroutine ends when the group
	// is closed.
	go func() {
		for err := range c.group.Errors() {
			c.log.Error("kafka: consumer group error", "err", err)
		}
	}()
```

Replace the `ConsumeClaim` loop body and add a helper:

```go
func (h *kafkaSessionHandler) ConsumeClaim(sess sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for {
		select {
		case m, ok := <-claim.Messages():
			if !ok {
				return nil
			}
			msg := Message{
				Topic:     m.Topic,
				Payload:   m.Value,
				Key:       string(m.Key),
				Headers:   kafkaHeaders(m.Headers),
				Partition: m.Partition,
				Offset:    m.Offset,
			}
			if err := h.dispatch(sess.Context(), msg); err != nil {
				// The worker logged the failure with its correlation fields.
				// Surface to sarama: the session rebalances and the message is
				// redelivered because the offset is not advanced (no MarkMessage).
				return err
			}
			sess.MarkMessage(m, "")
		case <-sess.Context().Done():
			return nil
		}
	}
}

func kafkaHeaders(hs []*sarama.RecordHeader) map[string]string {
	out := make(map[string]string, len(hs))
	for _, h := range hs {
		if h != nil {
			out[string(h.Key)] = string(h.Value)
		}
	}
	return out
}
```

(the `log` field of `kafkaSessionHandler` becomes unused; delete it and the `log: c.log` in its construction.)

`templates/transport/worker/rabbitmq_consumer.go.tmpl`: replace `handle` and add a helper:

```go
// handle dispatches one delivery and acks or nacks it. A dispatch error
// requeues the message so another replica (or the next redelivery) gets a
// shot; switch to a DLQ in production. The worker already logged the failure.
func (c *rabbitmqConsumer) handle(ctx context.Context, d amqp.Delivery, dispatch DispatchFunc) {
	msg := Message{
		Topic:       d.RoutingKey,
		Payload:     d.Body,
		Headers:     amqpHeaders(d.Headers),
		Partition:   -1,
		DeliveryTag: d.DeliveryTag,
		MessageID:   d.MessageId,
	}
	if dispErr := dispatch(ctx, msg); dispErr != nil {
		if nackErr := d.Nack(false, true); nackErr != nil {
			c.log.Error("rabbitmq: nack failed", "delivery_tag", d.DeliveryTag, "err", nackErr)
		}
		return
	}
	if ackErr := d.Ack(false); ackErr != nil {
		c.log.Error("rabbitmq: ack failed", "delivery_tag", d.DeliveryTag, "err", ackErr)
	}
}

func amqpHeaders(t amqp.Table) map[string]string {
	out := make(map[string]string, len(t))
	for k, v := range t {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}
```

`templates/transport/worker/v1/user/handler.go.tmpl`: delete the line `log.Info("user.created: recorded", "user_id", msg.UserID)` (the worker's outcome line is the single boundary log; `log` is still used for the malformed-payload error).

Apply the same `Message`/`DispatchFunc`/`dispatch`/consumer changes to `skel/worker/worker.go.tmpl`, `skel/worker/kafka_consumer.go.tmpl` and `skel/worker/rabbitmq_consumer.go.tmpl`, keeping their `*logger.Logger` field type (the skel tree is a separate package; `w.log.With(...)` already returns the port).

- [x] **Step 4: Publisher injects correlation headers and a key**

In `templates/adapter/pubsub/publisher.go.tmpl`, extend the kafka/rabbitmq import block (inside `{{- if or (eq .MessageQueue "kafka") (eq .MessageQueue "rabbitmq")}}`) with:

```go
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"{{.ModuleName}}/pkg/httputil"
```

(keep import groups: std, third-party, project), and add inside the same condition, after the imports:

```go
// correlationHeaders carries the request id and W3C trace context of the
// publishing request onto the message, so the consumer's log lines and spans
// join the HTTP request that produced the event.
func correlationHeaders(ctx context.Context) map[string]string {
	headers := map[string]string{}
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headers))
	if reqID := httputil.RequestIDFrom(ctx); reqID != "" {
		headers["x-request-id"] = reqID
	}
	return headers
}
```

Kafka `publish` becomes `publish(ctx context.Context, topic, key string, payload any) error` and sends:

```go
	headers := correlationHeaders(ctx)
	recordHeaders := make([]sarama.RecordHeader, 0, len(headers))
	for k, v := range headers {
		recordHeaders = append(recordHeaders, sarama.RecordHeader{Key: []byte(k), Value: []byte(v)})
	}
	_, _, err = p.producer.SendMessage(&sarama.ProducerMessage{
		Topic:   topic,
		Key:     sarama.StringEncoder(key), // same key → same partition → per-aggregate ordering
		Value:   sarama.ByteEncoder(data),
		Headers: recordHeaders,
	})
```

RabbitMQ `publish` becomes `publish(ctx context.Context, routingKey, _ string, payload any) error` and publishes:

```go
	headers := amqp.Table{}
	for k, v := range correlationHeaders(ctx) {
		headers[k] = v
	}
	if pubErr := p.ch.PublishWithContext(ctx, rabbitmqExchange, routingKey, false, false, amqp.Publishing{
		ContentType: "application/json",
		Headers:     headers,
		Body:        data,
	}); pubErr != nil {
```

NATS stub: `publish(ctx context.Context, subject, _ string, payload any) error`.

`templates/adapter/pubsub/user_publisher.go.tmpl`: import `"strconv"` and call `p.publish(ctx, "user.created", strconv.FormatInt(evt.UserID, 10), msg)`.

- [x] **Step 5: Verify**

Run: `go test -short -count=1 ./... && make lint` — Expected: PASS, `0 issues.`

Run (alone): `make test-all` — Expected: PASS (worker bootstrap compiles the new worker package).

- [ ] **Step 6: Commit**

Suggested message: `feat(templates): worker logs one outcome line per message with position, duration and producer correlation`.

---

### Task 5: Runners log the error chain and shutdown duration; gRPC gets interceptors

**Files:**

- Modify: `templates/app/api.go.tmpl`, `templates/app/worker.go.tmpl`, `skel/worker/app_worker.go.tmpl`, `templates/app/grpc.go.tmpl`, `templates/cmd/grpc.go.tmpl`
- Modify: `templates/infrastructure/di/fx.go.tmpl` (3 × `_ = fxApp.Stop`), `skel/worker/di_worker_fx.go.tmpl`
- Modify: `templates/infrastructure/tracing/otel.go.tmpl` (`Provider` logs shutdown errors), `templates/infrastructure/di/fx_provider.go.tmpl` (`provideTracer`)
- Modify: `templates/infrastructure/server/grpc.go.tmpl`
- Create: `templates/transport/grpc/interceptor/interceptor.go.tmpl`
- Modify: `internal/generator/generator.go` (`transportFiles`: interceptor; `buildFileList` unchanged)
- Test: `generator_test.go` (gRPC matrix rows + `wireGenGRPCStub`)

**Interfaces:**

- Produces: `app.RunGRPCServer() error`; `tracing.Provider(ctx, cfg, log observability.Logger)`; package `interceptor` with `RequestID()`, `Logging(observability.Logger)`, `Recovery()` returning `grpc.UnaryServerInterceptor`.

- [x] **Step 1: gRPC enters the vet matrix (failing until the server compiles with interceptors)**

In `generator_test.go` add:

```go
const wireGenGRPCStub = `// Code generated by Wire. DO NOT EDIT.
//go:build !wireinject
// +build !wireinject

package di

import "context"

func InitializeGRPCServer(_ context.Context) (*GRPCApp, func(), error) {
	return &GRPCApp{}, func() {}, nil
}
`

// grpcMatrixConfig is the gRPC transport over postgres; it has no cache,
// search or broker so the row stays cheap.
func grpcMatrixConfig(di string) *config.ProjectConfig {
	cfg := baseMatrixConfig("postgres", di)
	cfg.Transport = "grpc"
	cfg.Cache = "none"
	cfg.Search = "none"
	cfg.MessageQueue = "none"
	return cfg
}
```

and inside `TestGenerateMatrix`'s `for _, di := range dis` loop:

```go
		t.Run("grpc_"+di+"_postgres", func(t *testing.T) {
			t.Parallel()
			renderAndVet(t, grpcMatrixConfig(di), wireGenGRPCStub)
		})
```

Also extend `TestObservabilityContract` with:

```go
	t.Run("grpc", func(t *testing.T) {
		t.Parallel()
		dir := renderProject(t, grpcMatrixConfig("wire"))
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/transport/grpc/interceptor/interceptor.go")),
			"func RequestID()", "func Logging(", "func Recovery()", `"duration_ms"`)
		mustContainAll(t, readFile(t, filepath.Join(dir, "internal/infrastructure/server/grpc.go")),
			"otelgrpc.NewServerHandler()", "grpc.ChainUnaryInterceptor(")
		mustContainAll(t, readFile(t, filepath.Join(dir, "cmd/grpc.go")), "RunE:")
	})
```

Run: `go test -short -count=1 -run TestObservabilityContract ./internal/generator` — Expected: FAIL.

- [x] **Step 2: Interceptors**

Create `templates/transport/grpc/interceptor/interceptor.go.tmpl`:

```go
// Package interceptor holds the unary gRPC interceptors that mirror the HTTP
// middleware chain: request id, one access line per call, and panic
// recovery. infrastructure/server.NewGRPCServer installs them.
package interceptor

import (
	"context"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"{{.ModuleName}}/pkg/errors"
	"{{.ModuleName}}/pkg/httputil"
	"{{.ModuleName}}/pkg/logctx"
	"{{.ModuleName}}/pkg/observability"
)

// requestIDMetadataKey is the incoming metadata key clients use to propagate a
// correlation id; it mirrors the HTTP X-Request-ID header.
const requestIDMetadataKey = "x-request-id"

// RequestID adopts a valid inbound x-request-id or mints one, echoes it in the
// response headers, and stores it in ctx through the same plumbing the HTTP
// layer uses (httputil.RequestIDFrom).
func RequestID() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		id := ""
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if vals := md.Get(requestIDMetadataKey); len(vals) > 0 {
				id = vals[0]
			}
		}
		if !httputil.ValidRequestID(id) {
			id = uuid.New().String()
		}
		_ = grpc.SetHeader(ctx, metadata.Pairs(requestIDMetadataKey, id))
		return next(httputil.WithRequestID(ctx, id), req)
	}
}

// Logging attaches a request-scoped logger to ctx and emits one line per call
// with the gRPC status code, duration and any AppError detail. Server faults
// log at error, client-side codes at warn.
func Logging(base observability.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		start := time.Now()
		scoped := base.
			With("request_id", httputil.RequestIDFrom(ctx)).
			With("trace_id", traceIDOr(trace.SpanContextFromContext(ctx))).
			With("method", info.FullMethod)
		ctx = logctx.With(ctx, scoped)

		resp, err := next(ctx, req)

		code := status.Code(err)
		fields := []any{"code", code.String(), "duration_ms", time.Since(start).Milliseconds()}
		switch {
		case err == nil:
			scoped.Info("rpc", fields...)
		case isServerFault(code):
			scoped.Error("rpc", append(fields, errors.LogFields(err)...)...)
		default:
			scoped.Warn("rpc", append(fields, errors.LogFields(err)...)...)
		}
		return resp, err
	}
}

// Recovery converts a handler panic into codes.Internal and logs the stack, so
// one bad request cannot take the process down.
func Recovery() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logctx.From(ctx).Error("rpc: panic recovered",
					"method", info.FullMethod,
					"panic", r,
					"stack", string(debug.Stack()),
				)
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return next(ctx, req)
	}
}

func isServerFault(code codes.Code) bool {
	switch code {
	case codes.Internal, codes.Unknown, codes.DataLoss, codes.Unavailable, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
}

func traceIDOr(sc trace.SpanContext) string {
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
```

Register in `generator.go` `transportFiles` after the gRPC handler entry:

```go
		{
			"templates/transport/grpc/interceptor/interceptor.go.tmpl",
			"internal/transport/grpc/interceptor/interceptor.go",
			cfg.HasGRPC(),
		},
```

- [x] **Step 3: Server installs them; runner returns errors**

`templates/infrastructure/server/grpc.go.tmpl`: imports gain `"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"` and `"{{.ModuleName}}/internal/transport/grpc/interceptor"`; construct the server as

```go
		server: grpc.NewServer(
			grpc.StatsHandler(otelgrpc.NewServerHandler()),
			grpc.ChainUnaryInterceptor(
				interceptor.RequestID(),
				interceptor.Logging(log),
				interceptor.Recovery(),
			),
		),
```

and in `Start` replace `lis, err := net.Listen("tcp", s.addr)` with `lis, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", s.addr)` (the server has no request context at bind time).

`templates/app/grpc.go.tmpl` becomes:

```go
package app

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"{{.ModuleName}}/internal/infrastructure/di"
	"{{.ModuleName}}/pkg/errors"
)

// RunGRPCServer boots the gRPC server, blocks until the OS signals shutdown or
// the server stops with an error, and returns any failure so the caller
// (cobra RunE) sets a non-zero exit code. Mirrors RunHTTPServer.
func RunGRPCServer() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	app, cleanup, err := di.InitializeGRPCServer(ctx)
	if err != nil {
		return fmt.Errorf("initialize grpc server: %w", err)
	}

	log := app.Logger
	server := app.Server
	defer func() {
		start := time.Now()
		cleanup()
		log.Info("shutdown complete", "duration_ms", time.Since(start).Milliseconds())
	}()

	errChan := make(chan error, 1)
	go func() {
		// Serve returns nil after GracefulStop — only non-nil errors are failures.
		if startErr := server.Start(); startErr != nil {
			errChan <- startErr
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case startErr := <-errChan:
		log.Error("grpc server stopped with error", errors.LogFields(startErr)...)
		return startErr
	}

	log.Info("shutting down grpc server")
	return nil
}
```

`templates/cmd/grpc.go.tmpl`: `Run: func(*cobra.Command, []string) { app.RunGRPCServer() },` → `RunE: func(*cobra.Command, []string) error { return app.RunGRPCServer() },`.

- [x] **Step 4: HTTP and worker runners**

`templates/app/api.go.tmpl`: alias the standard library import as `stderrors "errors"`, add `"time"` and `"{{.ModuleName}}/pkg/errors"`; replace `defer cleanup()` (move it after `log := app.Logger`) with

```go
	log := app.Logger
	server := app.Server
	defer func() {
		start := time.Now()
		cleanup()
		log.Info("shutdown complete", "duration_ms", time.Since(start).Milliseconds())
	}()
```

use `stderrors.Is(startErr, http.ErrServerClosed)`, and log `log.Error("server stopped with error", errors.LogFields(startErr)...)`.

`templates/app/worker.go.tmpl` and `skel/worker/app_worker.go.tmpl` (identical edits): alias `stderrors "errors"`, add `"time"` and `"{{.ModuleName}}/pkg/errors"`, the same deferred shutdown block after `worker := app.Worker`, `stderrors.Is(startErr, context.Canceled)`, and `log.Error("worker: stopped with error", errors.LogFields(startErr)...)` / `log.Error("worker: stop failed", errors.LogFields(stopErr)...)`.

- [x] **Step 5: fx and tracing stop errors are logged**

`fx.go.tmpl` (three places) and `skel/worker/di_worker_fx.go.tmpl`: replace `_ = fxApp.Stop(stopCtx)` with

```go
		if stopErr := fxApp.Stop(stopCtx); stopErr != nil {
			log.Error("fx: stop", "err", stopErr)
		}
```

`templates/infrastructure/tracing/otel.go.tmpl`: import `"{{.ModuleName}}/pkg/observability"`, change `Provider` to `func Provider(ctx context.Context, cfg Config, log observability.Logger) (trace.Tracer, func(), error)` and its cleanup body to

```go
		if err := shutdown(shutdownCtx); err != nil {
			log.Error("tracing: shutdown", "err", err)
		}
```

`fx_provider.go.tmpl`: `provideTracer(lc fx.Lifecycle, ctx context.Context, cfg tracing.Config, log observability.Logger)` passing `log` to `tracing.Provider`. Wire needs no change (`observability.Logger` is already bound in `infraSet`).

- [x] **Step 6: Verify**

Run: `go test -short -count=1 ./... && make lint` — Expected: PASS, `0 issues.`

Run (alone): `make test-all` — Expected: PASS, including the two new gRPC matrix rows.

- [ ] **Step 7: Commit**

Suggested message: `feat(templates): log error chains and shutdown duration; add gRPC request-id, logging and recovery interceptors`.

---

### Task 6: Docs describe the observability contract

**Files:**

- Modify: `docs/13-cross-cutting.md`, `docs/10-shared-packages.md`, `docs/07-transport-layer.md`, `docs/09-infrastructure-layer.md`
- Modify: `docs/superpowers/plans/2026-10-04-review-remediation-roadmap.md` (phase 2 done, phase 3 link)

- [x] **Step 1: Fix the claims that changed**

Use `grep -n` to locate and update, in each doc, the sentences about: `trace_id` on every log line (now true once a tracer provider or an inbound `traceparent` exists; the server span comes from the framework's otel middleware installed first); the access-log field list (add `route`, `user_id`; `path` is the raw path in all four frameworks, `route` the pattern); inbound `X-Request-ID` validation (`httputil.ValidRequestID`, 128 chars, `[A-Za-z0-9._-]`); `logger.New(cfg *config.Config)` and the `config loaded` summary with the `service`/`version`/`env` fields; the production log level (`info`); the worker `Message`, its `worker: message handled|failed` outcome line and the `x-request-id` + `traceparent` headers the publisher sets; the gRPC interceptor chain under `internal/transport/grpc/interceptor/` (docs/07 already claims an interceptor directory; make the description match the three interceptors); `errors.LogFields` used by runners and the worker; `shutdown complete` with `duration_ms`.

- [x] **Step 2: Roadmap**

Mark Phase 2 `(done 2026-10-08, commit \`4858cc8\`)` and add `Detailed plan: [2026-10-08-phase3-observability.md](2026-10-08-phase3-observability.md).` under Phase 3.

- [x] **Step 3: Lint**

Run: `npx --yes markdownlint-cli2 docs/07-transport-layer.md docs/09-infrastructure-layer.md docs/10-shared-packages.md docs/13-cross-cutting.md "docs/superpowers/plans/*.md"` — Expected: `Summary: 0 issues`.

- [ ] **Step 4: Commit**

Suggested message: `docs: describe the logging and tracing contract after phase 3`.

---

## Self-review notes

- Spec coverage against the roadmap's phase 3 list: `{}` errors (Task 1 `AnErr`), dead tracing (Task 3 middleware, Task 5 otelgrpc), prod level warn (Task 1), service fields and config summary (Task 1), `user_id` in access logs incl. chi via the slot (Task 2), worker position/correlation/duration plus publisher headers (Task 4), `LogFields` in consumers and runners (Tasks 4, 5), RFC3339Nano (Task 1), `err`/`error` key drift (Task 1), readiness failures logged (Task 2), shutdown outcomes logged (Task 5), gRPC interceptors (Task 5), request-id validation (Task 2). Emails in `app_trace` are a phase 6 item (usecase wrap messages).
- Deliberately deferred: `otelpgx`/`redisotel` dependency-level spans and per-query latency (listed as medium in the review); 2xx access-log sampling knob; panic recovery and per-message timeout inside the worker (phase 5 reliability, alongside retries/DLQ).
- Type consistency: `DispatchFunc(ctx, Message)` is used identically by both consumers and both template trees; `publish(ctx, topic, key, payload)` has the same arity in all three broker variants; `tracing.Provider(ctx, cfg, log)` matches `provideTracer`'s call; `grpcMatrixConfig(di)` returns `*config.ProjectConfig` like its siblings.
