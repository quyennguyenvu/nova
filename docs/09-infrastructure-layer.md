# 9. Infrastructure layer

Layer 4 — frameworks and drivers. **Technical capabilities, no business logic.** Connection factories, server bootstrap, config loading, logging, crypto, tracing, DI. ([index](README.md))

```bash
infrastructure/
├── config/            # Config struct + Load() (embedded YAML + env overrides)
├── database/          # → *pgxpool.Pool / *sql.DB
├── cache/             # → *cache.Client over redis.UniversalClient
├── pubsub/            # → sarama.SyncProducer / ConsumerGroup / *amqp.Connection
├── search/            # → *search.Client
├── server/            # lifecycle: Start + graceful shutdown
├── logger/            # implements pkg/observability.Logger
├── jwt/               # TokenService — signs at login, verifies in middleware
├── security/          # implements domain/security.PasswordHasher
├── tracing/           # OpenTelemetry provider + Shutdown
└── di/                # wires everything ([11](11-di-and-entrypoints.md))
```

## The `(instance, cleanup, error)` contract

Almost every constructor here returns three values:

```go
func NewPostgresDB(cfg config.DatabaseConfig, log *logger.Logger) (*pgxpool.Pool, func(), error)
func New(cfg config.RedisConfig, log *logger.Logger) (*Client, func(), error)
func NewKafkaProducer(cfg config.KafkaConfig, log *logger.Logger) (sarama.SyncProducer, func(), error)
func NewHTTPServer(_ context.Context, cfg config.HTTPConfig, …) (*HTTPServer, func(), error)
```

**Each component owns its own shutdown.** No `Close()` on a domain port, no central teardown function listing every resource. Wire composes the cleanups automatically and runs them in reverse construction order; fx's lifecycle hooks do the same ([11](11-di-and-entrypoints.md#how-cleanup-works)). Adding a dependency therefore cannot leak it: if it needs teardown, its constructor returns the hook and the graph picks it up.

The other consistent choice: **constructors verify connectivity**. Postgres pings, Elasticsearch pings, RabbitMQ declares its exchange. A misconfigured dependency fails at startup with a clear error rather than on the first request in production. Redis is the one exception: it pings, logs `redis: ping failed, starting degraded` and returns the client anyway, because a cache outage is survivable — reads fall through to the database and readiness reports `redis: fail` until it is back.

## config/ — one struct, three layers of value

```go
// Config holds all application configuration.
//
// Defaults live outside this struct:
//   - internal/infrastructure/config/base.yaml — operational tuning
//     (<APP_ENV>.yaml is layered on top when APP_ENV is "development" or "production")
//   - .env.example — secrets and per-deployment endpoints
//
// At runtime every field can be overridden by its env var.
```

```go
//go:embed *.yaml
var configFS embed.FS

// Load resolves configuration from the embedded base.yaml, overlays the
// per-APP_ENV file, and applies env-var overrides (a ./.env file first, then
// the real environment). Embedding removes the runtime dependency on cwd for
// the YAML — the binary is self-contained.
func Load() (*Config, error) {
    if err := loadDotEnv(); err != nil {
        return nil, errors.Wrap(err, "config .env")
    }

    var cfg Config
    if err := readEmbedded("base.yaml", &cfg); err != nil {
        return nil, errors.Wrap(err, "config base")
    }

    switch env := os.Getenv("APP_ENV"); env {
    case "", EnvLocal:
    case EnvDev, EnvProd:
        …  // overlay <APP_ENV>.yaml
    default:
        return nil, fmt.Errorf("unknown APP_ENV %q (valid: %s, %s, %s)", env, EnvLocal, EnvDev, EnvProd)
    }

    if err := cleanenv.ReadEnv(&cfg); err != nil {
        return nil, errors.Wrap(err, "config env vars")
    }
    if cfg.Database.User == "" {   // [--database]
        return nil, errors.New("config database.user (DB_USER) is required")
    }
    …
    return &cfg, nil
}
```

Precedence, lowest to highest: **`base.yaml` → `<APP_ENV>.yaml` → `./.env` → environment variables**. `loadDotEnv` reads `./.env` with `godotenv.Read` and sets only the keys that are not already in the environment and have a value, treating a missing file as fine — so `cp .env.example .env && go run main.go api` works on the host, while a container keeps injecting its environment and ships no `.env`. An empty `KEY=` line means "keep the YAML default": cleanenv would otherwise apply the empty string and, for a list such as `CORS_ALLOW_ORIGINS`, erase the development origins. After the environment is applied, `normalizeLists` trims every list entry and drops empty ones, because cleanenv splits on `,` without trimming and `"a, b"` would otherwise reach the router as `" b"`.

Four consequences worth understanding before you change this:

- **`Load()` takes no path.** The YAML is `//go:embed`-ed, so the binary has no runtime dependency on the working directory — the same image runs from `/`, from a scratch container, from a Kubernetes job. The cost is that a config change is a rebuild, which is the intended trade for a service image.
- **`APP_ENV` is validated against a whitelist** (`EnvLocal`, `EnvDev`, `EnvProd` from `constant.go`; `local` or unset loads base only). A typo'd `APP_ENV=prod` is an error at startup, because the alternative — silently running base defaults such as `ssl_mode: disable` in what the operator believes is production — is the worse failure.
- **`DB_USER` is required.** With it empty the driver falls back to the OS user, which surfaces as a permissions error on the database; `Load` fails with the real cause instead.
- **`shutdown_timeout` at the top level** budgets the whole cleanup chain; the fx engine stops its hooks within it and logs the budget when a stop overruns ([11](11-di-and-entrypoints.md#how-cleanup-works)).
- **The split between YAML and `.env` is by _nature_, not convenience.** Operational tuning (timeouts, pool sizes, bcrypt cost, sample ratio) lives in YAML and is reviewed in code review. Secrets and per-deployment endpoints live in the environment. That's why `JWTConfig.PrivateKey` and `PublicKey` carry **only** an `env:` tag and no YAML tag — they cannot be committed by accident.
- **The struct is conditional.** `HTTP`, `GRPC`, `Database`, `Redis`, `Elasticsearch`, `Kafka`, `RabbitMQ`, `Worker` sections only exist when the corresponding flag was set, so a worker project's config has no HTTP block to misconfigure. `GRPC` carries `port`, `shutdown_timeout` and `reflection` (service discovery for `grpcurl`; base `true`, `production.yaml` `false`).

### The sub-config projection

Consumers take the **sub-struct** they need, never the whole `*Config`:

```go
func NewPostgresDB(cfg config.DatabaseConfig, …)
func NewBcryptHasher(cfg config.SecurityConfig) *BcryptHasher
func NewHTTPServer(_ context.Context, cfg config.HTTPConfig, …)
func LoginRateLimit(cfg config.LoginRateLimitConfig) fiber.Handler
```

Wire projects these with `wire.FieldsOf` ([11](11-di-and-entrypoints.md)). The payoff is that a component's signature documents exactly what it reads, and no constructor can quietly start depending on an unrelated section. `transport/http.NewRouter` is the one exception — it takes `*config.Config` because it configures the framework from `HTTP` _and_ branches on `AppEnv`.

### Defaults that carry a warning

```yaml
cors:
  # Production deployments MUST set CORS_ALLOW_ORIGINS (comma-separated env
  # var) to enumerate every allowed origin. Empty here means EVERY
  # cross-origin request is rejected — browsers will see no
  # Access-Control-Allow-Origin header and reject preflights. Never use "*".
  allow_origins: []
```

The insecure-by-default trap is inverted here: an unconfigured deployment rejects all cross-origin traffic (visibly broken) rather than accepting all of it (invisibly unsafe). The middleware is not installed at all when the list is empty, which is what stops fiber and echo from falling back to their `*` default. `development.yaml` ships localhost origins so dev tooling works without editing base.

```yaml
  # Deadline on every request's context; keep it below write_timeout so a
  # stalled dependency fails the request in the envelope, not at the socket.
  request_timeout: 20s
  # Reverse proxies (CIDR or IP) whose X-Forwarded-For is believed. Empty means
  # the app faces clients directly: the TCP peer is the client IP and forwarded
  # headers are ignored, so a client cannot spoof its address to the login
  # limiter or the access log. Set SERVER_TRUSTED_PROXIES per deployment.
  trusted_proxies: []
```

`trusted_proxies` follows the same inversion: empty means forwarded headers are ignored and the TCP peer is the client. List the load balancers' CIDRs per deployment through `SERVER_TRUSTED_PROXIES`; `config.Load` rejects anything that is not an IP or CIDR, so a typo fails at boot rather than silently trusting nobody. `request_timeout` is the deadline every request's context carries (`middleware.Timeout`); keep it below `write_timeout` so a stalled dependency fails in the envelope before the socket does.

`tracing.endpoint: ""` follows the same idea from the other direction — empty means a no-op tracer, so the app runs with no collector instead of failing.

[13](13-cross-cutting.md) has the full config/secrets rules.

## database/ — connection factory

```go
// Build the DSN via net/url so credentials are percent-encoded.
// A raw fmt.Sprintf corrupts the URL — and breaks pgxpool.ParseConfig —
// the moment the password contains '@', ':', '/', '?', '#' or a space.
u := url.URL{
    Scheme:   "postgres",
    User:     url.UserPassword(cfg.User, cfg.Password),
    Host:     net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
    Path:     cfg.Name,
    RawQuery: "sslmode=" + cfg.SSLMode,
}
```

**Config carries discrete typed fields** (`Host`, `Port`, `User`, `Password`, `Name`, `SSLMode`), not a single `DATABASE_URL`, and the DSN is assembled escape-safely. `net.JoinHostPort` handles IPv6 literals; `url.UserPassword` percent-encodes the credentials. The mysql variant uses `mysql.Config.FormatDSN()` for the same reason. A `fmt.Sprintf` DSN is the single most common way a generated project breaks on a rotated password.

Pool limits (`MaxConns`, `MinConns`, `MaxConnLifetime`, `MaxConnIdleTime`) come from config, and the factory pings under `cfg.ConnectTimeout` before returning. The mysql variant has the same surface: `tls` (`false` | `true` | `skip-verify` | `preferred`; `skip-verify` is the counterpart of `sslmode=require` and is what `production.yaml` sets), `max_conn_lifetime`, `max_conn_idle_time`, and `connect_timeout`, which bounds both the driver's dial and the startup ping.

## cache/ and search/ — thin wrappers, deliberately

```go
// Client is a thin wrapper so callers depend on an interface, not a concrete type.
// Both *redis.Client and *redis.ClusterClient satisfy redis.UniversalClient.
type Client struct {
    redis.UniversalClient
}

// New is the single factory entry-point — picks mode from config.
func New(cfg config.RedisConfig, log *logger.Logger) (*Client, func(), error)
```

The wrapper type is what makes single/sentinel/cluster mode a **config** choice rather than a code change: `cfg.Mode` selects the constructor, every caller depends on `*cache.Client`. The embedded interface means the full redis API is still available without re-exporting methods.

`search.Client` is the same pattern over `*elasticsearch.Client` — "the search counterpart to infrastructure/cache". Both ping on construct. Adapters take these wrapper types ([08](08-adapter-layer.md)).

## pubsub/ — producer and consumer group

`NewKafkaProducer` returns a `sarama.SyncProducer` with `RequiredAcks: WaitForAll` and `Return.Successes: true` — a synchronous, fully-acknowledged producer, because the publish path is best-effort-with-logging in the usecase and a silently-dropped ack would defeat that ([06](06-usecase-layer.md#register--check-hash-create-publish)). It dials the brokers at startup on purpose, unlike Redis: a publisher that cannot reach its broker is a misconfiguration, not a degraded mode, and the compose file waits on the broker's healthcheck before starting the app. Make it lazy only if you accept that every publish until the broker is up is a logged failure.

`NewKafkaConsumerGroup` is worker-only and **fails fast** on missing config:

```go
if cfg.ConsumerGroupID == "" {
    return nil, nil, fmt.Errorf("kafka: consumer_group_id is required for worker mode")
}
```

```go
// OffsetOldest replays from the earliest available offset on first run;
// after the consumer group has committed offsets, it resumes from there.
saramaCfg.Consumer.Offsets.Initial = sarama.OffsetOldest
```

`OffsetOldest` only applies to a group with no committed offsets — so a fresh deployment replays history rather than silently skipping messages published before it started. Group membership (not manual partition assignment) is what makes replicas rebalance automatically.

The factory hands the group to `transport/worker.kafkaConsumer`, which owns `Consume()`/`Close()` — the factory does no consuming, which is what keeps it infrastructure.

`NewRabbitMQConnection` dials and returns the cleanup that closes the connection (and with it every channel); `NewRabbitMQChannel` opens the worker's consuming channel with its own cleanup. `RabbitMQConfig` carries `exchange`, shared by the publisher and the worker so what one publishes the other can bind (the default, `events`, is the same in every nova project, so a publisher and a worker generated separately meet without edits), plus `queue` and `prefetch`, read only in worker mode. Kafka topic names are not configuration: they are wire contracts owned by the handlers and the publisher. The `worker` section (`handler_timeout`, `max_attempts`, `retry_backoff`) is projected onto `transport/worker.Policy` by `provideWorkerPolicy` ([07](07-transport-layer.md#one-policy-three-outcomes)).

## server/ — lifecycle only

```go
// HTTPServer is the lifecycle wrapper around a pre-built fiber app. The app
// itself (middleware, routes, healthz) is constructed in transport/http.NewRouter.
```

The split is strict: `transport/http` builds the app, `infrastructure/server` binds the port and shuts it down. `Start()` is one line (`app.Listen(addr)`); the cleanup hook is where the care is:

```go
// NewHTTPServer wraps app for graceful shutdown. The shutdown context is
// rooted at context.Background(), not the request-lifecycle ctx — by the time
// cleanup runs the parent ctx is already SIGINT-cancelled, which would
// collapse the configured ShutdownTimeout to zero and force a hard close.
cleanup := func() {
    cleanupCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
    defer cancel()
    …
}
```

That comment describes a bug this code exists to avoid: deriving the shutdown context from the same context the signal handler cancels means `ShutdownTimeout` is already expired when you use it, and every in-flight request is killed mid-response. Rooting it at `Background()` is what makes `shutdown_timeout: 10s` mean anything.

## logger/ — implements pkg/observability.Logger

```go
var _ observability.Logger = (*Logger)(nil)

// Logger is an instance-based structured logger. Pass it through DI; never use a package global.
type Logger struct {
    zl zerolog.Logger
}
```

The port is in `pkg/observability` so inner layers can log via `logctx.From(ctx)` without importing infrastructure ([10](10-shared-packages.md)). This is one of the two ports implemented outside `adapter/` — logging is cross-cutting, not a business operation ([03](03-architecture-rules.md#ports-implemented-outside-adapter)).

`New(cfg *config.Config)` takes the whole config: level and format come from `cfg.Log`, every line is stamped with `service`, `version` and `env` from the app fields, timestamps are RFC 3339 with nanoseconds, and the first line a process emits is `config loaded` with `cfg.Summary()` — every non-secret setting it runs with. `production.yaml` keeps `log.level: info` so access and lifecycle lines survive in production.

Two details that exist because wrapping a logger usually breaks its caller reporting:

```go
// callerSkip counts the wrapper frames between a caller's logging call and the
// zerolog Event.Caller() invocation inside emit: emit → Logger.<Level> → the
// real call site. Without it every line would report zerolog.go (the wrapper)
// as the source.
const callerSkip = 2

// shortCaller renders the caller as "package/file:line" (the directory holding
// the file + the file), e.g. "redis/user_cache.go:52" — enough to locate the
// source without absolute-path noise.
```

If you add a method that calls `emit` at a different depth, `callerSkip` must change with it, or every log line from that method points at the logger.

`format: "console"` swaps in `zerolog.ConsoleWriter` for local development; `json` is the default and what production should ship. `NewWithWriter` exists so tests can assert on log output.

## jwt/ — RS256 signer and verifier

```go
// Package jwtsec issues and verifies access tokens using RS256 (RSA-SHA-256,
// asymmetric). The private key signs at login; the public key verifies on
// every request. In a microservices fleet the token-issuing service holds
// both keys (signs + verifies) while downstream services configure only the
// public key and run verify-only — Sign errors when no private key is set.
```

Note the package name is `jwtsec`, not `jwt` — the directory is `jwt/` but the package avoids colliding with `github.com/golang-jwt/jwt`, which is why every import aliases it `jwtsec "…/internal/infrastructure/jwt"`.

**RS256 rather than HS256 is the load-bearing choice.** With a shared HMAC secret, every service that can _verify_ a token can also _forge_ one. Asymmetric keys mean only the issuer holds the signing key, and rotation is: publish the new public key fleet-wide, _then_ start signing with the new private key. Keys are base64-encoded PEM so they survive single-line env vars, and are parsed once at startup so the request path touches only in-memory keys.

The surface is deliberately small — `Sign`, `ParseAndVerifyAccessToken`, `AccessTokenTTL` — so the algorithm can be replaced without touching middleware or the usecase.

### Verification returns a generic 401, always

```go
// ParseAndVerifyAccessToken validates tokenStr against the public key and
// projects its claims into a UserPrincipal. Returns *AppError(401) on any
// verification failure … The client always gets a generic localized 401
// (locale.Unauthorized) — the specific reason is logged server-side, never
// returned.
```

Telling a caller _why_ their token failed ("expired" vs "bad signature") is an oracle. The distinction is preserved in the logs instead — and the log severity is triaged rather than uniform:

```go
// logVerifyFailure routes a token-parse failure by operational severity rather
// than logging every one. At millions of logins the routine rejections —
// expired tokens, malformed/garbage input from buggy or stale clients — are
// constant background noise and carry no signal, so they go to Debug (silent at
// the default Info level). An invalid signature or unverifiable token is the
// opposite: it means either the public key has drifted across the fleet (an ops
// bug) or someone is presenting a forged token, so it goes to Warn.
```

`classifyParseError` maps golang-jwt v5's sentinel errors (`ErrTokenExpired`, `ErrTokenSignatureInvalid`, …) to a stable `reason` field, and treats anything unrecognised as anomalous — so a new failure mode surfaces as a Warn instead of hiding in Debug. Issuer and audience are enforced by the parser options (`jwt.WithIssuer`, `jwt.WithAudience`) and classified as anomalous too: a token our key signed but another environment minted is replay or misconfiguration, not routine noise.

`formatSubject` stringifies the user ID for the `sub` claim, because "JSON numbers in JWT can lose precision in some libraries".

`AccessTokenClaims` embeds `jwt.RegisteredClaims` and adds `userId`/`email`/`name` — the same fields as `identity.UserPrincipal`, extended together. The registered set stays authoritative for lifecycle; custom claims are convenience.

## security/ — implements domain/security.PasswordHasher

```go
// Package security holds concrete implementations of the domain/security
// ports. Bcrypt is the default password hasher — swap implementations here
// without touching domain or usecase code.

var _ security.PasswordHasher = (*BcryptHasher)(nil)
```

The second port implemented outside `adapter/`: `Hash(plain string) (string, error)` is pure computation — no external system, no `context.Context`, nothing to map ([03](03-architecture-rules.md#ports-implemented-outside-adapter)).

```go
// NewBcryptHasher reads the cost from config, clamping invalid values to
// bcrypt.DefaultCost (10). Cost must be in [bcrypt.MinCost, bcrypt.MaxCost].
func NewBcryptHasher(cfg config.SecurityConfig) *BcryptHasher {
    cost := cfg.BcryptCost
    if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
        cost = bcrypt.DefaultCost
    }
    return &BcryptHasher{cost: cost}
}
```

Clamping rather than erroring means a misconfigured `BCRYPT_COST=0` yields a safe default instead of a boot failure — and, critically, never yields cost 0. `Verify` collapses every mismatch to `errors.ErrUnauthorized`, which is what lets `Login` return a uniform `InvalidCredentials` ([06](06-usecase-layer.md#login--uniform-failure-uniform-timing)).

## tracing/ — OpenTelemetry provider

```go
// Package tracing initialises the OpenTelemetry SDK and returns a tracer
// plus a shutdown hook. Use otelhttp / otelgrpc instrumentation at the
// transport layer, and open spans inside usecases/repositories:
//
//	ctx, span := tracer.Start(ctx, "UserUsecase.Register")
//	defer span.End()
//	if err != nil { span.RecordError(err); span.SetStatus(codes.Error, err.Error()); return err }
//
// The Logging middleware copies the trace_id onto every log line, so a single
// click in Jaeger/Tempo jumps from log → trace without extra plumbing.
```

```go
// If cfg.Endpoint is empty, a no-op tracer is returned so the app keeps working
// without a collector (e.g. local dev without the otel stack running).
if cfg.Endpoint == "" {
    otel.SetTextMapPropagator(newPropagator())
    return otel.Tracer(cfg.ServiceName), func(context.Context) error { return nil }, nil
}
```

Note the propagator is still installed in the no-op path, so inbound `traceparent` headers keep flowing through the service even with export disabled — you don't break a distributed trace by running one hop without a collector.

`Shutdown` flushes pending spans; without calling it, the last spans before exit are lost. `SampleRatio` defaults to 0.1 in `base.yaml`.

`Provider(ctx, cfg, log)` wraps `New` for DI and logs a flush failure at shutdown. The provider alone creates no spans: the HTTP routers install the framework's OTel middleware first (`otelfiber`, `otelgin`, `otelchi`, `otelecho`) and `NewGRPCServer` adds the `otelgrpc` stats handler, so every request has a server span and `trace_id` on its log lines.

## Nothing here knows about business rules

The test for whether a new package belongs in this layer:

| It does…                                       | Layer               |
| ---------------------------------------------- | ------------------- |
| Creates a client/pool/producer                 | **infrastructure/** |
| Uses that client to fulfil a domain port       | **adapter/**        |
| Binds a port, starts/stops a process           | **infrastructure/** |
| Translates a driver error into a domain error  | **adapter/**        |
| Satisfies a port with pure computation, no I/O | **infrastructure/** |

The full guide, with the flowchart, is in [03](03-architecture-rules.md#31-adapter-vs-infrastructure-the-decision-guide).
