# 11. DI and entry points

How the graph is composed (Wire or fx), and how a process starts, blocks, and shuts down. ([index](README.md))

```bash
main.go                         → cmd.Execute()
cmd/root.go                     → cobra root, registers subcommands
cmd/{api,grpc,worker}.go        → one subcommand per transport
internal/app/{api,grpc,worker}.go → lifecycle: boot → serve → signal → cleanup
internal/infrastructure/di/
├── provider.go                 # hand-written providers (any DI engine)
├── app.go                      # per-entry-point bundles
├── wire.go                     # [--di=wire] provider sets + injectors
├── fx.go                       # [--di=fx]   modules + equivalents
└── fx_provider.go              # [--di=fx]   fx-specific adapters
```

## One binary, one subcommand per transport

```go
// main.go
func main() { cmd.Execute() }
```

```go
// Execute is the cobra entrypoint. It registers translations once at startup
// (so every subcommand inherits the same mapping) and exits with status 1 on
// any command error — cobra has already printed the error message, so a
// panic stack trace would be noise.
func Execute() {
    locale.NewMapping()

    root := &cobra.Command{Use: "{project}", Short: "Project description"}
    root.AddCommand(
        apiCommand(),     // [HTTP]
        grpcCommand(),    // [gRPC]
        workerCommand(),  // [worker]
    )

    if err := root.Execute(); err != nil {
        os.Exit(1)
    }
}
```

`locale.NewMapping()` is called **here**, not in an `init()` and not in DI — one call, before any subcommand runs, so translations are registered exactly once and a CLI subcommand that never builds the DI graph still gets them ([10](10-shared-packages.md#translations-register-explicitly)).

Each subcommand is four lines and delegates immediately:

```go
func apiCommand() *cobra.Command {
    return &cobra.Command{
        Use:   "api",
        Short: "Start the HTTP server",
        RunE: func(*cobra.Command, []string) error {
            return app.RunHTTPServer()
        },
    }
}
```

**`RunE`, not `Run`.** Returning the error lets cobra print it and `Execute` set the exit code — whereas calling `os.Exit(1)` inside the command would skip every pending `defer`, including the DI cleanup that closes the pool and flushes spans. That is the whole reason for this shape.

## internal/app — the lifecycle

```go
// RunHTTPServer boots the HTTP server, blocks until the OS signals shutdown or
// the server stops with an error, and returns any startup/runtime failure so
// the caller (typically a cobra RunE) can set a non-zero exit code without
// having to call os.Exit and skip pending defers.
func RunHTTPServer() error {
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    app, cleanup, err := di.InitializeHTTPServer(ctx)
    if err != nil {
        return fmt.Errorf("initialize http server: %w", err)
    }
    defer cleanup()

    log := app.Logger
    server := app.Server

    errChan := make(chan error, 1)
    go func() {
        if startErr := server.Start(); startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
            errChan <- startErr
        }
    }()

    select {
    case <-ctx.Done():
        log.Info("shutdown signal received")
    case startErr := <-errChan:
        log.Error("server stopped with error", "error", startErr)
        return startErr
    }

    log.Info("shutting down server")
    return nil
}
```

Five things this shape gets right, all of which are easy to get wrong:

1. **`signal.NotifyContext` owns shutdown.** No manual `signal.Notify` + channel; SIGINT/SIGTERM cancels ctx, and `defer cancel()` releases the handler.
2. **`Start()` runs in a goroutine** so the function can `select` on both the signal and a startup failure. A blocking `Listen` in the main goroutine can't observe SIGTERM.
3. **`http.ErrServerClosed` is not an error.** It's what a graceful shutdown returns; treating it as a failure would make every clean stop exit non-zero.
4. **`errChan` is buffered (size 1)** so the goroutine can't leak by blocking on a send after the `select` has already returned via `ctx.Done()`.
5. **`defer cleanup()` before anything can fail.** Registered immediately after `Initialize*` succeeds, so every return path — signal, startup error, panic — runs it.

`RunWorker` mirrors it exactly (with `worker.Start(ctx)` / `worker.Stop()` and `context.Canceled` as its benign error), so switching transports doesn't mean learning a second lifecycle. The file is byte-identical to the one `nova add worker` generates — a nova test pins that.

## di/app.go — per-entry-point bundles

```go
// HTTPApp bundles every dependency the HTTP entry point needs from DI. The
// Logger is the single instance wired into every service. Tracer is held here
// so DI emits the tracing provider's cleanup hook (flush spans on shutdown)
// even when no service consumes the tracer constructor argument directly.
type HTTPApp struct {
    Logger *logger.Logger
    Server *server.HTTPServer
    Tracer trace.Tracer
}
```

`GRPCApp` and `WorkerApp` are the same shape (`Worker *worker.Worker` for the latter).

Two jobs, one struct:

- **It returns the _same_ logger** DI wired into every service, so the entry point's log lines correlate with the app's.
- **`Tracer` is a field the entry point never reads.** A DI graph only builds what the requested type transitively needs; if nothing referenced the tracer, the tracing provider — and therefore its cleanup hook — would be pruned, and the last spans before exit would be lost. Listing it here is what forces the flush. Delete the field and you silently lose trace data on shutdown.

The struct is also what prunes the graph _down_: `WorkerApp` doesn't mention `server.HTTPServer`, so no HTTP provider enters the worker binary.

## di/provider.go — the hand-written providers

Emitted for **any** DI engine (only the graph file is engine-specific).

```go
// provideValidator returns a single concurrent-safe *validator.Validate that
// every handler uses to enforce `validate:"..."` tags on incoming DTOs.
// One instance per process — internal caches make repeated Struct() calls
// cheap once tags have been parsed.
func provideValidator() *validator.Validate {
    return validator.New(validator.WithRequiredStructEnabled())
}
```

```go
// provideRegistrars assembles every feature's Registrar into a slice that
// transport/http.NewRouter consumes in order. Each Registrar carries its own
// Prefix() — the slice order determines mount order, not the prefix string.
// Add new features here as they appear.
func provideRegistrars(userRegistrar *userv1.Registrar) []transporthttp.Registrar {
    return []transporthttp.Registrar{userRegistrar}
}
```

```go
// provideTracingConfig projects the app config into the tracing package's
// neutral Config struct, so tracing has no compile-time dep on config.
func provideTracingConfig(cfg *config.Config) tracing.Config
```

`provideWorkerHandlers` is the worker analogue of `provideRegistrars`. **These two slice providers are the extension points**: adding a feature means adding its constructor to a set and its instance to one of these slices.

`provideTracingConfig` exists so `infrastructure/tracing` can define its own neutral `Config` and stay reusable — a small adapter to avoid an import.

## Wire (`--di=wire`)

```go
//go:build wireinject
// +build wireinject
```

The build tag means this file is **excluded from normal builds** — `wire` reads it and generates `wire_gen.go`, which is what compiles. That's why `go vet` alone can't validate it, and why nova's test matrix stubs `wire_gen.go`.

### Sets are layered, and unexported

```go
// infraSet provides the foundational dependencies every binary needs:
// config, logger, tracing, DB, redis, and the security primitives
// (TokenService, hasher, validator). Mode-specific providers live in
// httpInfraSet / workerInfraSet so unused Transport modes don't drag in
// providers that would fail at startup (e.g. NewTokenService rejecting
// a missing public key when the worker doesn't need JWTs).
```

| Set              | Contents                                                                   |
| ---------------- | -------------------------------------------------------------------------- |
| `infraSet`       | config, logger, tracing, DB, cache, search, broker, JWT, hasher, validator |
| `httpInfraSet`   | HTTP sub-config, health checker, router, HTTP server                       |
| `grpcInfraSet`   | gRPC sub-config, gRPC server                                               |
| `workerInfraSet` | consumer factory, consumer adapter, `NewWorker`, handler slice             |
| `adapterSet`     | repositories + publisher, each with its `wire.Bind`                        |
| `usecaseSet`     | usecase constructors                                                       |
| `transportSet`   | handler, registrar, `provideRegistrars`                                    |

All **unexported** — nothing outside `di` composes them, so they can be reorganized freely.

The reason for splitting mode-specific sets is in that comment and is not cosmetic: a worker project has no JWT keys configured, and `NewTokenService` errors on a missing public key. Keeping it out of `workerInfraSet` means the worker never constructs it.

### wire.FieldsOf projects sub-configs

```go
config.Load,
wire.FieldsOf(new(*config.Config), "Log", "JWT", "Security"),
```

This is what lets every constructor take the sub-struct it actually reads (`config.DatabaseConfig`, `config.HTTPConfig`, …) instead of the whole `*Config` ([09](09-infrastructure-layer.md#the-sub-config-projection)). Nested projection works too:

```go
wire.FieldsOf(new(*config.Config), "HTTP"),
wire.FieldsOf(new(config.HTTPConfig), "LoginRateLimit"),
```

### wire.Bind connects port to implementation

```go
// Transport depends on the observability.Logger port; bind it to the
// concrete *logger.Logger here so the App bundle + infra consumers keep
// the concrete while transport stays decoupled from infrastructure/logger.
wire.Bind(new(observability.Logger), new(*logger.Logger)),

wire.Bind(new(domainsec.PasswordHasher), new(*infrasec.BcryptHasher)),
wire.Bind(new(domain.UserRepository), new(*postgresrepo.UserRepository)),
wire.Bind(new(domain.UserPublisher), new(*pubsub.Publisher)),
```

**This is where dependency inversion is actually executed.** Every `wire.Bind` is one arrow in the layer diagram: the usecase names `domain.UserRepository`, and this line is the only place that says "postgres". Swapping in the Redis cache decorator ([08](08-adapter-layer.md#adapterrepositoryredis--cache-aside-decorator)) means changing exactly one of these lines.

### The injectors

```go
// InitializeHTTPServer returns the HTTPApp bundle so the entry point gets
// the same *logger.Logger that DI wired into every service.
func InitializeHTTPServer(ctx context.Context) (*HTTPApp, func(), error) {
    panic(wire.Build(
        infraSet, httpInfraSet, adapterSet, usecaseSet, transportSet,
        wire.Struct(new(HTTPApp), "*"),
    ))
}
```

The `panic(wire.Build(…))` body is never executed — it's a declaration wire reads, replaced in `wire_gen.go`. `wire.Struct(new(HTTPApp), "*")` fills every field.

```go
// InitializeWorker returns the WorkerApp bundle for the consumer process.
// Wire only constructs providers reachable from WorkerApp's struct fields,
// so HTTP/gRPC-only providers (server.NewHTTPServer, transporthttp.NewRouter,
// health.NewChecker, transportSet) aren't dragged into the worker binary.
```

**Run `wire ./...` (or `make gen`) after editing wire.go.** Editing the sets without regenerating changes nothing about the built binary — a confusing failure mode worth internalizing.

## fx (`--di=fx`)

```go
// This file is the Uber fx counterpart to Wire's wire.go. It produces the same
// Initialize* entry points with identical (*App, func(), error) signatures, so
// the app layer (internal/app) is byte-for-byte the same whichever DI engine
// was selected. fx constructs lazily — only the providers reachable from the
// populated App fields are built, mirroring Wire's pruning of unused providers.
```

One `fx.Option` module per Wire set — `infraModule()`, `httpModule()`, `transportModule()`, `grpcModule()`, `workerModule()`, `adapterModule()`, `usecaseModule()` — composed per entry point.

```go
func InitializeHTTPServer(ctx context.Context) (*HTTPApp, func(), error) {
    var (
        log    *logger.Logger
        srv    *server.HTTPServer
        tracer trace.Tracer
    )
    fxApp := fx.New(
        fx.NopLogger,
        fx.Provide(func() context.Context { return ctx }),
        infraModule(), httpModule(), adapterModule(), usecaseModule(), transportModule(),
        fx.Populate(&log, &srv, &tracer),
    )
    if err := fxApp.Start(ctx); err != nil {
        return nil, nil, fmt.Errorf("fx: start http app: %w", err)
    }
    cleanup := func() {
        stopCtx, cancel := context.WithTimeout(context.Background(), stopTimeout)
        defer cancel()
        _ = fxApp.Stop(stopCtx)
    }
    return &HTTPApp{Logger: log, Server: srv, Tracer: tracer}, cleanup, nil
}
```

`fx.Populate` is the analogue of `wire.Struct` — and note it populates `tracer` for the same reason Wire's bundle holds it. `fx.NopLogger` silences fx's own startup chatter so the app's structured logs are the only output.

`fx_provider.go` adapts each `(instance, cleanup, error)` constructor into an fx provider with an `OnStop` hook, which is how the two engines converge on the same cleanup semantics.

```go
// stopTimeout bounds how long fx waits for every OnStop hook to finish during
// cleanup. Each underlying cleanup also enforces its own deadline, so this is a
// process-wide backstop, not the per-resource budget.
```

### Wire vs fx

|                     | Wire                     | fx                       |
| ------------------- | ------------------------ | ------------------------ |
| Resolution          | Compile time (codegen)   | Runtime (reflection)     |
| Missing dependency  | `wire` fails to generate | Error from `fxApp.Start` |
| Extra build step    | Yes — `wire ./...`       | No                       |
| Startup cost        | None                     | Reflection on boot       |
| `internal/app` code | Identical                | Identical                |

The identical `Initialize*` signatures are the point: the entry points and lifecycle don't know which engine built the graph.

## How cleanup works

Every infrastructure constructor returns its own teardown ([09](09-infrastructure-layer.md#the-instance-cleanup-error-contract)), and the DI engine composes them in **reverse construction order**:

```bash
# Each constructor returns (instance, cleanup, error):
NewPostgresDB()    → (*pgxpool.Pool,       func() { pool.Close() },              nil)
NewKafkaProducer() → (sarama.SyncProducer, func() { producer.Close() },          nil)
NewHTTPServer()    → (*HTTPServer,         func() { app.ShutdownWithContext() }, nil)
```

```bash
SIGINT/SIGTERM received
    │
    ▼
ctx cancelled → RunHTTPServer returns → defer cleanup() runs
    │
    ├── 1. HTTPServer cleanup   ← stop accepting requests, drain in-flight
    ├── 2. Kafka cleanup        ← flush pending messages, close producer
    ├── 3. Postgres cleanup     ← close connection pool
    └── 4. Tracing Shutdown     ← flush pending spans
    │
    ▼
Process exits cleanly
```

Reverse order is what makes this correct rather than just tidy: the server stops accepting requests _before_ the pool it depends on closes, so no in-flight request meets a closed pool. You get this for free — but only if every constructor that owns a resource returns its cleanup. A constructor that closes nothing leaks it, and no test will tell you.

## Adding a feature — the full checklist

For a new `order` feature on an HTTP project:

1. `domain/entity/order.go`, `domain/order.go` (port + filter) — [05](05-domain-layer.md)
2. `usecase/order/{service,dto}.go` — [06](06-usecase-layer.md)
3. `adapter/repository/postgres/order_repository.go` + `mapper/order.go`, `sqlc/query/order.sql`, a migration — [08](08-adapter-layer.md)
4. `transport/http/v1/order/{dto,assembler,handler,registrar}.go` — [07](07-transport-layer.md)
5. **`di/wire.go`**: `postgresrepo.NewOrderRepository` + its `wire.Bind` in `adapterSet`, `ordersvc.NewService` in `usecaseSet`, `orderv1.NewHandler`/`NewRegistrar` in `transportSet`
6. **`di/provider.go`**: add `orderRegistrar` to `provideRegistrars`
7. Run `wire ./...` (Wire only)

Steps 5–6 are the ones that are easy to forget, and their failure modes differ: skip 5 and wire fails to generate; skip 6 and everything compiles but the routes are never mounted.

`nova add` automates most of this — see [01](01-cli-options.md) and the manifest notes in [../CLAUDE.md](../CLAUDE.md).
