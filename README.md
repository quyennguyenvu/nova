# Nova

A Go CLI that generates production-ready Go projects following **Clean Architecture**.

```bash
nova new                          # interactive — walks you through every option
nova new myapp --transport=http   # non-interactive — any flag skips prompts
nova generate entity Order        # scaffold a single component into an existing project
```

## Install

### From a release binary (recommended)

Pick your platform from the [Releases page](https://github.com/quyennguyenvu/nova/releases):

```bash
# macOS (Apple Silicon)
curl -Lo nova.tar.gz https://github.com/quyennguyenvu/nova/releases/latest/download/nova_darwin_arm64.tar.gz

# macOS (Intel)
curl -Lo nova.tar.gz https://github.com/quyennguyenvu/nova/releases/latest/download/nova_darwin_amd64.tar.gz

# Linux (x86_64)
curl -Lo nova.tar.gz https://github.com/quyennguyenvu/nova/releases/latest/download/nova_linux_amd64.tar.gz

tar xzf nova.tar.gz
sudo mv nova /usr/local/bin/
```

### From source

```bash
go install github.com/quyennguyenvu/nova@latest
```

## What gets generated

A full project that follows Clean Architecture's dependency rule (`domain → usecase → adapter → infrastructure`), including a working **User CRUD** wired across all layers as a reference.

```bash
myproject/
├── main.go
├── go.mod
├── .env.example
├── cmd/                           # Cobra subcommands (api, grpc, cron…)
├── internal/
│   ├── domain/                    # Layer 1 — entities, repository interfaces, value objects, events
│   ├── usecase/                   # Layer 2 — business logic, DTOs, errors
│   ├── adapter/                   # Layer 3 — repository impls, presenters, assemblers
│   ├── transport/                 #            HTTP/gRPC/cron handlers + middleware
│   └── infrastructure/            # Layer 4 — config, DB, cache, server, DI wiring
├── pkg/                           # Cross-cutting utilities (errors, locale, validator…)
├── migrations/
├── api/openapi/openapi.yaml
├── Dockerfile, docker-compose.yaml
├── Makefile
└── .github/workflows/ci.yaml
```

The architectural rationale (why files live where they do, adapter vs infrastructure, interface ownership) is documented in [instruction.md](instruction.md).

## Commands

### `nova new`

Generates a complete project. With no flags it runs interactively; pass any flag and it skips prompts entirely.

| Flag               | Values                                | Default                   |
| ------------------ | ------------------------------------- | ------------------------- |
| `--module`         | Go module path                        | `github.com/myorg/<name>` |
| `--transport`      | `http`, `grpc`, `cron`, `cli`         | _(prompted)_              |
| `--http-framework` | `fiber`, `gin`, `chi`, `echo`         | _(prompted)_              |
| `--database`       | `postgres`, `mysql`, `none`           | `postgres`                |
| `--db-driver`      | `pgx`, `sqlx`, `gorm`, `database/sql` | `pgx`                     |
| `--query`          | `sqlc`, `raw`, `gorm`                 | `sqlc`                    |
| `--cache`          | `redis`, `none`                       | `redis`                   |
| `--queue`          | `kafka`, `none`                       | `none`                    |
| `--config`         | `yaml`, `toml`, `env`                 | `yaml`                    |
| `--di`             | `wire`                                | `wire`                    |
| `--docker`         | _(bool)_                              | `true`                    |
| `--makefile`       | _(bool)_                              | `true`                    |
| `--ci`             | `github`, `none`                      | `github`                  |

Example — full non-interactive run:

```bash
nova new myproject \
  --module=github.com/myorg/myproject \
  --transport=http --http-framework=fiber \
  --database=postgres --db-driver=pgx --query=sqlc \
  --cache=redis --di=wire \
  --docker --makefile --ci=github
```

### `nova generate`

Scaffold an individual component into an existing project. Stubs are intentionally minimal — fill in the logic yourself.

```bash
nova generate entity Order                     # domain entity + repository interface
nova generate usecase order                    # service, DTOs, errors
nova generate handler order                    # HTTP handler
nova generate repository order --type=postgres # repository implementation
```

## Architecture in one diagram

```text
Domain  →  Use Case  →  Adapter / Transport  →  Infrastructure
(pure Go,   (business     (HTTP & gRPC handlers,   (config, DB,
 zero deps)  logic)        repository impls)        server, DI)
```

Inner layers never import outer layers. Interfaces live where the **consumer** is — repository interfaces in `domain/` (used by use cases), framework-specific helpers under their transport.

For the full rationale see [instruction.md](instruction.md).

## Development

```bash
make build            # build the nova binary to bin/nova
make rebuild          # clean + build
make test             # go test -v ./...
make lint             # golangci-lint run
make fmt              # golangci-lint fmt
make vet              # go vet ./...

make generate         # build + run `nova new` interactively
make generate-all     # generate a full project (Fiber/Postgres/pgx/sqlc/Redis/Wire)
make generate-minimal # generate a minimal project (no DB, no cache)
make verify-gen       # generate + list every output file
make diff-gen         # generate + print key generated files for review
```

Output for `generate-*` targets lands in `/tmp/nova-test-output`.

## Releasing

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions runs GoReleaser, cross-compiles for Linux/macOS/Windows × amd64/arm64, and uploads `.tar.gz`/`.zip` archives + checksums to GitHub Releases.

## License

MIT
