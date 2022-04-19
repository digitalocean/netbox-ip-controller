package main

import (
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := newRootCommand()
	rootCmd.AddCommand(newCleanCommand())

	cobra.CheckErr(rootCmd.Execute())
}
