package prompt

import (
	"fmt"
	"slices"

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
	if err := promptDatabase(cfg); err != nil {
		return err
	}
	if err := promptCache(cfg); err != nil {
		return err
	}
	if err := promptSearch(cfg); err != nil {
		return err
	}
	if err := promptMessageQueue(cfg); err != nil {
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
		Options: config.Transports,
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
		Options: config.HTTPFrameworks,
		Default: "fiber",
	}, &cfg.HTTPFramework); err != nil {
		return err
	}
	return nil
}

func promptDatabase(cfg *config.ProjectConfig) error {
	if err := survey.AskOne(&survey.Select{
		Message: "Database:",
		Options: config.Databases,
		Default: "postgres",
	}, &cfg.Database); err != nil {
		return err
	}
	return nil
}

func promptCache(cfg *config.ProjectConfig) error {
	if err := survey.AskOne(&survey.Select{
		Message: "Cache:",
		Options: config.Caches,
		Default: "redis",
	}, &cfg.Cache); err != nil {
		return err
	}
	return nil
}

func promptSearch(cfg *config.ProjectConfig) error {
	if err := survey.AskOne(&survey.Select{
		Message: "Search engine:",
		Options: config.Searches,
		Default: "none",
	}, &cfg.Search); err != nil {
		return err
	}
	return nil
}

func promptMessageQueue(cfg *config.ProjectConfig) error {
	opts := slices.Clone(config.MessageQueues)
	defaultMQ := "none"
	if cfg.HasWorker() {
		// A worker is a consumer; "none" would fail Validate anyway.
		opts = slices.DeleteFunc(opts, func(s string) bool { return s == "none" })
		defaultMQ = "kafka"
	}
	if err := survey.AskOne(&survey.Select{
		Message: "Message queue:",
		Options: opts,
		Default: defaultMQ,
	}, &cfg.MessageQueue); err != nil {
		return err
	}
	return nil
}

func promptDI(cfg *config.ProjectConfig) error {
	if err := survey.AskOne(&survey.Select{
		Message: "Dependency injection:",
		Options: config.DIs,
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

// SupportedComponents is the menu `nova add` offers — exactly the component
// generators implemented today. Keep in lockstep with the dispatch in runAdd
// and the Generate* methods in internal/generator/component.go.
//
//nolint:gochecknoglobals // immutable menu; treated as a const.
var SupportedComponents = []string{"worker", "entity", "usecase", "repository", "handler", "all"}

// Component holds the answers `nova add` collects — interactively or from args.
type Component struct {
	Type string
	Name string
	DB   string // repository/all only — postgres or mysql
}

// RunComponentInteractive prompts for the component type, its name, and (for
// repository/all) the database engine. defaultDB seeds the engine select from
// the project's manifest stack.
func RunComponentInteractive(c *Component, defaultDB string) error {
	if err := survey.AskOne(&survey.Select{
		Message: "Component:",
		Options: SupportedComponents,
		Default: SupportedComponents[0],
	}, &c.Type); err != nil {
		return err
	}
	if err := survey.AskOne(&survey.Input{
		Message: "Name:",
	}, &c.Name, survey.WithValidator(survey.Required)); err != nil {
		return err
	}
	if c.Type == "repository" || c.Type == "all" {
		return promptRepoDB(c, defaultDB)
	}
	return nil
}

func promptRepoDB(c *Component, defaultDB string) error {
	if defaultDB != "postgres" && defaultDB != "mysql" {
		defaultDB = "postgres"
	}
	return survey.AskOne(&survey.Select{
		Message: "Repository type:",
		Options: []string{"postgres", "mysql"},
		Default: defaultDB,
	}, &c.DB)
}
