# Nova CLI — Walkthrough

## What Was Built

**Nova** is a Go CLI that generates production-ready Clean Architecture projects. It supports interactive prompts, CLI flags, and scaffolding individual components.

## Commands

| Command | Description |
|---------|-------------|
| `nova new [name]` | Generate a new project (interactive or flags) |
| `nova generate entity <Name>` | Scaffold a domain entity |
| `nova generate usecase <name>` | Scaffold a use case |
| `nova generate handler <name>` | Scaffold an HTTP handler |
| `nova generate repository <name>` | Scaffold a repository |

## Project Structure

```
nova/
├── main.go                           # CLI entry point
├── cmd/                              # Cobra commands (root, new, generate)
├── internal/
│   ├── config/config.go              # ProjectConfig struct
│   ├── prompt/prompt.go              # Interactive survey prompts
│   └── generator/
│       ├── generator.go              # Template rendering engine
│       ├── component.go              # Individual component scaffolding
│       └── templates/                # 40+ embedded Go templates
├── .goreleaser.yaml                  # Cross-platform binary builds
├── .github/workflows/
│   └── release.yaml                  # Tag-triggered release pipeline
├── makefile                          # Dev workflow commands
└── README.md                         # User-facing documentation
```

## Template Coverage

- **4 HTTP frameworks**: Fiber, Gin, Chi, Echo
- **2 SQL databases**: PostgreSQL (pgx), MySQL
- **Cache**: Redis with read-through decorator
- **DI**: Manual container or Google Wire
- **DevOps**: Dockerfile, docker-compose, Makefile, GitHub Actions CI, PR template, OpenAPI spec

## Release Pipeline

Configured via [.goreleaser.yaml](file:///Users/lap02445/workspace/gh_leo/nova/.goreleaser.yaml) + [.github/workflows/release.yaml](file:///Users/lap02445/workspace/gh_leo/nova/.github/workflows/release.yaml):

1. Tag a version: `git tag v0.1.0 && git push origin v0.1.0`
2. GitHub Actions triggers GoReleaser
3. Cross-compiles for **6 targets**: Linux/macOS/Windows × amd64/arm64
4. Uploads `.tar.gz` / `.zip` archives + checksums to GitHub Releases
5. Users download a single binary — no Go toolchain needed

## Verification

- ✅ `go build` compiles successfully
- ✅ `nova --help` shows both subcommands
- ✅ `nova new testproject` generates 41 files with correct Clean Architecture structure
- ✅ Template conditionals work (framework/database/cache selection)
- ✅ GoReleaser config and release workflow added
- ✅ README.md created with install/usage/options documentation
