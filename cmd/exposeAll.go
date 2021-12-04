package cmd

import (
	"context"
	"net"
	"os"
	"os/signal"

	"github.com/tzneal/supplant/model"
	"github.com/tzneal/supplant/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/spf13/cobra"
	"github.com/tzneal/supplant/kube"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

// exposeAllCmd represents the exposeAll command
var exposeAllCmd = &cobra.Command{
	Use:   "expose-all",
	Short: "runs port forwarding for every service that exposes TCP ports",
	Long: `expose-all is primarily intended for use in debugging
and general 'poking around' a running K8s cluster. It
enumerates all services and launches port forwarding for
every exposed service and port.`,
	Run: func(cmd *cobra.Command, args []string) {

		f := cmdutil.NewFactory(kubeConfigFlags)
		cs, err := f.KubernetesClientSet()
		if err != nil {
			util.LogError("error getting kubernetes client: %s", err)
			return
		}

		svcs, err := cs.CoreV1().Services(*kubeConfigFlags.Namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			util.LogError("error reading services: %s", err)
			return
		}
		localIp, err := cmd.Flags().GetIP("localip")
		if err != nil {
			util.LogError("error determining listen ip: %s", err)
			return
		}

		var portForwards []kube.PortForwarder
		pl := model.NewPortLookup(cs)
		portForwardingAtLeastOne := false
		for _, svc := range svcs.Items {
			// can't forward to selector'less services
			if len(svc.Spec.Selector) == 0 {
				continue
			}
			for _, port := range svc.Spec.Ports {
				portNumber := pl.LookupPort(svc, port.TargetPort)
				// doesn't support UDP port forwarding yet see https://github.com/kubernetes/kubernetes/issues/47862
				if port.Protocol != "TCP" {
					continue
				}
				fw, err := kube.PortForward(f, svc.Namespace, svc.Name, portNumber, localIp, 0)
				if err != nil {
					util.LogError("error forwarding port for %s: %s", svc.Name, err)
					return
				}
				portForwards = append(portForwards, fw)
			}
			portForwardingAtLeastOne = true
		}
		if !portForwardingAtLeastOne {
			util.LogError("no services found for port forwarding, exiting...")
			return
		}

		// wait for all of the port forwards to be ready
		for _, fw := range portForwards {
			<-fw.Forwarder.Ready
			util.LogInfoHeader("forwarding for %s", fw.Name)
			ports, err := fw.Forwarder.GetPorts()
			if err != nil {
				util.LogError("port forward error: %s", err)
			}
			for _, port := range ports {
				util.LogInfoListItem("%s:%d points to remote %s:%d", localIp, port.Local, fw.Name, port.Remote)
			}
		}

		util.LogInfo("forwarding ports, hit Ctrl+C to exit")
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)

		// wait on the Ctrl+C
		<-signals

		util.LogInfoHeader("cleaning up....")
		for _, fw := range portForwards {
			util.LogInfoListItem("closing port forward %s:%d", fw.Name, fw.Port)
			fw.Forwarder.Close()
		}
	},
}

func init() {

	exposeAllCmd.Flags().IP("localip", net.IPv4(127, 0, 0, 1), "IP address that is used to listen")
	rootCmd.AddCommand(exposeAllCmd)
}
