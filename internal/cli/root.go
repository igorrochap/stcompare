package cli

import "github.com/spf13/cobra"

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:          "stcompare",
		SilenceUsage: true,
	}
	root.AddCommand(newConfigCommand())

	return root
}
