# 1. CLI options

What `nova new` asks, which flag maps to each answer, and what happens when you pick an option that isn't implemented yet. ([index](README.md))

## Interactive prompts

`nova new` with no flags asks the questions below, in this order — see [internal/prompt/prompt.go](../internal/prompt/prompt.go) (`RunInteractive`). Each has a matching `nova new` flag; passing **any** flag skips every prompt and fills the rest from `config.DefaultConfig()`.

| Prompt                | Flag               | Options (default first)                        | Implemented today                 |
| --------------------- | ------------------ | ---------------------------------------------- | --------------------------------- |
| Project name          | positional arg     | —                                              | yes                               |
| Go module name        | `--module`         | —                                              | yes                               |
| Transport layer       | `--transport`      | `http`, `grpc`, `worker`, `cron`, `cli`        | `http`, `grpc`, `worker`          |
| HTTP framework        | `--http-framework` | `fiber`, `gin`, `chi`, `echo`, `nethttp`       | all but `nethttp`                 |
| Include gRPC-Gateway? | `--grpc-gateway`   | yes/no (asked only for `grpc`)                 | flag is stored, emits nothing     |
| Database              | `--database`       | `postgres`, `mysql`, `sqlite`, `mongodb`, none | `postgres`, `mysql`, `none`       |
| Database driver       | `--db-driver`      | `pgx`, `sqlx`, `gorm`, `database/sql`          | `pgx` (mysql uses `database/sql`) |
| Query generation      | `--query`          | `sqlc`, `raw`, `gorm`                          | `sqlc`                            |
| Cache                 | `--cache`          | `redis`, `bigcache`, none                      | `redis`, `none`                   |
| Search engine         | `--search`         | none, `elasticsearch`                          | both                              |
| Message queue         | `--queue`          | `kafka`, `rabbitmq`, `nats`, none              | `kafka`, `rabbitmq`, `none`       |
| Configuration format  | `--config`         | `yaml`, `toml`                                 | `yaml`                            |
| Dependency injection  | `--di`             | `wire`, `fx`                                   | both                              |
| Include Docker setup? | `--docker`         | yes/no                                         | yes                               |
| Include CI/CD?        | `--ci=github`      | yes/no                                         | yes                               |

The Makefile, the lint config and the git pre-commit hook are always emitted — there is no prompt for them.

## Unimplemented options fail in three different ways

Check this before filing a bug:

- `nethttp`, `sqlite`, `mongodb` are rejected up front — `generator.New()` whitelists frameworks (`fiber`/`gin`/`chi`/`echo`), databases (`postgres`/`mysql`/`none`) and DI (`wire`/`fx`), and returns an error for anything else.
- `cron` and `cli` generate a project **that does not compile**: no `cmd/`, no `internal/app/`, no transport package, and (with `--di=wire`) a `wire.go` referencing an `App` graph that was never emitted. Same for a flag-driven run that never sets `--transport`, since `DefaultConfig()` leaves it empty.
- `sqlx`/`gorm`/`database/sql`, `raw`/`gorm` queries, `bigcache` and `toml` are accepted and then **silently ignored** — the templates only branch on `pgx`+`sqlc`, `redis` and `yaml`. `nats` is the one that fails latest: the publisher compiles and returns `locale.Unimplemented` at runtime.
