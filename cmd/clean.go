package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/tzneal/supplant/model"
	"github.com/tzneal/supplant/util"
	"gopkg.in/yaml.v3"
)

// cleanCmd represents the clean command
var cleanCmd = &cobra.Command{
	Use:   "clean [flags] config.yml",
	Short: "clean removes all disabled items from a configuration file",
	Long: `clean removes all disabled items.  The standard workflow
is to use the 'create' commnad to construct a new configuration file
and then edit/modify it as necessary, enabling services that are to be
replaced and made available externally.  Then the clean command can be 
used to remove all of the disabled services for a tidier config file.`,
	Args: cobra.ExactValidArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		inputFile := args[0]
		cfg := readConfig(inputFile)
		if cfg == nil {
			return
		}

		// filter out everything that is disabled
		cfg.Supplant = filterSupplant(cfg.Supplant)
		cfg.External = filterExternal(cfg.External)
		writeConfig(*cfg, inputFile)
	},
}

func readConfig(inputFile string) *model.Config {
	f, err := os.Open(inputFile)
	if err != nil {
		util.LogError("error opening %s: %s", inputFile, err)
		return nil
	}
	dec := yaml.NewDecoder(f)
	cfg := model.Config{}
	if err = dec.Decode(&cfg); err != nil {
		util.LogError("error decoding %s: %s", inputFile, err)
		return nil
	}
	return &cfg
}

func filterSupplant(supplant []model.SupplantService) []model.SupplantService {
	var ret []model.SupplantService
	for _, svc := range supplant {
		if svc.Enabled {
			ret = append(ret, svc)
		}
	}
	return ret
}
func filterExternal(supplant []model.ExternalService) []model.ExternalService {
	var ret []model.ExternalService
	for _, svc := range supplant {
		if svc.Enabled {
			ret = append(ret, svc)
		}
	}
	return ret
}

func init() {
	configCmd.AddCommand(cleanCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// cleanCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// cleanCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
