package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"nova/internal/config"
	"nova/internal/generator"
	"nova/internal/prompt"
)

var newCmd = &cobra.Command{
	Use:   "new [project-name]",
	Short: "Generate a new Go Clean Architecture project",
	Long: `Generate a new Go project with Clean Architecture structure.

Run without arguments for interactive mode, or use flags to skip prompts.

Examples:
  nova new
  nova new myproject --module=github.com/myorg/myproject --transport=http
  nova new myproject --http-framework=fiber --database=postgres --db-driver=pgx`,
	Args: cobra.MaximumNArgs(1),
	RunE: runNew,
}

func init() {
	rootCmd.AddCommand(newCmd)

	f := newCmd.Flags()
	f.String("module", "", "Go module name (e.g. github.com/myorg/myproject)")
	f.String("transport", "", "Transport layer: http, grpc, both")
	f.String("http-framework", "", "HTTP framework: fiber, gin, chi, echo, nethttp")
	f.Bool("grpc-gateway", false, "Include gRPC-Gateway")
	f.String("database", "", "Database: postgres, mysql, sqlite, mongodb, none")
	f.String("db-driver", "", "Database driver: pgx, sqlx, gorm, database/sql")
	f.String("query", "", "Query generation: sqlc, raw, gorm")
	f.String("cache", "", "Cache: redis, bigcache, none")
	f.String("queue", "", "Message queue: kafka, rabbitmq, nats, none")
	f.String("config", "", "Configuration format: yaml, toml, env")
	f.String("di", "", "Dependency injection: wire, fx, manual")
	f.Bool("docker", false, "Include Docker setup")
	f.Bool("makefile", false, "Include Makefile")
	f.String("ci", "", "CI/CD: github, none")
}

func runNew(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()

	if len(args) > 0 {
		cfg.ProjectName = args[0]
	}

	// Check if any flag was explicitly set
	flagsSet := false
	cmd.Flags().Visit(func(f *pflag.Flag) {
		flagsSet = true
	})

	if flagsSet {
		applyFlags(cmd, cfg)
	} else {
		fmt.Println("🚀 Nova — Go Clean Architecture Project Generator")
		fmt.Println()
		if err := prompt.RunInteractive(cfg); err != nil {
			return fmt.Errorf("prompt error: %w", err)
		}
	}

	// Set module name from project name if not explicitly set
	if cfg.ModuleName == "" {
		cfg.ModuleName = fmt.Sprintf("github.com/myorg/%s", cfg.ProjectName)
	}

	fmt.Printf("\n📦 Generating project: %s\n", cfg.ProjectName)
	fmt.Printf("   Module: %s\n", cfg.ModuleName)
	fmt.Printf("   Transport: %s\n", cfg.Transport)
	if cfg.HasHTTP() {
		fmt.Printf("   HTTP Framework: %s\n", cfg.HTTPFramework)
	}
	if cfg.HasDatabase() {
		fmt.Printf("   Database: %s (%s)\n", cfg.Database, cfg.DBDriver)
	}
	if cfg.HasCache() {
		fmt.Printf("   Cache: %s\n", cfg.Cache)
	}
	fmt.Println()

	gen := generator.New(cfg)
	outputDir := cfg.ProjectName

	if err := gen.Generate(outputDir); err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	fmt.Printf("✅ Project generated successfully in ./%s\n\n", cfg.ProjectName)
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", cfg.ProjectName)
	fmt.Println("  go mod tidy")
	if cfg.IncludeMake {
		fmt.Println("  make run")
	} else {
		fmt.Println("  go run ./cmd/api")
	}
	if cfg.IncludeDocker {
		fmt.Println("  # or: docker-compose up")
	}
	fmt.Println()

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
	if v, _ := cmd.Flags().GetBool("makefile"); v {
		cfg.IncludeMake = v
	}
	if v, _ := cmd.Flags().GetString("ci"); v == "github" {
		cfg.IncludeCI = true
	} else if v == "none" {
		cfg.IncludeCI = false
	}
}
