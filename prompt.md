# AI Prompt: Go Clean Architecture Project Generator

Create a Go CLI tool called "nova" that generates a new Go project with Clean Architecture. The tool should have interactive prompts and generate a complete, production-ready project structure.

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
├── cmd/
│   ├── api/
│   │   └── main.go                 # HTTP server entry point
│   ├── grpc/
│   │   └── main.go                 # gRPC server entry point (if selected)
│   └── migrate/
│       └── main.go                 # Database migration CLI
│
├── internal/
│   │
│   ├── domain/                     # Layer 1: Enterprise Business Rules (innermost)
│   │   │                           # ⚠️  ZERO external dependencies allowed
│   │   ├── entity/                 # Business entities - pure Go structs
│   │   │   └── user.go
│   │   ├── repository/             # Repository INTERFACES (ports) - not implementations!
│   │   │   └── user_repository.go
│   │   ├── service/                # Domain service INTERFACES
│   │   │   └── user_service.go
│   │   └── valueobject/            # Value objects (Email, Money, etc.)
│   │       └── email.go
│   │
│   ├── usecase/                    # Layer 2: Application Business Rules
│   │   │                           # Depends on: domain only
│   │   └── user/
│   │       ├── service.go          # Use case implementation (orchestrates domain)
│   │       ├── dto.go              # Input/Output DTOs for this use case
│   │       └── errors.go           # Use case specific errors
│   │
│   ├── adapter/                    # Layer 3: Interface Adapters
│   │   │                           # Depends on: domain, usecase
│   │   │                           # PURPOSE: Implements domain interfaces
│   │   │
│   │   ├── repository/             # Repository IMPLEMENTATIONS
│   │   │   ├── postgres/           # Implements domain.UserRepository
│   │   │   │   ├── user_repository.go
│   │   │   │   └── queries.sql.go  # SQLC generated (if using SQLC)
│   │   │   └── cache/              # Cache repository decorator
│   │   │       └── user_cache.go
│   │   │
│   │   ├── handler/                # HTTP/gRPC handlers (controllers)
│   │   │   ├── http/
│   │   │   │   ├── v1/
│   │   │   │   │   ├── user_handler.go
│   │   │   │   │   └── router.go
│   │   │   │   └── middleware/     # HTTP-specific middleware
│   │   │   │       ├── auth.go
│   │   │   │       ├── logging.go
│   │   │   │       └── recovery.go
│   │   │   └── grpc/
│   │   │       └── user_handler.go
│   │   │
│   │   ├── presenter/              # Response formatters (transforms domain → API response)
│   │   │   └── json_presenter.go
│   │   │
│   │   └── external/               # External service adapters (calls other APIs)
│   │       └── payment_gateway.go  # Implements domain.PaymentGateway interface
│   │
│   └── infrastructure/             # Layer 4: Frameworks & Drivers (outermost)
│       │                           # Depends on: all layers
│       │                           # PURPOSE: Technical capabilities, NOT business logic
│       │
│       ├── config/                 # Configuration loading
│       │   ├── config.go
│       │   └── config.yaml
│       │
│       ├── database/               # Database CONNECTION (not queries!)
│       │   └── postgres.go         # Returns *pgxpool.Pool - connection factory
│       │
│       ├── cache/                  # Cache CONNECTION
│       │   └── redis.go            # Returns *redis.Client - connection factory
│       │
│       ├── pubsub/                 # Message queue CONNECTION
│       │   └── kafka.go            # Returns kafka producer/consumer
│       │
│       ├── server/                 # Server bootstrap
│       │   ├── http.go             # HTTP server setup + graceful shutdown
│       │   └── grpc.go             # gRPC server setup
│       │
│       ├── logger/                 # Logging setup
│       │   └── logger.go
│       │
│       ├── observability/          # Monitoring setup
│       │   ├── tracer.go           # OpenTelemetry tracing setup
│       │   └── metrics.go          # Prometheus metrics setup
│       │
│       └── di/                     # Dependency injection wiring
│           ├── wire.go
│           └── wire_gen.go
│
├── pkg/ # Public shared packages
│ ├── errors/
│ │ └── errors.go # Custom error types
│ ├── validator/
│ │ └── validator.go # Input validation
│ └── httputil/
│ └── response.go # HTTP response helpers
│
├── api/ # API Definitions
│ ├── openapi/
│ │ └── openapi.yaml # OpenAPI 3.0 spec
│ └── proto/
│ └── user/
│ └── user.proto # Protobuf definitions
│
├── migrations/ # SQL migrations (if using golang-migrate)
│ ├── 000001_create_users_table.up.sql
│ └── 000001_create_users_table.down.sql
│
├── scripts/
│ └── generate.sh # Code generation script
│
├── .github/
│ └── workflows/
│ ├── ci.yaml # CI pipeline
│ └── pull_request_template.md # Pull request template
│ └── release.yaml # Release pipeline
│
├── config/
│ ├── config.yaml # Development config
│ ├── config.prod.yaml # Production config
│ └── config.example.yaml # Example config for documentation
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
| **Examples**                 | Repository implementations, HTTP handlers, gRPC handlers, external API clients, presenters |

**Adapter components:**

```bash
adapter/
├── repository/           # Implements domain.XxxRepository
│   ├── postgres/         # SQL implementation
│   ├── mongodb/          # MongoDB implementation
│   └── cache/            # Cache decorator
├── handler/              # Receives requests, calls use cases
│   ├── http/             # HTTP controllers
│   └── grpc/             # gRPC handlers
├── presenter/            # Transforms domain → API response
└── external/             # Implements domain interfaces for external APIs
    └── stripe_payment.go # Implements domain.PaymentGateway
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
├── config/           # Config loading (returns Config struct)
├── database/         # Connection factory (returns *pgxpool.Pool)
├── cache/            # Connection factory (returns *redis.Client)
├── pubsub/           # Connection factory (returns kafka.Producer)
├── server/           # Server bootstrap and graceful shutdown
├── logger/           # Logger setup (returns *slog.Logger)
├── observability/    # Tracer/metrics setup
└── di/               # Wires everything together
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
| `UserHandler.Create(c *fiber.Ctx)`             | **adapter/**        | Calls use case, transforms request/response      |
| `StripeClient.Charge()`                        | **adapter/**        | Implements `domain.PaymentGateway`               |
| `logger.New()` → returns `*slog.Logger`        | **infrastructure/** | Logger setup                                     |
| `wire.Build()` → wires dependencies            | **infrastructure/** | DI setup                                         |

#### **The Key Insight**

> **Adapter** = "I know how to talk TO the business" (implements domain interfaces)
>
> **Infrastructure** = "I know how to SET UP technical resources" (provides raw capabilities)

Think of it this way:

- Infrastructure gives you `*pgxpool.Pool`
- Adapter uses that pool to implement `domain.UserRepository`

### 4. Code Templates to Generate

#### internal/domain/entity/user.go

```go
package entity

import "time"

type User struct {
    ID        int64     `json:"id"`
    Email     string    `json:"email"`
    Name      string    `json:"name"`
    Password  string    `json:"-"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
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

#### internal/domain/repository/user_repository.go

```go
package repository

import (
    "context"
    "{module}/internal/domain/entity"
)

type UserRepository interface {
    Create(ctx context.Context, user *entity.User) error
    GetByID(ctx context.Context, id int64) (*entity.User, error)
    GetByEmail(ctx context.Context, email string) (*entity.User, error)
    Update(ctx context.Context, user *entity.User) error
    Delete(ctx context.Context, id int64) error
    List(ctx context.Context, limit, offset int) ([]*entity.User, error)
}
```

#### internal/usecase/user/service.go

```go
package user

import (
    "context"
    "log/slog"

    "{module}/internal/domain/entity"
    "{module}/internal/domain/repository"
)

type Service struct {
    userRepo repository.UserRepository
    logger   *slog.Logger
}

func NewService(userRepo repository.UserRepository, logger *slog.Logger) *Service {
    return &Service{
        userRepo: userRepo,
        logger:   logger,
    }
}

func (s *Service) CreateUser(ctx context.Context, input CreateUserInput) (*entity.User, error) {
    user := entity.NewUser(input.Email, input.Name, input.Password)

    if err := s.userRepo.Create(ctx, user); err != nil {
        s.logger.Error("failed to create user", "error", err)
        return nil, err
    }

    return user, nil
}

func (s *Service) GetUser(ctx context.Context, id int64) (*entity.User, error) {
    return s.userRepo.GetByID(ctx, id)
}
```

#### internal/usecase/user/dto.go

```go
package user

type CreateUserInput struct {
    Email    string `json:"email" validate:"required,email"`
    Name     string `json:"name" validate:"required,min=2,max=100"`
    Password string `json:"password" validate:"required,min=8"`
}

type UpdateUserInput struct {
    Name string `json:"name" validate:"omitempty,min=2,max=100"`
}
```

#### internal/adapter/handler/http/v1/user_handler.go

```go
package v1

import (
    "github.com/gofiber/fiber/v2"

    useruc "{module}/internal/usecase/user"
)

type UserHandler struct {
    userService *useruc.Service
}

func NewUserHandler(userService *useruc.Service) *UserHandler {
    return &UserHandler{userService: userService}
}

func (h *UserHandler) Create(c *fiber.Ctx) error {
    var input useruc.CreateUserInput
    if err := c.BodyParser(&input); err != nil {
        return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
    }

    user, err := h.userService.CreateUser(c.Context(), input)
    if err != nil {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
    }

    return c.Status(fiber.StatusCreated).JSON(user)
}

func (h *UserHandler) GetByID(c *fiber.Ctx) error {
    id, err := c.ParamsInt("id")
    if err != nil {
        return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid id"})
    }

    user, err := h.userService.GetUser(c.Context(), int64(id))
    if err != nil {
        return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
    }

    return c.JSON(user)
}
```

#### internal/adapter/handler/http/v1/router.go

```go
package v1

import "github.com/gofiber/fiber/v2"

func RegisterRoutes(app *fiber.App, userHandler *UserHandler) {
    api := app.Group("/api/v1")

    users := api.Group("/users")
    users.Post("/", userHandler.Create)
    users.Get("/:id", userHandler.GetByID)
}
```

#### internal/infrastructure/server/http.go

```go
package server

import (
    "context"
    "log/slog"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/gofiber/fiber/v2"

    "{module}/internal/infrastructure/config"
)

type HTTPServer struct {
    app    *fiber.App
    cfg    *config.Config
    logger *slog.Logger
}

func NewHTTPServer(cfg *config.Config, logger *slog.Logger) *HTTPServer {
    app := fiber.New(fiber.Config{
        ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
        WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
    })

    return &HTTPServer{
        app:    app,
        cfg:    cfg,
        logger: logger,
    }
}

func (s *HTTPServer) App() *fiber.App {
    return s.app
}

func (s *HTTPServer) Start() error {
    go func() {
        if err := s.app.Listen(":" + s.cfg.Server.Port); err != nil {
            s.logger.Error("server error", "error", err)
        }
    }()

    // Graceful shutdown
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    s.logger.Info("shutting down server...")

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    return s.app.ShutdownWithContext(ctx)
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
    Log      LogConfig      `yaml:"log"`
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

#### internal/infrastructure/di/wire.go

```go
//go:build wireinject

package di

import (
    "github.com/google/wire"

    "{module}/internal/adapter/handler/http/v1"
    "{module}/internal/adapter/repository/postgres"
    "{module}/internal/infrastructure/config"
    "{module}/internal/infrastructure/database"
    "{module}/internal/infrastructure/server"
    "{module}/internal/usecase/user"
)

func InitializeApp(cfg *config.Config) (*App, error) {
    wire.Build(
        // Infrastructure
        database.NewPostgresDB,
        server.NewHTTPServer,
        provideLogger,

        // Repositories
        postgres.NewUserRepository,
        wire.Bind(new(repository.UserRepository), new(*postgres.UserRepository)),

        // Use cases
        user.NewService,

        // Handlers
        v1.NewUserHandler,

        // App
        NewApp,
    )
    return nil, nil
}
```

#### Makefile

```makefile
.PHONY: build run test lint migrate wire generate

# Build
build:
 go build -o bin/api ./cmd/api

run:
 go run ./cmd/api

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
 migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
 migrate -path migrations -database "$(DATABASE_URL)" down

migrate-create:
 migrate create -ext sql -dir migrations -seq $(name)

# Code generation
wire:
 cd internal/infrastructure/di && wire

sqlc:
 sqlc generate

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

volumes:
  postgres_data:
  redis_data:
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
- internal/domain/repository/ - Repository interfaces
- internal/usecase/{feature}/ - Business logic services
- internal/adapter/repository/ - Repository implementations
- internal/adapter/handler/http/ - HTTP handlers
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
