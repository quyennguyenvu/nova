# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Working principles

Behavioral guidelines (adapted from [andrej-karpathy-skills](https://github.com/forrestchang/andrej-karpathy-skills)) to reduce common LLM mistakes. They bias toward caution over speed; for trivial tasks, use judgment.

1. **Think before coding.** Don't assume, don't hide confusion, surface tradeoffs. State assumptions explicitly; if multiple interpretations exist, present them instead of picking silently; if a simpler approach exists, say so.
2. **Simplicity first.** Minimum code that solves the problem, nothing speculative — no unrequested features, no abstractions for single-use code, no error handling for impossible cases. If a senior engineer would call it overcomplicated, rewrite it smaller.
3. **Surgical changes.** Every changed line should trace to the request. Don't refactor or reformat adjacent code; match the surrounding template/style even if you'd do it differently. Remove only the imports/vars/funcs your own change orphaned; flag pre-existing dead code rather than deleting it unasked.
4. **Goal-driven execution.** Turn the task into a verifiable goal and loop until it passes. Here that means `make lint` and `make test` stay green, and generator changes are proven by the render-then-`go vet` matrix in [internal/generator/generator_test.go](internal/generator/generator_test.go) — add the matrix case in the same change, not after. "Fix the bug" → write a failing test first, then make it pass.

## Project

Nova is a Go CLI that scaffolds production-ready Go services following Clean Architecture. Two commands:

- `nova new [name]` — generates a full project (interactive, or non-interactive when any flag is passed)
- `nova generate <type> <name>` — scaffolds a single component (`entity`, `usecase`, `handler`, `repository`, or `all`) into an existing project

Module is `nova` (local name), binary is `bin/nova`. Requires Go 1.25.1+.

## Commands

```bash
make build            # go build -o bin/nova .
make rebuild          # clean + build
make test             # go test -v ./...
make lint             # golangci-lint run
make fmt              # golangci-lint fmt
make vet              # go vet ./...

make gen              # build + run `nova new` interactively
make gen-api          # non-interactive: Fiber/Postgres/pgx/sqlc/Redis/Kafka/Wire → /tmp/nova-test-output
make gen-worker       # non-interactive: worker (Kafka/Postgres/pgx/sqlc/Redis/Wire) → /tmp/nova-test-output
make verify-gen       # gen-api + `find` all output files
make diff-gen         # gen-api + cat key outputs (go.mod, cmd/api.go, di/wire.go)
```

Run a single test: `go test -v -run TestName ./path/to/pkg`.

## Architecture

### Control flow

`main.go → cmd.Execute() → cobra root → newCommand`

- [cmd/root.go](cmd/root.go) — wires Cobra root and subcommands; calls `locale.NewMapping()` once at startup
- [cmd/new.go](cmd/new.go) — `new`: builds `config.ProjectConfig`, either from flags (`applyFlags`) or via `prompt.RunInteractive`, then calls `generator.New(cfg).Generate(outputDir)`
- [cmd/generate.go](cmd/generate.go) — `generate <type> <name>`: scaffolds one component via `generator.NewComponentGenerator(".")` into the current project

### Rendering strategy

[internal/generator/generator.go](internal/generator/generator.go) renders `text/template` files from an `embed.FS` (`//go:embed all:templates`). The template tree lives at [internal/generator/templates/](internal/generator/templates/). Files are selected by `cond` booleans in `buildFileList()` (which composes `entryPointFiles`, `rootFiles`, `domainFiles`, `usecaseFiles`, `adapterFiles`, `transportFiles`, `workerFiles`, `infrastructureFiles`, `pkgFiles`, `migrationFiles`, `sqlcFiles`, `toolingFiles`).

### Template naming convention

Variant-prefixed source → clean output name:

- `templates/transport/http/v1/user/{fiber,gin,chi,echo}_handler.go.tmpl` → `internal/transport/http/v1/user/handler.go`
- `templates/transport/http/middleware/{framework}_{auth,cors,locale,logging,loginlimit,recovery,requestid}.go.tmpl` → `internal/transport/http/middleware/{name}.go`
- `templates/sqlc/{pg,mysql}_sqlc.yaml.tmpl` → `sqlc/sqlc.yaml` (engine alias from `sqlcEngineAlias()`)
- `templates/infrastructure/server/{fiber,gin,chi,echo}_http.go.tmpl` → `internal/infrastructure/server/http.go`

All framework variants coexist in the template tree; the generator picks one at render time using `cfg.HTTPFramework` / `cfg.Database` / `cfg.QueryGen`. **To add a new HTTP framework or DB engine, add all the prefixed template files AND extend the whitelist in `generator.New()` — `generator.go` validates the choice at construction time before resolving template paths.**

### Config is the single source of truth

[internal/config/config.go](internal/config/config.go) defines `ProjectConfig` plus boolean helpers (`HasHTTP`, `HasGRPC`, `HasWorker`, `HasSQL`, `HasCache`, `HasMessageQueue`, `HasRedis`, `UseWire`, `UseFx`). Templates and `buildFileList()` condition everything on these methods — add a new helper here before using it in either place. `DefaultConfig()` drives Quick Start (`nova new` with no flags, no prompts — though note `cmd/new.go` only skips prompts when at least one flag is set).

### Generated-project architecture (what templates produce)

The scaffolded projects follow Clean Architecture's dependency rule: `domain → usecase → adapter → infrastructure`. Domain has zero deps; adapters implement domain interfaces. Generated layout is documented in [README.md](README.md). Includes a working User CRUD wired across all layers as a reference.

## Conventions

- Template files use `.tmpl` suffix and receive `*config.ProjectConfig` as data. Available funcs: `lower`, `upper`, `title`, `contains`, `replace` (see `generator.New`).
- Generator writes files 0o600, dirs 0o750.
- Lint config is strict ([.golangci.yaml](.golangci.yaml), based on maratori's golden config, v2.4.0). Run `make lint` before committing; `make fmt` uses golangci-lint's formatter (goimports + golines, max-len 120).
- [instruction.md](instruction.md) is a long-form design note — consult it when a template's intent is unclear.
