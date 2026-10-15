# Graph Report - .  (2026-10-15)

## Corpus Check
- 53 files · ~119,784 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 746 nodes · 1882 edges · 44 communities (37 shown, 7 thin omitted)
- Extraction: 88% EXTRACTED · 12% INFERRED · 0% AMBIGUOUS · INFERRED: 221 edges (avg confidence: 0.84)
- Token cost: 865,105 input · 0 output

## Community Hubs (Navigation)
- Docs Claims and Compose Tests
- HTTP Parity and Security Hardening
- Component Generator Safety Guards
- Remediation Roadmap and Phase 1
- sqlc Repository Code-Gen
- Component Generator Tests
- add Command Dispatch
- Error Classification and Translation
- Design Docs Index
- User Domain and Persistence
- HTTP User Handler and Middleware
- Config, Health and Deploy
- gRPC Transport Decision
- nova new Command
- ProjectConfig Capability Helpers
- Paging and List Queries
- Worker Dead-Letter Reliability
- Phase 7 Infra Hygiene
- Go AST Merge Helpers
- Event Publish and Consume
- Layer Placement Rules
- DI Wiring and Lifecycle
- Phase 9 Close-out Fixes
- Option Validation (Phase 2)
- Remediation Review Findings
- CLI Control Flow and Config
- Config Defaults and Validate Tests
- Transport Registrars
- Toolchain Resource Limits
- Domain Services and Value Objects
- dotenv and Makefile Fixes
- Lint Configuration
- graphify Setup
- Fiber ClientIP Fix
- Release Workflow
- Key Leak via Dockerignore
- Kafka Host Boot Fix
- gRPC Recovery Localization
- GoReleaser Build
- Transport Placement
- depguard Deny-List
- ComponentGenerator Type
- Nova Module Path
- Test Helper Type T

## God Nodes (most connected - your core abstractions)
1. `New()` - 32 edges
2. `readFile()` - 30 edges
3. `httpMatrixConfig()` - 29 edges
4. `Phase 8: gRPC Decision and Docs Honesty Plan` - 29 edges
5. `Nova Remediation Review (2026-10-14)` - 29 edges
6. `newComponentGen()` - 28 edges
7. `Phase 7: Infrastructure and Deploy Hygiene Plan` - 27 edges
8. `Phase 9: Close-out Plan` - 27 edges
9. `Nova Review Remediation Roadmap` - 25 edges
10. `Generator` - 24 edges

## Surprising Connections (you probably didn't know these)
- `TestLoggingFields (planned observability exit test)` --semantically_similar_to--> `TestObservabilityContract()`  [INFERRED] [semantically similar]
  docs/superpowers/plans/2026-10-04-review-remediation-roadmap.md → internal/generator/generator_test.go
- `Mutually exclusive transports` --references--> `ProjectConfig`  [INFERRED]
  docs/02-project-layout.md → internal/config/config.go
- `Generated Projects Pass Their Own golangci-lint` --references--> `TestWorkerTemplatesIdentical()`  [INFERRED]
  docs/superpowers/plans/2026-10-04-phase1-generated-output-builds-and-lints.md → internal/generator/component_test.go
- `Worker Template Byte-Identity Rule` --references--> `TestWorkerCmdTemplatesIdentical()`  [INFERRED]
  docs/superpowers/plans/2026-10-04-review-remediation-roadmap.md → internal/generator/component_test.go
- `--type Engine Match Guard` --references--> `TestGenerateRepositoryRejectsForeignEngine()`  [EXTRACTED]
  docs/superpowers/plans/2026-10-07-phase2-cli-hardening.md → internal/generator/component_test.go

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **nova add component scaffolding pipeline** — readme_nova_add, claude_component_layout_manifest, claude_skel_template_tree, claude_sqlc_repository_generator, claude_generate_worker [INFERRED 0.85]
- **Heavy-toolchain memory guardrails** — claude_resource_limits, _golangci_run_concurrency, _github_workflows_ci_test, claude_test_all_matrix [INFERRED 0.85]
- **Release and distribution pipeline** — readme_releasing, _goreleaser_builds, _goreleaser_archives, readme_install_release_binary [INFERRED 0.85]
- **Log once at the transport boundary (wrap -> stash -> one access line)** — docs_10_shared_packages_errors_wrap, docs_10_shared_packages_apperror_stash, docs_07_transport_layer_handler, docs_07_transport_layer_writeerror, docs_07_transport_layer_logging_middleware, docs_13_cross_cutting_log_once [EXTRACTED 1.00]
- **Classify at origin, translate at edge (error + locale flow)** — docs_10_shared_packages_errors_l, docs_10_shared_packages_localehttpstatus, docs_07_transport_layer_locale_middleware, docs_10_shared_packages_language_ctx, docs_07_transport_layer_writeerror, docs_10_shared_packages_errorresponsefromapp, docs_10_shared_packages_newmapping, docs_13_cross_cutting_classify_translate [EXTRACTED 1.00]
- **At-least-once event pipeline (publish, consume, retry/park, record once)** — docs_08_adapter_layer_pubsub_publisher, docs_08_adapter_layer_usercreatedmessage, docs_07_transport_layer_worker, docs_07_transport_layer_worker_policy, docs_07_transport_layer_poison, docs_07_transport_layer_delivery_settlement, docs_06_usecase_layer_useraudit [INFERRED 0.85]
- **nova add Write-Safety Guards** — docs_superpowers_plans_2026_10_07_phase2_cli_hardening_component_name_validation, docs_superpowers_plans_2026_10_07_phase2_cli_hardening_output_path_confinement, docs_superpowers_plans_2026_10_07_phase2_cli_hardening_overwrite_guard, docs_superpowers_plans_2026_10_07_phase2_cli_hardening_migration_dedup, docs_superpowers_plans_2026_10_07_phase2_cli_hardening_engine_type_guard [EXTRACTED 1.00]
- **Real-Toolchain Verification Harness for Generated Output** — docs_superpowers_plans_2026_10_04_phase1_generated_output_builds_and_lints_gofmt_on_write, docs_superpowers_plans_2026_10_04_phase1_generated_output_builds_and_lints_real_toolchain_bootstrap, docs_superpowers_plans_2026_10_04_phase1_generated_output_builds_and_lints_make_gen_bootstrap_order, docs_superpowers_plans_2026_10_04_phase1_generated_output_builds_and_lints_generated_project_lint_fixes, internal_generator_generator_test_testgeneratedprojectbootstrap [EXTRACTED 1.00]
- **End-to-End Request and Trace Correlation Flow** — docs_superpowers_plans_2026_10_08_phase3_observability_request_id_validation, docs_superpowers_plans_2026_10_08_phase3_observability_otel_http_middleware, docs_superpowers_plans_2026_10_08_phase3_observability_publisher_correlation_headers, docs_superpowers_plans_2026_10_08_phase3_observability_worker_outcome_line, docs_superpowers_plans_2026_10_08_phase3_observability_grpc_interceptors [INFERRED 0.85]
- **Phase 4 HTTP hardening contract shared by fiber, gin, chi and echo** — docs_superpowers_plans_2026_10_12_phase4_http_parity_security_trusted_proxy_client_ip, docs_superpowers_plans_2026_10_12_phase4_http_parity_security_cors_never_widens, docs_superpowers_plans_2026_10_12_phase4_http_parity_security_timeout_middleware, docs_superpowers_plans_2026_10_12_phase4_http_parity_security_bodylimit_middleware, docs_superpowers_plans_2026_10_12_phase4_http_parity_security_errors_fromstatus, docs_superpowers_plans_2026_10_12_phase4_http_parity_security_rfc6750_bearer_challenge, docs_superpowers_plans_2026_10_12_phase4_http_parity_security_login_rate_limiter, docs_superpowers_plans_2026_10_12_phase4_http_parity_security_readiness_checker, internal_generator_generator_test_testhttpparity [EXTRACTED 1.00]
- **Worker retry-and-park dead-letter flow** — docs_superpowers_plans_2026_10_13_phase5_worker_reliability_messaging_worker_policy, docs_superpowers_plans_2026_10_13_phase5_worker_reliability_messaging_worker_run_retry_loop, docs_superpowers_plans_2026_10_13_phase5_worker_reliability_messaging_worker_poison, docs_superpowers_plans_2026_10_13_phase5_worker_reliability_messaging_err_parked, docs_superpowers_plans_2026_10_13_phase5_worker_reliability_messaging_kafka_consumer, docs_superpowers_plans_2026_10_13_phase5_worker_reliability_messaging_rabbitmq_consumer, docs_superpowers_plans_2026_10_13_phase5_worker_reliability_messaging_dead_letter_topology [EXTRACTED 1.00]
- **Concurrent duplicate registration answered with 409** — docs_superpowers_plans_2026_10_14_phase6_layers_domain_correctness_tx_manager_in_usecase, docs_superpowers_plans_2026_10_14_phase6_layers_domain_correctness_unique_violation_translate, docs_superpowers_plans_2026_10_14_phase6_layers_domain_correctness_generated_layer_tests [EXTRACTED 1.00]
- **gRPC unary interceptor chain (RequestID, Locale, Logging, Recovery, Auth)** — docs_superpowers_plans_2026_10_14_phase8_grpc_docs_honesty_transportgrpc_new, docs_superpowers_plans_2026_10_14_phase8_grpc_docs_honesty_interceptor_locale, docs_superpowers_plans_2026_10_14_phase8_grpc_docs_honesty_logging_interceptor_stash, docs_superpowers_plans_2026_10_14_phase8_grpc_docs_honesty_recovery_closure, docs_superpowers_plans_2026_10_14_phase8_grpc_docs_honesty_interceptor_auth [EXTRACTED 1.00]
- **Host quick-start .env path (loader, Makefile, defects)** — docs_superpowers_plans_2026_10_14_phase7_infra_deploy_hygiene_loaddotenv, docs_superpowers_plans_2026_10_14_phase9_close_out_dotenv_nonempty, docs_superpowers_plans_2026_10_14_phase9_close_out_makefile_hygiene, docs_superpowers_reviews_2026_10_14_remediation_review_med_quickstart_migration, docs_superpowers_reviews_2026_10_14_remediation_review_med_empty_env_keys [INFERRED 0.85]
- **Broker defaults and dead-letter remediation** — docs_superpowers_reviews_2026_10_14_remediation_review_high_rabbitmq_demo, docs_superpowers_reviews_2026_10_14_remediation_review_med_rabbitmq_exchange_crash, docs_superpowers_reviews_2026_10_14_remediation_review_high_kafka_host_boot, docs_superpowers_reviews_2026_10_14_remediation_review_med_kafka_dlq_loop, docs_superpowers_plans_2026_10_14_phase9_close_out_rabbitmq_events_exchange, docs_superpowers_plans_2026_10_14_phase9_close_out_kafka_compose_listeners, docs_superpowers_plans_2026_10_14_phase9_close_out_kafka_park_retry [INFERRED 0.95]

## Communities (44 total, 7 thin omitted)

### Community 0 - "Docs Claims and Compose Tests"
Cohesion: 0.10
Nodes (83): Phase 1: Generated Output Builds and Lints, composeFile, composeService, assertCORSAndTraceOrder(), assertNovaTargetsListed(), assertRejectedValues(), docFile(), funcBody() (+75 more)

### Community 1 - "HTTP Parity and Security Hardening"
Cohesion: 0.05
Nodes (46): embed.FS text/template rendering with go/format, gofmt-on-Write for Rendered Go (formatGoSource), Access log resolves handler errors inline, middleware.BodyLimit (http.MaxBytesReader), chi middleware.ClientIP, context.DeadlineExceeded to 504 classification, errors.FromStatus / statusLocale, HTTPConfig.TrustedProxyNets() (+38 more)

### Community 2 - "Component Generator Safety Guards"
Cohesion: 0.09
Nodes (32): Injectable io.Writer for Generator Progress Output, Output Path Confinement Under Project Root, Feature-File Overwrite Guard (nova add --force), One Acronym-Aware Snake and Title in manifest, tmplData, fileExists(), ComponentGenerator, Writer (+24 more)

### Community 3 - "Remediation Roadmap and Phase 1"
Cohesion: 0.09
Nodes (43): Phase 1 Plan: Generated Output Builds and Lints, Generated Projects Pass Their Own golangci-lint, In-Memory UserRepository for --database=none, make gen Order: sqlc -> tidy -> wire -> proto, Per-Engine users Migration DDL, Gated Readiness Probe Body, Real-Toolchain Bootstrap Proof (make gen -> go build -> golangci-lint run), Nova Review Remediation Roadmap (+35 more)

### Community 4 - "sqlc Repository Code-Gen"
Cohesion: 0.13
Nodes (33): GenerateWorker runnable worker scaffold, skel/ template tree (one dir per add command), Field-driven sqlc repository generator, Irregular Plural Rules for Table Names, Expr, entityField, fieldMapping, genFile (+25 more)

### Community 5 - "Component Generator Tests"
Cohesion: 0.23
Nodes (33): assertFileContains(), ComponentGenerator, T, kafkaManifest(), newComponentGen(), TestGenerateEntityDefaultLayout(), TestGenerateEntityRefusesOverwriteWithoutForce(), TestGenerateHandlerCustomManifest() (+25 more)

### Community 6 - "add Command Dispatch"
Cohesion: 0.16
Nodes (24): addCommand(), dispatchAdd(), generateAll(), Command, ComponentGenerator, runAdd(), T, TestValidateComponentNames() (+16 more)

### Community 7 - "Error Classification and Translation"
Cohesion: 0.12
Nodes (25): Usecase error discipline (errors.L vs Wrapf, never log), Auth middleware (bearer token -> principal), Locale middleware (Accept-Language), rpcstatus.FromError (AppError -> gRPC status), httpwriter.WriteError, zerolog Logger (implements observability.Logger), AppError, AppError context stash (PrepareAppErrorContext / StashAppError) (+17 more)

### Community 8 - "Design Docs Index"
Cohesion: 0.31
Nodes (22): 01 — CLI options, 02 — Project layout, 03 — Architecture rules, 04 — Placement rationale, Domain Layer (doc 05), Usecase Layer (doc 06), Transport Layer (doc 07), Adapter Layer (doc 08) (+14 more)

### Community 9 - "User Domain and Persistence"
Cohesion: 0.14
Nodes (21): entity.User, NewUser / Rename entity invariants, domain.UserRepository port, Best-effort event publish, Register (hash outside tx, check+insert in WithinTx), Uniqueness check is advisory; the constraint decides, Elasticsearch user index (derived read model), mapper package (row <-> entity) (+13 more)

### Community 10 - "HTTP User Handler and Middleware"
Cohesion: 0.15
Nodes (21): Login uniform failure and timing (timingHash), requireSelf ownership check, svc import alias convention (usersvc), usersvc.Service (user usecase), httpwriter.Bind / BindQuery / ParseID, ClientIP middleware (trusted proxies), CORS middleware (empty list installs nothing), transport/cronjob (not scaffolded) (+13 more)

### Community 11 - "Config, Health and Deploy"
Cohesion: 0.13
Nodes (20): health.Checker (Liveness/Readiness), Recovery middleware, config.Load (embedded YAML + env overrides), Config precedence: base.yaml -> <APP_ENV>.yaml -> .env -> env vars, Escape-safe DSN assembly (net/url, mysql.Config.FormatDSN), NewKafkaProducer (sync, WaitForAll), YAML vs .env split by nature, Build and Deploy (doc 12) (+12 more)

### Community 12 - "gRPC Transport Decision"
Cohesion: 0.22
Nodes (18): Phase 8: gRPC Decision and Docs Honesty Plan, buf generate with go-run-pinned protoc plugins, Rendered gRPC end-to-end tests over bufconn, NewGRPCServer flips health to NOT_SERVING before GracefulStop, GRPCConfig.Reflection flag (on in base, off in production), gRPC Registrar interface (Register, Protected), int64 pagination fields in proto (deviation), gRPC Auth interceptor (bearer token in authorization metadata) (+10 more)

### Community 13 - "nova new Command"
Cohesion: 0.25
Nodes (15): applyFlags(), checkTargetDir(), Command, ProjectConfig, newCommand(), nextSteps(), runNew(), ProjectConfig (+7 more)

### Community 15 - "Paging and List Queries"
Cohesion: 0.15
Nodes (17): Filter passthrough rule, Two read shapes: List+Count vs ListAfter, domain.UserFilter, List (page-based, policy clamped in usecase), PublicList (cursor/keyset paging), Usecase DTOs (Output, ListOutput, PublicOutput, LoginOutput), assembler.go projections (usecase -> wire), RequestID middleware (+9 more)

### Community 16 - "Worker Dead-Letter Reliability"
Cohesion: 0.17
Nodes (17): Dead-letter wire contract (.dlq, .dlx, x-dead-letter-reason/source), Dispatch outcomes: handled, parked, abandoned, worker.ErrConsumerClosed, domain.ErrEventAlreadyRecorded, worker.ErrHandlerPanic, worker.ErrParked, UserCreatedMessage.EventID / x-event-id header, Idempotent user_audit_log insert (event_id unique index) (+9 more)

### Community 17 - "Phase 7 Infra Hygiene"
Cohesion: 0.13
Nodes (17): Phase 7: Infrastructure and Deploy Hygiene Plan, APP_ENV whitelist (local, development, production), cache.New rejects empty Redis addrs, DB_USER required check, Elasticsearch lint cost (Search off in bootstrap), Config-driven fx stop budget, wire as a go tool dependency, Bounded migrate-down n (default 1) (+9 more)

### Community 18 - "Go AST Merge Helpers"
Cohesion: 0.19
Nodes (14): CommentGroup, Decl, Go AST Merge Helpers Moved to internal/gosrc, File, GenDecl, ImportSpec, declDoc(), importGroup() (+6 more)

### Community 19 - "Event Publish and Consume"
Cohesion: 0.17
Nodes (16): event.EventPublisher port (not scaffolded), domain.UserPublisher port + UserCreated event, useraudit RecordUserCreated (record each event once), Delivery settlement (ack / dead-letter / redeliver), Duplicated topic constant (user.created), Logging middleware (one access line per request), worker.Poison, transport/worker Worker orchestrator (+8 more)

### Community 20 - "Layer Placement Rules"
Cohesion: 0.14
Nodes (15): HTTP envelope vs writer split, jwt/ in infrastructure, not adapter, observability Logger port in pkg/, TxManager interface/implementation split, Adapter vs Infrastructure decision guide, Dependency Inversion (interfaces in domain), Dependency Rule (dependencies point inward), Ports implemented outside adapter/ (+7 more)

### Community 21 - "DI Wiring and Lifecycle"
Cohesion: 0.19
Nodes (15): transport/http.NewRouter, HTTPServer lifecycle wrapper, (instance, cleanup, error) constructor contract, Sub-config projection, Constructors verify connectivity (fail fast), errors.FromStatus, HTTPApp / GRPCApp / WorkerApp bundles, fx modules (Uber fx DI) (+7 more)

### Community 22 - "Phase 9 Close-out Fixes"
Cohesion: 0.15
Nodes (15): Docs honesty guarded by a test, Phase 9: Close-out Plan, nova add worker refuses a non-kafka/rabbitmq broker, nova new --force reuses existing migration name, gRPC Deadline interceptor (grpc.request_timeout, 10s), gRPC deny-by-default auth (Public(), Throttled()), gRPC Throttle interceptor (per-peer token bucket on Login and CreateUser), Kafka park retries dead-letter write with capped backoff (+7 more)

### Community 23 - "Option Validation (Phase 2)"
Cohesion: 0.24
Nodes (9): Phase 2: CLI Hardening, Phase 2 Plan: CLI Hardening, --type Engine Match Guard, Flags().Changed for Boolean Flags, Skip Existing Create-Table Migration on Repository Re-run, Single Option Registry and Validate Gate, Validate Normalization of Engine-Dependent Fields, ProjectConfig (+1 more)

### Community 24 - "Remediation Review Findings"
Cohesion: 0.21
Nodes (13): Bootstrap coverage of gen-api/gen-worker/gen-grpc and wireinject vet, RabbitMQ shared default exchange events and <queue>.dlx, Validate normalizations and empty DefaultConfig module/driver, Nova Remediation Review (2026-10-14), Roadmap marks a phase done when code lands, not when exit criteria pass, TestRenderedGoIsGofmtClean cannot fail, High: RabbitMQ README demo cannot deliver, High: worker without a database cannot compile (+5 more)

### Community 25 - "CLI Control Flow and Config"
Cohesion: 0.18
Nodes (10): Component layout manifest (nova add), ProjectConfig as single source of truth, CLI control flow (main.go → cmd.Execute → cobra root), Variant-prefixed template naming convention, Execute(), --database=none in-memory UserRepository, Up-front config validation (ProjectConfig.Validate), main() (+2 more)

### Community 26 - "Config Defaults and Validate Tests"
Cohesion: 0.33
Nodes (9): Interactive prompts and flag mapping, DefaultConfig(), ProjectConfig, T, TestValidateErrorNamesEveryProblem(), TestValidateFillsDefaults(), TestValidateNormalizes(), TestValidateRejectsUnimplemented() (+1 more)

### Community 27 - "Transport Registrars"
Cohesion: 0.27
Nodes (10): gRPC Registrar (Register/Public/Throttled), transport/grpc.New (gRPC server factory), Per-feature transport package (dto, assembler, handler, registrar), HTTP Registrar interface, Transport validation is shape, not policy, user Registrar (auth boundary at group level), v1.Prefixed / APIPrefix (/api/v1), worker Handler and Consumer ports (+2 more)

### Community 28 - "Toolchain Resource Limits"
Cohesion: 0.33
Nodes (7): CI Test job (go test -short), Lint analyzer concurrency cap (4), Resource limits (one heavy toolchain command at a time), make test-all (render-then-vet matrix + real-toolchain bootstraps), Working principles (think, simplicity, surgical, goal-driven), Elasticsearch typedapi lint-time cost, make gen order (sqlc → buf → tidy → wire)

### Community 29 - "Domain Services and Value Objects"
Cohesion: 0.29
Nodes (7): Return data vs return a decision test, Domain service (PricingService, not scaffolded), Value objects (not scaffolded), valueobject.Email, valueobject.Money, Repository vs domain service vs usecase, pricing.Service (domain service implementation)

### Community 30 - "dotenv and Makefile Fixes"
Cohesion: 0.40
Nodes (6): loadDotEnv (.env loading on host runs), loadDotEnv sets only non-empty unset values; list entries trimmed, Generated Makefile hygiene (MIGRATE_URL from .env, ## help, $(CURDIR)), Medium: empty .env keys wipe YAML values, Medium: make -C deletes in the wrong place ($(PWD)), Medium: quick-start migration fails

### Community 31 - "Lint Configuration"
Cohesion: 0.40
Nodes (5): CI Lint job (golangci-lint v2.11.4, pinned), goimports + golines formatters (max-len 120), golangci-lint golden config (maratori, v2.4.0 base), Template and lint conventions, Template wins over doc rule

### Community 32 - "graphify Setup"
Cohesion: 0.40
Nodes (5): Absolute-path sidecar gotcha (.graphify_root, .graphify_python), Committed graphify-out/ graph, graphify post-commit rebuild hook, graphify knowledge-graph tool, Graph shrink guard

### Community 33 - "Fiber ClientIP Fix"
Cohesion: 0.67
Nodes (4): Fiber ClientIP middleware (RemoteIP), Rendered router_test.go behavior tests per HTTP framework, Grep-only contract tests (no rendered HTTP behavior tests), High: Fiber trusts a forged client IP

### Community 34 - "Release Workflow"
Cohesion: 0.67
Nodes (3): Release Workflow Job (on push tag v*), Version-less release archive names, Install from release binary

### Community 35 - "Key Leak via Dockerignore"
Cohesion: 1.00
Nodes (3): .dockerignore template, .gitignore/.dockerignore exclude *.key, *.pem, jwt.pub, Medium: JWT key pair can leak

### Community 36 - "Kafka Host Boot Fix"
Cohesion: 1.00
Nodes (3): Kafka producer dials at startup (deliberate), Compose Kafka listeners (kafka:29092 in-network, localhost:9092 host), High: Kafka projects cannot boot on the host

### Community 37 - "gRPC Recovery Localization"
Cohesion: 1.00
Nodes (3): gRPC Recovery interceptor via closure (deviation), gRPC Recovery answers with the localized internal-error message, Low: gRPC panics answer in English

## Knowledge Gaps
- **24 isolated node(s):** `github.com/quyennguyenvu/nova`, `Release Workflow Job (on push tag v*)`, `ComponentGenerator`, `ComponentGenerator`, `genFile` (+19 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **7 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `New()` connect `Docs Claims and Compose Tests` to `HTTP Parity and Security Hardening`, `Component Generator Safety Guards`, `sqlc Repository Code-Gen`, `Component Generator Tests`, `add Command Dispatch`, `nova new Command`, `Option Validation (Phase 2)`?**
  _High betweenness centrality (0.217) - this node is a cross-community bridge._
- **Why does `Phase 5: Worker Reliability and Messaging Plan` connect `Design Docs Index` to `Docs Claims and Compose Tests`, `Remediation Roadmap and Phase 1`, `Component Generator Tests`, `Config, Health and Deploy`, `Worker Dead-Letter Reliability`?**
  _High betweenness centrality (0.140) - this node is a cross-community bridge._
- **Why does `Nova Review Remediation Roadmap` connect `Remediation Roadmap and Phase 1` to `Docs Claims and Compose Tests`, `Design Docs Index`, `gRPC Transport Decision`, `Phase 7 Infra Hygiene`, `Phase 9 Close-out Fixes`, `Option Validation (Phase 2)`, `Remediation Review Findings`?**
  _High betweenness centrality (0.115) - this node is a cross-community bridge._
- **Are the 25 inferred relationships involving `New()` (e.g. with `validateComponent()` and `runNew()`) actually correct?**
  _`New()` has 25 INFERRED edges - model-reasoned connections that need verification._
- **Are the 3 inferred relationships involving `readFile()` (e.g. with `assertCORSAndTraceOrder()` and `repoFile()`) actually correct?**
  _`readFile()` has 3 INFERRED edges - model-reasoned connections that need verification._
- **Are the 2 inferred relationships involving `httpMatrixConfig()` (e.g. with `assertRejectedValues()` and `TestDocsClaims()`) actually correct?**
  _`httpMatrixConfig()` has 2 INFERRED edges - model-reasoned connections that need verification._
- **What connects `github.com/quyennguyenvu/nova`, `Release Workflow Job (on push tag v*)`, `ComponentGenerator` to the rest of the system?**
  _24 weakly-connected nodes found - possible documentation gaps or missing edges._