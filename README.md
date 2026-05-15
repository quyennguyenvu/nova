# Nova

A CLI tool that generates production-ready Go projects with **Clean Architecture**.

## Install

### From GitHub Releases (recommended)

Download the latest binary for your platform from [Releases](https://github.com/quyennguyenvu/nova/releases):

```bash
# macOS (Apple Silicon)
curl -Lo nova.tar.gz https://github.com/quyennguyenvu/nova/releases/latest/download/nova_darwin_arm64.tar.gz

# macOS (Intel)
curl -Lo nova.tar.gz https://github.com/quyennguyenvu/nova/releases/latest/download/nova_darwin_amd64.tar.gz

# Linux
curl -Lo nova.tar.gz https://github.com/quyennguyenvu/nova/releases/latest/download/nova_linux_amd64.tar.gz

tar xzf nova.tar.gz
sudo mv nova /usr/local/bin/
```

### From source

```bash
go install github.com/quyennguyenvu/nova@latest
```

## Quick Start

```bash
# Interactive mode — walks you through all options
nova new

# Or skip prompts with flags
nova new myproject \
  --module=github.com/myorg/myproject \
  --transport=http \
  --http-framework=fiber \
  --database=postgres \
  --db-driver=pgx \
  --cache=redis \
  --di=manual \
  --docker --makefile --ci=github
```

## What Gets Generated

A complete project following Clean Architecture's dependency rule:

```bash
myproject/
├── main.go
├── go.mod
├── .env.example
├── cmd/
│   └── api.go                               # Server bootstrap (or grpc.go)
├── internal/
│   ├── domain/          ← Layer 1: pure Go, zero deps
│   │   ├── entity/
│   │   ├── repository/  (interfaces only)
│   │   ├── service/
│   │   ├── valueobject/
│   │   └── event/
│   ├── usecase/         ← Layer 2: business logic
│   │   └── user/        (service, DTOs, errors)
│   ├── adapter/         ← Layer 3: implements domain interfaces
│   │   ├── assembler/
│   │   ├── presenter/
│   │   ├── repository/postgres/
│   │   ├── handler/http/v1/
│   │   └── handler/http/middleware/
│   └── infrastructure/  ← Layer 4: config, DB, server, DI
│       ├── config/
│       ├── database/
│       ├── cache/
│       ├── logger/
│       ├── server/
│       └── di/
├── pkg/                                     # Shared packages
│   ├── errors/
│   ├── httputil/
│   ├── locale/
│   └── validator/
├── migrations/                              # SQL migrations
├── api/openapi/openapi.yaml
├── Dockerfile, docker-compose.yaml
├── Makefile
└── .github/workflows/ci.yaml
```

Includes a working **User CRUD** example wired across all layers.

## Commands

### `nova new`

Generate a new project. Run without flags for interactive mode, or pass flags to skip prompts.

```bash
nova new                          # interactive
nova new myapp --transport=http   # non-interactive (any flag skips prompts)
```

**All flags:**

| Flag               | Values                                | Default                   |
| ------------------ | ------------------------------------- | ------------------------- |
| `--module`         | Go module path                        | `github.com/myorg/<name>` |
| `--transport`      | `http`, `grpc`                        | `http`                    |
| `--http-framework` | `fiber`, `gin`, `chi`, `echo`         | `fiber`                   |
| `--database`       | `postgres`, `mysql`, `none`           | `postgres`                |
| `--db-driver`      | `pgx`, `sqlx`, `gorm`, `database/sql` | `pgx`                     |
| `--query`          | `sqlc`, `raw`, `gorm`                 | `sqlc`                    |
| `--cache`          | `redis`, `none`                       | `redis`                   |
| `--queue`          | `kafka`, `none`                       | `none`                    |
| `--config`         | `yaml`, `toml`, `env`                 | `yaml`                    |
| `--di`             | `wire`, `manual`                      | `manual`                  |
| `--docker`         | _(bool)_                              | `true`                    |
| `--makefile`       | _(bool)_                              | `true`                    |
| `--ci`             | `github`, `none`                      | `github`                  |

### `nova generate`

Scaffold individual components into an existing project:

```bash
nova generate entity Order                     # domain entity + repository interface
nova generate usecase order                    # service, DTOs, errors
nova generate handler order                    # HTTP handler
nova generate repository order --type=postgres # repository implementation
```

## Architecture Overview

Nova projects follow the **dependency rule** — inner layers never import outer layers:

```bash
Domain  →  Use Case  →  Adapter  →  Infrastructure
(entities,   (business    (handlers,     (config, DB,
 interfaces)  logic)       repos impl)    server, DI)
```

| Layer              | Path                       | Responsibility                                                         |
| ------------------ | -------------------------- | ---------------------------------------------------------------------- |
| **Domain**         | `internal/domain/`         | Entities, repository interfaces, value objects, domain events          |
| **Use Case**       | `internal/usecase/`        | Business logic, DTOs, service errors                                   |
| **Adapter**        | `internal/adapter/`        | HTTP/gRPC handlers, repository implementations, assemblers, presenters |
| **Infrastructure** | `internal/infrastructure/` | Config, database connections, caching, server setup, DI wiring         |
| **Pkg**            | `pkg/`                     | Shared utilities (errors, HTTP helpers, validation, i18n)              |

## Development

```bash
make build            # Build the nova binary to bin/nova
make run              # Build and show help
make clean            # Remove build artifacts
make rebuild          # Clean + rebuild

make generate         # Generate a project in interactive mode
make generate-all     # Generate a full project (Fiber/Postgres/Redis/Wire)
make generate-minimal # Generate a minimal project (no DB, no cache)
make verify-gen       # Generate + list all output files
make diff-gen         # Generate + print key files for review

make test             # Run tests
make lint             # Run golangci-lint
make fmt              # Format source files
make vet              # Run go vet
```

```bash
make build          # Build nova binary
make generate       # Interactive generation
make generate-all   # Generate with default flags for testing
make verify-gen     # Generate + list all output files
make help           # Show all commands
```

## Releasing

```bash
git tag v0.1.0
git push origin v0.1.0
# → GitHub Actions builds binaries for Linux/macOS/Windows and uploads to Releases
```

## License

MIT
