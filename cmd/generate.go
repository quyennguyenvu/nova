package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"nova/internal/generator"
)

var generateCmd = &cobra.Command{
	Use:   "generate <type> <name>",
	Short: "Generate a component in an existing project",
	Long: `Generate individual components in an existing Clean Architecture project.

Supported types:
  entity      Generate a domain entity
  usecase     Generate a use case (service + DTO + errors)
  handler     Generate an HTTP handler
  repository  Generate a repository implementation

Examples:
  nova generate entity Order
  nova generate usecase order
  nova generate handler order
  nova generate repository order --type=postgres`,
	Args: cobra.ExactArgs(2),
	RunE: runGenerate,
}

func init() {
	rootCmd.AddCommand(generateCmd)
	generateCmd.Flags().String("type", "postgres", "Repository type: postgres, mysql, mongodb")
}

func runGenerate(cmd *cobra.Command, args []string) error {
	componentType := strings.ToLower(args[0])
	name := args[1]
	repoType, _ := cmd.Flags().GetString("type")

	gen := generator.NewComponentGenerator(".")

	switch componentType {
	case "entity":
		return gen.GenerateEntity(name)
	case "usecase":
		return gen.GenerateUseCase(name)
	case "handler":
		return gen.GenerateHandler(name)
	case "repository":
		return gen.GenerateRepository(name, repoType)
	default:
		return fmt.Errorf("unknown component type: %s (supported: entity, usecase, handler, repository)", componentType)
	}
}
