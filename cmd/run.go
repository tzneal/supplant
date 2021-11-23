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
	Use:   "run",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Args: cobra.ExactValidArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		inputFile := args[0]

		printHeader("connecting to kubernetes")
		f := cmdutil.NewFactory(kubeConfigFlags)
		cs, err := f.KubernetesClientSet()
		if err != nil {
			log.Fatalf("error getting kubernetes client: %s", err)
		}

		ver, err := cs.ServerVersion()
		if err != nil {
			log.Fatalf("error getting kubernetes version: %s", err)
		}
		printHeader("version: %s", ver.String())

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

		var serviceBackups []*v1.Service
		cfg := readConfig(inputFile)
		for _, supplantSvc := range cfg.Supplant {
			if !supplantSvc.Enabled {
				printWarn("skipping disabled service {}", supplantSvc.Name)
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

			for _, port := range supplantSvc.Ports {
				_, match := svcPorts[port.Port]
				if !match {
					log.Fatalf("no match found for port %d in service %s", port.Port, svc.Name)
				}
			}

			// clear the selector and ports
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
				newPort.TargetPort = intstr.FromInt(int(port.ListenPort))
				newPort.Protocol = svcPorts[port.Port].Protocol
				svc.Spec.Ports = append(svc.Spec.Ports, newPort)
				printList("%s:%d is now the endpoint for %s:%d", ip, port.ListenPort, supplantSvc.Name, port.Port)
			}
			appendLabel(&svc.ObjectMeta, "supplant", "true")

			_, err = cs.CoreV1().Services(svc.Namespace).Update(ctx, &svc, metav1.UpdateOptions{})
			if err != nil {
				log.Fatalf("error updating service %s: %s", svc.Name, err)
			}

			endpoints := cs.CoreV1().Endpoints(svc.Namespace)
			err = endpoints.Delete(ctx, svc.Name, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				log.Fatalf("error deleting endpoint %s", svc.Name)
			}

			ep := &v1.Endpoints{
				ObjectMeta: metav1.ObjectMeta{Name: svc.Name},
				Subsets: []v1.EndpointSubset{{
					Addresses: []v1.EndpointAddress{{
						IP: ip.String(),
					}}}},
			}

			appendLabel(&ep.ObjectMeta, "supplant", "true")

			for _, port := range supplantSvc.Ports {
				ep.Subsets[0].Ports = append(ep.Subsets[0].Ports, v1.EndpointPort{
					Port: port.ListenPort,
				})
			}
			_, err = endpoints.Create(ctx, ep, metav1.CreateOptions{})
			if err != nil {
				log.Fatalf("error creating endpoint %s: %s", svc.Name, err)
			}
		}

		localIp, err := cmd.Flags().GetIP("localip")
		if err != nil {
			log.Fatalf("error determining listen ip: %s", err)
		}
		var portForwards []kube.PortForwarder
		for _, externalSvc := range cfg.External {
			if !externalSvc.Enabled {
				printWarn("skipping disabled service {}", externalSvc.Name)
				continue
			}
			for _, port := range externalSvc.Ports {
				fw, err := kube.PortForward(f, externalSvc.Namespace, externalSvc.Name, port.TargetPort, localIp, port.LocalPort)
				if err != nil {
					log.Fatalf("error forwarding port for %s: %s", externalSvc.Name, err)
				}
				portForwards = append(portForwards, fw)
			}
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

		printInfo("forwarding ports, hit Ctrl+C to exit")
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt)

		<-signals
		printHeader("cleaning up....")
		for _, fw := range portForwards {
			printList("closing port forward %s:%d", fw.Name, fw.Port)
			fw.Forwarder.Close()
		}

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

		for _, sb := range serviceBackups {
			printList("restoring service %s", sb.Name)
			currentSvc, err := cs.CoreV1().Services(sb.Namespace).Get(ctx, sb.Name, metav1.GetOptions{})

			// should just need to restore the selector and ports
			currentSvc.Spec.Selector = sb.Spec.Selector
			currentSvc.Spec.Ports = sb.Spec.Ports
			if currentSvc.ObjectMeta.Labels != nil {
				delete(currentSvc.ObjectMeta.Labels, "supplant")
			}
			_, err = cs.CoreV1().Services(sb.Namespace).Update(ctx, currentSvc, metav1.UpdateOptions{})

			if err != nil {
				printError("error restoring %s: err", sb.Name, err)
			}
		}
	},
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

func appendLabel(meta *metav1.ObjectMeta, key string, value string) {
	if meta.Labels == nil {
		meta.Labels = map[string]string{}
	}
	meta.Labels[key] = value
}
