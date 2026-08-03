# Nova design docs

This directory is the **design reference for what `nova new` generates** — not docs for the
CLI itself (see the [root README](../README.md) for that). It captures the intent behind every
layer, file, and placement decision so the templates stay consistent as new framework/DB
variants are added.

Where a doc and a template disagree, **the template wins** — and the doc is the bug. Fix it in
the same change.

## The docs

| Doc | Answers |
| --- | --- |
| [01 — CLI options](01-cli-options.md) | Which prompts/flags exist, what each option means, and how the unimplemented ones fail |
| [02 — Project layout](02-project-layout.md) | The full generated tree, and which flag gates each file |
| [03 — Architecture rules](03-architecture-rules.md) | The dependency rule, and the adapter-vs-infrastructure decision guide |
| [04 — Placement rationale](04-placement-rationale.md) | Why each package lives where it does, and who owns an adapter's interface |
| [05 — Domain layer](05-domain-layer.md) | Entities, value objects, repository ports, domain services, events |
| [06 — Usecase layer](06-usecase-layer.md) | Application services, usecase DTOs, domain-service implementations |
| [07 — Transport layer](07-transport-layer.md) | Per-feature HTTP packages, router/registrars, middleware, the writer, the locale flow |
| [08 — Adapter layer](08-adapter-layer.md) | Transaction manager, tx-aware queries, sqlc config + SQL, repositories, publishers |
| [09 — Infrastructure layer](09-infrastructure-layer.md) | Config loading, server bootstrap, connection factories |
| [10 — Shared packages](10-shared-packages.md) | `pkg/errors`, `pkg/locale`, `pkg/httputil`, `pkg/logctx`, `pkg/observability` |
| [11 — DI and entry points](11-di-and-entrypoints.md) | The Wire/fx graph, app lifecycle, cobra commands, cleanup order |
| [12 — Build and deploy](12-build-and-deploy.md) | Makefile targets, the git hook, Dockerfile, docker-compose (with benchmark limits), CI |
| [13 — Cross-cutting features](13-cross-cutting.md) | The production concerns every generated project must ship |

## Reading the reference code (05 – 12)

Docs 05 – 12 are a **pattern reference**, not an inventory of output — [02](02-project-layout.md)
is the inventory. Several layers are documented for when you grow a project into them but are
**not scaffolded by `nova new` today**:

- `domain/valueobject/`
- `domain/service/`
- `domain/event/`
- `adapter/external/`
- `transport/cronjob/`

Those sections are marked inline. Everything else mirrors a real template under
[internal/generator/templates/](../internal/generator/templates/), with `{module}` standing in
for the project's module path.
