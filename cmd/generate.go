package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"nova/internal/generator"
)

const (
	generateExactArgs = 2
)

func generateCommand() *cobra.Command {
	var generateCmd = &cobra.Command{
		Use:   "generate <type> <name>",
		Short: "Generate a component in an existing project",
		Long: `
Generate individual components in an existing Clean Architecture project.

Supported types:
	entity      Generate a domain entity
	usecase     Generate a use case (service + DTO + errors)
	handler     Generate an HTTP handler
	repository  Generate a repository implementation

Examples:
	nova generate entity Order
	nova generate usecase order
	nova generate handler order
	nova generate repository order --type=postgres
	nova generate all order`, Args: cobra.ExactArgs(generateExactArgs),
		RunE: runGenerate,
	}

	generateCmd.Flags().String("type", "postgres", "Repository type: postgres, mysql, mongodb")

	return generateCmd
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
	// gen all components for the use case
	case "all":
		if err := gen.GenerateEntity(name); err != nil {
			return err
		}
		if err := gen.GenerateUseCase(name); err != nil {
			return err
		}
		if err := gen.GenerateHandler(name); err != nil {
			return err
		}
		if err := gen.GenerateRepository(name, repoType); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf(
			"unknown component type: %s (supported: entity, usecase, handler, repository, all)",
			componentType,
		)
	}
}
