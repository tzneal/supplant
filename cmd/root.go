package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	version = "dev"
	date    = "unknown"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "supplant",
	Short: "supplant replaces services in a K8s cluster",
	Long: `supplant is used to replace a service in a K8s cluster
and point this service at your local machine. This allows
you to develop/debug a service locally and let it interact
with the rest of the services in a cluster.`,
	Version: version,
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

	rootCmd.SetVersionTemplate(fmt.Sprintf("supplant version {{.Version}} / %s\n", date))
}
