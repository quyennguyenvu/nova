package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/quyennguyenvu/nova/internal/config"
	"github.com/quyennguyenvu/nova/internal/generator"
	"github.com/quyennguyenvu/nova/internal/prompt"
)

func newCommand() *cobra.Command {
	var newCmd = &cobra.Command{
		Use:   "new [project-name]",
		Short: "Generate a new Go Clean Architecture project",
		Long: `
Generate a new Go project with Clean Architecture structure.
Run without arguments for interactive mode, or use flags to skip prompts.

Examples:
	nova new
	nova new myproject --module=github.com/myorg/myproject --transport=http
	nova new myproject --http-framework=fiber --database=postgres --cache=redis`,
		Args: cobra.MaximumNArgs(1),
		RunE: runNew,
	}

	f := newCmd.Flags()
	f.String("module", "", "Go module path (e.g. github.com/myorg/myproject)")
	f.String("transport", "", "Transport layer: "+strings.Join(config.Transports, ", "))
	f.String("http-framework", "", "HTTP framework: "+strings.Join(config.HTTPFrameworks, ", "))
	f.String("database", "", "Database: "+strings.Join(config.Databases, ", "))
	f.String("db-driver", "", "Database driver (pgx for postgres, database/sql for mysql; defaults per engine)")
	f.String("query", "", "Query generation: sqlc (default for SQL engines)")
	f.String("cache", "", "Cache: "+strings.Join(config.Caches, ", "))
	f.String("search", "", "Search engine: "+strings.Join(config.Searches, ", "))
	f.String("queue", "", "Message queue: "+strings.Join(config.MessageQueues, ", "))
	f.String("config", "", "Configuration format: "+strings.Join(config.ConfigFormats, ", "))
	f.String("di", "", "Dependency injection: "+strings.Join(config.DIs, ", "))
	f.Bool("docker", true, "Include Docker setup (--docker=false to skip)")
	f.String("ci", "github", "CI/CD: "+strings.Join(config.CIs, ", "))
	f.Bool("force", false, "Generate into a non-empty directory, overwriting files that already exist")

	return newCmd
}

func runNew(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()

	if len(args) > 0 {
		cfg.ProjectName = args[0]
	}

	// Check if any flag was explicitly set
	flagsSet := false
	cmd.Flags().Visit(func(_ *pflag.Flag) {
		flagsSet = true
	})

	if flagsSet {
		if err := applyFlags(cmd, cfg); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(os.Stdout, "🚀 Nova — Go Clean Architecture Project Generator")
		fmt.Fprintln(os.Stdout)
		if err := prompt.RunInteractive(cfg); err != nil {
			return fmt.Errorf("prompt error: %w", err)
		}
	}

	// Set module name from project name if not explicitly set
	if cfg.ModuleName == "" {
		cfg.ModuleName = fmt.Sprintf("github.com/myorg/%s", cfg.ProjectName)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}

	fmt.Fprintf(os.Stdout, "\n📦 Generating project: %s\n", cfg.ProjectName)
	fmt.Fprintf(os.Stdout, "   Module: %s\n", cfg.ModuleName)
	fmt.Fprintf(os.Stdout, "   Transport: %s\n", cfg.Transport)
	if cfg.HasHTTP() {
		fmt.Fprintf(os.Stdout, "   HTTP Framework: %s\n", cfg.HTTPFramework)
	}
	if cfg.HasDatabase() {
		fmt.Fprintf(os.Stdout, "   Database: %s (%s)\n", cfg.Database, cfg.DBDriver)
	}
	if cfg.HasCache() {
		fmt.Fprintf(os.Stdout, "   Cache: %s\n", cfg.Cache)
	}
	if cfg.HasSearch() {
		fmt.Fprintf(os.Stdout, "   Search: %s\n", cfg.Search)
	}
	fmt.Fprintln(os.Stdout)

	gen, err := generator.New(cfg)
	if err != nil {
		return fmt.Errorf("generator init: %w", err)
	}
	outputDir := cfg.ProjectName
	force, _ := cmd.Flags().GetBool("force")
	if dirErr := checkTargetDir(outputDir, force); dirErr != nil {
		return dirErr
	}

	if genErr := gen.Generate(outputDir); genErr != nil {
		return fmt.Errorf("generation failed: %w", genErr)
	}

	fmt.Fprintf(os.Stdout, "✅ Project generated successfully in ./%s\n\n", cfg.ProjectName)
	fmt.Fprintln(os.Stdout, "Next steps:")
	for _, step := range nextSteps(cfg) {
		fmt.Fprintf(os.Stdout, "  %s\n", step)
	}
	fmt.Fprintln(os.Stdout)

	return nil
}

// nextSteps is the post-generation checklist in the order that actually
// works: code generators first (sqlc must emit dbgen before `go mod tidy`
// can resolve its import), then the transport's own subcommand.
func nextSteps(cfg *config.ProjectConfig) []string {
	steps := []string{
		"cd " + cfg.ProjectName,
		"cp .env.example .env   # fill in secrets (JWT keys, DB password)",
		"make gen               # code generators, then dependency resolution",
	}
	switch {
	case cfg.HasWorker():
		steps = append(steps, "go run main.go worker")
	case cfg.HasGRPC():
		steps = append(steps, "go run main.go grpc")
	default:
		steps = append(steps, "go run main.go api")
	}
	if cfg.IncludeDocker {
		steps = append(steps, "# or: docker compose up")
	}
	return steps
}

func applyFlags(cmd *cobra.Command, cfg *config.ProjectConfig) error {
	flags := cmd.Flags()
	for flag, field := range map[string]*string{
		"module":         &cfg.ModuleName,
		"transport":      &cfg.Transport,
		"http-framework": &cfg.HTTPFramework,
		"database":       &cfg.Database,
		"db-driver":      &cfg.DBDriver,
		"query":          &cfg.QueryGen,
		"cache":          &cfg.Cache,
		"search":         &cfg.Search,
		"queue":          &cfg.MessageQueue,
		"config":         &cfg.ConfigFormat,
		"di":             &cfg.DI,
	} {
		if v, _ := flags.GetString(flag); v != "" {
			*field = v
		}
	}
	// Booleans must honour an explicit false, so test Changed rather than value.
	if flags.Changed("docker") {
		cfg.IncludeDocker, _ = flags.GetBool("docker")
	}
	if flags.Changed("ci") {
		switch v, _ := flags.GetString("ci"); v {
		case "github":
			cfg.IncludeCI = true
		case "none":
			cfg.IncludeCI = false
		default:
			return fmt.Errorf("ci %q is not supported (valid: %s)", v, strings.Join(config.CIs, ", "))
		}
	}
	return nil
}

// checkTargetDir refuses to render into a directory that already has content
// unless --force was given: Generate overwrites file by file and would
// silently clobber edits.
func checkTargetDir(dir string, force bool) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("inspect %s: %w", dir, err)
	case len(entries) == 0 || force:
		return nil
	default:
		return fmt.Errorf("%s already exists and is not empty; re-run with --force to overwrite", dir)
	}
}
