package cmd

import (
	"github.com/spf13/cobra"
)

// configCmd represents the model command
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "config is the base config command",
	Long: `config is the base command used to create and 'clean' 
configuration files describing which services will be replaced
and which the replacing services will need to connect to.`,
}

func init() {
	rootCmd.AddCommand(configCmd)
}
