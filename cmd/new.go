package cmd

import (
	"fmt"
	"os"

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
	nova new myproject --http-framework=fiber --database=postgres --db-driver=pgx`,
		Args: cobra.MaximumNArgs(1),
		RunE: runNew,
	}

	f := newCmd.Flags()
	f.String("module", "", "Go module name (e.g. github.com/myorg/myproject)")
	f.String("type", "", "Project type: api, worker, cron, cli")
	f.String("transport", "", "Transport layer: http, grpc, cron, cli, worker")
	f.String("http-framework", "", "HTTP framework: fiber, gin, chi, echo, nethttp")
	f.Bool("grpc-gateway", false, "Include gRPC-Gateway")
	f.String("database", "", "Database: postgres, mysql, sqlite, mongodb, none")
	f.String("db-driver", "", "Database driver: pgx, sqlx, gorm, database/sql")
	f.String("query", "", "Query generation: sqlc, raw, gorm")
	f.String("cache", "", "Cache: redis, bigcache, none")
	f.String("search", "", "Search engine: elasticsearch, none")
	f.String("queue", "", "Message queue: kafka, rabbitmq, nats, none")
	f.String("config", "", "Configuration format: yaml, toml, env")
	f.String("di", "", "Dependency injection: wire, fx")
	f.Bool("docker", false, "Include Docker setup")
	f.String("ci", "", "CI/CD: github, none")

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
		applyFlags(cmd, cfg)
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

	if genErr := gen.Generate(outputDir); genErr != nil {
		return fmt.Errorf("generation failed: %w", genErr)
	}

	fmt.Fprintf(os.Stdout, "✅ Project generated successfully in ./%s\n\n", cfg.ProjectName)
	fmt.Fprintln(os.Stdout, "Next steps:")
	fmt.Fprintf(os.Stdout, "  cd %s\n", cfg.ProjectName)
	fmt.Fprintln(os.Stdout, "  cp env.example .env  # if using env config")
	fmt.Fprintln(os.Stdout, "  go mod tidy")
	fmt.Fprintln(os.Stdout, "  make gen")
	fmt.Fprintln(os.Stdout, "  go run main.go api")
	if cfg.IncludeDocker {
		fmt.Fprintln(os.Stdout, "  # or: docker-compose up")
	}
	fmt.Fprintln(os.Stdout)

	return nil
}

func applyFlags(cmd *cobra.Command, cfg *config.ProjectConfig) {
	if v, _ := cmd.Flags().GetString("module"); v != "" {
		cfg.ModuleName = v
	}
	if v, _ := cmd.Flags().GetString("transport"); v != "" {
		cfg.Transport = v
	}
	if v, _ := cmd.Flags().GetString("http-framework"); v != "" {
		cfg.HTTPFramework = v
	}
	if v, _ := cmd.Flags().GetBool("grpc-gateway"); v {
		cfg.GRPCGateway = v
	}
	if v, _ := cmd.Flags().GetString("database"); v != "" {
		cfg.Database = v
	}
	if v, _ := cmd.Flags().GetString("db-driver"); v != "" {
		cfg.DBDriver = v
	}
	if v, _ := cmd.Flags().GetString("query"); v != "" {
		cfg.QueryGen = v
	}
	if v, _ := cmd.Flags().GetString("cache"); v != "" {
		cfg.Cache = v
	}
	if v, _ := cmd.Flags().GetString("search"); v != "" {
		cfg.Search = v
	}
	if v, _ := cmd.Flags().GetString("queue"); v != "" {
		cfg.MessageQueue = v
	}
	if v, _ := cmd.Flags().GetString("config"); v != "" {
		cfg.ConfigFormat = v
	}
	if v, _ := cmd.Flags().GetString("di"); v != "" {
		cfg.DI = v
	}
	if v, _ := cmd.Flags().GetBool("docker"); v {
		cfg.IncludeDocker = v
	}
	if v, _ := cmd.Flags().GetString("ci"); v == "github" {
		cfg.IncludeCI = true
	} else if v == "none" {
		cfg.IncludeCI = false
	}
}
