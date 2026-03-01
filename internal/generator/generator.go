package generator

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"nova/internal/config"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

//go:embed all:templates
var templateFS embed.FS

// Generator creates a new project from templates.
type Generator struct {
	cfg  *config.ProjectConfig
	tmpl *template.Template
}

// New creates a new Generator.
func New(cfg *config.ProjectConfig) *Generator {
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
	}
}

// templateFile maps a template path to its output path.
type templateFile struct {
	tmplPath string // path within embedded FS
	outPath  string // output path relative to project root
	cond     bool   // whether to include this file
}

// Generate creates the full project structure in the given output directory.
func (g *Generator) Generate(outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
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
	cfg := g.cfg
	files := []templateFile{
		// go.mod & root files
		{"templates/gomod.tmpl", "go.mod", true},
		{"templates/gitignore.tmpl", ".gitignore", true},
		{"templates/env.example.tmpl", ".env.example", true},
		{"templates/README.md.tmpl", "README.md", true},

		// Domain layer
		{"templates/domain/entity/user.go.tmpl", "internal/domain/entity/user.go", true},
		{"templates/domain/repository/user_repository.go.tmpl", "internal/domain/repository/user_repository.go", true},
		{"templates/domain/service/user_service.go.tmpl", "internal/domain/service/user_service.go", true},
		{"templates/domain/valueobject/email.go.tmpl", "internal/domain/valueobject/email.go", true},

		// Use case layer
		{"templates/usecase/user/service.go.tmpl", "internal/usecase/user/service.go", true},
		{"templates/usecase/user/dto.go.tmpl", "internal/usecase/user/dto.go", true},
		{"templates/usecase/user/errors.go.tmpl", "internal/usecase/user/errors.go", true},

		// Adapter - repository
		{"templates/adapter/repository/postgres/user_repository.go.tmpl", "internal/adapter/repository/postgres/user_repository.go", cfg.HasSQL() && (cfg.Database == "postgres")},
		{"templates/adapter/repository/mysql/user_repository.go.tmpl", "internal/adapter/repository/mysql/user_repository.go", cfg.HasSQL() && (cfg.Database == "mysql")},
		{"templates/adapter/repository/cache/user_cache.go.tmpl", "internal/adapter/repository/cache/user_cache.go", cfg.HasCache()},

		// Adapter - HTTP handler
		{"templates/adapter/handler/http/v1/user_handler.go.tmpl", "internal/adapter/handler/http/v1/user_handler.go", cfg.HasHTTP()},
		{"templates/adapter/handler/http/v1/router.go.tmpl", "internal/adapter/handler/http/v1/router.go", cfg.HasHTTP()},
		{"templates/adapter/handler/http/middleware/auth.go.tmpl", "internal/adapter/handler/http/middleware/auth.go", cfg.HasHTTP()},
		{"templates/adapter/handler/http/middleware/logging.go.tmpl", "internal/adapter/handler/http/middleware/logging.go", cfg.HasHTTP()},
		{"templates/adapter/handler/http/middleware/recovery.go.tmpl", "internal/adapter/handler/http/middleware/recovery.go", cfg.HasHTTP()},
		{"templates/adapter/handler/http/middleware/requestid.go.tmpl", "internal/adapter/handler/http/middleware/requestid.go", cfg.HasHTTP()},
		{"templates/adapter/handler/http/middleware/cors.go.tmpl", "internal/adapter/handler/http/middleware/cors.go", cfg.HasHTTP()},

		// Adapter - gRPC handler
		{"templates/adapter/handler/grpc/user_handler.go.tmpl", "internal/adapter/handler/grpc/user_handler.go", cfg.HasGRPC()},

		// Adapter - presenter
		{"templates/adapter/presenter/json_presenter.go.tmpl", "internal/adapter/presenter/json_presenter.go", cfg.HasHTTP()},

		// Infrastructure
		{"templates/infrastructure/config/config.go.tmpl", "internal/infrastructure/config/config.go", true},
		{"templates/infrastructure/config/config.yaml.tmpl", "config/config.yaml", cfg.ConfigFormat == "yaml"},
		{"templates/infrastructure/config/config.example.yaml.tmpl", "config/config.example.yaml", cfg.ConfigFormat == "yaml"},
		{"templates/infrastructure/database/postgres.go.tmpl", "internal/infrastructure/database/postgres.go", cfg.Database == "postgres"},
		{"templates/infrastructure/database/mysql.go.tmpl", "internal/infrastructure/database/mysql.go", cfg.Database == "mysql"},
		{"templates/infrastructure/cache/redis.go.tmpl", "internal/infrastructure/cache/redis.go", cfg.HasRedis()},
		{"templates/infrastructure/server/http.go.tmpl", "internal/infrastructure/server/http.go", cfg.HasHTTP()},
		{"templates/infrastructure/server/grpc.go.tmpl", "internal/infrastructure/server/grpc.go", cfg.HasGRPC()},
		{"templates/infrastructure/logger/logger.go.tmpl", "internal/infrastructure/logger/logger.go", true},
		{"templates/infrastructure/di/container.go.tmpl", "internal/infrastructure/di/container.go", cfg.UseManualDI()},
		{"templates/infrastructure/di/wire.go.tmpl", "internal/infrastructure/di/wire.go", cfg.UseWire()},

		// Entry points
		{"templates/cmd/api/main.go.tmpl", "cmd/api/main.go", cfg.HasHTTP()},
		{"templates/cmd/grpc/main.go.tmpl", "cmd/grpc/main.go", cfg.HasGRPC()},
		{"templates/cmd/migrate/main.go.tmpl", "cmd/migrate/main.go", cfg.HasDatabase()},

		// Public packages
		{"templates/pkg/errors/errors.go.tmpl", "pkg/errors/errors.go", true},
		{"templates/pkg/validator/validator.go.tmpl", "pkg/validator/validator.go", true},
		{"templates/pkg/httputil/response.go.tmpl", "pkg/httputil/response.go", cfg.HasHTTP()},

		// Migrations
		{"templates/migrations/000001_create_users_table.up.sql.tmpl", "migrations/000001_create_users_table.up.sql", cfg.HasSQL()},
		{"templates/migrations/000001_create_users_table.down.sql.tmpl", "migrations/000001_create_users_table.down.sql", cfg.HasSQL()},

		// Docker
		{"templates/Dockerfile.tmpl", "Dockerfile", cfg.IncludeDocker},
		{"templates/docker-compose.yaml.tmpl", "docker-compose.yaml", cfg.IncludeDocker},

		// Makefile
		{"templates/Makefile.tmpl", "Makefile", cfg.IncludeMake},

		// CI & GitHub
		{"templates/ci/github-ci.yaml.tmpl", ".github/workflows/ci.yaml", cfg.IncludeCI},
		{"templates/github/pull_request_template.md.tmpl", ".github/pull_request_template.md", cfg.IncludeCI},

		// API
		{"templates/api/openapi/openapi.yaml.tmpl", "api/openapi/openapi.yaml", cfg.HasHTTP()},
	}

	return files
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
	if err := tmpl.Execute(&buf, g.cfg); err != nil {
		return fmt.Errorf("failed to execute template %s: %w", tmplPath, err)
	}

	// Create directory
	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write file
	if err := os.WriteFile(outPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", outPath, err)
	}

	fmt.Printf("   📄 %s\n", outPath)
	return nil
}
