# Nova

A Go CLI that generates production-ready Go projects following **Clean Architecture**.

```bash
nova new                          # interactive — walks you through every option
nova new myapp --transport=http   # non-interactive — any flag skips prompts
nova add                          # interactive — pick a component to scaffold
nova add entity Order             # non-interactive — scaffold a single component
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
├── nova.yaml                      # Layout manifest — `nova add` reads this
├── CLAUDE.md
├── cmd/                           # Cobra subcommand for the chosen transport
├── internal/
│   ├── domain/                    # Layer 1 — entities, ports (repository, publisher, hasher)
│   ├── usecase/                   # Layer 2 — business logic + DTOs
│   ├── adapter/                   # Layer 3 — repository impls, cache, search, publishers
│   ├── transport/                 #            HTTP / gRPC / worker handlers + middleware
│   └── infrastructure/            # Layer 4 — config, DB, cache, jwt, server, DI wiring
├── pkg/                           # Cross-cutting (errors, httputil, locale, logctx, observability)
├── sqlc/                          # sqlc.yaml + query/ + migrations/
├── api/openapi/openapi.yaml       # HTTP only
├── Dockerfile
├── Makefile
└── .github/workflows/ci.yaml
```

The full per-file tree, with the flag that gates each entry, is [docs/02 — Project layout](docs/02-project-layout.md).

The architectural rationale (why files live where they do, adapter vs infrastructure, interface ownership) is documented in [docs/](docs/README.md) — start with [03 — Architecture rules](docs/03-architecture-rules.md) and [04 — Placement rationale](docs/04-placement-rationale.md).

## Commands

### `nova new`

Generates a complete project. With no flags it runs interactively; pass any flag and it skips prompts entirely.

| Flag               | Values                            | Default                   |
| ------------------ | --------------------------------- | ------------------------- |
| `--module`         | Go module path                    | `github.com/myorg/<name>` |
| `--transport`      | `http`, `grpc`, `worker`          | _(none — must be set)_    |
| `--http-framework` | `fiber`, `gin`, `chi`, `echo`     | `fiber`                   |
| `--database`       | `postgres`, `mysql`, `none`       | `postgres`                |
| `--db-driver`      | `pgx`                             | `pgx`                     |
| `--query`          | `sqlc`                            | `sqlc`                    |
| `--cache`          | `redis`, `none`                   | `redis`                   |
| `--search`         | `elasticsearch`, `none`           | `none`                    |
| `--queue`          | `kafka`, `rabbitmq`, `none`       | `none`                    |
| `--config`         | `yaml`                            | `yaml`                    |
| `--di`             | `wire`, `fx`                      | `wire`                    |
| `--docker`         | _(bool)_                          | `true`                    |
| `--ci`             | `github`, `none`                  | `github`                  |

Because any flag skips the prompts, a flag-driven run **must** pass `--transport` — otherwise it
generates a project with no entry point. The prompts and flags also accept values that are not
implemented yet (`cron`/`cli` transports, `nethttp`, `sqlite`/`mongodb`, `sqlx`/`gorm`, `raw`
queries, `bigcache`, `nats`, `toml`); [docs/01 — CLI options](docs/01-cli-options.md) lists exactly
how each one fails.

Example — full non-interactive run:

```bash
nova new myproject \
  --module=github.com/myorg/myproject \
  --transport=http --http-framework=fiber \
  --database=postgres --db-driver=pgx --query=sqlc \
  --cache=redis --di=wire \
  --docker --ci=github
```

### `nova add`

Scaffold an individual component into an existing project. Run it with no args for an interactive menu, or pass the type and name directly. Stubs are intentionally minimal — fill in the logic yourself. (Aliases: `generate`, `gen`.)

```bash
nova add                                  # interactive — choose entity/usecase/repository/handler/worker/all
nova add entity Order                     # domain entity + repository interface
nova add usecase order                    # service + DTOs
nova add handler order                    # HTTP handler
nova add repository order --type=postgres # repository implementation
nova add worker order                     # full worker transport + order feature handler
nova add all order                        # entity + usecase + repository + handler
```

#### worker — a runnable second service

`nova add worker <Name>` scaffolds a **runnable worker service** you can run alongside an existing one (e.g. an HTTP API) from the same repo — `go run main.go worker`. It generates the whole inbound worker subsystem plus its entry points:

- `internal/transport/worker/` — the `Handler` interface, `Worker` orchestrator, and a broker consumer (`kafka`/`rabbitmq`, from `stack.message_queue`)
- `internal/transport/worker/v1/<name>/` — a per-feature handler + message DTO
- `cmd/worker.go` + `internal/app/worker.go` — the `worker` subcommand and its bootstrap
- and it registers `workerCommand()` in `cmd/root.go`

The bootstrap boots through the project's **DI graph** (`di.InitializeWorker`), byte-identical to what `nova new --transport=worker` emits, so it is broker-agnostic. `add` also drops a minimal `WorkerApp` struct + `InitializeWorker` injector into `internal/infrastructure/di` (wire or fx, from `stack.di`) — each symbol only when that package doesn't already declare it, so a worker-enabled project is left untouched and re-runs never duplicate. This assumes a nova-new (or `nova.yaml`) layout; with no `di` package it prints a hint instead. Complete the injector with your provider sets, run `go mod tidy` and (for wire) `wire`, then `go run main.go worker`. Shared files are written only when **absent**, so a second `nova add worker payment` just appends its feature handler.

#### sqlc-backed repositories

When the project's stack uses **sqlc** (the default), `nova add repository <Name>` generates a complete, typed persistence slice driven by the **existing entity** — so the entity must be created first (`nova add entity <Name>`), otherwise it stops with an error. It reads the entity's fields and emits four artifacts, engine-aware (postgres ⇄ mysql):

- a **migration** (`CREATE TABLE`) with each field's column type,
- a **sqlc query** file (`Create`/`GetByID`/`Update`/`Delete`/`List`),
- the **repository impl** (typed against `*entity.<Name>`, satisfies the domain port),
- the entity ⇄ row **mapper**.

Go types are mapped to SQL + the sqlc row type as far as possible — `int64`→`BIGINT`, `string`→`TEXT`/`VARCHAR`, `bool`→`BOOLEAN`, `float64`→`DOUBLE PRECISION`/`DOUBLE`, `time.Time`→`TIMESTAMP`/`DATETIME` (postgres timestamps go through `pgtype`), `json.RawMessage`→`JSONB`/`JSON`. Fields with unsupported types are skipped with a warning so the output always compiles. After generating, run `make gen` (sqlc) and wire the repository into your DI provider. For `raw`/`gorm` stacks a generic hand-fillable stub is emitted instead.

#### `nova.yaml` — targeting non-nova layouts

`add` decides _where_ each component's files go from a `nova.yaml` at the project root. Projects generated by `nova new` follow nova's standard layout and need no file — the built-in defaults apply. To scaffold into a project with a **different** layout, drop a `nova.yaml` describing it:

```yaml
version: 1
module: github.com/myorg/shop
stack:
  http_framework: fiber
  database: postgres
  cache: redis
layout:
  entity:
    dir: internal/domain/entity
    file: "{snake}.go"
  port:
    dir: internal/domain
    file: "{snake}.go"
  usecase:
    dir: internal/usecase/{lower}
  repository:
    dir: internal/adapter/repository/{db}
    file: "{snake}_repository.go"
  handler:
    dir: internal/transport/http/v1/{lower}
```

Path placeholders: `{lower}`, `{snake}`, `{title}` (the component name in each form), `{db}` (the repository engine). Any key you omit falls back to nova's default for that component.

## Architecture in one diagram

```text
Domain  →  Use Case  →  Adapter / Transport  →  Infrastructure
(pure Go,   (business     (HTTP & gRPC handlers,   (config, DB,
 zero deps)  logic)        repository impls)        server, DI)
```

Inner layers never import outer layers. Interfaces live where the **consumer** is — repository interfaces in `domain/` (used by use cases), framework-specific helpers under their transport.

For the full rationale see [docs/04 — Placement rationale](docs/04-placement-rationale.md).

## Development

```bash
make build            # build the nova binary to bin/nova
make rebuild          # clean + build
make test             # go test -v ./...
make lint             # golangci-lint run
make fmt              # golangci-lint fmt
make vet              # go vet ./...

make gen              # build + run `nova new` interactively
make gen-api          # generate a full HTTP project (Fiber/Postgres/pgx/sqlc/Redis/Kafka/Wire)
make gen-worker       # generate a worker project (Kafka/Postgres/pgx/sqlc/Redis/Wire)
make verify-gen       # gen-api + list every output file
make diff-gen         # gen-api + print key generated files for review
```

Output for the `gen-api` / `gen-worker` targets lands in `/tmp/nova-test-output`.

### Knowledge graph

This repo ships a committed [graphify](https://github.com/Graphify-Labs/graphify) knowledge graph in `graphify-out/` — ask your assistant about the codebase and it answers from the graph. See [GRAPHIFY.md](GRAPHIFY.md) for first-time setup.

## Releasing

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions runs GoReleaser, cross-compiles for Linux/macOS/Windows × amd64/arm64, and uploads `.tar.gz`/`.zip` archives + checksums to GitHub Releases.

## License

MIT
