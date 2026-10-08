package config

import (
	"strings"
	"testing"
)

func validHTTP() *ProjectConfig {
	cfg := DefaultConfig()
	cfg.Transport = "http"
	cfg.HTTPFramework = "fiber"
	return cfg
}

// TestValidateRejectsUnimplemented pins the one gate every advertised option
// goes through: anything outside the registry fails before a file is written.
func TestValidateRejectsUnimplemented(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*ProjectConfig){
		"transport cron":       func(c *ProjectConfig) { c.Transport = "cron" },
		"transport cli":        func(c *ProjectConfig) { c.Transport = "cli" },
		"transport typo":       func(c *ProjectConfig) { c.Transport = "htpp" },
		"transport empty":      func(c *ProjectConfig) { c.Transport = "" },
		"framework nethttp":    func(c *ProjectConfig) { c.HTTPFramework = "nethttp" },
		"database sqlite":      func(c *ProjectConfig) { c.Database = "sqlite" },
		"driver sqlx":          func(c *ProjectConfig) { c.DBDriver = "sqlx" },
		"driver mismatch":      func(c *ProjectConfig) { c.Database = "mysql"; c.DBDriver = "pgx" },
		"query raw":            func(c *ProjectConfig) { c.QueryGen = "raw" },
		"cache bigcache":       func(c *ProjectConfig) { c.Cache = "bigcache" },
		"queue nats":           func(c *ProjectConfig) { c.MessageQueue = "nats" },
		"worker without queue": func(c *ProjectConfig) { c.Transport = "worker"; c.MessageQueue = "none" },
		"config toml":          func(c *ProjectConfig) { c.ConfigFormat = "toml" },
		"di manual":            func(c *ProjectConfig) { c.DI = "manual" },
		"name with space":      func(c *ProjectConfig) { c.ProjectName = "bad name" },
		"name traversal":       func(c *ProjectConfig) { c.ProjectName = "../escaped" },
		"name quote":           func(c *ProjectConfig) { c.ProjectName = `bad"x` },
		"module with space":    func(c *ProjectConfig) { c.ModuleName = "foo bar" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := validHTTP()
			mutate(cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate accepted %s", name)
			}
		})
	}
}

// TestValidateNormalizes pins the canonical shape templates rely on.
func TestValidateNormalizes(t *testing.T) {
	t.Parallel()
	cfg := validHTTP()
	cfg.Database = "none"
	cfg.Cache = ""
	cfg.Search = ""
	cfg.MessageQueue = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.DBDriver != "" || cfg.QueryGen != "" {
		t.Errorf("no database must blank driver/query, got %q/%q", cfg.DBDriver, cfg.QueryGen)
	}
	for field, got := range map[string]string{"cache": cfg.Cache, "search": cfg.Search, "queue": cfg.MessageQueue} {
		if got != "none" {
			t.Errorf("%s = %q, want none", field, got)
		}
	}

	mysql := validHTTP()
	mysql.Database = "mysql"
	mysql.DBDriver = ""
	mysql.QueryGen = ""
	if err := mysql.Validate(); err != nil {
		t.Fatalf("Validate mysql: %v", err)
	}
	if mysql.DBDriver != "database/sql" || mysql.QueryGen != "sqlc" {
		t.Errorf("mysql defaults = %q/%q, want database/sql/sqlc", mysql.DBDriver, mysql.QueryGen)
	}

	worker := DefaultConfig()
	worker.Transport = "worker"
	worker.MessageQueue = "kafka"
	if err := worker.Validate(); err != nil {
		t.Fatalf("Validate worker: %v", err)
	}
}

func TestValidateErrorNamesEveryProblem(t *testing.T) {
	t.Parallel()
	cfg := validHTTP()
	cfg.Transport = "cron"
	cfg.Cache = "bigcache"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{`transport "cron"`, `cache "bigcache"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}
