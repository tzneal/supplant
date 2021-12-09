package cmd

import (
	"context"
	"net"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
	"github.com/tzneal/supplant/kube"
	"github.com/tzneal/supplant/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run [flags] config.yml",
	Short: "run launches a configuration",
	Long: `run launches a configuration, pointing services to local ports
on your machine and forwarding local ports to services
inside the cluster as described by the configuration file.`,
	Args: cobra.ExactValidArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		inputFile := args[0]

		util.LogInfoHeader("connecting to K8s")
		f := cmdutil.NewFactory(kubeConfigFlags)
		cs, err := f.KubernetesClientSet()
		if err != nil {
			util.LogError("error getting kubernetes client: %s", err)
			return
		}

		ver, err := cs.ServerVersion()
		if err != nil {
			util.LogError("error getting kubernetes version: %s", err)
			return
		}

		util.LogInfoHeader("K8s version: %s", ver.String())
		ctx := context.Background()
		type svcKey struct {
			namespace string
			name      string
		}

		// get a map of the running services
		svcMap := map[svcKey]v1.Service{}
		svcList, err := cs.CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			util.LogError("error listing services: %s", err)
			return
		}

		for _, svc := range svcList.Items {
			key := svcKey{svc.Namespace, svc.Name}
			svcMap[key] = svc
		}

		supplantingAtLeastOne := false
		cfg := readConfig(inputFile)
		if cfg == nil {
			return
		}

		// defer the deletion of endpoints so we can try to ensure we always put things
		// back like they were
		defer deleteSupplantedEndpoints(cs)

		for _, supplantSvc := range cfg.Supplant {
			if !supplantSvc.Enabled {
				continue
			}

			for i := range supplantSvc.Ports {
				port := &supplantSvc.Ports[i]
				// we need to choose a port for the user
				if port.LocalPort == 0 {
					listener, err := net.Listen("tcp", ":0")
					if err != nil {
						util.LogError("error choosing local port for service %s: %s", supplantSvc.Name, err)
						return
					}
					port.LocalPort = int32(listener.Addr().(*net.TCPAddr).Port)
					listener.Close()
				}
			}

			key := svcKey{supplantSvc.Namespace, supplantSvc.Name}
			svc, ok := svcMap[key]
			if !ok {
				util.LogError("unable to find service %s in namespace %s", supplantSvc.Name, supplantSvc.Namespace)
			}

			// backup the service before we change it so we can replace them it when
			// exiting
			serviceBackup := svc.DeepCopy()

			svcPorts := map[int32]v1.ServicePort{}
			for _, port := range svc.Spec.Ports {
				svcPorts[port.Port] = port
			}

			// ensure that we are covering all of the ports
			for _, port := range supplantSvc.Ports {
				_, match := svcPorts[port.Port]
				if !match {
					util.LogError("no match found for port %d in service %s", port.Port, svc.Name)
					return
				}
			}

			if svc.Spec.Selector == nil || len(svc.Spec.Selector) == 0 {
				util.LogError("attempted to supplant a service with no selectors")
				return
			}

			// clear the selector and ports
			svc.ObjectMeta.Labels = nil
			svc.Spec.Selector = nil
			svc.Spec.Ports = nil

			ip, err := cmd.Flags().GetIP(flagExternalIP)
			if err != nil {
				util.LogError("error getting external IP: %s", err)
				return
			}

			util.LogInfoHeader("updating service %s", svc.Name)
			// and specify our new port mappings
			for _, port := range supplantSvc.Ports {
				var newPort v1.ServicePort
				newPort.Name = port.Name
				newPort.Port = port.Port
				newPort.TargetPort = intstr.FromInt(int(port.LocalPort))
				newPort.Protocol = svcPorts[port.Port].Protocol
				svc.Spec.Ports = append(svc.Spec.Ports, newPort)
				util.LogInfoListItem("%s:%d is now the endpoint for %s:%d", ip, port.LocalPort, supplantSvc.Name, port.Port)
			}
			appendAnnotation(&svc.ObjectMeta, "supplant", "true")

			// delete the existing service
			err = cs.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				util.LogError("error deleting existing service %s: %s", svc.Name, err)
				return
			}

			// always try to restore the service
			defer restoreService(cs, serviceBackup)

			// Prepare to recreate a new service without a selector.  I attempted to just remove the selector
			// on the existing service, which somewhat worked but it would then load-balance across the existing service
			// and our replacement.  Removing the service seems to make this more reliable.
			prepareServiceForCreation(&svc)

			_, err = cs.CoreV1().Services(svc.Namespace).Create(ctx, &svc, metav1.CreateOptions{})
			if err != nil {
				util.LogError("error updating service %s: %s", svc.Name, err)
				return
			}

			// delete the existing endpoint
			endpoints := cs.CoreV1().Endpoints(svc.Namespace)

			err = endpoints.Delete(ctx, svc.Name, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				util.LogError("error deleting endpoint %s", svc.Name)
				return
			}

			// and prepare to create our own that points back to our local IP address
			ep := &v1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{Name: svc.Name},
				Subsets: []v1.EndpointSubset{{
					Addresses: []v1.EndpointAddress{{
						IP: ip.String(),
					}}}},
			}

			appendAnnotation(&ep.ObjectMeta, "supplant", "true")

			for _, port := range supplantSvc.Ports {
				ep.Subsets[0].Ports = append(ep.Subsets[0].Ports, v1.EndpointPort{
					Name: port.Name,
					Port: port.LocalPort,
				})
			}
			_, err = endpoints.Create(ctx, ep, metav1.CreateOptions{})
			if err != nil {
				util.LogError("error creating endpoint %s: %s", svc.Name, err)
				return
			}
			supplantingAtLeastOne = true
		}

		localIp, err := cmd.Flags().GetIP(flagLocalIP)
		if err != nil {
			util.LogError("error determining listen ip: %s", err)
			return
		}

		portForwardingAtLeastOne := false
		var portForwards []kube.PortForwarder
		for _, externalSvc := range cfg.External {
			if !externalSvc.Enabled {
				continue
			}
			var pc []kube.PortConfig
			for _, port := range externalSvc.Ports {
				pc = append(pc, kube.PortConfig{
					LocalPort:  port.LocalPort,
					TargetPort: port.TargetPort,
				})
			}

			if len(pc) > 0 {
				fw, err := kube.PortForward(f, externalSvc.Namespace, externalSvc.Name, localIp, pc)
				if err != nil {
					util.LogError("error forwarding port for %s: %s", externalSvc.Name, err)
					return
				}
				// ensure we close it
				defer closePortForward(fw)
				portForwards = append(portForwards, fw)
				portForwardingAtLeastOne = true
			}
		}

		if !supplantingAtLeastOne && !portForwardingAtLeastOne {
			util.LogError("no services configured for supplanting or port forwarding, exiting...")
			return
		}
		// wait for all of the port forwards to be ready
		for _, fw := range portForwards {
			<-fw.Forwarder.Ready
			util.LogInfoHeader("forwarding for %s", fw.Name)
			ports, err := fw.Forwarder.GetPorts()
			if err != nil {
				util.LogError("port forward error: %s", err)
				return
			}
			for _, port := range ports {
				util.LogInfoListItem("%s:%d points to remote %s:%d", localIp, port.Local, fw.Name, port.Remote)
			}
		}

		// we've now replaced the services and are forwarding the requested ports. Wait for the user to hit Ctrl+C
		// so we can undo all of our changes
		util.LogInfo("forwarding ports, hit Ctrl+C to exit")
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)

		// wait on the Ctrl+C
		<-signals

		util.LogInfoHeader("cleaning up....")
		// all of the cleanup is done via defers so we can hopefully always return the state to what it was
		// before we changed things
	},
}

func restoreService(cs *kubernetes.Clientset, sb *v1.Service) {
	ctx := context.TODO()
	util.LogInfoListItem("restoring service %s", sb.Name)
	err := cs.CoreV1().Services(sb.Namespace).Delete(ctx, sb.Name, metav1.DeleteOptions{})
	if err != nil {
		util.LogError("error deleting existing service %s: %s", sb.Name, err)
	}

	// try to re-create the service even if the deletion failed (maybe it was already gone?)
	prepareServiceForCreation(sb)
	_, err = cs.CoreV1().Services(sb.Namespace).Create(ctx, sb, metav1.CreateOptions{})

	if err != nil {
		util.LogError("error restoring %s: %s", sb.Name, err)
	}
}

func closePortForward(fw kube.PortForwarder) {
	for _, p := range fw.Ports {
		util.LogInfoListItem("closing port forward %s:%d", fw.Name, p.LocalPort)
	}
	fw.Forwarder.Close()
}

// deleteSupplantedEndpoints deletes all supplants that we've created (either in this run or a previous run)
func deleteSupplantedEndpoints(cs *kubernetes.Clientset) {
	lo := metav1.ListOptions{
		LabelSelector: "supplant=true",
	}
	ctx := context.Background()
	eps, err := cs.CoreV1().Endpoints(metav1.NamespaceAll).List(ctx, lo)
	if err != nil {
		util.LogError("error listing endpoints: %s", err)
	} else {
		for _, ep := range eps.Items {
			err = cs.CoreV1().Endpoints(ep.Namespace).Delete(ctx, ep.Name, metav1.DeleteOptions{})
			if err != nil {
				util.LogError("error deleting endpoints: %s", err)
			}
		}
	}
}

// prepareServiceForCreation clears out some the properties on a service retrieved from K8s so we can use it
// to recreate a new service.  We retain the cluster IPs to hopefully provide minimal disruption.
func prepareServiceForCreation(svc *v1.Service) {
	svc.ResourceVersion = ""
	svc.UID = ""
	svc.ObjectMeta.CreationTimestamp = metav1.Time{}
}

const flagExternalIP = "externalip"
const flagLocalIP = "localip"

func init() {
	rootCmd.AddCommand(runCmd)

	ip, err := getOutboundIP()
	if err != nil {
		ip = net.IP{}
	}
	runCmd.Flags().IP(flagExternalIP, ip, "IP address that services within the cluster will connect to")
	runCmd.Flags().IP(flagLocalIP, net.IPv4(127, 0, 0, 1), "IP address that is used to listen")
}

func getOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return net.IP{}, err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP, nil
}

func appendAnnotation(meta *metav1.ObjectMeta, key string, value string) {
	if meta.Annotations == nil {
		meta.Annotations = map[string]string{}
	}
	meta.Annotations[key] = value
}
