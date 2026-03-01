package config

// ProjectConfig holds all user choices for project generation.
type ProjectConfig struct {
	// Project basics
	ProjectName string `json:"project_name"`
	ModuleName  string `json:"module_name"`

	// Transport
	Transport     string `json:"transport"`      // "http", "grpc", "both"
	HTTPFramework string `json:"http_framework"`  // "fiber", "gin", "chi", "echo", "nethttp"
	GRPCGateway   bool   `json:"grpc_gateway"`

	// Database
	Database   string `json:"database"`    // "postgres", "mysql", "sqlite", "mongodb", "none"
	DBDriver   string `json:"db_driver"`   // "pgx", "sqlx", "gorm", "database/sql"
	QueryGen   string `json:"query_gen"`   // "sqlc", "raw", "gorm"

	// Cache
	Cache string `json:"cache"` // "redis", "bigcache", "none"

	// Message Queue
	MessageQueue string `json:"message_queue"` // "kafka", "rabbitmq", "nats", "none"

	// Configuration format
	ConfigFormat string `json:"config_format"` // "yaml", "toml", "env"

	// Dependency Injection
	DI string `json:"di"` // "wire", "fx", "manual"

	// Optional features
	IncludeDocker bool `json:"include_docker"`
	IncludeCI     bool `json:"include_ci"`
	IncludeMake   bool `json:"include_makefile"`
}

// HasHTTP returns true if the project includes HTTP transport.
func (c *ProjectConfig) HasHTTP() bool {
	return c.Transport == "http" || c.Transport == "both"
}

// HasGRPC returns true if the project includes gRPC transport.
func (c *ProjectConfig) HasGRPC() bool {
	return c.Transport == "grpc" || c.Transport == "both"
}

// HasDatabase returns true if a database is selected.
func (c *ProjectConfig) HasDatabase() bool {
	return c.Database != "none" && c.Database != ""
}

// HasSQL returns true if a SQL database is selected.
func (c *ProjectConfig) HasSQL() bool {
	return c.Database == "postgres" || c.Database == "mysql" || c.Database == "sqlite"
}

// HasCache returns true if a cache is selected.
func (c *ProjectConfig) HasCache() bool {
	return c.Cache != "none" && c.Cache != ""
}

// HasMessageQueue returns true if a message queue is selected.
func (c *ProjectConfig) HasMessageQueue() bool {
	return c.MessageQueue != "none" && c.MessageQueue != ""
}

// HasRedis returns true if Redis cache is selected.
func (c *ProjectConfig) HasRedis() bool {
	return c.Cache == "redis"
}

// UseWire returns true if Google Wire DI is selected.
func (c *ProjectConfig) UseWire() bool {
	return c.DI == "wire"
}

// UseFx returns true if Uber fx DI is selected.
func (c *ProjectConfig) UseFx() bool {
	return c.DI == "fx"
}

// UseManualDI returns true if manual DI is selected.
func (c *ProjectConfig) UseManualDI() bool {
	return c.DI == "manual"
}

// DefaultConfig returns a sensible default configuration (Quick Start).
func DefaultConfig() *ProjectConfig {
	return &ProjectConfig{
		ProjectName:   "myproject",
		ModuleName:    "github.com/myorg/myproject",
		Transport:     "http",
		HTTPFramework: "fiber",
		Database:      "postgres",
		DBDriver:      "pgx",
		QueryGen:      "sqlc",
		Cache:         "redis",
		MessageQueue:  "none",
		ConfigFormat:  "yaml",
		DI:            "manual",
		IncludeDocker: true,
		IncludeCI:     true,
		IncludeMake:   true,
	}
}
