package prompt

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"

	"nova/internal/config"
)

// RunInteractive prompts the user for all project configuration options.
func RunInteractive(cfg *config.ProjectConfig) error {
	// Project basics
	if err := survey.AskOne(&survey.Input{
		Message: "Project name:",
		Default: cfg.ProjectName,
	}, &cfg.ProjectName, survey.WithValidator(survey.Required)); err != nil {
		return err
	}

	if err := survey.AskOne(&survey.Input{
		Message: "Go module name:",
		Default: fmt.Sprintf("github.com/myorg/%s", cfg.ProjectName),
	}, &cfg.ModuleName, survey.WithValidator(survey.Required)); err != nil {
		return err
	}

	// Transport
	if err := survey.AskOne(&survey.Select{
		Message: "Transport layer:",
		Options: []string{"http", "grpc", "both"},
		Default: "http",
	}, &cfg.Transport); err != nil {
		return err
	}

	// HTTP Framework
	if cfg.HasHTTP() {
		if err := survey.AskOne(&survey.Select{
			Message: "HTTP framework:",
			Options: []string{"fiber", "gin", "chi", "echo", "nethttp"},
			Default: "fiber",
		}, &cfg.HTTPFramework); err != nil {
			return err
		}
	}

	// gRPC Gateway
	if cfg.HasGRPC() {
		if err := survey.AskOne(&survey.Confirm{
			Message: "Include gRPC-Gateway?",
			Default: false,
		}, &cfg.GRPCGateway); err != nil {
			return err
		}
	}

	// Database
	if err := survey.AskOne(&survey.Select{
		Message: "Database:",
		Options: []string{"postgres", "mysql", "sqlite", "mongodb", "none"},
		Default: "postgres",
	}, &cfg.Database); err != nil {
		return err
	}

	// DB Driver (only for SQL databases)
	if cfg.HasSQL() {
		if err := survey.AskOne(&survey.Select{
			Message: "Database driver:",
			Options: []string{"pgx", "sqlx", "gorm", "database/sql"},
			Default: "pgx",
		}, &cfg.DBDriver); err != nil {
			return err
		}

		if err := survey.AskOne(&survey.Select{
			Message: "Query generation:",
			Options: []string{"sqlc", "raw", "gorm"},
			Default: "sqlc",
		}, &cfg.QueryGen); err != nil {
			return err
		}
	}

	// Cache
	if err := survey.AskOne(&survey.Select{
		Message: "Cache:",
		Options: []string{"redis", "bigcache", "none"},
		Default: "redis",
	}, &cfg.Cache); err != nil {
		return err
	}

	// Message Queue
	if err := survey.AskOne(&survey.Select{
		Message: "Message queue:",
		Options: []string{"kafka", "rabbitmq", "nats", "none"},
		Default: "none",
	}, &cfg.MessageQueue); err != nil {
		return err
	}

	// Config format
	if err := survey.AskOne(&survey.Select{
		Message: "Configuration format:",
		Options: []string{"yaml", "toml", "env"},
		Default: "yaml",
	}, &cfg.ConfigFormat); err != nil {
		return err
	}

	// DI
	if err := survey.AskOne(&survey.Select{
		Message: "Dependency injection:",
		Options: []string{"wire", "fx", "manual"},
		Default: "manual",
	}, &cfg.DI); err != nil {
		return err
	}

	// Optional features
	if err := survey.AskOne(&survey.Confirm{
		Message: "Include Docker setup?",
		Default: true,
	}, &cfg.IncludeDocker); err != nil {
		return err
	}

	if err := survey.AskOne(&survey.Confirm{
		Message: "Include CI/CD (GitHub Actions)?",
		Default: true,
	}, &cfg.IncludeCI); err != nil {
		return err
	}

	if err := survey.AskOne(&survey.Confirm{
		Message: "Include Makefile?",
		Default: true,
	}, &cfg.IncludeMake); err != nil {
		return err
	}

	return nil
}
