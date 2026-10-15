package generator

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/quyennguyenvu/nova/internal/config"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

//go:embed all:templates
var templateFS embed.FS

const (
	postgres = "postgres"
	mysql    = "mysql"
	mongodb  = "mongodb"
	yaml     = "yaml"
	toml     = "toml"
	kafka    = "kafka"
	rabbitmq = "rabbitmq"
	sqlc     = "sqlc"
)

// Generator creates a new project from templates.
type Generator struct {
	cfg    *config.ProjectConfig
	tmpl   *template.Template
	Out    io.Writer // progress output; os.Stdout by default
	outDir string    // set by Generate; migrationFiles looks for existing names there
}

// New creates a new Generator. It validates cfg first so a typo (e.g. "echo2")
// or an unimplemented option fails here instead of producing a half-rendered
// project with cryptic "template not found in embedded FS" errors.
func New(cfg *config.ProjectConfig) (*Generator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("generator: %w", err)
	}

	funcMap := template.FuncMap{
		"lower":    strings.ToLower,
		"upper":    strings.ToUpper,
		"title":    cases.Title(language.English).String,
		"contains": strings.Contains,
		"replace":  strings.ReplaceAll,
	}

	return &Generator{
		cfg:  cfg,
		tmpl: template.New("").Funcs(funcMap),
		Out:  os.Stdout,
	}, nil
}

// templateFile maps a template path to its output path.
type templateFile struct {
	tmplPath string // path within embedded FS
	outPath  string // output path relative to project root
	cond     bool   // whether to include this file
}

// Generate creates the full project structure in the given output directory.
func (g *Generator) Generate(outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	g.outDir = outputDir

	files := g.buildFileList()

	for _, f := range files {
		if !f.cond {
			continue
		}

		if err := g.renderFile(f.tmplPath, filepath.Join(outputDir, f.outPath)); err != nil {
			return fmt.Errorf("failed to render %s: %w", f.outPath, err)
		}
	}

	return nil
}

func (g *Generator) buildFileList() []templateFile {
	var files []templateFile
	files = append(files, g.entryPointFiles()...)
	files = append(files, g.rootFiles()...)
	files = append(files, g.domainFiles()...)
	files = append(files, g.usecaseFiles()...)
	files = append(files, g.adapterFiles()...)
	files = append(files, g.transportFiles()...)
	files = append(files, g.workerFiles()...)
	files = append(files, g.infrastructureFiles()...)
	files = append(files, g.pkgFiles()...)
	files = append(files, g.migrationFiles()...)
	files = append(files, g.sqlcFiles()...)
	files = append(files, g.toolingFiles()...)
	return files
}

// sqlcEngineAlias maps cfg.Database to the template-filename prefix used for
// sqlc config + queries (pg_sqlc.yaml.tmpl, mysql_sqlc.yaml.tmpl, …).
func sqlcEngineAlias(db string) string {
	switch db {
	case postgres:
		return "pg"
	case mysql:
		return mysql
	default:
		return ""
	}
}

// sqlcFiles registers the sqlc config + per-engine query templates. Source
// templates are engine-prefixed (pg_sqlc.yaml.tmpl, mysql_user.sql.tmpl) so
// both variants live side-by-side, but the generated project sees clean
// names: sqlc/sqlc.yaml, sqlc/query/user.sql.
func (g *Generator) sqlcFiles() []templateFile {
	cfg := g.cfg
	if cfg.QueryGen != sqlc || !cfg.HasSQL() {
		return nil
	}
	engine := sqlcEngineAlias(cfg.Database)
	if engine == "" {
		return nil
	}
	return []templateFile{
		{
			fmt.Sprintf("templates/sqlc/%s_sqlc.yaml.tmpl", engine),
			"sqlc/sqlc.yaml",
			true,
		},
		{
			fmt.Sprintf("templates/sqlc/query/%s_user.sql.tmpl", engine),
			"sqlc/query/user.sql",
			true,
		},
		// Worker projects also write to user_audit_log; the query file is
		// engine-prefixed because sqlc syntax diverges (RETURNING vs
		// :execlastid). Only emit when this project is the consumer side.
		{
			fmt.Sprintf("templates/sqlc/query/%s_user_audit.sql.tmpl", engine),
			"sqlc/query/user_audit.sql",
			cfg.HasWorker(),
		},
	}
}

func (g *Generator) rootFiles() []templateFile {
	return []templateFile{
		{"templates/gomod.tmpl", "go.mod", true},
		{"templates/gitignore.tmpl", ".gitignore", true},
		{"templates/main.go.tmpl", "main.go", true},
		{"templates/env.example.tmpl", ".env.example", true},
		{"templates/README.md.tmpl", "README.md", true},
		{"templates/CLAUDE.md.tmpl", "CLAUDE.md", true},
		{"templates/nova.yaml.tmpl", "nova.yaml", true},
	}
}

func (g *Generator) domainFiles() []templateFile {
	cfg := g.cfg
	return []templateFile{
		{"templates/domain/entity/user.go.tmpl", "internal/domain/entity/user.go", true},
		{"templates/domain/user.go.tmpl", "internal/domain/user.go", true},
		{"templates/domain/tx_manager.go.tmpl", "internal/domain/tx_manager.go", true},
		{"templates/domain/identity/principal.go.tmpl", "internal/domain/identity/principal.go", true},
		{"templates/domain/identity/context.go.tmpl", "internal/domain/identity/context.go", true},
		{"templates/domain/security/hasher.go.tmpl", "internal/domain/security/hasher.go", true},
		{"templates/domain/security/token.go.tmpl", "internal/domain/security/token.go", true},
		{"templates/domain/user_publisher.go.tmpl", "internal/domain/user_publisher.go", cfg.HasMessageQueue()},
		// UserAudit aggregate — port + entry for the consumer worker's
		// example DB write. Only emitted in worker mode with a SQL backend.
		{"templates/domain/user_audit.go.tmpl", "internal/domain/user_audit.go", cfg.HasWorker() && cfg.HasSQL()},
	}
}

func (g *Generator) usecaseFiles() []templateFile {
	cfg := g.cfg
	return []templateFile{
		{"templates/usecase/user/service.go.tmpl", "internal/usecase/user/service.go", true},
		{"templates/usecase/user/dto.go.tmpl", "internal/usecase/user/dto.go", true},
		// Unit tests with fakes for every port; the bootstrap runs them.
		{"templates/usecase/user/service_test.go.tmpl", "internal/usecase/user/service_test.go", true},
		// useraudit usecase — worker-only. Records inbound user.* events
		// to user_audit_log via the audit repo.
		{
			"templates/usecase/useraudit/service.go.tmpl",
			"internal/usecase/useraudit/service.go",
			cfg.HasWorker() && cfg.HasSQL(),
		},
		{
			"templates/usecase/useraudit/dto.go.tmpl",
			"internal/usecase/useraudit/dto.go",
			cfg.HasWorker() && cfg.HasSQL(),
		},
	}
}

func (g *Generator) adapterFiles() []templateFile {
	cfg := g.cfg
	files := []templateFile{
		// Pubsub
		{
			"templates/adapter/pubsub/user_message.go.tmpl",
			"internal/adapter/pubsub/user_message.go",
			cfg.HasMessageQueue(),
		},
		{"templates/adapter/pubsub/publisher.go.tmpl", "internal/adapter/pubsub/publisher.go", cfg.HasMessageQueue()},
		{
			"templates/adapter/pubsub/user_publisher.go.tmpl",
			"internal/adapter/pubsub/user_publisher.go",
			cfg.HasMessageQueue(),
		},
	}
	files = append(files, g.sqlRepositoryFiles(postgres)...)
	files = append(files, g.sqlRepositoryFiles(mysql)...)
	return append(files, []templateFile{
		// Repository — in-memory. Keeps the User slice runnable when the project
		// has no database; the SQL adapters replace it otherwise.
		{
			"templates/adapter/repository/memory/user_repository.go.tmpl",
			"internal/adapter/repository/memory/user_repository.go",
			!cfg.HasDatabase(),
		},
		{
			"templates/adapter/repository/memory/tx_manager.go.tmpl",
			"internal/adapter/repository/memory/tx_manager.go",
			!cfg.HasDatabase(),
		},
		// Repository — cache
		{
			"templates/adapter/repository/redis/user_cache.go.tmpl",
			"internal/adapter/repository/redis/user_cache.go",
			cfg.HasRedis(),
		},
		{
			"templates/adapter/repository/redis/mapper/user.go.tmpl",
			"internal/adapter/repository/redis/mapper/user.go",
			cfg.HasRedis(),
		},
		// Search — elasticsearch. Standalone reference adapter (a derived read
		// model over entity.User); not wired into the DI graph by default.
		{
			"templates/adapter/repository/elasticsearch/user_search.go.tmpl",
			"internal/adapter/repository/elasticsearch/user_search.go",
			cfg.HasElasticsearch(),
		},
	}...)
}

// sqlRepositoryFiles lists the per-engine repository package: both engines
// share one layout, so the paths derive from the engine name.
func (g *Generator) sqlRepositoryFiles(engine string) []templateFile {
	cfg := g.cfg
	base := cfg.HasSQL() && cfg.Database == engine
	tmpl := "templates/adapter/repository/" + engine + "/"
	out := "internal/adapter/repository/" + engine + "/"
	return []templateFile{
		{tmpl + "qx.go.tmpl", out + "qx.go", base},
		{tmpl + "tx_manager.go.tmpl", out + "tx_manager.go", base},
		{tmpl + "mapper/user.go.tmpl", out + "mapper/user.go", base},
		{tmpl + "user_repository.go.tmpl", out + "user_repository.go", base},
		// Driver error translation (unique violation → ErrAlreadyExists) + its test.
		{tmpl + "errors.go.tmpl", out + "errors.go", base},
		{tmpl + "errors_test.go.tmpl", out + "errors_test.go", base},
		// Audit repo + mapper — worker-only.
		{tmpl + "mapper/user_audit.go.tmpl", out + "mapper/user_audit.go", base && cfg.HasWorker()},
		{tmpl + "user_audit_repository.go.tmpl", out + "user_audit_repository.go", base && cfg.HasWorker()},
	}
}

func (g *Generator) transportFiles() []templateFile {
	cfg := g.cfg
	files := []templateFile{
		// HTTP root router — Registrar interface + NewRouter (middleware, healthz,
		// feature routes). Source is framework-prefixed so all variants live
		// side-by-side; generated as transport/http/router.go.
		{
			fmt.Sprintf("templates/transport/http/%s_router.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/router.go",
			cfg.HasHTTP(),
		},

		// HTTP response writer — framework-specific WriteJSON/WriteError.
		// Lives under transport/http (not pkg/) because it binds to one framework.
		{
			fmt.Sprintf("templates/transport/http/httpwriter/%s_writer.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/httpwriter/writer.go",
			cfg.HasHTTP(),
		},

		// Health probes — framework-agnostic Liveness + Readiness checker.
		{
			"templates/transport/http/health/checker.go.tmpl",
			"internal/transport/http/health/checker.go",
			cfg.HasHTTP(),
		},

		// Behavior tests for the real router: envelope on 404/405, 413, 504,
		// client-IP resolution through the login limiter, the access line.
		{
			fmt.Sprintf("templates/transport/http/%s_router_test.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/router_test.go",
			cfg.HasHTTP(),
		},

		// HTTP v1/user — framework-agnostic DTO + assembler.
		{
			"templates/transport/http/v1/user/dto.go.tmpl",
			"internal/transport/http/v1/user/dto.go",
			cfg.HasHTTP(),
		},
		{
			"templates/transport/http/v1/user/assembler.go.tmpl",
			"internal/transport/http/v1/user/assembler.go",
			cfg.HasHTTP(),
		},
		// HTTP v1 — the section base (package v1): APIPrefix + the Prefixed
		// embed that every v1 feature registrar reuses for its Prefix().
		{
			"templates/transport/http/v1/v1.go.tmpl",
			"internal/transport/http/v1/v1.go",
			cfg.HasHTTP(),
		},
		// HTTP v1/user — per-framework handler + registrar. Source templates are
		// framework-prefixed (fiber_handler.go.tmpl, …) so all variants live
		// side-by-side, but the generated output drops the prefix so the
		// project sees clean names: handler.go, registrar.go. The registrar is
		// feature-owned (package user) and embeds v1.Prefixed for its Prefix().
		{
			fmt.Sprintf("templates/transport/http/v1/user/%s_handler.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/v1/user/handler.go",
			cfg.HasHTTP(),
		},
		{
			fmt.Sprintf("templates/transport/http/v1/user/%s_registrar.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/v1/user/registrar.go",
			cfg.HasHTTP(),
		},
		// Pubsub adapters live in adapterFiles() — not duplicated here.
	}
	files = append(files, g.httpMiddlewareFiles()...)
	return append(files, g.grpcFiles()...)
}

// grpcFiles lists the gRPC transport: the server factory (interceptor chain,
// feature services, health, reflection), the user service over the
// buf-generated stubs in api/gen (created by `make gen`, like dbgen), the
// status mapping and the interceptors. All gated on cfg.HasGRPC().
func (g *Generator) grpcFiles() []templateFile {
	grpc := g.cfg.HasGRPC()
	return []templateFile{
		{"templates/transport/grpc/server.go.tmpl", "internal/transport/grpc/server.go", grpc},
		{"templates/transport/grpc/user.go.tmpl", "internal/transport/grpc/user.go", grpc},
		{"templates/transport/grpc/user_test.go.tmpl", "internal/transport/grpc/user_test.go", grpc},
		{"templates/transport/grpc/rpcstatus/status.go.tmpl", "internal/transport/grpc/rpcstatus/status.go", grpc},
		{
			"templates/transport/grpc/rpcstatus/status_test.go.tmpl",
			"internal/transport/grpc/rpcstatus/status_test.go",
			grpc,
		},
		{
			"templates/transport/grpc/interceptor/interceptor.go.tmpl",
			"internal/transport/grpc/interceptor/interceptor.go",
			grpc,
		},
		{"templates/transport/grpc/interceptor/auth.go.tmpl", "internal/transport/grpc/interceptor/auth.go", grpc},
		{
			"templates/transport/grpc/interceptor/deadline.go.tmpl",
			"internal/transport/grpc/interceptor/deadline.go",
			grpc,
		},
		{
			"templates/transport/grpc/interceptor/throttle.go.tmpl",
			"internal/transport/grpc/interceptor/throttle.go",
			grpc,
		},
		{
			"templates/transport/grpc/interceptor/interceptor_test.go.tmpl",
			"internal/transport/grpc/interceptor/interceptor_test.go",
			grpc,
		},
		{"templates/transport/grpc/interceptor/locale.go.tmpl", "internal/transport/grpc/interceptor/locale.go", grpc},
	}
}

// httpMiddlewareFiles lists the middleware package: framework-prefixed
// sources with clean output names, the framework-agnostic principal slot,
// and the extras a framework needs only when it lacks a native mechanism
// (body limit outside fiber, client IP for chi and fiber).
func (g *Generator) httpMiddlewareFiles() []templateFile {
	cfg := g.cfg
	return []templateFile{
		{
			fmt.Sprintf("templates/transport/http/middleware/%s_auth.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/middleware/auth.go",
			cfg.HasHTTP(),
		},
		{
			fmt.Sprintf("templates/transport/http/middleware/%s_cors.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/middleware/cors.go",
			cfg.HasHTTP(),
		},
		{
			fmt.Sprintf("templates/transport/http/middleware/%s_locale.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/middleware/locale.go",
			cfg.HasHTTP(),
		},
		{
			fmt.Sprintf("templates/transport/http/middleware/%s_logging.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/middleware/logging.go",
			cfg.HasHTTP(),
		},
		{
			fmt.Sprintf("templates/transport/http/middleware/%s_recovery.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/middleware/recovery.go",
			cfg.HasHTTP(),
		},
		{
			fmt.Sprintf("templates/transport/http/middleware/%s_requestid.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/middleware/requestid.go",
			cfg.HasHTTP(),
		},
		{
			fmt.Sprintf("templates/transport/http/middleware/%s_loginlimit.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/middleware/loginlimit.go",
			cfg.HasHTTP(),
		},
		// Principal slot shared by Auth and Logging — framework-agnostic.
		{
			"templates/transport/http/middleware/principal.go.tmpl",
			"internal/transport/http/middleware/principal.go",
			cfg.HasHTTP(),
		},
		{
			fmt.Sprintf("templates/transport/http/middleware/%s_timeout.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/middleware/timeout.go",
			cfg.HasHTTP(),
		},
		// Body limit: fiber enforces it in fiber.Config; the net/http frameworks
		// need a middleware.
		{
			fmt.Sprintf("templates/transport/http/middleware/%s_bodylimit.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/middleware/bodylimit.go",
			cfg.HasHTTP() && cfg.HTTPFramework != "fiber",
		},
		// Client IP: gin and echo resolve trusted proxies natively and safely;
		// chi has no resolver and fiber's returns the raw header, so both get
		// a middleware that walks X-Forwarded-For past the trusted proxies.
		{
			fmt.Sprintf("templates/transport/http/middleware/%s_clientip.go.tmpl", cfg.HTTPFramework),
			"internal/transport/http/middleware/clientip.go",
			cfg.HasHTTP() && (cfg.HTTPFramework == "chi" || cfg.HTTPFramework == "fiber"),
		},
	}
}

func (g *Generator) infrastructureFiles() []templateFile {
	cfg := g.cfg
	return []templateFile{
		{"templates/infrastructure/config/config.go.tmpl", "internal/infrastructure/config/config.go", true},
		// Pins how .env combines with the YAML (needs the HTTP section).
		{
			"templates/infrastructure/config/config_test.go.tmpl",
			"internal/infrastructure/config/config_test.go",
			cfg.HasHTTP(),
		},
		{"templates/infrastructure/config/constant.go.tmpl", "internal/infrastructure/config/constant.go", true},
		{
			"templates/infrastructure/config/base.yaml.tmpl",
			"internal/infrastructure/config/base.yaml",
			cfg.ConfigFormat == yaml,
		},
		{
			"templates/infrastructure/config/development.yaml.tmpl",
			"internal/infrastructure/config/development.yaml",
			cfg.ConfigFormat == yaml,
		},
		{
			"templates/infrastructure/config/production.yaml.tmpl",
			"internal/infrastructure/config/production.yaml",
			cfg.ConfigFormat == yaml,
		},
		{
			"templates/infrastructure/jwt/claims.go.tmpl",
			"internal/infrastructure/jwt/claims.go",
			true,
		},
		{
			"templates/infrastructure/jwt/service.go.tmpl",
			"internal/infrastructure/jwt/service.go",
			true,
		},
		{
			"templates/infrastructure/database/postgres.go.tmpl",
			"internal/infrastructure/database/postgres.go",
			cfg.Database == postgres,
		},
		{
			"templates/infrastructure/database/mysql.go.tmpl",
			"internal/infrastructure/database/mysql.go",
			cfg.Database == mysql,
		},
		{"templates/infrastructure/cache/redis.go.tmpl", "internal/infrastructure/cache/redis.go", cfg.HasRedis()},
		{
			"templates/infrastructure/search/elasticsearch.go.tmpl",
			"internal/infrastructure/search/elasticsearch.go",
			cfg.HasElasticsearch(),
		},
		{
			"templates/infrastructure/pubsub/kafka.go.tmpl",
			"internal/infrastructure/pubsub/kafka.go",
			cfg.MessageQueue == kafka,
		},
		{
			"templates/infrastructure/pubsub/rabbitmq.go.tmpl",
			"internal/infrastructure/pubsub/rabbitmq.go",
			cfg.MessageQueue == rabbitmq,
		},
		{
			fmt.Sprintf("templates/infrastructure/server/%s_http.go.tmpl", cfg.HTTPFramework),
			"internal/infrastructure/server/http.go",
			cfg.HasHTTP(),
		},
		{"templates/infrastructure/server/grpc.go.tmpl", "internal/infrastructure/server/grpc.go", cfg.HasGRPC()},
		{"templates/infrastructure/logger/zerolog.go.tmpl", "internal/infrastructure/logger/zerolog.go", true},
		{"templates/infrastructure/security/bcrypt.go.tmpl", "internal/infrastructure/security/bcrypt.go", true},
		{"templates/infrastructure/tracing/otel.go.tmpl", "internal/infrastructure/tracing/otel.go", true},
		{"templates/infrastructure/di/wire.go.tmpl", "internal/infrastructure/di/wire.go", cfg.UseWire()},
		{
			"templates/infrastructure/di/fx.go.tmpl",
			"internal/infrastructure/di/fx.go",
			cfg.UseFx() && (cfg.HasHTTP() || cfg.HasGRPC() || cfg.HasWorker()),
		},
		{
			"templates/infrastructure/di/fx_provider.go.tmpl",
			"internal/infrastructure/di/fx_provider.go",
			cfg.UseFx() && (cfg.HasHTTP() || cfg.HasGRPC() || cfg.HasWorker()),
		},
		{
			"templates/infrastructure/di/wire_provider.go.tmpl",
			"internal/infrastructure/di/provider.go",
			cfg.HasHTTP() || cfg.HasGRPC() || cfg.HasWorker(),
		},
		{
			"templates/infrastructure/di/app.go.tmpl",
			"internal/infrastructure/di/app.go",
			cfg.HasHTTP() || cfg.HasGRPC() || cfg.HasWorker(),
		},
	}
}

func (g *Generator) entryPointFiles() []templateFile {
	cfg := g.cfg
	return []templateFile{
		// cmd/root.go is the cobra entrypoint — emit whenever any subcommand
		// exists (HTTP api, gRPC, cron, or worker).
		{
			"templates/cmd/root.go.tmpl",
			"cmd/root.go",
			cfg.HasHTTP() || cfg.HasGRPC() || cfg.HasCron() || cfg.HasWorker(),
		},
		{"templates/cmd/api.go.tmpl", "cmd/api.go", cfg.HasHTTP()},
		{"templates/cmd/grpc.go.tmpl", "cmd/grpc.go", cfg.HasGRPC()},
		{"templates/cmd/worker.go.tmpl", "cmd/worker.go", cfg.HasWorker()},
		{"templates/app/api.go.tmpl", "internal/app/api.go", cfg.HasHTTP()},
		{"templates/app/grpc.go.tmpl", "internal/app/grpc.go", cfg.HasGRPC()},
		{"templates/app/worker.go.tmpl", "internal/app/worker.go", cfg.HasWorker()},
	}
}

// workerFiles registers the inbound consumer transport — the Worker
// orchestrator, the broker-specific consumer, and the per-feature handlers.
// All gated on cfg.HasWorker() so HTTP-only / gRPC-only projects never
// emit these files.
func (g *Generator) workerFiles() []templateFile {
	cfg := g.cfg
	if !cfg.HasWorker() {
		return nil
	}
	files := []templateFile{
		{"templates/transport/worker/handler.go.tmpl", "internal/transport/worker/handler.go", true},
		{"templates/transport/worker/worker.go.tmpl", "internal/transport/worker/worker.go", true},
		{"templates/transport/worker/policy.go.tmpl", "internal/transport/worker/policy.go", true},
		// Broker-less unit tests for the policy; the bootstrap runs them.
		{"templates/transport/worker/worker_test.go.tmpl", "internal/transport/worker/worker_test.go", true},
		// Per-feature handlers — currently just user.created.
		{
			"templates/transport/worker/v1/user/dto.go.tmpl",
			"internal/transport/worker/v1/user/dto.go",
			true,
		},
		{
			"templates/transport/worker/v1/user/handler.go.tmpl",
			"internal/transport/worker/v1/user/handler.go",
			true,
		},
	}
	// Broker-specific consumer impl. Picked from cfg.MessageQueue — must be
	// kafka or rabbitmq; NATS doesn't yet have a consumer template.
	switch cfg.MessageQueue {
	case kafka, rabbitmq:
		// Each consumer ships the test pinning how it settles the three
		// dispatch outcomes; the bootstrap runs it.
		files = append(files,
			templateFile{
				fmt.Sprintf("templates/transport/worker/%s_consumer.go.tmpl", cfg.MessageQueue),
				"internal/transport/worker/consumer.go",
				true,
			},
			templateFile{
				fmt.Sprintf("templates/transport/worker/%s_consumer_test.go.tmpl", cfg.MessageQueue),
				"internal/transport/worker/consumer_test.go",
				true,
			},
		)
	}
	return files
}

func (g *Generator) pkgFiles() []templateFile {
	cfg := g.cfg
	return []templateFile{
		{"templates/pkg/errors/errors.go.tmpl", "pkg/errors/errors.go", true},
		{"templates/pkg/errors/errors_test.go.tmpl", "pkg/errors/errors_test.go", true},
		{"templates/pkg/httputil/response.go.tmpl", "pkg/httputil/response.go", cfg.HasHTTP()},
		// Request-id context plumbing shared by HTTP, gRPC, the worker and the publishers.
		{"templates/pkg/requestid/requestid.go.tmpl", "pkg/requestid/requestid.go", true},
		{"templates/pkg/locale/locale.go.tmpl", "pkg/locale/locale.go", true},
		{"templates/pkg/locale/locale_en.go.tmpl", "pkg/locale/locale_en.go", true},
		{"templates/pkg/locale/locale_vi.go.tmpl", "pkg/locale/locale_vi.go", true},
		{"templates/pkg/observability/observability.go.tmpl", "pkg/observability/observability.go", true},
		{"templates/pkg/logctx/logctx.go.tmpl", "pkg/logctx/logctx.go", true},
	}
}

// migrationName returns <stamp>_<suffix>, reusing the stamp of a migration
// already in the target directory: re-rendering with --force then overwrites
// it instead of adding a second copy a migration runner would apply again.
func (g *Generator) migrationName(dir, suffix, stamp string) string {
	existing, _ := filepath.Glob(filepath.Join(g.outDir, dir, "*_"+suffix+".up.sql"))
	if len(existing) > 0 {
		return strings.TrimSuffix(filepath.Base(existing[0]), ".up.sql")
	}
	return stamp + "_" + suffix
}

func (g *Generator) migrationFiles() []templateFile {
	cfg := g.cfg

	now := time.Now().Format("20060102150405")
	dir := "migrations"
	if cfg.QueryGen == sqlc {
		dir = "sqlc/migrations"
	}

	filename := g.migrationName(dir, "create_users_table", now)
	// The audit migration timestamp is bumped by one second so migration
	// runners apply create_users_table first.
	auditNow := time.Now().Add(time.Second).Format("20060102150405")
	auditFilename := g.migrationName(dir, "create_user_audit_log", auditNow)
	return []templateFile{
		{
			"templates/migrations/create_users_table.up.sql.tmpl",
			fmt.Sprintf("%s/%s.up.sql", dir, filename),
			cfg.HasSQL(),
		},
		{
			"templates/migrations/create_users_table.down.sql.tmpl",
			fmt.Sprintf("%s/%s.down.sql", dir, filename),
			cfg.HasSQL(),
		},
		// user_audit_log table — written by the consumer worker
		// (Transport=worker). Only emit when both the worker side and a
		// SQL backend are present.
		{
			"templates/migrations/create_user_audit_log.up.sql.tmpl",
			fmt.Sprintf("%s/%s.up.sql", dir, auditFilename),
			cfg.HasSQL() && cfg.HasWorker(),
		},
		{
			"templates/migrations/create_user_audit_log.down.sql.tmpl",
			fmt.Sprintf("%s/%s.down.sql", dir, auditFilename),
			cfg.HasSQL() && cfg.HasWorker(),
		},
	}
}

func (g *Generator) toolingFiles() []templateFile {
	cfg := g.cfg
	return []templateFile{
		{"templates/Dockerfile.tmpl", "Dockerfile", cfg.IncludeDocker},
		{"templates/dockerignore.tmpl", ".dockerignore", cfg.IncludeDocker},
		// docker-compose.yaml runs the app + its dependencies with per-service
		// CPU/memory limits, so a benchmark is reproducible.
		{"templates/docker-compose.yaml.tmpl", "docker-compose.yaml", cfg.IncludeDocker},
		{"templates/Makefile.tmpl", "Makefile", true},
		{"templates/golangci.yaml.tmpl", ".golangci.yaml", true},
		{"templates/ci/github-ci.yaml.tmpl", ".github/workflows/ci.yaml", cfg.IncludeCI},
		{"templates/github/pull_request_template.md.tmpl", ".github/pull_request_template.md", cfg.IncludeCI},
		{"templates/api/openapi/openapi.yaml.tmpl", "api/openapi/openapi.yaml", cfg.HasHTTP()},
		// Embeds the document so the service serves it at /openapi.yaml.
		{"templates/api/openapi/openapi.go.tmpl", "api/openapi/openapi.go", cfg.HasHTTP()},
		// The gRPC contract and its buf config; `make gen` writes api/gen.
		{"templates/api/proto/user/v1/user.proto.tmpl", "api/proto/user/v1/user.proto", cfg.HasGRPC()},
		{"templates/buf.yaml.tmpl", "buf.yaml", cfg.HasGRPC()},
		{"templates/buf.gen.yaml.tmpl", "buf.gen.yaml", cfg.HasGRPC()},
		{"templates/sqlfluff.tmpl", ".sqlfluff", true},
		// Activated by `make setup` (git config core.hooksPath .githooks).
		// Rendered with the exec bit — see outFileMode.
		{"templates/githooks/pre-commit.tmpl", ".githooks/pre-commit", true},
	}
}

// Permission bits for rendered files.
const (
	fileMode     os.FileMode = 0o600
	fileModeExec os.FileMode = 0o700
)

// outFileMode returns the permission bits for a rendered file.
func outFileMode(outPath string) os.FileMode {
	// Git silently skips a hook that isn't executable, so files under
	// .githooks/ (wired up by `make setup`) need the exec bit.
	if filepath.Base(filepath.Dir(outPath)) == ".githooks" {
		return fileModeExec
	}
	return fileMode
}

func (g *Generator) renderFile(tmplPath, outPath string) error {
	// Read template content
	content, err := templateFS.ReadFile(tmplPath)
	if err != nil {
		return fmt.Errorf("failed to read template %s: %w", tmplPath, err)
	}

	// Parse template
	tmpl, err := g.tmpl.New(tmplPath).Parse(string(content))
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %w", tmplPath, err)
	}

	// Render
	var buf bytes.Buffer
	if exeErr := tmpl.Execute(&buf, g.cfg); exeErr != nil {
		return fmt.Errorf("failed to execute template %s: %w", tmplPath, exeErr)
	}

	// Create directory
	dir := filepath.Dir(outPath)
	if mkdirErr := os.MkdirAll(dir, 0o750); mkdirErr != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, mkdirErr)
	}

	out, fmtErr := formatGoSource(outPath, buf.Bytes())
	if fmtErr != nil {
		return fmt.Errorf("template %s: %w", tmplPath, fmtErr)
	}

	// Write file
	if writeErr := os.WriteFile(outPath, out, outFileMode(outPath)); writeErr != nil {
		return fmt.Errorf("failed to write file %s: %w", outPath, writeErr)
	}

	fmt.Fprintf(g.Out, "   📄 %s\n", outPath)
	return nil
}
