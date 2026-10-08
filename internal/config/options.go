package config

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Supported option values. These slices are the single source of truth for
// the `nova new` flag help, the interactive prompts and Validate; the
// generator trusts a validated config. Add a value here only together with
// its templates.
//
//nolint:gochecknoglobals // immutable option sets; treated as consts.
var (
	Transports     = []string{"http", "grpc", "worker"}
	HTTPFrameworks = []string{"fiber", "gin", "chi", "echo"}
	Databases      = []string{"postgres", "mysql", noneStr}
	Caches         = []string{"redis", noneStr}
	Searches       = []string{"elasticsearch", noneStr}
	MessageQueues  = []string{"kafka", "rabbitmq", noneStr}
	ConfigFormats  = []string{"yaml"}
	DIs            = []string{"wire", "fx"}
	CIs            = []string{"github", noneStr}
)

// dbDrivers maps each SQL engine to the one driver its templates use.
//
//nolint:gochecknoglobals // immutable; treated as a const.
var dbDrivers = map[string]string{"postgres": "pgx", "mysql": "database/sql"}

const queryGenSQLC = "sqlc"

var (
	projectNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	modulePathRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~/-]*$`)
)

// Validate rejects unsupported or inconsistent choices before anything is
// rendered and normalizes the dependent fields so templates see one
// canonical shape per database. It is the only place option values are
// checked; the CLI, the prompts and generator.New all rely on it. Every
// problem is reported, not just the first.
func (c *ProjectConfig) Validate() error {
	for _, f := range []*string{&c.Database, &c.Cache, &c.Search, &c.MessageQueue} {
		if *f == "" {
			*f = noneStr
		}
	}

	var errs []error
	if c.ProjectName == "." || c.ProjectName == ".." || !projectNameRE.MatchString(c.ProjectName) {
		errs = append(errs, fmt.Errorf(
			"project name %q: use letters, digits, '.', '_' or '-' only (no spaces, quotes or path separators)",
			c.ProjectName,
		))
	}
	if c.ModuleName != "" && !modulePathRE.MatchString(c.ModuleName) {
		errs = append(errs, fmt.Errorf("module %q is not a valid Go module path", c.ModuleName))
	}
	errs = append(errs, oneOf("transport", c.Transport, Transports))
	if c.HasHTTP() {
		errs = append(errs, oneOf("http-framework", c.HTTPFramework, HTTPFrameworks))
	}
	errs = append(errs, oneOf("database", c.Database, Databases))
	errs = append(errs, c.validateSQL()...)
	errs = append(errs, oneOf("cache", c.Cache, Caches))
	errs = append(errs, oneOf("search", c.Search, Searches))
	errs = append(errs, oneOf("queue", c.MessageQueue, MessageQueues))
	if c.HasWorker() && !c.HasMessageQueue() {
		errs = append(errs, errors.New("transport worker needs --queue kafka or rabbitmq"))
	}
	errs = append(errs, oneOf("config", c.ConfigFormat, ConfigFormats))
	errs = append(errs, oneOf("di", c.DI, DIs))
	return errors.Join(errs...)
}

// validateSQL checks the engine-dependent fields and fills their defaults.
// Without a database they are meaningless, so they are blanked; otherwise the
// Makefile, nova.yaml and CLAUDE.md templates would still mention sqlc.
func (c *ProjectConfig) validateSQL() []error {
	if !c.HasDatabase() {
		c.DBDriver = ""
		c.QueryGen = ""
		return nil
	}
	want, known := dbDrivers[c.Database]
	if !known {
		return nil // the database check already reported the engine
	}
	var errs []error
	switch c.DBDriver {
	case "", want:
		c.DBDriver = want
	default:
		errs = append(errs, fmt.Errorf(
			"db-driver %q is not supported for %s (templates use %s)", c.DBDriver, c.Database, want,
		))
	}
	switch c.QueryGen {
	case "", queryGenSQLC:
		c.QueryGen = queryGenSQLC
	default:
		errs = append(errs, fmt.Errorf("query %q is not supported (only sqlc)", c.QueryGen))
	}
	return errs
}

func oneOf(flag, got string, allowed []string) error {
	if slices.Contains(allowed, got) {
		return nil
	}
	return fmt.Errorf("%s %q is not supported (valid: %s)", flag, got, strings.Join(allowed, ", "))
}
