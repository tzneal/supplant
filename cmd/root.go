package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "supplant",
	Short: "A brief description of your application",
	Long: `A longer description that spans multiple lines and likely contains
examples and usage of using your application. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

var kubeConfigFlags = genericclioptions.NewConfigFlags(false)

func init() {
	flags := pflag.NewFlagSet("supplant", pflag.ExitOnError)
	pflag.CommandLine = flags
	flags.AddFlagSet(rootCmd.PersistentFlags())
	kubeConfigFlags.AddFlags(flags)
}
