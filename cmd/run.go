package cmd

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
	"github.com/tzneal/supplant/kube"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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

		printHeader("connecting to K8s")
		f := cmdutil.NewFactory(kubeConfigFlags)
		cs, err := f.KubernetesClientSet()
		if err != nil {
			log.Fatalf("error getting kubernetes client: %s", err)
		}

		ver, err := cs.ServerVersion()
		if err != nil {
			log.Fatalf("error getting kubernetes version: %s", err)
		}
		printHeader("K8s version: %s", ver.String())

		ctx := context.Background()
		type svcKey struct {
			namespace string
			name      string
		}

		// get a map of the running services
		svcMap := map[svcKey]v1.Service{}
		svcList, err := cs.CoreV1().Services(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			log.Fatalf("error listing services: %s", err)
		}
		for _, svc := range svcList.Items {
			key := svcKey{svc.Namespace, svc.Name}
			svcMap[key] = svc
		}

		supplantingAtLeastOne := false
		var serviceBackups []*v1.Service
		cfg := readConfig(inputFile)
		for _, supplantSvc := range cfg.Supplant {
			if !supplantSvc.Enabled {
				continue
			}
			key := svcKey{supplantSvc.Namespace, supplantSvc.Name}
			svc, ok := svcMap[key]
			if !ok {
				log.Fatalf("unable to find service %s in namespace %s", supplantSvc.Name, supplantSvc.Namespace)
			}

			// backup the services before we change them so we can replace them when
			// exiting
			serviceBackups = append(serviceBackups, svc.DeepCopy())

			svcPorts := map[int32]v1.ServicePort{}
			for _, port := range svc.Spec.Ports {
				svcPorts[port.Port] = port
			}

			// ensure that we are covering all of the ports
			for _, port := range supplantSvc.Ports {
				_, match := svcPorts[port.Port]
				if !match {
					log.Fatalf("no match found for port %d in service %s", port.Port, svc.Name)
				}
			}

			if svc.Spec.Selector == nil || len(svc.Spec.Selector) == 0 {
				log.Fatalf("attempted to supplant a service with no selectors")
			}

			// clear the selector and ports
			svc.ObjectMeta.Labels = nil
			svc.Spec.Selector = nil
			svc.Spec.Ports = nil

			ip, err := cmd.Flags().GetIP("ip")
			if err != nil {
				log.Fatalf("error getting IP: %s", err)
			}

			printHeader("updating service %s", svc.Name)
			// and specify our new port mappings
			for _, port := range supplantSvc.Ports {
				var newPort v1.ServicePort
				newPort.Port = port.Port
				newPort.TargetPort = intstr.FromInt(int(port.LocalPort))
				newPort.Protocol = svcPorts[port.Port].Protocol
				svc.Spec.Ports = append(svc.Spec.Ports, newPort)
				printList("%s:%d is now the endpoint for %s:%d", ip, port.LocalPort, supplantSvc.Name, port.Port)
			}
			appendAnnotation(&svc.ObjectMeta, "supplant", "true")

			// delete the existing service
			err = cs.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				log.Fatalf("error deleting existing service %s: %s", svc.Name, err)
			}

			// Prepare to recreate a new service without a selector.  I attempted to just remove the selector
			// on the existing service, which somewhat worked but it would then load-balance across the existing service
			// and our replacement.  Removing the service and
			prepareServiceForCreation(&svc)

			_, err = cs.CoreV1().Services(svc.Namespace).Create(ctx, &svc, metav1.CreateOptions{})
			if err != nil {
				log.Fatalf("error updating service %s: %s", svc.Name, err)
			}

			// delete the existing endpoint
			endpoints := cs.CoreV1().Endpoints(svc.Namespace)

			err = endpoints.Delete(ctx, svc.Name, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				log.Fatalf("error deleting endpoint %s", svc.Name)
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
					Port: port.LocalPort,
				})
			}
			_, err = endpoints.Create(ctx, ep, metav1.CreateOptions{})
			if err != nil {
				log.Fatalf("error creating endpoint %s: %s", svc.Name, err)
			}
			supplantingAtLeastOne = true
		}

		localIp, err := cmd.Flags().GetIP("localip")
		if err != nil {
			log.Fatalf("error determining listen ip: %s", err)
		}

		portForwardingAtLeastOne := false
		var portForwards []kube.PortForwarder
		for _, externalSvc := range cfg.External {
			if !externalSvc.Enabled {
				continue
			}
			for _, port := range externalSvc.Ports {
				fw, err := kube.PortForward(f, externalSvc.Namespace, externalSvc.Name, port.TargetPort, localIp, port.LocalPort)
				if err != nil {
					log.Fatalf("error forwarding port for %s: %s", externalSvc.Name, err)
				}
				portForwards = append(portForwards, fw)
			}
			portForwardingAtLeastOne = true
		}

		if !supplantingAtLeastOne && !portForwardingAtLeastOne {
			printError("no services configured for supplanting or port forwarding, exiting...")
			os.Exit(0)
		}
		// wait for all of the port forwards to be ready
		for _, fw := range portForwards {
			<-fw.Forwarder.Ready
			printHeader("forwarding for %s", fw.Name)
			ports, err := fw.Forwarder.GetPorts()
			if err != nil {
				log.Fatalf("port forward error: %s", err)
			}
			for _, port := range ports {
				printList("%s:%d points to remote %s:%d", localIp, port.Local, fw.Name, port.Remote)
			}
		}

		// we've now replaced the services and are forwarding the requested ports. Wait for the user to hit Ctrl+C
		// so we can undo all of our changes
		printInfo("forwarding ports, hit Ctrl+C to exit")
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)

		// wait on the Ctrl+C
		<-signals

		printHeader("cleaning up....")
		for _, fw := range portForwards {
			printList("closing port forward %s:%d", fw.Name, fw.Port)
			fw.Forwarder.Close()
		}

		// deleter our endpoints
		lo := metav1.ListOptions{
			LabelSelector: "supplant=true",
		}

		eps, err := cs.CoreV1().Endpoints(metav1.NamespaceAll).List(ctx, lo)
		if err != nil {
			printError("error listing endpoints: %s", err)
		} else {
			for _, ep := range eps.Items {
				err = cs.CoreV1().Endpoints(ep.Namespace).Delete(ctx, ep.Name, metav1.DeleteOptions{})
				if err != nil {
					printError("error deleting endpoints: %s", err)
				}
			}
		}

		// and replace our services which will re-create the endpoints based on the selectors
		for _, sb := range serviceBackups {
			printList("restoring service %s", sb.Name)
			err = cs.CoreV1().Services(sb.Namespace).Delete(ctx, sb.Name, metav1.DeleteOptions{})
			if err != nil {
				log.Fatalf("error deleting existing service %s: %s", sb.Name, err)
			}

			prepareServiceForCreation(sb)
			_, err = cs.CoreV1().Services(sb.Namespace).Create(ctx, sb, metav1.CreateOptions{})

			if err != nil {
				printError("error restoring %s: err", sb.Name, err)
			}
		}
	},
}

// prepareServiceForCreation clears out the properties on a service retrieved from K8s so we can use it
// to recreate a new service
func prepareServiceForCreation(svc *v1.Service) {
	svc.ResourceVersion = ""
	svc.UID = ""
	svc.Spec.ClusterIPs = nil
	svc.Spec.ClusterIP = ""
	svc.ObjectMeta.CreationTimestamp = metav1.Time{}
}

func init() {
	rootCmd.AddCommand(runCmd)

	ip, err := getOutboundIP()
	if err != nil {
		ip = net.IP{}
	}
	runCmd.Flags().IP("ip", ip, "IP address that services within the cluster will connect to")
	runCmd.Flags().IP("localip", net.IPv4(127, 0, 0, 1), "IP address that is used to listen")
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
