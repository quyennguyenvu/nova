# Graph Report - .  (2026-08-04)

## Corpus Check
- 37 files · ~51,363 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 459 nodes · 910 edges · 27 communities (16 shown, 11 thin omitted)
- Extraction: 87% EXTRACTED · 13% INFERRED · 0% AMBIGUOUS · INFERRED: 117 edges (avg confidence: 0.83)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- Generated Layout and Option Gaps
- Component Scaffolding and Handler Wiring
- Template Rendering and Layer Patterns
- Render Test Matrix and Compose Asserts
- nova new Flags and ProjectConfig
- Component Generator AST Scaffolding
- graphify Knowledge Graph Setup
- sqlc Repository Code-Gen
- add Command Dispatch
- Layout Manifest (nova.yaml)
- SQL Repository and Query Patterns
- Template File Selection
- Worker, Messaging and Middleware
- Lint Configuration
- Release Build Matrix
- Opaque Client Error Mapping
- Domain Service Placement
- Liveness vs Readiness
- Error Sentinel Translation
- Sub-Config Projection
- Explicit Registration Patterns
- ProjectConfig Source of Truth
- Route-Group Auth
- Compose Env Limits
- ComponentGenerator Type
- Test Helper Type T
- Nova Module Path

## God Nodes (most connected - your core abstractions)
1. `ProjectConfig` - 39 edges
2. `New()` - 28 edges
3. `Default()` - 25 edges
4. `newComponentGen()` - 24 edges
5. `Generator` - 19 edges
6. `assertFileContains()` - 17 edges
7. `ComponentGenerator` - 16 edges
8. `httpMatrixConfig()` - 16 edges
9. `RunInteractive()` - 16 edges
10. `templateFile` - 14 edges

## Surprising Connections (you probably didn't know these)
- `overlay()` --semantically_similar_to--> `Config precedence: base.yaml -> <APP_ENV>.yaml -> env vars`  [INFERRED] [semantically similar]
  internal/manifest/manifest.go → docs/09-infrastructure-layer.md
- `sqlc repository generation requires the entity to exist first` --conceptually_related_to--> `dispatchAdd()`  [INFERRED]
  README.md → cmd/add.go
- `Up-front whitelist rejection (nethttp, sqlite, mongodb)` --references--> `New()`  [EXTRACTED]
  docs/01-cli-options.md → internal/generator/generator.go
- `TestGenerateHandlerFrameworks()` --references--> `One DTO, every framework (dual query:/form: tags)`  [INFERRED]
  internal/generator/component_test.go → docs/07-transport-layer.md
- `TestGenerateWorkerScaffoldsWireDI()` --references--> `provideRegistrars / provideWorkerHandlers are the extension points`  [INFERRED]
  internal/generator/component_test.go → docs/11-di-and-entrypoints.md

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **graphify first-time setup flow (install, register, hook, query)** — graphify_2_install_the_package, graphify_3_register_graphify_with_your_assistant, graphify_4_install_the_git_hooks, graphify_using_the_committed_graph [EXTRACTED 1.00]
- **Trade-offs of tracking the graph in git** — graphify_committed_graph_team_map, graphify_graph_json_merge_conflicts, graphify_absolute_path_sidecars, graphify_hooks_are_per_developer [INFERRED 0.85]
- **The four cross-cutting mechanisms** — docs_13_cross_cutting_cross_cutting_features, docs_13_cross_cutting_context_propagation, docs_13_cross_cutting_log_once_at_boundary, docs_13_cross_cutting_classify_at_origin_translate_at_edge, docs_13_cross_cutting_component_owns_shutdown [EXTRACTED 1.00]
- **Adapter-vs-infrastructure-vs-pkg placement decision system** — docs_03_architecture_rules_dependency_rule, docs_03_architecture_rules_adapter_layer, docs_03_architecture_rules_infrastructure_layer, docs_03_architecture_rules_decision_flowchart, docs_03_architecture_rules_ports_outside_adapter, docs_03_architecture_rules_key_insight [EXTRACTED 1.00]
- **JWT interface-ownership decision across docs 02/03/04** — docs_04_placement_rationale_consumer_owns_the_interface, docs_04_placement_rationale_no_domain_interface_for_transport_only, docs_04_placement_rationale_shipped_jwt_variant, docs_02_project_layout_jwt_in_infrastructure, docs_03_architecture_rules_ports_outside_adapter [INFERRED 0.95]
- **Domain event publishing pattern (shipped per-aggregate vs grow-into event package)** — docs_05_domain_layer_domain_events, docs_05_domain_layer_userpublisher, docs_05_domain_layer_eventpublisher, docs_05_domain_layer_typed_publish_methods [EXTRACTED 1.00]

## Communities (27 total, 11 thin omitted)

### Community 0 - "Generated Layout and Option Gaps"
Cohesion: 0.06
Nodes (49): nats publisher compiles then returns locale.Unimplemented at runtime, cron and cli transports generate a non-compiling project, Silently ignored options (sqlx, gorm, database/sql, raw, bigcache, toml), Unimplemented options fail in three different ways, Up-front whitelist rejection (nethttp, sqlite, mongodb), di/provider.go and di/app.go emitted for any transport; only graph file is DI-specific, Generated project tree (union of every transport), jwt lives in infrastructure, not adapter (+41 more)

### Community 1 - "Component Scaffolding and Handler Wiring"
Cohesion: 0.14
Nodes (42): GenerateWorker scaffolds a runnable worker service, skel/ tree — one directory per `add` command, ComponentGenerator, Filter passthrough rule (do you need a usecase DTO?), List — pagination policy clamped in the usecase, One DTO, every framework (dual query:/form: tags), v1.Prefixed — the shared /api/v1 prefix by embedding, Registrar interface + deterministic slice mount order (+34 more)

### Community 2 - "Template Rendering and Layer Patterns"
Cohesion: 0.05
Nodes (42): embed.FS + text/template rendering strategy, Variant-prefixed template naming convention, Nova working principles (think first, simplicity, surgical, goal-driven), Usecase error discipline: errors.L vs errors.Wrapf, Usecase package `svc` import alias, Usecase layer (Layer 2), The full locale flow (header -> ctx -> translated envelope), Per-feature transport package (dto/assembler/handler/registrar) (+34 more)

### Community 3 - "Render Test Matrix and Compose Asserts"
Cohesion: 0.18
Nodes (39): composeFile, composeService, New(), assertComposeEnv(), assertComposeServices(), assertComposeVolumes(), assertHealthyDeps(), assertRequestIDContract() (+31 more)

### Community 4 - "nova new Flags and ProjectConfig"
Cohesion: 0.09
Nodes (22): applyFlags(), Command, newCommand(), runNew(), ProjectConfig, Any flag skips every prompt and fills the rest from DefaultConfig, nova new interactive prompts and flag mapping, Doc 01 - CLI options (+14 more)

### Community 5 - "Component Generator AST Scaffolding"
Cohesion: 0.11
Nodes (21): CommentGroup, Decl, File, GenDecl, renderSpec, tmplData, ImportSpec, declDoc() (+13 more)

### Community 6 - "graphify Knowledge Graph Setup"
Cohesion: 0.08
Nodes (32): 1. Prerequisites, 2. Install the package, 3. Register graphify with your assistant, 4. Install the git hooks, 5. (Optional) API key, Tracked sidecars hold machine-absolute paths, Code is AST-parsed locally, so no API key is required, Commit graphify-out so a clone starts with the map (+24 more)

### Community 7 - "sqlc Repository Code-Gen"
Cohesion: 0.16
Nodes (28): sqlc repository generator is field-driven code-gen, not a template, Known bug: `nova new` migration emits postgres DDL for mysql, Expr, entityField, fieldMapping, genFile, implContext, columnNames() (+20 more)

### Community 8 - "add Command Dispatch"
Cohesion: 0.10
Nodes (23): addCommand(), dispatchAdd(), generateAll(), Command, ComponentGenerator, runAdd(), validateComponent(), Execute() (+15 more)

### Community 9 - "Layout Manifest (nova.yaml)"
Cohesion: 0.16
Nodes (22): Component layout manifest for `nova add`, expand(), findRoot(), Load(), overlay(), overlayStack(), readModule(), Manifest.Resolve() (+14 more)

### Community 10 - "SQL Repository and Query Patterns"
Cohesion: 0.09
Nodes (23): Login — uniform failure AND uniform timing, PublicList — cursor (keyset) paging, assembler.go — the only place that maps (usecase -> wire), mapper/ — pure row <-> entity functions, Optional filters as one prepared statement (sqlc.arg idiom), Two ports satisfied outside adapter/, qx — the TX-aware query executor, The three-file SQL repository (tx_manager + qx + repository) (+15 more)

### Community 11 - "Template File Selection"
Cohesion: 0.22
Nodes (6): FileMode, Generator, templateFile, outFileMode(), sqlcEngineAlias(), Template

### Community 12 - "Worker, Messaging and Middleware"
Cohesion: 0.12
Nodes (18): Best-effort event publish (the one place a service logs), Register — switch on the lookup error, Worker ack decision: poison message acks, failed downstream nacks, Topic constant deliberately duplicated on both sides, gRPC transport is a compiling placeholder, Handlers never log — WriteError is the single funnel, Per-IP token bucket on /login only, Middleware order is load-bearing (+10 more)

### Community 13 - "Lint Configuration"
Cohesion: 0.40
Nodes (5): depguard Deny Rules (protobuf, satori/uuid, math/rand, log), Lint Exclusion Rules (test files, revive comment checks), Formatters: goimports + golines max-len 120, maratori Golden golangci-lint Config v2.4.0, sloglint no-global + context:scope

### Community 14 - "Release Build Matrix"
Cohesion: 0.67
Nodes (4): Release Workflow Job (on push tag v*), Archive + Checksum Naming (tar.gz, zip on windows), GoReleaser Cross-Platform Build Matrix (linux/darwin/windows × amd64/arm64), Version/Commit Injection via ldflags into main

### Community 15 - "Opaque Client Error Mapping"
Cohesion: 0.50
Nodes (4): Binders collapse every failure to one generic localized 400, Token verification returns a generic 401, reason logged by severity, ErrorResponseFromApp — locale code becomes the client code, WithCause — diagnostics without changing classification

## Knowledge Gaps
- **24 isolated node(s):** `github.com/quyennguyenvu/nova`, `ComponentGenerator`, `ComponentGenerator`, `genFile`, `depguard Deny Rules (protobuf, satori/uuid, math/rand, log)` (+19 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **11 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `New()` connect `Render Test Matrix and Compose Asserts` to `Generated Layout and Option Gaps`, `Component Scaffolding and Handler Wiring`, `Template Rendering and Layer Patterns`, `nova new Flags and ProjectConfig`, `Component Generator AST Scaffolding`, `sqlc Repository Code-Gen`, `add Command Dispatch`, `Template File Selection`?**
  _High betweenness centrality (0.407) - this node is a cross-community bridge._
- **Why does `Cross-cutting features every generated project ships` connect `Template Rendering and Layer Patterns` to `SQL Repository and Query Patterns`, `Worker, Messaging and Middleware`?**
  _High betweenness centrality (0.242) - this node is a cross-community bridge._
- **Why does `Nova design docs (index)` connect `Template Rendering and Layer Patterns` to `Worker, Messaging and Middleware`, `graphify Knowledge Graph Setup`?**
  _High betweenness centrality (0.198) - this node is a cross-community bridge._
- **Are the 19 inferred relationships involving `New()` (e.g. with `validateComponent()` and `runNew()`) actually correct?**
  _`New()` has 19 INFERRED edges - model-reasoned connections that need verification._
- **Are the 20 inferred relationships involving `Default()` (e.g. with `TestGenerateHandlerCustomManifest()` and `TestGenerateHandlerDefaultLayout()`) actually correct?**
  _`Default()` has 20 INFERRED edges - model-reasoned connections that need verification._
- **What connects `github.com/quyennguyenvu/nova`, `ComponentGenerator`, `ComponentGenerator` to the rest of the system?**
  _24 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Generated Layout and Option Gaps` be split into smaller, more focused modules?**
  _Cohesion score 0.06207482993197279 - nodes in this community are weakly interconnected._