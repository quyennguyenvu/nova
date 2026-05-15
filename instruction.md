# Go Clean Architecture Project Generator

Go CLI tool called "nova" that generates a new Go project with Clean Architecture. The tool should have interactive prompts and generate a complete, production-ready project structure.

## Table of Contents

- [Go Clean Architecture Project Generator](#go-clean-architecture-project-generator)
  - [Table of Contents](#table-of-contents)
  - [Requirements](#requirements)
    - [1. Interactive Prompts (using promptui or survey library)](#1-interactive-prompts-using-promptui-or-survey-library)
    - [2. Project Structure to Generate](#2-project-structure-to-generate)
    - [3. Clean Architecture Principles to Enforce](#3-clean-architecture-principles-to-enforce)
    - [3.1. Adapter vs Infrastructure: The Decision Guide](#31-adapter-vs-infrastructure-the-decision-guide)
      - [**ADAPTER Layer** - "Implements Business Interfaces"](#adapter-layer---implements-business-interfaces)
      - [**INFRASTRUCTURE Layer** - "Provides Technical Capabilities"](#infrastructure-layer---provides-technical-capabilities)
      - [**Decision Flowchart**](#decision-flowchart)
      - [**Concrete Examples**](#concrete-examples)
      - [**The Key Insight**](#the-key-insight)
    - [3.2. Placement Rationale — Why Things Live Where They Do](#32-placement-rationale--why-things-live-where-they-do)
      - [Why `middleware/` is under `transport/http/`, NOT `transport/`](#why-middleware-is-under-transporthttp-not-transport)
      - [Why `httputil/` is under `transport/http/`, NOT `pkg/`](#why-httputil-is-under-transporthttp-not-pkg)
      - [Why `pkg/locale/` is in `pkg/`, not `internal/`](#why-pkglocale-is-in-pkg-not-internal)
      - [Why `transport/` is a sibling of `adapter/`, not inside it](#why-transport-is-a-sibling-of-adapter-not-inside-it)
      - [`pkg/` vs `internal/shared/` — do you actually need `pkg/`?](#pkg-vs-internalshared--do-you-actually-need-pkg)
    - [3.3. Where to Put Adapter Interfaces — Consumer Owns the Interface](#33-where-to-put-adapter-interfaces--consumer-owns-the-interface)
      - [If the usecase calls it → interface in `domain/`](#if-the-usecase-calls-it--interface-in-domain)
      - [If only transport calls it → NO domain interface](#if-only-transport-calls-it--no-domain-interface)
      - [Decision flowchart](#decision-flowchart-1)
      - [Summary table](#summary-table)
      - [File layout for `domain/` with adapter interfaces](#file-layout-for-domain-with-adapter-interfaces)
    - [4. Code Templates to Generate](#4-code-templates-to-generate)
      - [Domain Layer](#domain-layer)
      - [internal/domain/entity/user.go](#internaldomainentityusergo)
      - [internal/domain/valueobject/ — What are Value Objects?](#internaldomainvalueobject--what-are-value-objects)
      - [internal/domain/valueobject/email.go](#internaldomainvalueobjectemailgo)
      - [internal/domain/valueobject/money.go](#internaldomainvalueobjectmoneygo)
      - [internal/domain/user.go — Repository Interface + Filter (one file per aggregate)](#internaldomainusergo--repository-interface--filter-one-file-per-aggregate)
      - [internal/domain/service/pricing_service.go](#internaldomainservicepricing_servicego)
      - [internal/domain/event/event.go](#internaldomaineventeventgo)
      - [internal/domain/event/publisher.go](#internaldomaineventpublishergo)
      - [Use Case Layer](#use-case-layer)
      - [internal/usecase/pricing/service.go (Domain Service IMPLEMENTATION)](#internalusecasepricingservicego-domain-service-implementation)
      - [How domain services are USED (injected into use cases, never called from handlers)](#how-domain-services-are-used-injected-into-use-cases-never-called-from-handlers)
      - [internal/usecase/user/service.go](#internalusecaseuserservicego)
      - [internal/usecase/user/dto.go](#internalusecaseuserdtogo)
      - [Transport Layer](#transport-layer)
      - [internal/transport/http/v1/user/ — Per-Feature Package](#internaltransporthttpv1user--per-feature-package)
      - [internal/transport/http/v1/user/dto.go](#internaltransporthttpv1userdtogo)
      - [internal/transport/http/v1/user/assembler.go](#internaltransporthttpv1userassemblergo)
      - [internal/transport/http/v1/user/handler.go](#internaltransporthttpv1userhandlergo)
      - [internal/transport/http/v1/user/router.go](#internaltransporthttpv1userroutergo)
      - [internal/transport/http/v1/registrar.go](#internaltransporthttpv1registrargo)
      - [internal/transport/cronjob/ — Cron Job Handlers](#internaltransportcronjob--cron-job-handlers)
      - [Shared Packages (pkg/)](#shared-packages-pkg)
      - [pkg/locale/locale.go — Error Codes + Locale Type](#pkglocalelocalego--error-codes--locale-type)
      - [pkg/locale/locale_en.go — English Translations](#pkglocalelocale_engo--english-translations)
      - [pkg/locale/locale_vi.go — Vietnamese Translations](#pkglocalelocale_vigo--vietnamese-translations)
      - [pkg/locale/resolve.go — Translate Error to User's Language](#pkglocaleresolvego--translate-error-to-users-language)
      - [Middleware \& HTTP Utilities](#middleware--http-utilities)
      - [internal/transport/http/middleware/locale.go — Language Middleware](#internaltransporthttpmiddlewarelocalego--language-middleware)
      - [internal/transport/http/httputil/error_parser.go — Locale Error → HTTP Response](#internaltransporthttphttputilerror_parsergo--locale-error--http-response)
      - [Infrastructure Layer](#infrastructure-layer)
      - [internal/infrastructure/server/http.go](#internalinfrastructureserverhttpgo)
      - [internal/infrastructure/config/config.go](#internalinfrastructureconfigconfiggo)
      - [internal/infrastructure/database/postgres.go — PostgreSQL Connection Pool](#internalinfrastructuredatabasepostgresgo--postgresql-connection-pool)
      - [Adapter Layer](#adapter-layer)
      - [internal/adapter/repository/postgres/tx_manager.go — Transaction Manager](#internaladapterrepositorypostgrestx_managergo--transaction-manager)
      - [internal/adapter/repository/postgres/qx.go — TX-Aware Query Executor](#internaladapterrepositorypostgresqxgo--tx-aware-query-executor)
      - [SQLC Configuration \& SQL](#sqlc-configuration--sql)
      - [sqlc/sqlc.yaml (SQLC Configuration)](#sqlcsqlcyaml-sqlc-configuration)
      - [sqlc/schema/001_users.sql](#sqlcschema001_userssql)
      - [sqlc/query/user.sql](#sqlcqueryusersql)
      - [internal/adapter/repository/postgres/user_repository.go (uses SQLC)](#internaladapterrepositorypostgresuser_repositorygo-uses-sqlc)
      - [internal/infrastructure/pubsub/kafka.go — Kafka Connection Factory](#internalinfrastructurepubsubkafkago--kafka-connection-factory)
      - [internal/adapter/pubsub/message.go — Wire-format Message DTOs](#internaladapterpubsubmessagego--wire-format-message-dtos)
      - [internal/adapter/pubsub/kafka_publisher.go — Kafka EventPublisher Implementation](#internaladapterpubsubkafka_publishergo--kafka-eventpublisher-implementation)
      - [Dependency Injection \& App Lifecycle](#dependency-injection--app-lifecycle)
      - [internal/infrastructure/di/wire.go](#internalinfrastructurediwirego)
      - [internal/app/server.go — Server Lifecycle](#internalappservergo--server-lifecycle)
      - [Entry Points](#entry-points)
      - [main.go — Single Entry Point](#maingo--single-entry-point)
      - [cmd/root.go — Cobra Root Command](#cmdrootgo--cobra-root-command)
      - [cmd/api.go — API Subcommand](#cmdapigo--api-subcommand)
      - [Build \& Deploy](#build--deploy)
      - [Makefile](#makefile)
      - [docker-compose.yaml](#docker-composeyaml)
    - [5. CLI Usage](#5-cli-usage)
    - [6. Key Features to Include](#6-key-features-to-include)
  - [Quick Start (Simple Version)](#quick-start-simple-version)

## Requirements

### 1. Interactive Prompts (using promptui or survey library)

Ask the user for:

- Project name / Go module name
- Transport layer: HTTP only / gRPC only / Both
- HTTP framework (if HTTP selected): Fiber / Gin / Chi / Echo / net/http
- gRPC: with or without gRPC-Gateway
- Database: PostgreSQL / MySQL / SQLite / MongoDB / None
- Database approach (if SQL selected): pgx / sqlx / GORM / database/sql
- Query generation: SQLC / Raw SQL / GORM
- Cache: Redis / In-memory (bigcache) / None
- Message queue: Kafka / RabbitMQ / NATS / None
- Configuration: YAML / TOML / Environment variables only
- Dependency Injection: Google Wire / Uber fx / Manual
- Include Docker setup: Yes / No
- Include CI/CD (GitHub Actions): Yes / No
- Include Makefile: Yes / No

### 2. Project Structure to Generate

```bash
{project}/
├── main.go                                 # Single entry point — calls cmd.Execute()
├── cmd/
│   ├── root.go                             # Cobra root command + subcommand registration
│   ├── api.go                              # "api" subcommand → starts HTTP server
│   ├── grpc.go                             # "grpc" subcommand → starts gRPC server (if selected)
│   └── cron.go                             # "cron" subcommand → runs scheduled jobs
│
├── internal/
│   │
│   ├── app/                                # Application lifecycle (start → signal → cleanup)
│   │   ├── server.go                       # RunServer() — shared lifecycle for HTTP/gRPC
│   │   └── cron.go                         # RunCronjob() — one-shot job runner
│   │
│   ├── domain/                             # Layer 1: Enterprise Business Rules (innermost)
│   │   │                                   # ⚠️  ZERO external dependencies allowed
│   │   ├── entity/                         # Business entities - pure Go structs
│   │   │   └── user.go
│   │   ├── user.go                         # UserRepository interface + UserFilter + UserListResult
│   │   ├── order.go                        # OrderRepository interface + OrderFilter
│   │   ├── service/                        # Domain service INTERFACES (business rules/computation ONLY)
│   │   │   └── pricing_service.go          # Cross-entity business RULES (e.g. pricing, eligibility)
│   │   ├── event/                          # Domain events + event publisher port
│   │   │   ├── event.go                    # Event types (UserCreated, OrderPlaced, etc.)
│   │   │   └── publisher.go                # EventPublisher interface (Kafka/RabbitMQ/NATS)
│   │   └── valueobject/                    # Value objects (Email, Money, etc.)
│   │       └── email.go
│   │
│   ├── usecase/                            # Layer 2: Application Business Rules
│   │   │                                   # Depends on: domain only
│   │   └── user/
│   │       ├── service.go                  # Use case implementation (orchestrates domain)
│   │       └── dto.go                      # Input/Output DTOs for this use case
│   │
│   ├── adapter/                            # Layer 3: Interface Adapters
│   │   │                                   # Depends on: domain, usecase
│   │   │                                   # PURPOSE: Implements domain interfaces
│   │   │
│   │   ├── repository/                     # Repository IMPLEMENTATIONS
│   │   │   ├── postgres/                   # Implements domain.UserRepository
│   │   │   │   ├── user_repository.go
│   │   │   │   └── dbgen/                  # SQLC generated code (auto-generated)
│   │   │   │       ├── db.go               # DBTX interface
│   │   │   │       ├── models.go           # Generated structs from schema
│   │   │   │       └── user.sql.go         # Generated query methods
│   │   │   └── cache/                      # Cache repository decorator
│   │   │       └── user_cache.go
│   │   │
│   │   ├── pubsub/                         # Event publisher IMPLEMENTATIONS
│   │   │   ├── message.go                  # Wire-format message DTOs (json tags live HERE)
│   │   │   └── kafka_publisher.go          # Implements event.EventPublisher
│   │   │
│   │   ├── jwt/                            # JWT authentication adapter (if selected)
│   │   │   └── jwt.go                      # JWT generation and validation
│   │   │
│   │   └── external/                       # External service adapters (calls other APIs)
│   │       └── payment_gateway.go          # Implements domain.PaymentGateway interface
│   │
│   ├── transport/                          # Delivery mechanisms (HTTP, gRPC, cronjob)
│   │   │                                   # Each transport owns its own middleware/utils.
│   │   │                                   # Nothing HTTP-specific leaks to sibling transports.
│   │   │
│   │   ├── http/
│   │   │   ├── app.go                      # Fiber app factory (middleware registration)
│   │   │   │
│   │   │   ├── middleware/                 # ← HTTP-specific (Fiber handlers).
│   │   │   │   ├── auth.go                 #    gRPC would have grpc/interceptor/ instead.
│   │   │   │   ├── locale.go               #    Kept under http/ because middleware is a
│   │   │   │   ├── logging.go              #    Fiber/net-http concept, NOT a gRPC one.
│   │   │   │   └── recovery.go
│   │   │   │
│   │   │   ├── httputil/                   # ← HTTP-specific utilities.
│   │   │   │   └── error_parser.go         #    Imports Fiber + middleware.LangFromCtx.
│   │   │   │                               #    Can't be pkg/ (imports internal), not
│   │   │   │                               #    reusable across transports.
│   │   │   │
│   │   │   ├── dto/                        # ← Shared HTTP DTOs (pagination, filters).
│   │   │   │   └── pagination.go           #    gRPC uses different pagination (page tokens).
│   │   │   │
│   │   │   └── v1/                         # API version 1
│   │   │       ├── registrar.go            # Registers all v1 feature routes
│   │   │       └── user/                   # ← one package per feature
│   │   │           ├── handler.go          # HTTP endpoint logic
│   │   │           ├── dto.go              # Request/response structs (json + validate tags)
│   │   │           ├── assembler.go        # Maps dto ↔ usecase input/output
│   │   │           └── router.go           # Route registration for this feature
│   │   │
│   │   ├── grpc/                           # gRPC transport
│   │   │   ├── interceptor/                # ← gRPC equivalent of HTTP middleware
│   │   │   │   └── auth.go
│   │   │   └── v1/
│   │   │       └── user/
│   │   │           └── handler.go
│   │   │
│   │   └── cronjob/                        # Cron job handlers (no middleware needed)
│   │       └── scan_expired.go             # Thin wrapper → calls usecase
│   │
│   └── infrastructure/                     # Layer 4: Frameworks & Drivers (outermost)
│       │                                   # Depends on: all layers
│       │                                   # PURPOSE: Technical capabilities, NOT business logic
│       │
│       ├── config/                         # Configuration loading
│       │   ├── config.go
│       │   └── config.yaml
│       │
│       ├── database/                       # Database CONNECTION (not queries!)
│       │   └── postgres.go                 # Returns *pgxpool.Pool - connection factory
│       │
│       ├── cache/                          # Cache CONNECTION
│       │   └── redis.go                    # Returns *redis.Client - connection factory
│       │
│       ├── pubsub/                         # Message queue CONNECTION
│       │   └── kafka.go                    # Returns kafka producer/consumer
│       │
│       ├── server/                         # Server bootstrap
│       │   ├── http.go                     # HTTP server setup + graceful shutdown
│       │   └── grpc.go                     # gRPC server setup
│       │
│       ├── logger/                         # Logging setup
│       │   └── logger.go
│       │
│       ├── observability/                  # Monitoring setup
│       │   ├── tracer.go                   # OpenTelemetry tracing setup
│       │   └── metrics.go                  # Prometheus metrics setup
│       │
│       └── di/                             # Dependency injection wiring
│           ├── wire.go
│           └── wire_gen.go
│
├── pkg/                                    # Public shared packages
│ ├── errors/
│ │   └── errors.go                         # Custom error types
│ ├── locale/                               # i18n / localized error messages
│ │   ├── locale.go                         # Error code constants + Locale type + core API
│ │   ├── locale_en.go                      # English translations
│ │   └── locale_vi.go                      # Vietnamese translations
│ └── validator/
│     └── validator.go                      # Input validation
│
├── api/                                    # API Definitions
│ ├── openapi/
│ │ └── openapi.yaml                        # OpenAPI 3.0 spec
│ └── proto/
│     └── user/
│         └── user.proto                    # Protobuf definitions
│
├── sqlc/                                   # SQLC configuration & SQL files (if using SQLC)
│   ├── sqlc.yaml                           # SQLC configuration
│   ├── schema/                             # Database schema definitions
│   │   └── users.sql                       # CREATE TABLE statements
│   ├── query/                              # SQL queries (SQLC reads these)
│   │   └── user.sql                        # SELECT, INSERT, UPDATE, DELETE for users
│   └── migrations/                         # Database migrations (golang-migrate format)
│       ├── 000001_create_users_table.up.sql
│       └── 000001_create_users_table.down.sql
│
├── scripts/
│   └── generate.sh                         # Code generation script
│
├── .github/
│   └── workflows/
│       ├── ci.yaml                         # CI pipeline
│       ├── pull_request_template.md        # Pull request template
│       └── release.yaml                    # Release pipeline
│
├── config/
│ ├── config.yaml                           # Development config
│ ├── config.prod.yaml                      # Production config
│ └── config.example.yaml                   # Example config for documentation
│
├── Dockerfile
├── docker-compose.yaml
├── Makefile
├── go.mod
├── go.sum
├── .gitignore
├── .env.example
└── README.md
```

### 3. Clean Architecture Principles to Enforce

1. **Dependency Rule**: Source code dependencies point inward only
   - Domain layer has ZERO external dependencies (no framework imports)
   - Use case layer depends only on domain
   - Adapter layer depends on domain and use case
   - Infrastructure depends on all inner layers

2. **Dependency Inversion**:
   - High-level modules (use cases) define interfaces
   - Low-level modules (repositories) implement them
   - Interfaces live in the domain layer

3. **Separation of Concerns**:
   - Entities contain enterprise-wide business rules
   - Use cases contain application-specific business rules
   - Adapters convert data between layers
   - Infrastructure handles external concerns

### 3.1. Adapter vs Infrastructure: The Decision Guide

This is the most confusing part of Clean Architecture. Here's the definitive distinction:

#### **ADAPTER Layer** - "Implements Business Interfaces"

| Characteristic               | Description                                                                                |
| ---------------------------- | ------------------------------------------------------------------------------------------ |
| **Purpose**                  | Implements interfaces defined in domain layer                                              |
| **Question to ask**          | "Does this implement a domain interface?"                                                  |
| **Contains business logic?** | Yes - data transformation, mapping, business-aware error handling                          |
| **Examples**                 | Repository implementations, HTTP handlers, gRPC handlers, external API clients, assemblers |

**Adapter components:**

```bash
adapter/
├── repository/              # Implements domain.XxxRepository
│   ├── postgres/            # SQL implementation
│   ├── mongodb/             # MongoDB implementation
│   └── cache/               # Cache decorator
├── pubsub/                  # Event publishing implementations
│   └── kafka_publisher.go   # Implements event.EventPublisher
└── external/                # Implements domain interfaces for external APIs
    └── stripe_payment.go    # Implements domain.PaymentGateway

transport/                   # Delivery mechanisms (separate from adapter)
├── http/
│   ├── middleware/          # HTTP-specific middleware (Fiber handlers)
│   ├── httputil/            # HTTP-specific utilities (error parser)
│   ├── dto/                 # Shared HTTP DTOs (pagination)
│   └── v1/
│       ├── user/            # Per-feature: handler, dto, assembler, router
│       └── order/
├── grpc/
│   ├── interceptor/         # gRPC-specific middleware (interceptors)
│   └── v1/seller/
└── cronjob/                 # Cron job handlers (no middleware)
```

#### **INFRASTRUCTURE Layer** - "Provides Technical Capabilities"

| Characteristic               | Description                                                                      |
| ---------------------------- | -------------------------------------------------------------------------------- |
| **Purpose**                  | Bootstraps and provides raw technical resources                                  |
| **Question to ask**          | "Is this just setup/configuration with no business logic?"                       |
| **Contains business logic?** | NO - only technical setup                                                        |
| **Examples**                 | Connection factories, server bootstrap, config loading, logging setup, DI wiring |

**Infrastructure components:**

```bash
infrastructure/
├── config/            # Config loading (returns Config struct)
├── database/          # Connection factory (returns *pgxpool.Pool)
├── cache/             # Connection factory (returns *redis.Client)
├── pubsub/            # Connection factory (returns kafka.Producer)
├── server/            # Server bootstrap and graceful shutdown
├── logger/            # Logger setup (returns *slog.Logger)
├── observability/     # Tracer/metrics setup
└── di/                # Wires everything together
```

#### **Decision Flowchart**

```bash
┌─────────────────────────────────────────────────────────┐
│           Does it implement a domain interface?         │
└─────────────────────────────────────────────────────────┘
                         │
            ┌────────────┴────────────┐
            │                         │
           YES                        NO
            │                         │
            ▼                         ▼
     ┌──────────┐          ┌──────────────────────────┐
     │ ADAPTER  │          │ Does it return a client/ │
     └──────────┘          │  connection/resource?    │
                           └──────────────────────────┘
                                      │
                         ┌────────────┴────────────┐
                         │                         │
                        YES                        NO
                         │                         │
                         ▼                         ▼
                  ┌──────────────┐          ┌──────────┐
                  │INFRASTRUCTURE│          │   pkg/   │
                  └──────────────┘          └──────────┘
```

#### **Concrete Examples**

| Component                                      | Where?              | Why?                                             |
| ---------------------------------------------- | ------------------- | ------------------------------------------------ |
| `postgres.NewPool()` → returns `*pgxpool.Pool` | **infrastructure/** | Just creates connection, no business logic       |
| `UserRepository.Create(user)` → runs INSERT    | **adapter/**        | Implements `domain.UserRepository`               |
| `redis.NewClient()` → returns `*redis.Client`  | **infrastructure/** | Just creates connection                          |
| `UserCacheRepo.GetByID()` → checks cache       | **adapter/**        | Implements caching strategy for domain interface |
| `config.Load()` → returns `Config`             | **infrastructure/** | Pure technical config loading                    |
| `HTTPServer.Start()` → listens on port         | **infrastructure/** | Server bootstrap                                 |
| `UserHandler.Create(c *fiber.Ctx)`             | **transport/**      | Calls use case, transforms request/response      |
| `StripeClient.Charge()`                        | **adapter/**        | Implements `domain.PaymentGateway`               |
| `logger.New()` → returns `*slog.Logger`        | **infrastructure/** | Logger setup                                     |
| `wire.Build()` → wires dependencies            | **infrastructure/** | DI setup                                         |
| `KafkaPublisher.Publish()`                     | **adapter/**        | Implements `event.EventPublisher`                |
| `kafka.NewProducer()` → returns Kafka producer | **infrastructure/** | Just creates connection                          |

#### **The Key Insight**

> **Adapter** = "I know how to talk TO the business" (implements domain interfaces)
>
> **Infrastructure** = "I know how to SET UP technical resources" (provides raw capabilities)

Think of it this way:

- Infrastructure gives you `*pgxpool.Pool`
- Adapter uses that pool to implement `domain.UserRepository`

### 3.2. Placement Rationale — Why Things Live Where They Do

Every directory placement should survive the question: **"Why HERE and not somewhere else?"**
Copying a layout from another project without this reasoning leads to inconsistencies.

#### Why `middleware/` is under `transport/http/`, NOT `transport/`

Middleware is a **Fiber/net-http concept** — it wraps `fiber.Handler` or `http.Handler`.
gRPC has a completely different mechanism: **interceptors** (`grpc.UnaryServerInterceptor`).
Placing middleware at `transport/middleware/` implies it applies to all transports, but it doesn't.

```bash
transport/
├── http/
│   └── middleware/       # ← HTTP middleware (Fiber signatures)
└── grpc/
    └── interceptor/      # ← gRPC middleware (interceptor signatures)
```

If you put them at the same level (`transport/middleware/`), you either:

- Mix HTTP and gRPC middleware in one package (different signatures, confusing)
- Name it `http_middleware/` anyway (defeats the purpose)

**Each transport owns its cross-cutting concerns.** This also means if you remove gRPC,
you just delete `transport/grpc/` — nothing under `transport/http/` changes.

#### Why `httputil/` is under `transport/http/`, NOT `pkg/`

`httputil/error_parser.go` has these imports:

```go
import (
    "github.com/gofiber/fiber/v2"                  // framework-specific
    "{module}/internal/transport/http/middleware"  // internal package
    "{module}/pkg/locale"
)
```

Three disqualifiers from `pkg/`:

1. **Imports `internal/`** — Go forbids external packages from importing `internal/`.
   If `httputil` were in `pkg/`, it couldn't import the middleware package.
2. **Framework-coupled** — it accepts `*fiber.Ctx`, not `http.ResponseWriter`.
   Switching HTTP frameworks breaks this code.
3. **Project-specific** — `mapHTTPStatus()` maps THIS project's locale codes to HTTP statuses.
   Another project has different error codes.

General rule: if a package imports `internal/` or a specific framework, it must live inside `internal/`.

#### Why `pkg/locale/` is in `pkg/`, not `internal/`

Locale codes are **consumed by multiple layers**:

- **usecase/** returns `locale.UserNotFound.Err()`
- **transport/** translates codes to HTTP/gRPC responses

`pkg/` (or an equivalent like `internal/shared/`) signals: "this package has no layer affiliation —
it's a cross-cutting utility." It depends on nothing inside `internal/`.

> **Trade-off note**: Strict Clean Architecture says usecases should only depend on domain.
> Having usecases import `pkg/locale` couples them to your error code system. The purist
> alternative is: usecases return plain `error` values or domain error types, and the transport
> layer maps those to locale codes. We choose the pragmatic approach — `locale.SomeCode.Err()`
> is simpler and avoids duplicating error mapping in every transport handler.

**If you prefer stricter layering**, define error types in `domain/`:

```go
// domain/errors.go
var ErrUserNotFound = errors.New("user not found")

// usecase/user/service.go
return domain.ErrUserNotFound

// transport/http/httputil/error_parser.go
switch {
case errors.Is(err, domain.ErrUserNotFound):
    code = locale.UserNotFound
}
```

#### Why `transport/` is a sibling of `adapter/`, not inside it

In Clean Architecture theory, HTTP handlers ARE adapters — they adapt external requests
to internal use case calls. So `transport/` is technically part of the adapter layer.

We separate them for **practical, not theoretical** reasons:

- **Scale**: `adapter/` (repos, pubsub, external clients) and `transport/` (HTTP, gRPC, cronjob)
  serve different roles. As the project grows, mixing them creates a bloated `adapter/` directory.
- **Team ownership**: the team working on HTTP endpoints rarely touches repository implementations.
- **Deletion boundary**: removing gRPC means deleting `transport/grpc/`. No adapter code changes.

> Think of it as: `adapter/` implements **outbound** interfaces (your app calls external systems),
> `transport/` handles **inbound** delivery (external world calls your app).

#### `pkg/` vs `internal/shared/` — do you actually need `pkg/`?

In Go, `pkg/` has **no special compiler meaning** (unlike `internal/` which enforces visibility).
It's purely convention signaling "reusable across projects."

| Approach           | Use when                                                      |
| ------------------ | ------------------------------------------------------------- |
| `pkg/`             | You build a library or want to signal cross-project reuse     |
| `internal/shared/` | Private monorepo; no external consumers will ever import this |

For a microservice that nobody imports, `internal/shared/` is equally valid.
We use `pkg/` here because it's a well-known Go convention and visually distinguishes
"shared utilities" from "layered architecture code" inside `internal/`.

### 3.3. Where to Put Adapter Interfaces — Consumer Owns the Interface

Adapters (JWT, payment gateway, email sender, external APIs) need interfaces for dependency
inversion. The rule: **the consumer owns the interface, not the implementor**.

#### If the usecase calls it → interface in `domain/`

When a usecase needs to call an external service, the interface lives in `domain/` —
same package as repository interfaces, one file per concern:

```go
// internal/domain/payment.go
package domain

import "context"

type PaymentGateway interface {
    Charge(ctx context.Context, orderID int64, amount int64) error
    Refund(ctx context.Context, transactionID string) error
}
```

```go
// internal/domain/notification.go
package domain

type NotificationSender interface {
    SendEmail(ctx context.Context, to, subject, body string) error
}
```

```go
// internal/domain/auth.go — only if usecase needs to GENERATE tokens (e.g. login flow)
package domain

type TokenGenerator interface {
    GenerateToken(userID int64, role string) (string, error)
}
```

Implementations live in `adapter/`:

```go
// internal/adapter/external/stripe.go
var _ domain.PaymentGateway = (*StripeClient)(nil)

// internal/adapter/jwt/jwt.go — implements domain.TokenGenerator
var _ domain.TokenGenerator = (*JWTGenerator)(nil)
```

#### If only transport calls it → NO domain interface

JWT **verification** is typically a middleware concern — it runs before the handler, extracts
user identity, and passes it via context. The usecase never calls "verify JWT" directly.

```bash
Request → [JWT Middleware] → Handler → UseCase
              ↑                          ↑
         extracts userID           receives userID as a plain parameter
         from token                (doesn't know about JWT at all)
```

No domain interface needed — the adapter is used directly by middleware:

```go
// internal/adapter/jwt/jwt.go — used directly by transport middleware
package jwt

type Verifier struct { secret []byte }
func (v *Verifier) Verify(tokenString string) (*Claims, error) { ... }
```

```go
// internal/transport/http/middleware/auth.go
func Auth(verifier *jwt.Verifier) fiber.Handler {
    return func(c *fiber.Ctx) error {
        claims, err := verifier.Verify(c.Get("Authorization"))
        // store claims in context, call c.Next()
    }
}
```

#### Decision flowchart

```bash
"Who calls this adapter?"
        │
   ┌────┴─────────┐
   │              │
UseCase        Transport only
   │              │
   ▼              ▼
domain/        No domain interface.
(one file      Adapter used directly
per concern)   by middleware/handler.
```

#### Summary table

| Adapter                   | Usecase calls it? | Interface location       | Implementation                          |
| ------------------------- | ----------------- | ------------------------ | --------------------------------------- |
| User repository           | Yes               | `domain/user.go`         | `adapter/repository/postgres/`          |
| Payment gateway           | Yes               | `domain/payment.go`      | `adapter/external/stripe.go`            |
| Email/SMS sender          | Yes               | `domain/notification.go` | `adapter/external/sendgrid.go`          |
| Token generator (login)   | Yes               | `domain/auth.go`         | `adapter/jwt/jwt.go`                    |
| JWT verifier (middleware) | No                | None                     | `adapter/jwt/jwt.go` used by middleware |
| Rate limiter              | No                | None                     | Middleware concern                      |

#### File layout for `domain/` with adapter interfaces

```bash
domain/
├── entity/
│   ├── user.go
│   └── order.go
├── user.go              # UserRepository + UserFilter + UserListResult
├── order.go             # OrderRepository + OrderFilter
├── payment.go           # PaymentGateway (usecase calls it)
├── notification.go      # NotificationSender (usecase calls it)
├── auth.go              # TokenGenerator (usecase calls it for login)
├── service/             # Domain service interfaces (business rules)
├── event/               # Domain events + EventPublisher interface
└── valueobject/         # Value objects (Email, Money, etc.)
```

> **One sentence rule**: If a usecase method needs to call it, define the interface in `domain/`.
> If only the transport layer uses it, skip the interface and use the adapter directly.

### 4. Code Templates to Generate

#### Domain Layer

#### internal/domain/entity/user.go

Domain entities have NO framework tags. Serialization is a transport concern.

```go
package entity

import "time"

type User struct {
    ID        int64
    Email     string
    Name      string
    Password  string
    CreatedAt time.Time
    UpdatedAt time.Time
}

func NewUser(email, name, password string) *User {
    now := time.Now()
    return &User{
        Email:     email,
        Name:      name,
        Password:  password,
        CreatedAt: now,
        UpdatedAt: now,
    }
}
```

#### internal/domain/valueobject/ — What are Value Objects?

Value objects represent **concepts that are defined by their attributes, not by identity**.
Two value objects with the same attributes are considered equal (unlike entities which have unique IDs).

Use value objects to:

- **Enforce invariants** at creation time (e.g. an Email must be valid)
- **Eliminate primitive obsession** (don't pass raw `string` for email, `int64` for money)
- **Make the domain self-documenting** (function signatures tell you what they expect)

```go
// ❌ Primitive obsession — what does this mean?
func CreateOrder(userID int64, amount int64, currency string, email string) error

// ✅ Value objects — self-documenting, valid by construction
func CreateOrder(userID int64, amount valueobject.Money, email valueobject.Email) error
```

#### internal/domain/valueobject/email.go

```go
package valueobject

import (
    "fmt"
    "regexp"
    "strings"
)

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// Email is a value object — immutable, valid by construction.
type Email struct {
    value string
}

func NewEmail(raw string) (Email, error) {
    normalized := strings.TrimSpace(strings.ToLower(raw))
    if !emailRegex.MatchString(normalized) {
        return Email{}, fmt.Errorf("invalid email: %s", raw)
    }
    return Email{value: normalized}, nil
}

func (e Email) String() string { return e.value }
func (e Email) IsZero() bool   { return e.value == "" }
```

#### internal/domain/valueobject/money.go

```go
package valueobject

import "fmt"

// Money represents a monetary amount in its smallest unit (e.g. cents, đồng).
// Value object: two Money values with same Amount+Currency are equal.
type Money struct {
    Amount   int64  // smallest unit (cents/đồng)
    Currency string // ISO 4217: "VND", "USD"
}

func NewMoney(amount int64, currency string) Money {
    return Money{Amount: amount, Currency: currency}
}

func (m Money) Add(other Money) (Money, error) {
    if m.Currency != other.Currency {
        return Money{}, fmt.Errorf("cannot add %s to %s", m.Currency, other.Currency)
    }
    return Money{Amount: m.Amount + other.Amount, Currency: m.Currency}, nil
}

func (m Money) Multiply(quantity int) Money {
    return Money{Amount: m.Amount * int64(quantity), Currency: m.Currency}
}

func (m Money) GreaterThan(other Money) bool {
    return m.Amount > other.Amount
}

func (m Money) IsZero() bool { return m.Amount == 0 }
```

**When to use Value Objects vs plain types:**

| Scenario      | Use                     | Why                                              |
| ------------- | ----------------------- | ------------------------------------------------ |
| Email address | `valueobject.Email`     | Must validate format, normalize case             |
| Money/price   | `valueobject.Money`     | Must track currency, prevent mixed-currency math |
| User ID       | `int64` (plain)         | No invariants to enforce, just an identifier     |
| Date range    | `valueobject.DateRange` | Must enforce start < end                         |
| Status string | `string` or `const`     | Simple enum, no complex rules                    |

#### internal/domain/user.go — Repository Interface + Filter (one file per aggregate)

Repository interfaces, filter structs, and result types live **in the `domain` package** as
one file per aggregate. This avoids the `repository.UserRepository` stutter — call site
becomes `domain.UserRepository` which reads naturally.

**Why not a `domain/repository/` sub-package?**

- `repository.UserRepository` stutters — the package name repeats in the type name
- A single `port.go` file mixing all interfaces + filter structs gets messy fast
- One file per aggregate keeps each file focused: interface + filter + result together

```go
// internal/domain/user.go
package domain

import (
    "context"
    "{module}/internal/domain/entity"
)

// UserFilter defines filtering/pagination for user queries.
// This is part of the repository contract — no framework tags.
type UserFilter struct {
    Role   *string // nil = don't filter
    Status *string
    Limit  int
    Offset int
}

// UserListResult is the paginated result from List.
type UserListResult struct {
    Users []*entity.User
    Total int64
}

// UserRepository defines the data access contract for users.
type UserRepository interface {
    Create(ctx context.Context, user *entity.User) error
    GetByID(ctx context.Context, id int64) (*entity.User, error)
    GetByEmail(ctx context.Context, email string) (*entity.User, error)
    Update(ctx context.Context, user *entity.User) error
    Delete(ctx context.Context, id int64) error
    List(ctx context.Context, filter UserFilter) (*UserListResult, error)
}
```

**Call site examples:**

```go
// ✅ domain.UserRepository — clean, no stutter
func NewService(userRepo domain.UserRepository) *Service { ... }

// ✅ domain.UserFilter — filter lives next to the interface that uses it
result, err := s.userRepo.List(ctx, domain.UserFilter{Role: &role, Limit: 20})
```

For projects with many aggregates, each gets its own file:

```bash
domain/
├── entity/
│   ├── user.go
│   └── order.go
├── user.go          # UserRepository + UserFilter + UserListResult
├── order.go         # OrderRepository + OrderFilter + OrderListResult
├── service/
├── event/
└── valueobject/
```

**Filter passthrough rule**: If the use case does zero transformation on the filter
(no validation, no business rules), skip the usecase-level DTO and let the transport
assembler map directly to `domain.UserFilter`. Only add a usecase DTO when the use case
**does something** (applies authorization rules, combines data from multiple repos, etc.).

#### internal/domain/service/pricing_service.go

Domain services contain **cross-entity business rules** that don't naturally belong to a single entity.
They are INTERFACES in the domain layer, implemented in the usecase layer.

> **⚠️ Common misconception: "multiple tables" ≠ "domain service"**
>
> A query that JOINs multiple tables (e.g., "list users with role=admin") is **NOT** a domain service.
> It's a **repository method** — pure data retrieval with zero business logic.
> The fact that it requires a SQL JOIN is an implementation detail of how the data is stored.
>
> Domain services are for **business rules, computations, and decisions** — not data fetching.

**The mental test — ask yourself:**

```bash
"Does this method RETURN DATA or RETURN A DECISION/COMPUTATION?"

Return data       → Repository    (even if it requires JOINs across 10 tables)
Return a decision → Domain service (even if it only involves 1 entity)
```

**Concrete examples:**

| Operation                                               | Where?             | Why?                                         |
| ------------------------------------------------------- | ------------------ | -------------------------------------------- |
| "Get users where role=admin and status=active" (JOIN)   | **Repository**     | Pure data retrieval, no business logic       |
| "Get top 10 sellers by revenue" (JOIN orders + users)   | **Repository**     | Aggregation query, no decision               |
| "Calculate discount based on user tier + order history" | **Domain service** | Business computation using multiple entities |
| "Determine if order should be auto-approved"            | **Domain service** | Business decision based on rules             |

> **Key insight**: If you switched from PostgreSQL (JOINs) to MongoDB (embedded documents),
> the JOIN disappears but the repository interface stays the same. The caller says
> "give me admin users" — it doesn't care about JOINs. That's why it's a repository concern.

```go
package service

import (
    "context"
    "{module}/internal/domain/entity"
    "{module}/internal/domain/valueobject"
)

// PricingService defines cross-entity pricing rules.
// This belongs in domain because pricing is an enterprise-wide business rule
// that may involve multiple entities (Order, Product, Discount, Tax).
//
// NOTE: This is for COMPUTATION, not data retrieval. Cross-entity queries
// (even with JOINs) belong in the repository interface, not here.
type PricingService interface {
    CalculateOrderTotal(ctx context.Context, items []entity.OrderItem, discount *entity.Discount) (valueobject.Money, error)
    IsEligibleForDiscount(ctx context.Context, user *entity.User, order *entity.Order) (bool, error)
}
```

#### internal/domain/event/event.go

Domain events represent **something that happened** in the system.
They are published by use cases after successful business operations.

```go
package event

import "time"

// UserCreated is published when a new user registers.
type UserCreated struct {
    UserID    int64
    Email     string
    Name      string
    CreatedAt time.Time
}

// OrderPlaced is published when an order is successfully placed.
type OrderPlaced struct {
    OrderID  int64
    UserID   int64
    Total    int64
    PlacedAt time.Time
}
```

#### internal/domain/event/publisher.go

The `EventPublisher` interface is a **port** — it defines WHAT can be done (publish events),
not HOW (Kafka, RabbitMQ, NATS). This allows swapping message brokers without changing business logic.

Note: the interface accepts domain event structs (no `json` tags). The **adapter** is responsible
for mapping domain events → message DTOs with `json` tags before serializing to the wire.

```go
package event

import "context"

// EventPublisher is a port for publishing domain events to a message broker.
// Implemented by adapter/pubsub/ (e.g. KafkaPublisher, RabbitMQPublisher).
//
// The adapter implementation maps domain events → wire-format DTOs (with json tags)
// before publishing. Domain events stay free of serialization concerns.
//
// No Close() method — resource cleanup is owned by each component's constructor
// via the (instance, cleanup, error) return pattern. Wire composes all cleanups.
type EventPublisher interface {
    PublishUserCreated(ctx context.Context, evt UserCreated) error
    PublishOrderPlaced(ctx context.Context, evt OrderPlaced) error
}
```

**Why typed methods instead of `Publish(topic string, payload any)`?**

- Compile-time safety: you can't accidentally publish the wrong event type
- Each method knows its topic name — callers don't pass raw strings
- The adapter maps each event to its own message DTO with correct `json` field names

#### Use Case Layer

#### internal/usecase/pricing/service.go (Domain Service IMPLEMENTATION)

Domain service implementations live in `usecase/` because they contain pure business logic.
They are **injected into** other use cases, never called directly from handlers.

```go
package pricing

import (
    "context"

    domainservice "{module}/internal/domain/service"
    "{module}/internal/domain/entity"
    "{module}/internal/domain/valueobject"
)

// Service implements domain.PricingService
type Service struct{}

func NewService() *Service {
    return &Service{}
}

// Ensure interface compliance
var _ domainservice.PricingService = (*Service)(nil)

func (s *Service) CalculateOrderTotal(ctx context.Context, items []entity.OrderItem, discount *entity.Discount) (valueobject.Money, error) {
    var total valueobject.Money
    for _, item := range items {
        total = total.Add(item.Price.Multiply(item.Quantity))
    }
    if discount != nil {
        total = discount.Apply(total)
    }
    return total, nil
}

func (s *Service) IsEligibleForDiscount(ctx context.Context, user *entity.User, order *entity.Order) (bool, error) {
    // Enterprise business rule: only verified users with 3+ orders qualify
    return user.IsVerified && order.TotalAmount.GreaterThan(valueobject.NewMoney(100)), nil
}
```

#### How domain services are USED (injected into use cases, never called from handlers)

```bash
┌──────────┐      ┌──────────────┐      ┌────────────────┐
│ Handler  │ ───▶ │   Use Case   │ ───▶ │ Domain Service │
│ (adapter)│      │  (usecase/)  │      │ (usecase/ impl)│
└──────────┘      └──────────────┘      └────────────────┘
     ✗ NEVER directly ──────────────────────────▶
```

```go
// ✅ CORRECT: Use case injects and calls domain service
package order

type Service struct {
    orderRepo      domain.OrderRepository
    pricingService domainservice.PricingService  // injected domain service
    logger         *slog.Logger
}

func NewService(
    orderRepo domain.OrderRepository,
    pricingService domainservice.PricingService,
    logger *slog.Logger,
) *Service {
    return &Service{
        orderRepo:      orderRepo,
        pricingService: pricingService,
        logger:         logger,
    }
}

func (s *Service) PlaceOrder(ctx context.Context, input PlaceOrderInput) (*OrderOutput, error) {
    // 1. Use domain service for business rules
    total, err := s.pricingService.CalculateOrderTotal(ctx, input.Items, input.Discount)
    if err != nil {
        return nil, err
    }

    // 2. Create entity
    order := entity.NewOrder(input.UserID, input.Items, total)

    // 3. Persist (side effect - that's why this is a use case, not a domain service)
    if err := s.orderRepo.Create(ctx, order); err != nil {
        return nil, err
    }

    return toOrderOutput(order), nil
}
```

```go
// ❌ WRONG: Handler calling domain service directly
func (h *OrderHandler) Create(c *fiber.Ctx) error {
    total := h.pricingService.CalculateOrderTotal(...)  // VIOLATES layering!
}
```

**When to use `domain/service/` vs `usecase/` vs `repository`:**

| Scenario                                                                  | Where?                     | Why?                                                             |
| ------------------------------------------------------------------------- | -------------------------- | ---------------------------------------------------------------- |
| "List users with role=admin" (requires JOIN)                              | `repository`               | Pure data retrieval, no business logic — JOINs are SQL mechanics |
| "Get top 10 sellers by revenue" (JOIN + aggregation)                      | `repository`               | Aggregation query — returns data, not a decision                 |
| "Calculate total price with tax and discount"                             | `domain/service/`          | Enterprise-wide pricing rule, reusable across use cases          |
| "Place an order" (validate → calculate price → create order → send email) | `usecase/`                 | Application workflow orchestrating multiple steps                |
| "Check if user is eligible for resale"                                    | `domain/service/`          | Business eligibility rule, pure logic                            |
| "Submit resale post" (check eligibility → create post → notify)           | `usecase/`                 | Application flow with side effects                               |
| "Determine loyalty tier from user + order history"                        | `domain/service/`          | Business computation using multiple entities                     |
| "Validate email format"                                                   | `domain/valueobject/`      | Single-entity validation, belongs on the value object            |
| "Hash password"                                                           | `domain/entity/` or `pkg/` | Utility, not a cross-entity rule                                 |

**Summary:**

| Aspect                    | Repository                 | Domain Service              | Use Case                    |
| ------------------------- | -------------------------- | --------------------------- | --------------------------- |
| **Purpose**               | Data retrieval/persistence | Business rules/computation  | Orchestrate workflows       |
| **Interface**             | `domain/{aggregate}.go`    | `domain/service/`           | N/A (concrete)              |
| **Implementation**        | `adapter/repository/`      | `usecase/{name}/service.go` | `usecase/{name}/service.go` |
| **Contains I/O?**         | YES (DB queries)           | NO (pure logic)             | YES (DB, HTTP, messaging)   |
| **Returns**               | Data (entities, lists)     | Decisions/computations      | Operation results           |
| **JOINs/multi-table?**    | YES — SQL is impl detail   | NO — receives data as args  | Uses repos to get data      |
| **Called by**             | Use cases                  | Use cases                   | Handlers (via assembler)    |
| **Called from handlers?** | NEVER directly             | NEVER directly              | YES                         |

> **Rule of thumb**:
>
> - **Returns data** (even with JOINs across 10 tables) → **repository**
> - **Returns a decision/computation** using multiple entities, no I/O → **domain service**
> - **Orchestrates** steps with side effects (DB writes, events, emails) → **use case**

#### internal/usecase/user/service.go

```go
package user

import (
    "context"
    "log/slog"

    "{module}/internal/domain"
    "{module}/internal/domain/entity"
    "{module}/internal/domain/event"
    "{module}/pkg/locale"
)

type Service struct {
    userRepo  domain.UserRepository
    publisher event.EventPublisher  // injected — can be Kafka, RabbitMQ, etc.
    logger    *slog.Logger
}

func NewService(
    userRepo domain.UserRepository,
    publisher event.EventPublisher,
    logger *slog.Logger,
) *Service {
    return &Service{
        userRepo:  userRepo,
        publisher: publisher,
        logger:    logger,
    }
}

func (s *Service) CreateUser(ctx context.Context, input CreateUserInput) (*entity.User, error) {
    user := entity.NewUser(input.Email, input.Name, input.Password)

    if err := s.userRepo.Create(ctx, user); err != nil {
        s.logger.Error("failed to create user", "error", err)
        return nil, locale.UserEmailExists.Err() // ← return locale error, not raw DB error
    }

    // Publish domain event — other services consume "user.created" topic
    if err := s.publisher.PublishUserCreated(ctx, event.UserCreated{
        UserID:    user.ID,
        Email:     user.Email,
        Name:      user.Name,
        CreatedAt: user.CreatedAt,
    }); err != nil {
        s.logger.Error("failed to publish user.created event", "error", err)
        // Don't fail the request — event publishing is a side-effect
    }

    return user, nil
}

func (s *Service) GetUser(ctx context.Context, id int64) (*entity.User, error) {
    return s.userRepo.GetByID(ctx, id)
}
```

#### internal/usecase/user/dto.go

Use case DTOs have NO framework tags (`json`, `validate`). They are pure Go structs.
The transport layer owns serialization. The assembler maps between them.

```go
package user

// Input DTOs - what the use case needs (no json/validate tags!)
type CreateUserInput struct {
    Email    string
    Name     string
    Password string
}

type UpdateUserInput struct {
    Name string
}

// Output DTOs - what the use case returns
type UserOutput struct {
    ID        int64
    Email     string
    Name      string
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

#### Transport Layer

#### internal/transport/http/v1/user/ — Per-Feature Package

Each feature is a **self-contained package** under `transport/http/v1/{feature}/`.
It contains `dto.go`, `assembler.go`, `handler.go`, and `router.go` — no more scattered
request/response files across shared directories.

**Why per-feature?** When you have 10+ features, a flat `v1/` with `request.go`, `response.go`,
`user_handler.go`, `order_handler.go` becomes unmanageable. Per-feature keeps everything
for one endpoint group in a single, self-contained package.

#### internal/transport/http/v1/user/dto.go

Transport-layer request/response structs own `json` and `validate` tags.

```go
package user

import "time"

// --- Requests ---

type CreateUserRequest struct {
    Email    string `json:"email" validate:"required,email"`
    Name     string `json:"name" validate:"required,min=2,max=100"`
    Password string `json:"password" validate:"required,min=8"`
}

type UpdateUserRequest struct {
    Name string `json:"name" validate:"omitempty,min=2,max=100"`
}

// --- Responses ---

type UserResponse struct {
    ID        int64     `json:"id"`
    Email     string    `json:"email"`
    Name      string    `json:"name"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}
```

#### internal/transport/http/v1/user/assembler.go

The assembler lives **inside the same feature package** — no separate `assembler/` directory.
It maps between the feature's transport DTOs and usecase DTOs.

```go
package user

import useruc "{module}/internal/usecase/user"

// Request → UseCase Input
func toCreateInput(req CreateUserRequest) useruc.CreateUserInput {
    return useruc.CreateUserInput{
        Email:    req.Email,
        Name:     req.Name,
        Password: req.Password,
    }
}

// UseCase Output → Response
func toUserResponse(output useruc.UserOutput) UserResponse {
    return UserResponse{
        ID:        output.ID,
        Email:     output.Email,
        Name:      output.Name,
        CreatedAt: output.CreatedAt,
        UpdatedAt: output.UpdatedAt,
    }
}
```

> Note: assembler functions are **unexported** (lowercase) — they're only used within this
> feature package. No need for a struct or constructor.

#### internal/transport/http/v1/user/handler.go

The handler calls the usecase and uses the local assembler functions.
Everything the handler needs is in the same package.

```go
package user

import (
    "github.com/gofiber/fiber/v2"

    "{module}/internal/transport/http/httputil"
    useruc "{module}/internal/usecase/user"
    "{module}/pkg/locale"
)

type Handler struct {
    userService *useruc.Service
}

func NewHandler(userService *useruc.Service) *Handler {
    return &Handler{userService: userService}
}

func (h *Handler) Create(c *fiber.Ctx) error {
    var req CreateUserRequest
    if err := c.BodyParser(&req); err != nil {
        return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
    }

    input := toCreateInput(req)
    output, err := h.userService.CreateUser(c.Context(), input)
    if err != nil {
        return httputil.WriteError(c, err) // locale-aware error response
    }

    return c.Status(fiber.StatusCreated).JSON(toUserResponse(*output))
}

func (h *Handler) GetByID(c *fiber.Ctx) error {
    id, err := c.ParamsInt("id")
    if err != nil {
        return httputil.WriteError(c, locale.InvalidRequest.Err())
    }

    output, err := h.userService.GetUser(c.Context(), int64(id))
    if err != nil {
        return httputil.WriteError(c, err)
    }

    return c.JSON(toUserResponse(*output))
}
```

#### internal/transport/http/v1/user/router.go

Each feature registers its own routes. The v1 registrar calls this.

```go
package user

import "github.com/gofiber/fiber/v2"

func (h *Handler) RegisterRoutes(router fiber.Router) {
    users := router.Group("/users")
    users.Post("/", h.Create)
    users.Get("/:id", h.GetByID)
}
```

#### internal/transport/http/v1/registrar.go

The registrar wires all feature routers under `/api/v1`.

```go
package v1

import (
    "github.com/gofiber/fiber/v2"

    "{module}/internal/transport/http/v1/user"
    // "{module}/internal/transport/http/v1/order"
)

type Registrar struct {
    userHandler  *user.Handler
    // orderHandler *order.Handler
}

func NewRegistrar(userHandler *user.Handler) *Registrar {
    return &Registrar{userHandler: userHandler}
}

func (r *Registrar) Register(app *fiber.App) {
    api := app.Group("/api/v1")
    r.userHandler.RegisterRoutes(api)
    // r.orderHandler.RegisterRoutes(api)
}
```

#### internal/transport/cronjob/ — Cron Job Handlers

Cronjob handlers are thin wrappers that call a usecase method.
They live in `transport/cronjob/` because they are a **delivery mechanism** —
they receive a trigger (cron schedule) and call into the usecase layer.

```go
package cronjob

import (
    "context"

    "{module}/internal/usecase/order"
)

type ScanExpiredOrders struct {
    orderService *order.Service
}

func NewScanExpiredOrders(orderService *order.Service) *ScanExpiredOrders {
    return &ScanExpiredOrders{orderService: orderService}
}

func (s *ScanExpiredOrders) Run(ctx context.Context) {
    s.orderService.HandleExpiredOrders(ctx)
}
```

#### Shared Packages (pkg/)

#### pkg/locale/locale.go — Error Codes + Locale Type

Locale lives in `pkg/` because it's a shared utility with no domain dependency.
Error codes are typed integers — usecases return them, handlers translate them.

```go
package locale

import "fmt"

// Language represents a supported language.
type Language string

const (
    LangEn Language = "en"
    LangVi Language = "vi"
)

// Locale is an error code constant. Each code maps to translations per language.
type Locale int

// Error code ranges — organize by feature:
//   General:  -1000 to -1099
//   User:     -1100 to -1199
//   Order:    -1200 to -1299
const (
    // General errors
    InternalError  Locale = -1000
    InvalidRequest Locale = -1001
    Unauthorized   Locale = -1002
    RecordNotFound Locale = -1003

    // User errors
    UserNotFound    Locale = -1100
    UserEmailExists Locale = -1101

    // Order errors
    OrderNotFound      Locale = -1200
    OrderAlreadyPaid   Locale = -1201
    ExceedMaxQtyPerOrder Locale = -1202
)

// LocaleError wraps a Locale code as an error.
type LocaleError struct {
    Code    Locale
    Message string // optional override (e.g. formatted with args)
}

func (e *LocaleError) Error() string {
    if e.Message != "" {
        return e.Message
    }
    return fmt.Sprintf("error code: %d", e.Code)
}

// Err creates a LocaleError from this code.
func (l Locale) Err() error {
    return &LocaleError{Code: l}
}

// ErrFormat creates a LocaleError with a formatted message.
// The format string comes from the translation at resolve time.
func (l Locale) ErrFormat(args ...any) error {
    return &LocaleError{Code: l, Message: fmt.Sprintf("error code: %d (args: %v)", l, args)}
}

// AsLocaleError tries to unwrap err as a *LocaleError.
func AsLocaleError(err error) (*LocaleError, bool) {
    var le *LocaleError
    if ok := errors.As(err, &le); ok {
        return le, true
    }
    return nil, false
}
```

#### pkg/locale/locale_en.go — English Translations

```go
package locale

// Mapping maps a Locale code → translated string.
type Mapping map[Locale]string

// translations is populated by NewMapping() — no init() side effects.
var translations = map[Language]Mapping{}

// NewMapping registers all language translations.
// Call this ONCE at app startup (e.g. in main.go or DI provider).
func NewMapping() {
    enMapping()
    viMapping()
}

func enMapping() {
    translations[LangEn] = Mapping{
        InternalError:        "Internal server error",
        InvalidRequest:       "Invalid request",
        Unauthorized:         "Unauthorized",
        RecordNotFound:       "Record not found",
        UserNotFound:         "User not found",
        UserEmailExists:      "Email already exists",
        OrderNotFound:        "Order not found",
        OrderAlreadyPaid:     "Order has already been paid",
        ExceedMaxQtyPerOrder: "Cannot select more than %d items per order",
    }
}
```

#### pkg/locale/locale_vi.go — Vietnamese Translations

```go
package locale

func viMapping() {
    translations[LangVi] = Mapping{
        InternalError:        "Lỗi hệ thống",
        InvalidRequest:       "Yêu cầu không hợp lệ",
        Unauthorized:         "Chưa đăng nhập",
        RecordNotFound:       "Không tìm thấy bản ghi",
        UserNotFound:         "Không tìm thấy người dùng",
        UserEmailExists:      "Email đã tồn tại",
        OrderNotFound:        "Không tìm thấy đơn hàng",
        OrderAlreadyPaid:     "Đơn hàng đã được thanh toán",
        ExceedMaxQtyPerOrder: "Không thể chọn quá %d sản phẩm mỗi đơn hàng",
    }
}
```

**Usage — call `NewMapping()` once at app startup:**

```go
// main.go
func main() {
    locale.NewMapping() // explicit initialization — no init() magic
    cmd.Execute()
}
```

#### pkg/locale/resolve.go — Translate Error to User's Language

```go
package locale

import "fmt"

// Translate resolves a Locale code to the user's language.
// Falls back to English if language or code is not found.
func Translate(lang Language, code Locale, args ...any) string {
    mapping, ok := translations[lang]
    if !ok {
        mapping = translations[LangEn] // fallback
    }
    msg, ok := mapping[code]
    if !ok {
        if fallback, ok := translations[LangEn][code]; ok {
            msg = fallback
        } else {
            return fmt.Sprintf("unknown error: %d", code)
        }
    }
    if len(args) > 0 {
        return fmt.Sprintf(msg, args...)
    }
    return msg
}
```

#### Middleware & HTTP Utilities

#### internal/transport/http/middleware/locale.go — Language Middleware

Extracts language from the `Accept-Language` header and attaches it to the request context.
All downstream handlers can read the language without touching headers themselves.

```go
package middleware

import (
    "context"
    "strings"

    "github.com/gofiber/fiber/v2"

    "{module}/pkg/locale"
)

type langCtxKey struct{}

// Locale extracts language from Accept-Language header and stores it in context.
func Locale() fiber.Handler {
    return func(c *fiber.Ctx) error {
        lang := parseLanguage(c.Get("Accept-Language"))
        ctx := context.WithValue(c.UserContext(), langCtxKey{}, lang)
        c.SetUserContext(ctx)
        return c.Next()
    }
}

// LangFromCtx retrieves the language from context. Defaults to English.
func LangFromCtx(ctx context.Context) locale.Language {
    if lang, ok := ctx.Value(langCtxKey{}).(locale.Language); ok {
        return lang
    }
    return locale.LangEn
}

func parseLanguage(header string) locale.Language {
    // Accept-Language: vi-VN,vi;q=0.9,en;q=0.8
    if header == "" {
        return locale.LangEn
    }
    primary := strings.SplitN(header, ",", 2)[0]       // "vi-VN"
    lang := strings.SplitN(strings.TrimSpace(primary), "-", 2)[0] // "vi"
    switch locale.Language(lang) {
    case locale.LangVi:
        return locale.LangVi
    default:
        return locale.LangEn
    }
}
```

#### internal/transport/http/httputil/error_parser.go — Locale Error → HTTP Response

The error parser bridges usecases and HTTP responses. It unwraps `LocaleError`,
maps error codes to HTTP status codes, and translates the message to the user's language.

```go
package httputil

import (
    "github.com/gofiber/fiber/v2"

    "{module}/internal/transport/http/middleware"
    "{module}/pkg/locale"
)

// ErrorResponse is the standard error body returned to users.
type ErrorResponse struct {
    Code    int    `json:"code"`    // locale error code (e.g. -1100)
    Message string `json:"message"` // localized human-readable message
}

// WriteError translates a usecase error to a localized HTTP response.
func WriteError(c *fiber.Ctx, err error) error {
    lang := middleware.LangFromCtx(c.UserContext())

    le, ok := locale.AsLocaleError(err)
    if !ok {
        // Unknown error — don't leak internals, log separately
        msg := locale.Translate(lang, locale.InternalError)
        return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{
            Code:    int(locale.InternalError),
            Message: msg,
        })
    }

    status := mapHTTPStatus(le.Code)
    msg := locale.Translate(lang, le.Code)

    return c.Status(status).JSON(ErrorResponse{
        Code:    int(le.Code),
        Message: msg,
    })
}

func mapHTTPStatus(code locale.Locale) int {
    switch code {
    case locale.RecordNotFound, locale.UserNotFound, locale.OrderNotFound:
        return fiber.StatusNotFound       // 404
    case locale.Unauthorized:
        return fiber.StatusUnauthorized   // 401
    case locale.InvalidRequest, locale.UserEmailExists:
        return fiber.StatusBadRequest     // 400
    case locale.ExceedMaxQtyPerOrder, locale.OrderAlreadyPaid:
        return fiber.StatusUnprocessableEntity // 422
    default:
        return fiber.StatusInternalServerError // 500
    }
}
```

**The full locale flow:**

```bash
Request (Accept-Language: vi)
    │
    ▼
┌─────────────────────────────────────────────────────────┐
│ middleware.Locale()                                     │
│   • Parses "vi" from Accept-Language header             │
│   • Stores locale.LangVi in context                     │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│ Handler                                                 │
│   • Calls usecase method                                │
│   • On error → httputil.WriteError(c, err)              │
└────────────────────────┬────────────────────────────────┘
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│ Usecase                                                 │
│   return locale.UserNotFound.Err()                      │
│   return locale.ExceedMaxQtyPerOrder.ErrFormat(10)      │
└────────────────────────┬────────────────────────────────┘
                         │ (error bubbles up)
                         ▼
┌─────────────────────────────────────────────────────────┐
│ httputil.WriteError()                                   │
│   1. locale.AsLocaleError(err) → unwrap code -1100      │
│   2. mapHTTPStatus(-1100) → 404                         │
│   3. locale.Translate(LangVi, -1100)                    │
│      → "Không tìm thấy người dùng"                      │
│   4. Return:                                            │
│      HTTP 404                                           │
│      {"code": -1100, "message": "Không tìm thấy..."}    │
└─────────────────────────────────────────────────────────┘
```

**Where each piece lives in Clean Architecture:**

| Component                                    | Layer                | Why                                                       |
| -------------------------------------------- | -------------------- | --------------------------------------------------------- |
| `pkg/locale/` (codes, translations, resolve) | **pkg/** (shared)    | Pure utility, no domain dependency                        |
| `http/middleware/locale.go`                  | **transport/http**   | HTTP concern — reads headers, sets context                |
| `http/httputil/error_parser.go`              | **transport/http**   | Translates errors to HTTP responses (imports Fiber)       |
| `locale.UserNotFound.Err()`                  | **usecase** (caller) | Usecase returns typed error codes                         |
| Domain entities/services                     | **domain**           | Never import locale — return plain errors or custom types |

> **Key rule**: Domain and usecase layers return `locale.SomeCode.Err()`.
> They never import translation files or care about language.
> The adapter layer (middleware + error parser) handles translation at the HTTP boundary.

#### Infrastructure Layer

#### internal/infrastructure/server/http.go

The constructor returns `(instance, cleanup, error)`. Wire collects all cleanup functions
and composes them into a single combined cleanup that runs in reverse dependency order.
This way each component owns its own shutdown logic — no centralized `App` struct needed.

```go
package server

import (
    "context"
    "log/slog"
    "time"

    "github.com/gofiber/fiber/v2"

    "{module}/internal/infrastructure/config"
)

type HTTPServer struct {
    app    *fiber.App
    cfg    *config.Config
    logger *slog.Logger
}

func NewHTTPServer(cfg *config.Config, logger *slog.Logger) (*HTTPServer, func(), error) {
    app := fiber.New(fiber.Config{
        ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
        WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
    })

    s := &HTTPServer{
        app:    app,
        cfg:    cfg,
        logger: logger,
    }

    // Cleanup — Wire calls this automatically during shutdown
    cleanup := func() {
        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()

        logger.Info("shutting down HTTP server...")
        if err := app.ShutdownWithContext(ctx); err != nil {
            logger.Error("http server shutdown error", "error", err)
        } else {
            logger.Info("http server stopped")
        }
    }

    return s, cleanup, nil
}

func (s *HTTPServer) App() *fiber.App {
    return s.app
}

func (s *HTTPServer) Start() error {
    s.logger.Info("starting server", "port", s.cfg.Server.Port)
    return s.app.Listen(":" + s.cfg.Server.Port)
}
```

#### internal/infrastructure/config/config.go

```go
package config

import (
    "github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
    Server   ServerConfig   `yaml:"server"`
    Database DatabaseConfig `yaml:"database"`
    Cache    CacheConfig    `yaml:"cache"`
    Kafka    KafkaConfig    `yaml:"kafka"`
    Log      LogConfig      `yaml:"log"`
}

type KafkaConfig struct {
    Brokers []string `yaml:"brokers" env:"KAFKA_BROKERS" env-separator:","`
}

type ServerConfig struct {
    Port         string `yaml:"port" env:"SERVER_PORT" env-default:"8080"`
    ReadTimeout  int    `yaml:"read_timeout" env:"SERVER_READ_TIMEOUT" env-default:"10"`
    WriteTimeout int    `yaml:"write_timeout" env:"SERVER_WRITE_TIMEOUT" env-default:"10"`
}

type DatabaseConfig struct {
    Host     string `yaml:"host" env:"DB_HOST" env-default:"localhost"`
    Port     int    `yaml:"port" env:"DB_PORT" env-default:"5432"`
    User     string `yaml:"user" env:"DB_USER" env-default:"postgres"`
    Password string `yaml:"password" env:"DB_PASSWORD"`
    Name     string `yaml:"name" env:"DB_NAME" env-default:"app"`
    SSLMode  string `yaml:"ssl_mode" env:"DB_SSL_MODE" env-default:"disable"`
}

type CacheConfig struct {
    Host     string `yaml:"host" env:"REDIS_HOST" env-default:"localhost"`
    Port     int    `yaml:"port" env:"REDIS_PORT" env-default:"6379"`
    Password string `yaml:"password" env:"REDIS_PASSWORD"`
    DB       int    `yaml:"db" env:"REDIS_DB" env-default:"0"`
}

type LogConfig struct {
    Level  string `yaml:"level" env:"LOG_LEVEL" env-default:"info"`
    Format string `yaml:"format" env:"LOG_FORMAT" env-default:"json"`
}

func Load(path string) (*Config, error) {
    var cfg Config
    if err := cleanenv.ReadConfig(path, &cfg); err != nil {
        return nil, err
    }
    return &cfg, nil
}
```

#### internal/infrastructure/database/postgres.go — PostgreSQL Connection Pool

This is a **connection factory** — it creates and validates the pool.
The constructor returns `(pool, cleanup, error)` — Wire calls cleanup automatically.

```go
package database

import (
    "context"
    "fmt"
    "log/slog"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"

    "{module}/internal/infrastructure/config"
)

// NewPostgresPool creates a configured pgx connection pool.
// Returns (pool, cleanup, error) — Wire calls cleanup automatically during shutdown.
func NewPostgresPool(ctx context.Context, cfg *config.DatabaseConfig) (*pgxpool.Pool, func(), error) {
    dsn := fmt.Sprintf(
        "host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
        cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode,
    )

    poolCfg, err := pgxpool.ParseConfig(dsn)
    if err != nil {
        return nil, nil, fmt.Errorf("parse postgres config: %w", err)
    }

    poolCfg.MaxConns = 25
    poolCfg.MinConns = 5
    poolCfg.MaxConnLifetime = 30 * time.Minute
    poolCfg.MaxConnIdleTime = 5 * time.Minute

    pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
    if err != nil {
        return nil, nil, fmt.Errorf("create postgres pool: %w", err)
    }

    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        return nil, nil, fmt.Errorf("ping postgres: %w", err)
    }

    cleanup := func() {
        slog.Info("closing database pool...")
        pool.Close()
        slog.Info("database pool closed")
    }

    return pool, cleanup, nil
}
```

#### Adapter Layer

#### internal/adapter/repository/postgres/tx_manager.go — Transaction Manager

The transaction manager sits in the adapter layer (not infrastructure) because it is **used by**
repository implementations. It manages the tx lifecycle and stores the active tx in context,
so all repositories in the same call chain share the same transaction.

```go
package postgres

import (
    "context"
    "fmt"

    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
)

// ---- context helpers for propagating the active tx ----

type txKey struct{}

func withTx(ctx context.Context, tx pgx.Tx) context.Context {
    return context.WithValue(ctx, txKey{}, tx)
}

func txFromCtx(ctx context.Context) (pgx.Tx, bool) {
    tx, ok := ctx.Value(txKey{}).(pgx.Tx)
    return tx, ok
}

// ---- TxManager ----

type TxManager struct {
    pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *TxManager {
    return &TxManager{pool: pool}
}

// WithinTx runs fn inside a database transaction.
// Nested calls reuse the ambient tx from context (no nested BEGIN).
func (t *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
    // Reuse existing tx if present (nested call)
    if _, ok := txFromCtx(ctx); ok {
        return fn(ctx)
    }

    tx, err := t.pool.BeginTx(ctx, pgx.TxOptions{})
    if err != nil {
        return fmt.Errorf("begin tx: %w", err)
    }
    defer func() { _ = tx.Rollback(ctx) }()

    if err = fn(withTx(ctx, tx)); err != nil {
        return err
    }
    return tx.Commit(ctx)
}
```

#### internal/adapter/repository/postgres/qx.go — TX-Aware Query Executor

`QX` wraps SQLC's `*dbgen.Queries` and automatically uses the active transaction
from context when available. Every repository embeds `QX` and calls `q.Q(ctx)`.

```go
package postgres

import (
    "context"

    "{module}/internal/adapter/repository/postgres/dbgen"
    "github.com/jackc/pgx/v5/pgxpool"
)

// QX provides tx-aware access to SQLC-generated queries.
type QX struct {
    pool *pgxpool.Pool
    base *dbgen.Queries // bound to pool (non-tx default)
}

func NewQX(pool *pgxpool.Pool) *QX {
    return &QX{
        pool: pool,
        base: dbgen.New(pool),
    }
}

// Q returns *dbgen.Queries bound to the current tx if present; otherwise to the pool.
func (q *QX) Q(ctx context.Context) *dbgen.Queries {
    if tx, ok := txFromCtx(ctx); ok {
        return dbgen.New(tx) // queries run inside the active transaction
    }
    return q.base
}
```

**How these 3 files work together:**

```bash
┌─────────────────────────────────────────────────────────────────────┐
│ infrastructure/database/postgres.go                                 │
│   NewPostgresPool() → (*pgxpool.Pool, cleanup, error)               │
└────────────────────────────┬────────────────────────────────────────┘
                             │ injected into
                ┌────────────┴────────────┐
                ▼                         ▼
┌──────────────────────────┐ ┌──────────────────────────────┐
│ adapter/repo/tx_manager  │ │ adapter/repo/qx.go           │
│   TxManager.WithinTx()   │ │   QX.Q(ctx) → *dbgen.Queries │
│   - Begins/commits tx    │ │   - Auto-detects tx in ctx   │
│   - Stores tx in ctx     │ │   - Falls back to pool       │
└──────────────────────────┘ └──────────────────────────────┘
                                          │
                                          ▼
                              ┌──────────────────────────┐
                              │ UserRepository           │
                              │   embeds QX              │
                              │   calls q.Q(ctx).Method  │
                              └──────────────────────────┘
```

#### SQLC Configuration & SQL

#### sqlc/sqlc.yaml (SQLC Configuration)

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "/query"
    schema:
      - "/migrations"
      - "/schema"
    database:
      uri: ${MIGRATE_DATABASE_URL}
    gen:
      go:
        package: "dbgen"
        out: "../internal/adapter/repository/postgres/dbgen"
        sql_package: "pgx/v5"
        emit_prepared_queries: true
        overrides:
          - db_type: jsonb
            go_type:
              import: "encoding/json"
              type: "RawMessage"
            nullable: true
```

#### sqlc/schema/001_users.sql

```sql
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    name VARCHAR(100) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_email ON users(email);
```

#### sqlc/query/user.sql

```sql
-- name: CreateUser :one
INSERT INTO users (email, name, password_hash, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: UpdateUser :one
UPDATE users
SET name = $2, updated_at = $3
WHERE id = $1
RETURNING *;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM users
ORDER BY id
LIMIT $1 OFFSET $2;
```

#### internal/adapter/repository/postgres/user_repository.go (uses SQLC)

```go
package postgres

import (
    "context"

    "{module}/internal/adapter/repository/postgres/dbgen"
    "{module}/internal/domain"
    "{module}/internal/domain/entity"
)

type UserRepository struct {
    q *dbgen.Queries
}

func NewUserRepository(db dbgen.DBTX) *UserRepository {
    return &UserRepository{
        q: dbgen.New(db),
    }
}

// Ensure interface compliance
var _ domain.UserRepository = (*UserRepository)(nil)

func (r *UserRepository) Create(ctx context.Context, user *entity.User) error {
    result, err := r.q.CreateUser(ctx, dbgen.CreateUserParams{
        Email:        user.Email,
        Name:         user.Name,
        PasswordHash: user.Password,
        CreatedAt:    user.CreatedAt,
        UpdatedAt:    user.UpdatedAt,
    })
    if err != nil {
        return err
    }
    user.ID = result.ID
    return nil
}

func (r *UserRepository) GetByID(ctx context.Context, id int64) (*entity.User, error) {
    row, err := r.q.GetUserByID(ctx, id)
    if err != nil {
        return nil, err
    }
    return r.toEntity(row), nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*entity.User, error) {
    row, err := r.q.GetUserByEmail(ctx, email)
    if err != nil {
        return nil, err
    }
    return r.toEntity(row), nil
}

func (r *UserRepository) toEntity(row dbgen.User) *entity.User {
    return &entity.User{
        ID:        row.ID,
        Email:     row.Email,
        Name:      row.Name,
        Password:  row.PasswordHash,
        CreatedAt: row.CreatedAt,
        UpdatedAt: row.UpdatedAt,
    }
}
```

#### internal/infrastructure/pubsub/kafka.go — Kafka Connection Factory

Infrastructure only creates the producer. Returns `(producer, cleanup, error)` —
Wire calls cleanup automatically during shutdown.

```go
package pubsub

import (
    "fmt"
    "log/slog"

    "github.com/IBM/sarama"

    "{module}/internal/infrastructure/config"
)

// NewKafkaProducer creates a Sarama sync producer.
// Returns (producer, cleanup, error) — Wire calls cleanup automatically during shutdown.
func NewKafkaProducer(cfg *config.KafkaConfig) (sarama.SyncProducer, func(), error) {
    saramaCfg := sarama.NewConfig()
    saramaCfg.Producer.Return.Successes = true
    saramaCfg.Producer.RequiredAcks = sarama.WaitForAll
    saramaCfg.Producer.Retry.Max = 3

    producer, err := sarama.NewSyncProducer(cfg.Brokers, saramaCfg)
    if err != nil {
        return nil, nil, fmt.Errorf("create kafka producer: %w", err)
    }

    cleanup := func() {
        slog.Info("closing kafka producer...")
        if err := producer.Close(); err != nil {
            slog.Error("kafka producer close error", "error", err)
        } else {
            slog.Info("kafka producer closed")
        }
    }

    return producer, cleanup, nil
}
```

#### internal/adapter/pubsub/message.go — Wire-format Message DTOs

Message DTOs live in the **adapter** layer and own `json` tags.
This is where you control field names for Kafka consumers (which may be Python, Java, etc.).
Domain events have NO `json` tags — the publisher maps domain → message DTO before serializing.

```go
package pubsub

import "time"

// UserCreatedMessage is the wire format for the "user.created" topic.
type UserCreatedMessage struct {
    UserID    int64     `json:"user_id"`
    Email     string    `json:"email"`
    Name      string    `json:"name"`
    CreatedAt time.Time `json:"created_at"`
}

// OrderPlacedMessage is the wire format for the "order.placed" topic.
type OrderPlacedMessage struct {
    OrderID  int64     `json:"order_id"`
    UserID   int64     `json:"user_id"`
    Total    int64     `json:"total"`
    PlacedAt time.Time `json:"placed_at"`
}
```

#### internal/adapter/pubsub/kafka_publisher.go — Kafka EventPublisher Implementation

This is an **adapter** because it implements the domain's `event.EventPublisher` interface.
It maps domain events → message DTOs (with json tags) → Kafka.
If you want to switch to RabbitMQ later, create `adapter/pubsub/rabbitmq_publisher.go`
implementing the same interface — no use case code changes needed.

```go
package pubsub

import (
    "context"
    "encoding/json"
    "fmt"

    "github.com/IBM/sarama"

    "{module}/internal/domain/event"
)

// KafkaPublisher implements event.EventPublisher using Kafka.
type KafkaPublisher struct {
    producer sarama.SyncProducer
}

func NewKafkaPublisher(producer sarama.SyncProducer) *KafkaPublisher {
    return &KafkaPublisher{producer: producer}
}

// Ensure interface compliance at compile time
var _ event.EventPublisher = (*KafkaPublisher)(nil)

func (p *KafkaPublisher) PublishUserCreated(ctx context.Context, evt event.UserCreated) error {
    // Map domain event → wire-format message DTO (with json tags)
    msg := UserCreatedMessage{
        UserID:    evt.UserID,
        Email:     evt.Email,
        Name:      evt.Name,
        CreatedAt: evt.CreatedAt,
    }
    return p.publish("user.created", msg)
}

func (p *KafkaPublisher) PublishOrderPlaced(ctx context.Context, evt event.OrderPlaced) error {
    msg := OrderPlacedMessage{
        OrderID:  evt.OrderID,
        UserID:   evt.UserID,
        Total:    evt.Total,
        PlacedAt: evt.PlacedAt,
    }
    return p.publish("order.placed", msg)
}

// publish serializes the message DTO and sends it to Kafka.
func (p *KafkaPublisher) publish(topic string, payload any) error {
    data, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("marshal event: %w", err)
    }

    _, _, err = p.producer.SendMessage(&sarama.ProducerMessage{
        Topic: topic,
        Value: sarama.ByteEncoder(data),
    })
    if err != nil {
        return fmt.Errorf("publish to %s: %w", topic, err)
    }
    return nil
}
```

**The full flow — domain event stays pure, adapter handles serialization:**

```bash
┌─────────────────────┐      ┌────────────────────────┐      ┌──────────────────────┐
│  Use Case           │      │  Adapter (publisher)   │      │  Kafka               │
│                     │      │                        │      │                      │
│  event.UserCreated  │ ───▶ │  UserCreatedMessage    │ ───▶ │ {"user_id": 1, ...}  │
│  (no json tags)     │      │  (has json tags)       │      │  (wire format)       │
└─────────────────────┘      └────────────────────────┘      └──────────────────────┘
```

**Swapping message brokers — just add a new adapter:**

```bash
adapter/pubsub/
├── message.go               # Wire-format DTOs (json tags) — shared across brokers
├── kafka_publisher.go       # implements event.EventPublisher via Kafka
├── rabbitmq_publisher.go    # implements event.EventPublisher via RabbitMQ (swap)
└── nats_publisher.go        # implements event.EventPublisher via NATS (swap)
```

Only the `wire.go` binding changes — use cases and handlers are untouched:

```go
// wire.go — swap Kafka for RabbitMQ by changing ONE line:
wire.Bind(new(event.EventPublisher), new(*pubsub.KafkaPublisher)),
// →
wire.Bind(new(event.EventPublisher), new(*pubsub.RabbitMQPublisher)),
```

#### Dependency Injection & App Lifecycle

#### internal/infrastructure/di/wire.go

Separate providers into logical sets so each layer is independently maintainable.
When adding a new feature, you only add to the relevant set.

No `App` struct — Wire returns the `*server.HTTPServer` directly. Each component's
constructor returns `(instance, cleanup, error)`, and Wire composes all cleanup
functions into a single combined cleanup that runs in **reverse dependency order**.

```go
//go:build wireinject

package di

import (
    "context"

    "github.com/google/wire"

    adapterpubsub "{module}/internal/adapter/pubsub"
    "{module}/internal/adapter/repository/postgres"
    "{module}/internal/domain"
    "{module}/internal/domain/event"
    "{module}/internal/infrastructure/config"
    "{module}/internal/infrastructure/database"
    infraps "{module}/internal/infrastructure/pubsub"
    "{module}/internal/infrastructure/server"
    "{module}/internal/transport/http/v1"
    "{module}/internal/transport/http/v1/user"
    "{module}/internal/usecase/user"
)

// InfraSet provides raw technical resources (connections, config, logging)
var InfraSet = wire.NewSet(
    database.NewPostgresPool,
    infraps.NewKafkaProducer,
    server.NewHTTPServer,
    provideLogger,
)

// RepoSet provides repository implementations bound to domain interfaces
var RepoSet = wire.NewSet(
    postgres.NewUserRepository,
    postgres.NewTxManager,
    postgres.NewQX,
    wire.Bind(new(domain.UserRepository), new(*postgres.UserRepository)),
)

// PubSubSet provides event publisher implementations
var PubSubSet = wire.NewSet(
    adapterpubsub.NewKafkaPublisher,
    wire.Bind(new(event.EventPublisher), new(*adapterpubsub.KafkaPublisher)),
)

// UseCaseSet provides application business logic services
var UseCaseSet = wire.NewSet(
    user.NewService,
)

// TransportSet provides HTTP/gRPC handlers and route registration
var TransportSet = wire.NewSet(
    user.NewHandler,
    v1.NewRegistrar,
)

// InitializeHTTPServer wires all dependencies and returns the HTTP server.
// Wire auto-generates a combined cleanup function from all components that
// return (instance, cleanup, error) — e.g. NewPostgresPool, NewKafkaProducer,
// NewHTTPServer. The cleanup runs in reverse dependency order.
func InitializeHTTPServer(ctx context.Context, cfg *config.Config) (*server.HTTPServer, func(), error) {
    wire.Build(
        InfraSet,
        RepoSet,
        PubSubSet,
        UseCaseSet,
        TransportSet,
    )
    return nil, nil, nil
}
```

**Why no `App` struct?** Each component owns its own cleanup via the `(instance, cleanup, error)`
return pattern. Wire composes all cleanup functions automatically. The `app/` package provides
a simple `RunServer` function for lifecycle management.

#### internal/app/server.go — Server Lifecycle

The `app` package manages the server lifecycle: start → wait for signal → exit.
Cleanup is handled entirely by Wire's composed cleanup function via `defer`.

```go
package app

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"

    "{module}/internal/infrastructure/config"
    "{module}/internal/infrastructure/di"
    "{module}/internal/infrastructure/server"
    "{module}/pkg/locale"
)

type initServerFn func(ctx context.Context, cfg *config.Config) (*server.HTTPServer, func(), error)

func RunHTTPServer() {
    RunServer(di.InitializeHTTPServer)
}

func RunServer(fn initServerFn) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // 1. Load config
    cfg, err := config.Load("config/config.yaml")
    if err != nil {
        slog.Error("failed to load config", "error", err)
        os.Exit(1)
    }

    // 2. Initialize locale translations (explicit, no init())
    locale.NewMapping()

    // 3. Wire all dependencies — cleanup closes everything in reverse order
    srv, cleanup, err := fn(ctx, cfg)
    if err != nil {
        slog.Error("failed to initialize server", "error", err)
        os.Exit(1)
    }
    defer cleanup()

    // 4. Start server in background
    errCh := make(chan error, 1)
    go func() {
        if err := srv.Start(); err != nil {
            errCh <- err
        }
    }()

    // 5. Wait for interrupt signal or server error
    waitSignal(errCh)
    slog.Info("shutting down...")
    // cleanup() runs via defer — closes HTTP server, Kafka, DB in reverse order
}

func waitSignal(errCh <-chan error) {
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

    select {
    case sig := <-quit:
        slog.Info("received signal", "signal", sig)
    case err := <-errCh:
        slog.Error("server error", "error", err)
    }
}
```

#### Entry Points

#### main.go — Single Entry Point

```go
package main

import "{module}/cmd"

func main() {
    cmd.Execute()
}
```

#### cmd/root.go — Cobra Root Command

```go
package cmd

import "github.com/spf13/cobra"

func Execute() {
    root := &cobra.Command{
        Use:   "{project}",
        Short: "Project description",
    }

    root.AddCommand(
        apiCommand(),
        // grpcCommand(),
        // cronjobCommand(),
    )

    if err := root.Execute(); err != nil {
        panic(err)
    }
}
```

#### cmd/api.go — API Subcommand

```go
package cmd

import (
    "github.com/spf13/cobra"

    "{module}/internal/app"
    "{module}/pkg/locale"
)

func apiCommand() *cobra.Command {
    return &cobra.Command{
        Use:   "api",
        Short: "Start the HTTP server",
        Run: func(*cobra.Command, []string) {
            locale.NewMapping()
            app.RunHTTPServer()
        },
    }
}
```

**How Wire cleanup works — each component owns its shutdown:**

```bash
# Each constructor returns (instance, cleanup, error):

NewPostgresPool()  → (*pgxpool.Pool, func() { pool.Close() }, nil)
NewKafkaProducer() → (sarama.SyncProducer, func() { producer.Close() }, nil)
NewHTTPServer()    → (*HTTPServer, func() { app.ShutdownWithContext(...) }, nil)

# Wire generates a combined cleanup that runs ALL cleanups in reverse order:
# cleanup = func() {
#     httpServerCleanup()    ← stop accepting requests
#     kafkaCleanup()         ← flush and close producer
#     postgresCleanup()      ← close connection pool
# }
```

**Cleanup order (fully automatic via Wire):**

```bash
SIGINT/SIGTERM received
    │
    ▼
defer cleanup() runs (Wire-generated, reverse dependency order)
    │
    ├── 1. HTTPServer cleanup   ← stop accepting requests, drain in-flight
    ├── 2. Kafka cleanup        ← flush pending messages, close producer
    └── 3. Postgres cleanup     ← close connection pool
    │
    ▼
Process exits cleanly
```

#### Build & Deploy

#### Makefile

```makefile
.PHONY: build run test lint migrate wire generate

# Build
build:
 go build -o bin/{project} .

run:
 go run . api

# Development
dev:
 air -c .air.toml

# Testing
test:
 go test -v -race -cover ./...

test-coverage:
 go test -coverprofile=coverage.out ./...
 go tool cover -html=coverage.out -o coverage.html

# Linting
lint:
 golangci-lint run

# Database
migrate-up:
 migrate -path sqlc/migrations -database "$(DATABASE_URL)" up

migrate-down:
 migrate -path sqlc/migrations -database "$(DATABASE_URL)" down

migrate-create:
 migrate create -ext sql -dir sqlc/migrations -seq $(name)

# Code generation
wire:
 cd internal/infrastructure/di && wire

sqlc:
 cd sqlc && sqlc generate

proto:
 protoc --go_out=. --go-grpc_out=. api/proto/**/*.proto

generate: wire sqlc

# Docker
docker-build:
 docker build -t $(APP_NAME) .

docker-up:
 docker-compose up -d

docker-down:
 docker-compose down
```

#### docker-compose.yaml

```yaml
version: "3.8"

services:
  app:
    build: .
    ports:
      - "8080:8080"
    environment:
      - DB_HOST=postgres
      - REDIS_HOST=redis
    depends_on:
      - postgres
      - redis

  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: app
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    volumes:
      - redis_data:/data

  kafka:
    image: bitnami/kafka:3.7
    ports:
      - "9092:9092"
    environment:
      - KAFKA_CFG_NODE_ID=0
      - KAFKA_CFG_PROCESS_ROLES=controller,broker
      - KAFKA_CFG_LISTENERS=PLAINTEXT://:9092,CONTROLLER://:9093
      - KAFKA_CFG_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT
      - KAFKA_CFG_CONTROLLER_QUORUM_VOTERS=0@kafka:9093
      - KAFKA_CFG_CONTROLLER_LISTENER_NAMES=CONTROLLER
    volumes:
      - kafka_data:/bitnami/kafka

volumes:
  postgres_data:
  redis_data:
  kafka_data:
```

### 5. CLI Usage

```bash
# Interactive mode
nova new

# With flags (skip prompts)
nova new myproject \
  --module=github.com/myorg/myproject \
  --transport=http \
  --http-framework=fiber \
  --database=postgres \
  --db-driver=pgx \
  --query=sqlc \
  --cache=redis \
  --queue=none \
  --config=yaml \
  --di=wire \
  --docker \
  --makefile \
  --ci=github

# Generate additional components
nova generate entity Order
nova generate usecase order
nova generate handler order
nova generate repository order --type=postgres
```

### 6. Key Features to Include

- **Graceful shutdown** with signal handling
- **Context propagation** through all layers
- **Structured logging** with slog (Go 1.21+)
- **Request ID** middleware for tracing
- **Health check** endpoint (`/health`, `/ready`)
- **Metrics** endpoint for Prometheus (`/metrics`)
- **OpenTelemetry** hooks for distributed tracing
- **Input validation** with go-playground/validator
- **Error handling** with custom error types and codes
- **API versioning** via URL path (`/api/v1/...`)
- **CORS** middleware (configurable)
- **Rate limiting** middleware
- **Request/response logging** middleware
- **Panic recovery** middleware

Generate the complete scaffolding tool with all templates embedded using go:embed directive.

---

## Quick Start (Simple Version)

If you just want to generate a project directly without building the CLI tool:

Generate a Go REST API project with Clean Architecture.

Stack:

- HTTP: Fiber framework
- Database: PostgreSQL with pgx driver
- Query: SQLC for type-safe queries
- Cache: Redis
- Config: YAML with cleanenv + env override
- DI: Google Wire
- CLI: Cobra for multiple entry points

Structure:

- internal/domain/entity/ - Business entities (pure Go)
- internal/domain/ - Repository interfaces + filter structs (one file per aggregate: user.go, order.go)
- internal/usecase/{feature}/ - Business logic services
- internal/adapter/repository/ - Repository implementations
- internal/transport/http/ - HTTP handlers, middleware, httputil (per-feature packages)
- internal/transport/cronjob/ - Cron job handlers
- internal/infrastructure/ - Config, DB, server setup, DI

Include:

- Example User CRUD
- Graceful shutdown
- Health endpoints
- Structured logging (slog)
- Docker + docker-compose
- Makefile
- GitHub Actions CI

The domain layer must have zero external dependencies.
