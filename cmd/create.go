package cmd

import (
	"context"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/tzneal/supplant/model"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

// createCmd represents the create command
var createCmd = &cobra.Command{
	Use:   "create",
	Short: "create constructs a configuration file based on the cluster",
	Long: `create constructs a configuration file by looking at the services
in the cluster.  This is intended to provide a template to allow 
easy construction of the configuration.`,
	Args: cobra.ExactValidArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		f := cmdutil.NewFactory(kubeConfigFlags)
		cs, err := f.KubernetesClientSet()
		if err != nil {
			log.Fatalf("error getting kubernetes client: %s", err)
		}

		ctx := context.Background()
		svcList, err := cs.CoreV1().Services(*kubeConfigFlags.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Fatalf("error listing services: %s", err)
		}
		cfg := model.Config{}
		for _, svc := range svcList.Items {
			cfg.Supplant = append(cfg.Supplant, model.MapSupplantService(svc))
			cfg.External = append(cfg.External, model.MapExternalService(svc))
		}

		writeConfig(cfg, args[0])
	},
}

func writeConfig(cfg model.Config, outputFile string) {
	fo, err := os.Create(outputFile)
	if err != nil {
		log.Fatalf("error opening %s: %s", outputFile, err)
	}
	defer fo.Close()
	enc := yaml.NewEncoder(fo)
	if err = enc.Encode(cfg); err != nil {
		log.Fatalf("error encoding config: %s", err)
	}
}

func init() {
	configCmd.AddCommand(createCmd)
}
