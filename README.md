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

```
myproject/
├── cmd/api/main.go                          # Server entry point
├── internal/
│   ├── domain/          ← Layer 1: pure Go, zero deps
│   │   ├── entity/
│   │   ├── repository/  (interfaces only)
│   │   └── valueobject/
│   ├── usecase/         ← Layer 2: business logic
│   ├── adapter/         ← Layer 3: implements domain interfaces
│   │   ├── repository/postgres/
│   │   ├── handler/http/v1/
│   │   └── handler/http/middleware/
│   └── infrastructure/  ← Layer 4: config, DB, server, DI
├── pkg/                                     # Shared packages
├── migrations/                              # SQL migrations
├── Dockerfile, docker-compose.yaml
├── Makefile, .github/workflows/ci.yaml
└── api/openapi/openapi.yaml
```

Includes a working **User CRUD** example wired across all layers.

## Supported Options

| Category | Choices |
|----------|---------|
| **HTTP Framework** | Fiber · Gin · Chi · Echo |
| **Database** | PostgreSQL · MySQL · SQLite · MongoDB · None |
| **DB Driver** | pgx · sqlx · gorm · database/sql |
| **Cache** | Redis · BigCache · None |
| **Message Queue** | Kafka · RabbitMQ · NATS · None |
| **Config** | YAML · TOML · Env-only |
| **DI** | Google Wire · Uber fx · Manual |
| **Extras** | Docker · Makefile · GitHub Actions CI |

## Scaffold Components

Add components to an existing project:

```bash
nova generate entity Order
nova generate usecase order
nova generate handler order
nova generate repository order --type=postgres
```

## Development

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
