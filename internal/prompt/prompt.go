package prompt

import (
	"fmt"

	"github.com/AlecAivazis/survey/v2"

	"github.com/quyennguyenvu/nova/internal/config"
)

// RunInteractive prompts the user for all project configuration options.
func RunInteractive(cfg *config.ProjectConfig) error {
	if err := promptProjectBasics(cfg); err != nil {
		return err
	}
	if err := promptTransport(cfg); err != nil {
		return err
	}
	if err := promptHTTPFramework(cfg); err != nil {
		return err
	}
	if err := promptGRPCGateway(cfg); err != nil {
		return err
	}
	if err := promptDatabase(cfg); err != nil {
		return err
	}
	if err := promptSQL(cfg); err != nil {
		return err
	}
	if err := promptCache(cfg); err != nil {
		return err
	}
	if err := promptMessageQueue(cfg); err != nil {
		return err
	}
	if err := promptConfigFormat(cfg); err != nil {
		return err
	}
	if err := promptDI(cfg); err != nil {
		return err
	}
	if err := promptOptionalFeatures(cfg); err != nil {
		return err
	}
	return nil
}

func promptProjectBasics(cfg *config.ProjectConfig) error {
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
	return nil
}

func promptTransport(cfg *config.ProjectConfig) error {
	if err := survey.AskOne(&survey.Select{
		Message: "Transport layer:",
		Options: []string{"http", "grpc", "worker", "cron", "cli"},
		Default: "http",
	}, &cfg.Transport); err != nil {
		return err
	}
	return nil
}

func promptHTTPFramework(cfg *config.ProjectConfig) error {
	if !cfg.HasHTTP() {
		return nil
	}

	if err := survey.AskOne(&survey.Select{
		Message: "HTTP framework:",
		Options: []string{"fiber", "gin", "chi", "echo", "nethttp"},
		Default: "fiber",
	}, &cfg.HTTPFramework); err != nil {
		return err
	}
	return nil
}

func promptGRPCGateway(cfg *config.ProjectConfig) error {
	if !cfg.HasGRPC() {
		return nil
	}

	if err := survey.AskOne(&survey.Confirm{
		Message: "Include gRPC-Gateway?",
		Default: false,
	}, &cfg.GRPCGateway); err != nil {
		return err
	}
	return nil
}

func promptDatabase(cfg *config.ProjectConfig) error {
	if err := survey.AskOne(&survey.Select{
		Message: "Database:",
		Options: []string{"postgres", "mysql", "sqlite", "mongodb", "none"},
		Default: "postgres",
	}, &cfg.Database); err != nil {
		return err
	}
	return nil
}

func promptSQL(cfg *config.ProjectConfig) error {
	if !cfg.HasSQL() {
		return nil
	}

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
	return nil
}

func promptCache(cfg *config.ProjectConfig) error {
	if err := survey.AskOne(&survey.Select{
		Message: "Cache:",
		Options: []string{"redis", "bigcache", "none"},
		Default: "redis",
	}, &cfg.Cache); err != nil {
		return err
	}
	return nil
}

func promptMessageQueue(cfg *config.ProjectConfig) error {
	defaultMQ := "none"
	if cfg.HasWorker() {
		defaultMQ = "kafka"
	}
	if err := survey.AskOne(&survey.Select{
		Message: "Message queue:",
		Options: []string{"kafka", "rabbitmq", "nats", "none"},
		Default: defaultMQ,
	}, &cfg.MessageQueue); err != nil {
		return err
	}
	return nil
}

func promptConfigFormat(cfg *config.ProjectConfig) error {
	if err := survey.AskOne(&survey.Select{
		Message: "Configuration format:",
		Options: []string{"yaml", "toml"},
		Default: "yaml",
	}, &cfg.ConfigFormat); err != nil {
		return err
	}
	return nil
}

func promptDI(cfg *config.ProjectConfig) error {
	if err := survey.AskOne(&survey.Select{
		Message: "Dependency injection:",
		Options: []string{"wire", "fx"},
		Default: "wire",
	}, &cfg.DI); err != nil {
		return err
	}
	return nil
}

func promptOptionalFeatures(cfg *config.ProjectConfig) error {
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
	return nil
}
