package generator

import (
	"bytes"
	"embed"
	"fmt"
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
)

// Generator creates a new project from templates.
type Generator struct {
	cfg  *config.ProjectConfig
	tmpl *template.Template
}

// supportedFrameworks is the whitelist for cfg.HTTPFramework. Each value must
// have a matching `<framework>_router.go.tmpl` + middleware + handler set in
// internal/generator/templates/. Add new frameworks here in lockstep with
// the template files.
//
//nolint:gochecknoglobals // immutable validation set; treated as a const.
var supportedFrameworks = map[string]bool{
	"fiber": true,
	"gin":   true,
	"chi":   true,
	"echo":  true,
}

// supportedDatabases lists the choices generator.New() will accept for
// cfg.Database. Empty string means "no database" (also valid).
//
//nolint:gochecknoglobals // immutable validation set; treated as a const.
var supportedDatabases = map[string]bool{
	"":         true,
	"none":     true,
	"postgres": true,
	"mysql":    true,
}

// supportedDI is the whitelist for cfg.DI. "wire" renders wire.go (+ wire_gen.go
// via `make gen`); "fx" renders fx.go, which wires the graph at runtime with
// Uber fx and needs no code generation. Both expose the same Initialize* entry
// points so the app layer is identical regardless of the choice.
// Values outside this set — the removed "manual", or a typo — fail fast in New()
// instead of rendering a project with no Initialize* functions.
//
//nolint:gochecknoglobals // immutable validation set; treated as a const.
var supportedDI = map[string]bool{
	"wire": true,
	"fx":   true,
}

// New creates a new Generator. It fails fast on misconfigured cfg.Framework /
// cfg.Database / cfg.DI so a typo (e.g. "echo2") surfaces here instead of producing a
// half-rendered project with cryptic "template not found in embedded FS" errors.
func New(cfg *config.ProjectConfig) (*Generator, error) {
	if cfg.HasHTTP() && !supportedFrameworks[cfg.HTTPFramework] {
		return nil, fmt.Errorf(
			"generator: unsupported HTTPFramework %q (valid: fiber, gin, chi, echo)",
			cfg.HTTPFramework,
		)
	}
	if !supportedDatabases[cfg.Database] {
		return nil, fmt.Errorf(
			"generator: unsupported Database %q (valid: postgres, mysql, none)",
			cfg.Database,
		)
	}
	if !supportedDI[cfg.DI] {
		return nil, fmt.Errorf(
			"generator: unsupported DI %q (valid: wire, fx)",
			cfg.DI,
		)
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
		return "mysql"
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
	if cfg.QueryGen != "sqlc" || !cfg.HasSQL() {
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
	return []templateFile{
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
		// Repository — postgres
		{
			"templates/adapter/repository/postgres/qx.go.tmpl",
			"internal/adapter/repository/postgres/qx.go",
			cfg.HasSQL() && (cfg.Database == postgres),
		},
		{
			"templates/adapter/repository/postgres/tx_manager.go.tmpl",
			"internal/adapter/repository/postgres/tx_manager.go",
			cfg.HasSQL() && (cfg.Database == postgres),
		},
		{
			"templates/adapter/repository/postgres/mapper/user.go.tmpl",
			"internal/adapter/repository/postgres/mapper/user.go",
			cfg.HasSQL() && (cfg.Database == postgres),
		},
		{
			"templates/adapter/repository/postgres/user_repository.go.tmpl",
			"internal/adapter/repository/postgres/user_repository.go",
			cfg.HasSQL() && (cfg.Database == postgres),
		},
		// Audit repo + mapper (postgres) — worker-only.
		{
			"templates/adapter/repository/postgres/mapper/user_audit.go.tmpl",
			"internal/adapter/repository/postgres/mapper/user_audit.go",
			cfg.HasWorker() && cfg.HasSQL() && cfg.Database == postgres,
		},
		{
			"templates/adapter/repository/postgres/user_audit_repository.go.tmpl",
			"internal/adapter/repository/postgres/user_audit_repository.go",
			cfg.HasWorker() && cfg.HasSQL() && cfg.Database == postgres,
		},
		// Repository — mysql
		{
			"templates/adapter/repository/mysql/qx.go.tmpl",
			"internal/adapter/repository/mysql/qx.go",
			cfg.HasSQL() && (cfg.Database == mysql),
		},
		{
			"templates/adapter/repository/mysql/tx_manager.go.tmpl",
			"internal/adapter/repository/mysql/tx_manager.go",
			cfg.HasSQL() && (cfg.Database == mysql),
		},
		{
			"templates/adapter/repository/mysql/mapper/user.go.tmpl",
			"internal/adapter/repository/mysql/mapper/user.go",
			cfg.HasSQL() && (cfg.Database == mysql),
		},
		{
			"templates/adapter/repository/mysql/user_repository.go.tmpl",
			"internal/adapter/repository/mysql/user_repository.go",
			cfg.HasSQL() && (cfg.Database == mysql),
		},
		// Audit repo + mapper (mysql) — worker-only.
		{
			"templates/adapter/repository/mysql/mapper/user_audit.go.tmpl",
			"internal/adapter/repository/mysql/mapper/user_audit.go",
			cfg.HasWorker() && cfg.HasSQL() && cfg.Database == mysql,
		},
		{
			"templates/adapter/repository/mysql/user_audit_repository.go.tmpl",
			"internal/adapter/repository/mysql/user_audit_repository.go",
			cfg.HasWorker() && cfg.HasSQL() && cfg.Database == mysql,
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
	}
}

func (g *Generator) transportFiles() []templateFile {
	cfg := g.cfg
	return []templateFile{
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
		// HTTP middleware — same pattern: framework-prefixed source, clean output.
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
		// gRPC handler
		{
			"templates/transport/grpc/user_handler.go.tmpl",
			"internal/transport/grpc/user.go",
			cfg.HasGRPC(),
		},
		// Pubsub adapters live in adapterFiles() — not duplicated here.
	}
}

func (g *Generator) infrastructureFiles() []templateFile {
	cfg := g.cfg
	return []templateFile{
		{"templates/infrastructure/config/config.go.tmpl", "internal/infrastructure/config/config.go", true},
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
	case kafka:
		files = append(files, templateFile{
			"templates/transport/worker/kafka_consumer.go.tmpl",
			"internal/transport/worker/consumer.go",
			true,
		})
	case rabbitmq:
		files = append(files, templateFile{
			"templates/transport/worker/rabbitmq_consumer.go.tmpl",
			"internal/transport/worker/consumer.go",
			true,
		})
	}
	return files
}

func (g *Generator) pkgFiles() []templateFile {
	cfg := g.cfg
	return []templateFile{
		{"templates/pkg/errors/errors.go.tmpl", "pkg/errors/errors.go", true},
		{"templates/pkg/httputil/response.go.tmpl", "pkg/httputil/response.go", cfg.HasHTTP()},
		{"templates/pkg/locale/locale.go.tmpl", "pkg/locale/locale.go", true},
		{"templates/pkg/locale/locale_en.go.tmpl", "pkg/locale/locale_en.go", true},
		{"templates/pkg/locale/locale_vi.go.tmpl", "pkg/locale/locale_vi.go", true},
		{"templates/pkg/observability/observability.go.tmpl", "pkg/observability/observability.go", true},
		{"templates/pkg/logctx/logctx.go.tmpl", "pkg/logctx/logctx.go", true},
	}
}

func (g *Generator) migrationFiles() []templateFile {
	cfg := g.cfg

	now := time.Now().Format("20060102150405")
	dir := "migrations"
	if cfg.QueryGen == "sqlc" {
		dir = "sqlc/migrations"
	}

	filename := fmt.Sprintf("%s_create_users_table", now)
	// The audit migration timestamp is bumped by one second so migration
	// runners apply create_users_table first.
	auditNow := time.Now().Add(time.Second).Format("20060102150405")
	auditFilename := fmt.Sprintf("%s_create_user_audit_log", auditNow)
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
		{"templates/Makefile.tmpl", "Makefile", true},
		{"templates/golangci.yaml.tmpl", ".golangci.yaml", true},
		{"templates/ci/github-ci.yaml.tmpl", ".github/workflows/ci.yaml", cfg.IncludeCI},
		{"templates/github/pull_request_template.md.tmpl", ".github/pull_request_template.md", cfg.IncludeCI},
		{"templates/api/openapi/openapi.yaml.tmpl", "api/openapi/openapi.yaml", cfg.HasHTTP()},
		{"templates/sqlfluff.tmpl", ".sqlfluff", true},
	}
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

	// Write file
	if writeErr := os.WriteFile(outPath, buf.Bytes(), 0o600); writeErr != nil {
		return fmt.Errorf("failed to write file %s: %w", outPath, writeErr)
	}

	fmt.Fprintf(os.Stdout, "   📄 %s\n", outPath)
	return nil
}
