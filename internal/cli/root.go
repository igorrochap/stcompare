package cli

import "github.com/spf13/cobra"

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "stcompare"}
	root.AddCommand(newConfigCommand())

	return root
}
