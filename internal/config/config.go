package config

import "runtime"

const (
	noneStr = "none"
)

// ProjectConfig holds all user choices for project generation.
type ProjectConfig struct {
	GoVersion string `json:"go_version"` // e.g. "1.20"
	// Project basics
	ProjectName string `json:"project_name"`
	ModuleName  string `json:"module_name"`

	// Allowed values for every option are the slices in options.go.
	Transport     string `json:"transport"`
	HTTPFramework string `json:"http_framework"` // HTTP only; Validate defaults it to fiber

	// Database
	Database string `json:"database"`
	DBDriver string `json:"db_driver"` // one per engine; Validate fills it
	QueryGen string `json:"query_gen"`

	Cache        string `json:"cache"`
	Search       string `json:"search"`
	MessageQueue string `json:"message_queue"`
	ConfigFormat string `json:"config_format"`

	// Dependency Injection
	DI string `json:"di"` // "wire", "fx"

	// Optional features
	IncludeDocker bool `json:"include_docker"`
	IncludeCI     bool `json:"include_ci"`
}

// HasHTTP returns true if the project includes HTTP transport.
func (c *ProjectConfig) HasHTTP() bool {
	return c.Transport == "http"
}

// HasGRPC returns true if the project includes gRPC transport.
func (c *ProjectConfig) HasGRPC() bool {
	return c.Transport == "grpc"
}

// HasCron returns true if the project includes cron transport.
func (c *ProjectConfig) HasCron() bool {
	return c.Transport == "cron"
}

// HasCLI returns true if the project includes CLI transport.
func (c *ProjectConfig) HasCLI() bool {
	return c.Transport == "cli"
}

// HasWorker returns true if the project is a consumer worker — a long-running
// process that subscribes to a message broker and dispatches messages to
// usecases. Mutually exclusive with HasHTTP/HasGRPC/HasCron/HasCLI.
func (c *ProjectConfig) HasWorker() bool {
	return c.Transport == "worker"
}

// HasDatabase returns true if a database is selected.
func (c *ProjectConfig) HasDatabase() bool {
	return c.Database != noneStr && c.Database != ""
}

// HasSQL returns true if a SQL database is selected.
func (c *ProjectConfig) HasSQL() bool {
	return c.Database == "postgres" || c.Database == "mysql" || c.Database == "sqlite"
}

// HasCache returns true if a cache is selected.
func (c *ProjectConfig) HasCache() bool {
	return c.Cache != noneStr && c.Cache != ""
}

// HasMessageQueue returns true if a message queue is selected.
func (c *ProjectConfig) HasMessageQueue() bool {
	return c.MessageQueue != noneStr && c.MessageQueue != ""
}

// HasRedis returns true if Redis cache is selected.
func (c *ProjectConfig) HasRedis() bool {
	return c.Cache == "redis"
}

// HasSearch returns true if a search backend is selected.
func (c *ProjectConfig) HasSearch() bool {
	return c.Search != noneStr && c.Search != ""
}

// HasElasticsearch returns true if Elasticsearch is selected.
func (c *ProjectConfig) HasElasticsearch() bool {
	return c.Search == "elasticsearch"
}

// HasDependencies reports whether the project dials any external service
// (database, cache, search or broker) that compose has to run beside the app.
func (c *ProjectConfig) HasDependencies() bool {
	return c.HasDatabase() || c.HasRedis() || c.HasElasticsearch() || c.HasMessageQueue()
}

// UseWire returns true if Google Wire DI is selected.
func (c *ProjectConfig) UseWire() bool {
	return c.DI == "wire"
}

// UseFx returns true if Uber fx DI is selected.
func (c *ProjectConfig) UseFx() bool {
	return c.DI == "fx"
}

// DefaultConfig returns the values a flag-driven `nova new` uses for every
// option it leaves unset. The
// module path, driver and HTTP framework stay empty: the CLI derives the
// module from the project name, and Validate picks the engine's driver and
// fiber for HTTP, so a flag that overrides one never fights a pre-filled value.
func DefaultConfig() *ProjectConfig {
	v := runtime.Version()
	return &ProjectConfig{
		ProjectName:   "myproject",
		GoVersion:     v[2:], // Strip "go" prefix from version string
		Database:      "postgres",
		QueryGen:      "sqlc",
		Cache:         "redis",
		Search:        noneStr,
		MessageQueue:  noneStr,
		ConfigFormat:  "yaml",
		DI:            "wire",
		IncludeDocker: true,
		IncludeCI:     true,
	}
}
