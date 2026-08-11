# 2. Project layout

The full tree `nova new` produces, and which flag gates each entry. ([index](README.md))

The tree below is the **union of every transport**, generated with everything else turned on:

```bash
nova new {project} --module=… --transport={http|grpc|worker} --http-framework=fiber \
  --database=postgres --db-driver=pgx --query=sqlc --cache=redis --search=elasticsearch \
  --queue=kafka --config=yaml --di=wire --docker --ci=github
```

Transports are **mutually exclusive** (`Transport` is a single string), so no one run produces all of `cmd/api.go`, `cmd/grpc.go` and `cmd/worker.go` — lines marked `[HTTP]`, `[gRPC]` and `[worker]` tell you which run emits what, and `[--flag]` marks the other conditionals. See [internal/generator/generator.go](../internal/generator/generator.go) (`buildFileList`) for the authoritative selection logic.

```bash
{project}/                                  # Root of the generated module
├── main.go                                 # Calls cmd.Execute()
├── go.mod
├── .env.example                            # Secrets + per-deploy endpoints only (rest lives in config/)
├── .gitignore
├── .golangci.yaml                          # Lint config (mirrors nova's own)
├── .sqlfluff                               # SQL formatter config (always emitted; `make lint` uses it for sqlc projects)
├── Dockerfile                              # [--docker]
├── docker-compose.yaml                     # [--docker] app + deps, each under a CPU/memory limit
├── Makefile
├── README.md
├── CLAUDE.md                               # Working guide for Claude Code in the new project
├── nova.yaml                               # Layout manifest consumed by `nova add`
│
├── .githooks/
│   └── pre-commit                          # fmt + lint + build; activated by `make setup`
│
├── cmd/                                    # Cobra subcommands (one per transport)
│   ├── root.go                             # Root command + subcommand wiring
│   ├── api.go                              # [HTTP]   starts the HTTP server
│   ├── grpc.go                             # [gRPC]   starts the gRPC server
│   └── worker.go                           # [worker] starts the queue consumer
│
├── internal/
│   ├── app/                                # Application lifecycle (start → signal → cleanup)
│   │   ├── api.go                          # [HTTP]   RunHTTPServer() — boot, serve, graceful shutdown
│   │   ├── grpc.go                         # [gRPC]   RunGRPCServer()
│   │   └── worker.go                       # [worker] RunWorker()
│   │
│   ├── domain/                             # Layer 1 — Enterprise Business Rules
│   │   │                                   # ⚠️ ZERO external deps; no framework/DB imports
│   │   ├── entity/
│   │   │   └── user.go                     # Pure Go structs (no json/db tags)
│   │   ├── identity/
│   │   │   ├── principal.go                # Authenticated-caller identity
│   │   │   └── context.go                  # Principal ↔ context.Context plumbing
│   │   ├── security/                       # Crypto ports (PasswordHasher)
│   │   │   └── hasher.go                   # Impls live in infrastructure/security/
│   │   ├── user.go                         # UserRepository interface + UserFilter
│   │   ├── user_audit.go                   # [worker] UserAuditRepository port
│   │   ├── user_publisher.go               # [--queue] UserPublisher port + UserCreated event
│   │   └── tx_manager.go                   # TxManager interface (usecase-facing seam)
│   │
│   ├── usecase/                            # Layer 2 — Application Business Rules
│   │   │                                   # Depends on: domain (+ infrastructure/jwt for Login)
│   │   ├── user/
│   │   │   ├── service.go                  # Orchestrates the domain
│   │   │   └── dto.go                      # Input/Output DTOs for this feature
│   │   └── useraudit/                      # [worker] audit-trail feature
│   │       ├── service.go
│   │       └── dto.go
│   │
│   ├── adapter/                            # Layer 3 — Interface Adapters (implements domain ports)
│   │   ├── repository/
│   │   │   ├── postgres/                   # [--database=postgres]
│   │   │   │   ├── tx_manager.go           # Implements domain.TxManager
│   │   │   │   ├── qx.go                   # TX-aware query executor
│   │   │   │   ├── user_repository.go      # Implements domain.UserRepository
│   │   │   │   ├── user_audit_repository.go # [worker] Implements domain.UserAuditRepository
│   │   │   │   └── mapper/
│   │   │   │       ├── user.go             # Row ↔ entity mapping (keeps repo file thin)
│   │   │   │       └── user_audit.go       # [worker]
│   │   │   ├── mysql/                      # [--database=mysql]   (same shape as postgres/)
│   │   │   ├── redis/                      # [--cache=redis]
│   │   │   │   ├── user_cache.go           # Read-through cache decorator
│   │   │   │   └── mapper/
│   │   │   │       └── user.go             # entity ↔ cache payload
│   │   │   └── elasticsearch/              # [--search=elasticsearch]
│   │   │       └── user_search.go          # Index + query users
│   │   └── pubsub/                         # [--queue]
│   │       ├── publisher.go                # Broker transport (kafka/rabbitmq variant)
│   │       ├── user_publisher.go           # Implements domain.UserPublisher
│   │       └── user_message.go             # Wire payloads for user events
│   │
│   ├── transport/                          # Delivery mechanisms — separate from adapter/
│   │   ├── http/                           # [HTTP]
│   │   │   ├── router.go                   # Root router + middleware registration + Registrar iface
│   │   │   ├── health/
│   │   │   │   └── checker.go              # /healthz (liveness) + /readyz (dependencies)
│   │   │   ├── httpwriter/                 # Framework-bound bind/write helpers
│   │   │   │   └── writer.go               # Bind / WriteJSON / WriteError for the chosen framework
│   │   │   ├── middleware/                 # Framework-specific (Fiber/Gin/Chi/Echo handlers)
│   │   │   │   ├── auth.go
│   │   │   │   ├── cors.go
│   │   │   │   ├── locale.go
│   │   │   │   ├── logging.go
│   │   │   │   ├── loginlimit.go
│   │   │   │   ├── recovery.go
│   │   │   │   └── requestid.go
│   │   │   └── v1/
│   │   │       ├── v1.go                   # Shared /api/v1 prefix for every feature registrar
│   │   │       └── user/                   # One package per feature
│   │   │           ├── handler.go          # Endpoint logic
│   │   │           ├── dto.go              # Request/response structs (json + validate tags)
│   │   │           ├── assembler.go        # usecase DTO → response mapping
│   │   │           └── registrar.go        # Route registration for this feature
│   │   ├── grpc/                           # [gRPC]
│   │   │   └── user.go                     # Service implementation
│   │   └── worker/                         # [worker]
│   │       ├── worker.go                   # Orchestrator: consumer → handler dispatch
│   │       ├── handler.go                  # Handler interface
│   │       ├── consumer.go                 # Kafka/RabbitMQ consumer
│   │       └── v1/user/                    # One package per feature
│   │           ├── handler.go
│   │           └── dto.go
│   │
│   └── infrastructure/                     # Layer 4 — Frameworks & Drivers
│       │                                   # PURPOSE: technical capabilities, no business logic
│       ├── config/
│       │   ├── config.go                   # Loader + Config struct
│       │   ├── constant.go
│       │   ├── base.yaml                   # [--config=yaml] shared defaults
│       │   ├── development.yaml            # [--config=yaml] dev overrides
│       │   └── production.yaml             # [--config=yaml] prod overrides
│       ├── database/
│       │   └── postgres.go                 # [--database=postgres] returns *pgxpool.Pool
│       ├── cache/
│       │   └── redis.go                    # [--cache=redis]       returns *redis.Client
│       ├── pubsub/
│       │   └── kafka.go                    # [--queue=kafka]       producer (+ consumer group for worker)
│       ├── search/
│       │   └── elasticsearch.go            # [--search=…]          returns the ES client
│       ├── jwt/                            # JWT TokenService — signs at login, verifies in middleware
│       │   ├── claims.go                   # (no domain interface; see 04 "Consumer owns…")
│       │   └── service.go
│       ├── logger/
│       │   └── zerolog.go                  # Implements pkg/observability.Logger
│       ├── security/
│       │   └── bcrypt.go                   # Implements domain/security.PasswordHasher
│       ├── server/
│       │   ├── http.go                     # [HTTP] HTTP server bootstrap + graceful shutdown
│       │   └── grpc.go                     # [gRPC] gRPC server bootstrap
│       ├── tracing/
│       │   └── otel.go                     # OpenTelemetry setup
│       └── di/
│           ├── provider.go                 # Hand-written providers (validator, registrars, tracing cfg)
│           ├── app.go                      # Per-entry-point bundles (HTTPApp / GRPCApp / WorkerApp)
│           ├── wire.go                     # [--di=wire] provider sets + Initialize* injectors
│           ├── fx.go                       # [--di=fx]   fx modules + Initialize* equivalents
│           └── fx_provider.go              # [--di=fx]   fx-specific provider adapters
│
├── pkg/                                    # Cross-cutting utilities (no layer affiliation)
│   ├── errors/
│   │   └── errors.go                       # AppError + wrap/classify/locale→status helpers
│   ├── httputil/
│   │   └── response.go                     # [HTTP] framework-free {success,data,error,meta} envelope
│   ├── locale/                             # i18n error codes + translations
│   │   ├── locale.go                       # Error code constants + Locale type + language ctx
│   │   ├── locale_en.go
│   │   └── locale_vi.go
│   ├── logctx/
│   │   └── logctx.go                       # Context-attached logger (With(ctx) / From(ctx))
│   └── observability/
│       └── observability.go                # Logger port — implementations live in infrastructure/
│
├── api/                                    # [HTTP]
│   └── openapi/
│       └── openapi.yaml                    # OpenAPI 3.0 spec
│
├── sqlc/                                   # [--query=sqlc]
│   ├── sqlc.yaml                           # SQLC config (pg or mysql variant)
│   ├── query/
│   │   ├── user.sql                        # Annotated SQL — SQLC generates Go from these
│   │   └── user_audit.sql                  # [worker]
│   └── migrations/
│       ├── {ts}_create_users_table.up.sql  # golang-migrate format
│       ├── {ts}_create_users_table.down.sql
│       ├── {ts}_create_user_audit_log.up.sql # [worker]
│       └── {ts}_create_user_audit_log.down.sql # [worker]
│
└── .github/                                # [--ci=github]
    ├── pull_request_template.md
    └── workflows/
        └── ci.yaml                         # Lint + test pipeline
```

## Notes on placement that differ from older versions of this spec

- HTTP responses are split in two. The **envelope** (`pkg/httputil/response.go`) is framework-free — it imports no HTTP framework and no `internal/` package, so it can live in `pkg/`. The **writer** (`internal/transport/http/httpwriter/writer.go`) binds requests and serializes that envelope through the chosen framework (`*fiber.Ctx`, `*gin.Context`, …), so it lives under `transport/http/` where the framework coupling belongs. Locale reaches both via `context.Context`, so neither imports the middleware package.
- `observability/` lives in `pkg/` as a **port** (Logger interface). The implementation lives in `internal/infrastructure/logger/`. This keeps inner layers (`usecase`, `adapter/repository`) able to depend on the logger via `logctx.From(ctx)` without importing infrastructure.
- `jwt/` lives in `infrastructure/`, not `adapter/`. Signing and verifying are pure computation, and verification is called only by the auth middleware — so per [04](04-placement-rationale.md#if-only-transport-calls-it--no-domain-interface) no domain interface is needed for the middleware path, and the package belongs with other technical bootstrapping.
- `tx_manager.go` is split: the interface lives in `domain/`, the postgres implementation in `adapter/repository/postgres/`. Usecases inject `domain.TxManager` and call it without knowing which DB is wired in.
- `di/provider.go` and `di/app.go` are emitted for **any** transport; only the graph file itself is DI-specific (`wire.go` vs `fx.go` + `fx_provider.go`).
