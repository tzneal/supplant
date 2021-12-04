package cmd

import (
	"context"
	"os"

	"github.com/spf13/cobra"
	"github.com/tzneal/supplant/model"
	"github.com/tzneal/supplant/util"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

// createCmd represents the create command
var createCmd = &cobra.Command{
	Use:   "create [flags] config.yml",
	Short: "create constructs a configuration file based on the cluster",
	Long: `create constructs a configuration file by looking at the services
in the cluster.  This is intended to provide a template to allow 
easy construction of the configuration.`,
	Args: cobra.ExactValidArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		f := cmdutil.NewFactory(kubeConfigFlags)
		cs, err := f.KubernetesClientSet()
		if err != nil {
			util.LogError("error getting kubernetes client: %s", err)
			return
		}

		ctx := context.Background()
		svcList, err := cs.CoreV1().Services(*kubeConfigFlags.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			util.LogError("error listing services: %s", err)
			return
		}
		cfg := model.Config{}
		includeAll, _ := cmd.Flags().GetBool("all")
		pl := model.NewPortLookup(cs)
		for _, svc := range svcList.Items {
			// skip kube-system services by default
			if skipByDefault(svc) && !includeAll {
				continue
			}
			cfg.Supplant = append(cfg.Supplant, model.MapSupplantService(pl, svc))
			cfg.External = append(cfg.External, model.MapExternalService(pl, svc))
		}

		writeConfig(cfg, args[0])
	},
}

func skipByDefault(svc v1.Service) bool {
	if svc.Namespace == "kube-system" {
		return true
	}
	if svc.Namespace == "default" && svc.Name == "kubernetes" {
		return true
	}
	return false
}

func writeConfig(cfg model.Config, outputFile string) {
	fo, err := os.Create(outputFile)
	if err != nil {
		util.LogError("error opening %s: %s", outputFile, err)
		return
	}
	defer fo.Close()
	enc := yaml.NewEncoder(fo)
	if err = enc.Encode(cfg); err != nil {
		util.LogError("error encoding config: %s", err)
		return
	}
}

func init() {
	configCmd.AddCommand(createCmd)
	createCmd.Flags().BoolP("all", "A", false, "If true, include items in the kube-system namespace")
}
