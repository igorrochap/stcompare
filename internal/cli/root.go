package cli

import (
	"github.com/spf13/cobra"

	"stcompare/internal/config"
)

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "stcompare"}
	configCommand := &cobra.Command{Use: "config"}
	initCommand := &cobra.Command{
		Use: "init",
		RunE: func(_ *cobra.Command, _ []string) error {
			return config.WriteDefault(config.DefaultFilename)
		},
	}

	configCommand.AddCommand(initCommand)
	root.AddCommand(configCommand)

	return root
}
