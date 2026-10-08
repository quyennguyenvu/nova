# 1. CLI options

What `nova new` asks, which flag maps to each answer, and how invalid or unimplemented choices are rejected. ([index](README.md))

## Interactive prompts

`nova new` with no flags asks the questions below, in this order — see [internal/prompt/prompt.go](../internal/prompt/prompt.go) (`RunInteractive`). Each has a matching `nova new` flag; passing **any** flag skips every prompt and fills the rest from `config.DefaultConfig()`. The choices offered are exactly the option sets in [internal/config/options.go](../internal/config/options.go).

| Prompt                | Flag               | Values (default first)                               |
| --------------------- | ------------------ | ---------------------------------------------------- |
| Project name          | positional arg     | letters, digits, `.`, `_`, `-` (default `myproject`) |
| Go module name        | `--module`         | a Go module path (default `github.com/myorg/<name>`) |
| Transport layer       | `--transport`      | `http`, `grpc`, `worker`                             |
| HTTP framework        | `--http-framework` | `fiber`, `gin`, `chi`, `echo`                        |
| Database              | `--database`       | `postgres`, `mysql`, `none`                          |
| Cache                 | `--cache`          | `redis`, `none`                                      |
| Search engine         | `--search`         | `none`, `elasticsearch`                              |
| Message queue         | `--queue`          | `none` (`kafka` for workers), `kafka`, `rabbitmq`    |
| Dependency injection  | `--di`             | `wire`, `fx`                                         |
| Include Docker setup? | `--docker`         | `true`; pass `--docker=false` to skip                |
| Include CI/CD?        | `--ci`             | `github`, `none`                                     |

Three flags have no prompt because only one value is implemented per engine: `--db-driver` (`pgx` for postgres, `database/sql` for mysql), `--query` (`sqlc`) and `--config` (`yaml`). Leave them unset; `Validate` fills them in. `--force` lets `nova new` render into a directory that already has files.

The Makefile, the lint config and the git pre-commit hook are always emitted — there is no prompt for them.

## Validation

Every value goes through `ProjectConfig.Validate()` ([internal/config/options.go](../internal/config/options.go)) before a single file is written, and `generator.New()` runs the same check so a config built in code cannot bypass it. The error lists every problem at once, for example `transport "cron" is not supported (valid: http, grpc, worker)`.

- Rejected as not implemented: the `cron` and `cli` transports, `nethttp`, `sqlite`, `mongodb`, `sqlx`, `gorm`, `raw`, `bigcache`, `nats`, `toml` and `env`. The former `--type` and `--grpc-gateway` flags were removed.
- `--transport` is required when any flag is given; a `worker` also needs `--queue kafka` or `rabbitmq`.
- Project names must match `[A-Za-z0-9][A-Za-z0-9._-]*` (no spaces, quotes or path separators) and `--module` must look like a Go module path, so the rendered Go, YAML and Makefile always parse.
- `--database=none` blanks `--db-driver`/`--query` and renders an in-memory `UserRepository` (`internal/adapter/repository/memory/`) so the User CRUD, handlers and DI graph still build and run; data lives only for the lifetime of the process. The health probe has no dependency to check in that configuration and mirrors liveness.
- A target directory that already contains files is refused unless `--force` is given.
