# 12. Build and deploy

Makefile targets, the git hook, Docker image, local compose stack (with benchmark limits), and the CI pipeline. ([index](README.md))

## Makefile

Every target is discoverable — `make help` greps the `##` comments:

```bash
make help            # list documented targets
make setup           # git config core.hooksPath .githooks

make test            # go test -v -race -cover ./...
make test-coverage   # coverage.out + coverage.html
make lint            # golangci-lint run [&& sqlfluff lint] [&& buf lint]
make fmt             # golangci-lint fmt  [&& sqlfluff fix] [&& buf format -w]
make vet             # go vet ./...

make migrate-create name=add_orders    # [--database]
make migrate-up                        # $MIGRATE_DATABASE_URL, else the value in .env
make migrate-down

make gen             # sqlc-gen → proto-gen → tidy → wire-gen (whichever apply)
make wire-gen        # [--di=wire]
make sqlc-gen        # [--query=sqlc]
make proto-gen       # [--transport=grpc] buf generate → api/gen/

make docker-build    # docker build -t {project}:local .
make docker-up       # docker compose up -d --build
make docker-down     # docker compose down
```

Notes on the ones with a decision behind them:

- **`test` runs `-race`.** Slower, but the alternative is discovering a data race in production. Nova's own suite uses the same flag.
- **`lint` chains sqlfluff** for sqlc projects (`--dialect postgres|mysql`), so hand-written SQL is held to the same bar as Go, and `buf lint` (the `STANDARD` rule set) for gRPC projects. `fmt` chains `sqlfluff fix` and `buf format -w`.
- **`proto-gen` runs `buf generate`.** The Go plugins are version-pinned `go run` invocations in `buf.gen.yaml` (`protoc-gen-go@v1.36.12`, `protoc-gen-go-grpc@v1.6.2`), so buf v1.70.0 is the only install; `go tool` cannot drive them because `go mod tidy` has to run after the stubs exist, and `go tool` needs a tidied module. Stale `api/gen` is removed first, like `wire-gen`.
- **`gen` is composed**, not a script: it expands to only the generators the project actually has, so a `--di=fx --query=none` project's `make gen` is a no-op rather than an error.
- **`wire-gen` deletes `wire_gen.go` first.** A stale generated file that still compiles is worse than a missing one — wire would leave it in place on a failed run. It runs `go tool wire`: the `tool` directive in `go.mod` pins wire's version and `go` builds it from the module cache, so there is nothing to install.
- **`migrate-down` rolls back `n` migrations, default 1** (`make migrate-down n=3`); a bare `migrate down` would unwind the whole schema.
- **`migrate-*` read `$MIGRATE_DATABASE_URL`**, not the app config. Migration tooling runs outside the app (CI, a job, your shell), so it takes a URL from the environment, falling back to the `MIGRATE_DATABASE_URL` line in `.env` so the quick start (`cp .env.example .env`, then `make migrate-up`) needs no export. The Go config ignores it.
- **Recipes resolve paths with `$(CURDIR)`**, the directory make runs in. `$(PWD)` would be the caller's directory under `make -C svc gen`, and the `rm` lines would delete there.

There is deliberately **no `make build` or `make run`** target: the binary is built by `go build`/`docker build`, and running it means choosing a subcommand (`./{project} api`) — a `make run` would have to guess.

## .githooks/pre-commit

Activated by `make setup` (`git config core.hooksPath .githooks`) — nothing runs until you opt in, because a hook that installs itself is hostile.

Three gates, in order:

```bash
>>> [1/3] make fmt     # then fails if it produced changes
>>> [2/3] make lint
>>> [3/3] go build
```

The interesting one is the first:

```sh
make fmt
…
if [ -n "$(git diff)" ]; then
  echo "FAILED: make fmt produced changes. Please review, stage them, and commit again."
  echo "Run 'git diff' to see what changed."
  exit 1
fi
```

It formats, then **fails if formatting changed anything** rather than silently amending your staged content. You review and stage the reformatting yourself — a hook that mutates the commit you're making is how surprising diffs land in a PR.

> The generator renders this file with the **executable bit** (`0o700`, via `outFileMode` in [generator.go](../internal/generator/generator.go)). Git silently skips a non-executable hook, so a hook without `+x` looks installed and does nothing.

## Dockerfile

Two stages, distroless-ish runtime:

```dockerfile
# ── Build stage ──
FROM golang:{goversion}-alpine AS builder
COPY go.mod go.sum ./
RUN go mod download          # cached layer — deps change less often than code
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-w -s" -o /out/api .

# ── Runtime stage ──
FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S app -G app
COPY --from=builder /out/api /app/api
COPY --from=builder /src/sqlc/migrations /app/migrations              # [--query=sqlc]
USER app
```

Six choices worth keeping:

- **`go.mod`/`go.sum` copied before the source** so `go mod download` is a cached layer, and a `.dockerignore` keeps `.git`, `.env*`, `bin/` and local state out of the context — the image can never contain a secret that was lying in the working tree.
- **`CGO_ENABLED=0`** → a static binary that runs on `alpine` (or `scratch`) without libc.
- **`-trimpath -ldflags="-w -s"`** drops the build host's paths and strips DWARF and the symbol table: reproducible, and no leaked directories in a panic.
- **Nothing but the binary and the migrations is copied.** The YAML config is embedded in the binary, so there is no config directory to ship.
- **`USER app`** — a non-root, no-login user. Running as root in a container is the default and the wrong default.
- **`ca-certificates` + `tzdata`** installed explicitly: a static Go binary has neither, and their absence shows up as TLS failures and UTC-only timestamps.

The header carries the one prerequisite:

```dockerfile
# This image assumes the repository contains the committed code generated by
# `make gen` (wire_gen.go, internal/adapter/repository/postgres/dbgen/).
# Run `make gen` locally after `nova new` and commit before building images.
```

Generated code (`wire_gen.go`, `dbgen/`, `api/gen/`) is **committed**, not generated in the image. That keeps the build hermetic (no `wire`/`sqlc`/`buf` in the builder stage, no network beyond module download) and makes the generated graph reviewable in a PR.

### Entrypoint, port, healthcheck

```dockerfile
ARG SERVER_PORT=8080                               # [HTTP] one knob: env var, EXPOSE, healthcheck
ENV SERVER_PORT=${SERVER_PORT}
EXPOSE ${SERVER_PORT}

# Probes the readiness endpoint, not the liveness one, so the container is
# only "healthy" once DB/cache are reachable. start-period=10s gives the app
# time to dial dependencies on cold boot.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:${SERVER_PORT}/readyz >/dev/null 2>&1 || exit 1

ARG GRPC_PORT=9090                                 # [gRPC]
ENV GRPC_PORT=${GRPC_PORT}
EXPOSE ${GRPC_PORT}

ENTRYPOINT ["/app/api"]
CMD ["api"]     # or "grpc" / "worker", per --transport
```

Splitting `ENTRYPOINT` from `CMD` means `docker run img worker` overrides the subcommand without re-specifying the binary. `EXPOSE` and the healthcheck are transport-gated, so a worker image declares neither — there's no port to probe. The port is a build arg that becomes the `SERVER_PORT` env var the app reads, so `docker build --build-arg SERVER_PORT=9000` moves the listener, the metadata and the probe together; compose maps `${SERVER_PORT:-8080}` for the same reason.

`/readyz` rather than `/healthz` in the healthcheck is the deliberate half: a container that is up but can't reach its database should not be routed traffic ([07](07-transport-layer.md#internaltransporthttphealthcheckergo)).

## docker-compose.yaml — the local stack, with limits

```bash
cp .env.example .env          # secrets + credentials, read via env_file
docker compose up -d --build  # whole stack
docker compose up -d postgres redis kafka   # dependencies only
```

**Requires Compose V2** (`docker compose`, not the legacy `docker-compose` binary): the resource limits are read from `deploy.resources.limits`, which V2 applies to a plain `up` — V1 ignores them outside swarm. `make docker-up` carries the same note.

### Every limit is an env var with a default

That is what makes this usable for benchmarking without editing the file:

```bash
LIMIT_APP_CPUS=1 LIMIT_APP_MEMORY=256M docker compose up -d --force-recreate app
```

```yaml
deploy:
  resources:
    limits:
      cpus: "${LIMIT_APP_CPUS:-2}"
      memory: ${LIMIT_APP_MEMORY:-512M}
```

| Variable                | Default | Applies to                           |
| ----------------------- | ------- | ------------------------------------ |
| `LIMIT_APP_CPUS`        | `2`     | app                                  |
| `LIMIT_APP_MEMORY`      | `512M`  | app                                  |
| `LIMIT_DB_CPUS`         | `2`     | postgres / mysql                     |
| `LIMIT_DB_MEMORY`       | `1G`    | postgres / mysql                     |
| `LIMIT_REDIS_CPUS`      | `1`     | redis                                |
| `LIMIT_REDIS_MEMORY`    | `256M`  | redis                                |
| `LIMIT_REDIS_MAXMEMORY` | `192mb` | redis (`--maxmemory`, `allkeys-lru`) |
| `LIMIT_KAFKA_CPUS`      | `2`     | kafka                                |
| `LIMIT_KAFKA_MEMORY`    | `1G`    | kafka                                |
| `LIMIT_RABBITMQ_CPUS`   | `1`     | rabbitmq                             |
| `LIMIT_RABBITMQ_MEMORY` | `512M`  | rabbitmq                             |
| `LIMIT_ES_CPUS`         | `2`     | elasticsearch                        |
| `LIMIT_ES_MEMORY`       | `1536M` | elasticsearch                        |
| `LIMIT_ES_HEAP`         | `512m`  | elasticsearch (`ES_JAVA_OPTS`)       |

A sweep is a shell loop over one variable, and every run is reproducible because the ceiling is explicit rather than "whatever the host had free".

### Config precedence inside the network

```yaml
env_file:
  - .env # secrets and per-deploy values
environment: # wins over env_file
  DB_HOST: postgres
  DB_PORT: "5432"
  REDIS_ADDRS: redis:6379
  ELASTICSEARCH_ADDRESSES: http://elasticsearch:9200
  KAFKA_BROKERS: kafka:29092
```

`.env` points at `localhost` so host-side runs work; inside the network each dependency answers on its **service name**. `environment` overriding `env_file` is what lets one `.env` serve both.

### Dependencies wait for health, not for start

```yaml
depends_on:
  postgres:
    condition: service_healthy
```

Every dependency declares a healthcheck (`pg_isready`, `mysqladmin ping`, `redis-cli ping`, an ES cluster-health probe), and the app waits on `service_healthy`. Without this the app dials a still-booting database, fails, and exits — the single most common "compose doesn't work" report.

Host ports are published for each dependency so host tooling (`make migrate-up`, k6, the kafka CLI) works against the same stack. Kafka runs single-node KRaft with dual listeners (`kafka:29092` in-network, `localhost:9092` from the host) for exactly that reason: the host listener is the address `.env.example` and `base.yaml` already use, so `go run main.go api` reaches the broker without edits. The listeners bind with empty hosts (`PLAINTEXT://:29092`), because Kafka 3.9 refuses to start when a `0.0.0.0` listener would be advertised, and the healthcheck probes the in-network listener so a remapped host port (`19092:9092`) keeps the container healthy.

Dead letters need a home the broker knows about. Compose enables Kafka topic auto-creation, so the worker's `<topic>.dlq` topics appear on first use; in production create them alongside the topics themselves. RabbitMQ declares its `<queue>.dlx` exchange and `<queue>.dlq` queue at worker start. Queue arguments are fixed at creation, which is why the dead-letter exchange is named after the queue rather than the work exchange: pointing a worker at another exchange never changes them. A work queue declared by an older build with different arguments must be deleted once before the new worker can declare it.

Data lives in named volumes, so `docker compose down` keeps it and `down -v` discards it.

## .github/workflows/ci.yaml — four jobs

```bash
lint ─┐
tidy ─┼──▶ build
test ─┘
```

| Job     | What it does                                                             |
| ------- | ------------------------------------------------------------------------ |
| `lint`  | `golangci-lint-action@v8` pinned to golangci-lint `v2.11.4`              |
| `tidy`  | `go mod tidy` then `git diff --exit-code go.mod go.sum`                  |
| `test`  | `go test -v -race -coverprofile=coverage.out ./...`, hermetic            |
| `build` | `go build -trimpath -ldflags="-w -s"` — `needs: [lint, tidy, test]`      |

```yaml
# Catches forgotten dep updates: re-running tidy should be a no-op on
# a clean PR; any drift fails the job.
```

The `tidy` job is the one people leave out and then regret: an untidy `go.mod` breaks other people's builds in ways that look unrelated to the PR that caused it.

`test` runs with no service containers and no environment: the rendered tests are hermetic (fakes for every port, driver errors built by hand, the worker's fake consumer). Add a service and its env vars only when an integration test needs them. The lint action's major (`v8`) is the one that drives golangci-lint v2 — the earlier `v6` only ran v1 against a v2 config — and the version is pinned so a lint release cannot change a PR's verdict.

The external tools are pinned in the README's prerequisites (`golangci-lint v2.11.4`, `sqlc v1.30.0`, `migrate v4.19.0`, `sqlfluff 4.2.2`); wire needs no install because `go tool` builds it at the version in `go.mod`.

`permissions: contents: read` scopes the token to the minimum — the default is broader than any of these jobs needs.

## .env.example — secrets only

```bash
# This file holds ONLY secrets and per-deployment endpoints — values that are
# unsafe to commit or that differ on every machine/deploy. Everything else
# (timeouts, pool sizes, log level, issuer/audience, topic & queue names, …)
# has a default in base.yaml with per-env overrides in <APP_ENV>.yaml.
```

The split is the same one described in [09](09-infrastructure-layer.md#config--one-struct-three-layers-of-value): tuning is reviewed in code, secrets are injected. Notable entries:

- **JWT keys**, with the `openssl` commands to generate a pair inline, and a note that only the issuing service sets `JWT_PRIVATE_KEY`.
- **`CORS_ALLOW_ORIGINS`**, empty, with "production MUST set this. Never `*`".
- **`SERVER_TRUSTED_PROXIES`**, empty: the load balancers whose `X-Forwarded-For` is believed; empty means forwarded headers are ignored and the TCP peer is the client.
- **Discrete DB fields** (`DB_HOST`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`) rather than a `DATABASE_URL` — the DSN is assembled escape-safely in code ([09](09-infrastructure-layer.md#database--connection-factory)).
- **`MIGRATE_DATABASE_URL`** separately, flagged as read by `make migrate-up`/`migrate-down` and ignored by the Go config (quoted for MySQL, whose `tcp(...)` is shell syntax).

Every value is sourceable as written: the database credentials default to the same values compose uses, the JWT keys are empty and marked required (they fail at startup until generated), and nothing is wrapped in `<angle brackets>`. The binary loads `./.env` itself on host runs ([09](09-infrastructure-layer.md#config--one-struct-three-layers-of-value)), so `cp .env.example .env`, generate the keys, and `go run main.go api` reaches "starting HTTP server" with only the compose dependencies up. nova's bootstrap proves that path on the no-database projects: it builds the binary, writes a `.env` with a generated keypair and a free port, waits for the port, and sends SIGTERM; `TestDeployContract` sources `.env.example` under `sh`. The image build, the compose healthcheck and the README worker demo were run by hand on 2026-10-14; they stay manual steps.

`APP_ENV=local` by default, which loads `base.yaml` only; any value other than `local`, `development` or `production` is rejected at startup.

## Lint configuration

`.golangci.yaml` mirrors nova's own (maratori's golden config): strict, with `goimports` + `golines` at 120 columns for `fmt`. `.sqlfluff` is always emitted; `make lint` only invokes it for sqlc projects.

The generated `CLAUDE.md` documents these commands for Claude Code inside the new project, so the conventions survive contact with an agent.
