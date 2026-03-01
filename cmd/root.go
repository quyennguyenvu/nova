package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "nova",
	Short: "Nova — Go Clean Architecture project generator",
	Long: `Nova generates production-ready Go projects following Clean Architecture principles.

It scaffolds a complete project structure with domain, use case, adapter, and
infrastructure layers, along with Docker, CI/CD, and other tooling.

Usage:
  nova new [project-name]    Generate a new project (interactive or with flags)
  nova generate <type> <name>   Generate a component in an existing project`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.SetHelpTemplate(fmt.Sprintf(`%s
%s`, rootCmd.Long, `
{{if .HasAvailableSubCommands}}Available Commands:{{range .Commands}}{{if .IsAvailableCommand}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}

Use "{{.CommandPath}} [command] --help" for more info.
`))
}
