package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/tzneal/supplant"
	"github.com/tzneal/supplant/kube"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/util/homedir"
)

func main() {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	namespace := flag.String("namespace", v1.NamespaceDefault, "namespace of service to supplant")
	ip := flag.String("ip", "<autodetect>", "IP address to redirect service to")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage %s: [OPTION]... SERVICE PORT [PORT]...\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) < 2 {
		fmt.Println("service and port must be specified")
		flag.Usage()
		os.Exit(1)
	}

	serviceName := args[0]
	ports := parsePorts(args[1:])

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Fatalf("error building kubeconfig: %s", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("error creating config: %s", err)
	}

	ctx := context.Background()
	// lookup the service we are replacing
	svc, err := clientset.CoreV1().Services(*namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		log.Fatalf("error retrieving service %s: %s", serviceName, err)
	}

	// user must specify the same number of ports as the service provides
	if len(ports) != len(svc.Spec.Ports) {
		log.Fatalf("service %s has %d ports, but only %d ports provided on command line", svc.Name, len(svc.Spec.Ports), len(ports))
	}

	svcPorts := map[int32]v1.ServicePort{}
	for _, port := range svc.Spec.Ports {
		svcPorts[port.Port] = port
	}

	if *ip == "<autodetect>" {
		*ip, err = supplant.GetOutboundIP()
		if err != nil {
			log.Fatalf("error determining outbound IP address: %s", err)
		}
	}

	// ensure that the user provided ports match the service's ports
	for _, port := range ports {
		_, ok := svcPorts[port.servicePort]
		if !ok {
			log.Fatalf("%d is not a valid service port for %s", port.servicePort, serviceName)
		}
		log.Printf("mapping %s:%d to %s:%d", serviceName, port.servicePort, *ip, port.listenPort)
	}

	// clear the selector and ports
	svc.Spec.Selector = nil
	svc.Spec.Ports = nil
	// and specify our new port mappings
	for _, port := range ports {
		var newPort v1.ServicePort
		newPort.Port = int32(port.servicePort)
		newPort.TargetPort = intstr.FromInt(int(port.listenPort))
		newPort.Protocol = svcPorts[port.servicePort].Protocol
		svc.Spec.Ports = append(svc.Spec.Ports, newPort)
	}

	kube.AppendLabel(svc.ObjectMeta, "supplant", "true")

	log.Printf("updating service %s", svc.Name)
	_, err = clientset.CoreV1().Services(*namespace).Update(ctx, svc, metav1.UpdateOptions{})
	if err != nil {
		log.Fatalf("error updating service %s: %s", serviceName, err)
	}

	endpoints := clientset.CoreV1().Endpoints(*namespace)
	err = endpoints.Delete(ctx, serviceName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		log.Fatalf("error deleting endpoint %s", serviceName)
	}

	ep := kube.NewEndpoint(serviceName, *ip)
	kube.AppendLabel(ep.ObjectMeta, "supplant", "true")

	for _, port := range ports {
		ep.Subsets[0].Ports = append(ep.Subsets[0].Ports, v1.EndpointPort{
			Port: port.listenPort,
		})
	}
	_, err = endpoints.Create(ctx, ep, metav1.CreateOptions{})
	if err != nil {
		log.Fatalf("error creating endpoint %s: %s", serviceName, err)
	}

	log.Println("creating port forwards")
	var portForwards []*portforward.PortForwarder
	for _, forwardSvc := range []string{"foobar"} {
		fw, err := kube.PortForward(clientset, *namespace, fmt.Sprintf("service/%s", forwardSvc))
		if err != nil {
			log.Fatalf("error finding pod for %s", serviceName)
		}
		portForwards = append(portForwards, fw)
	}

	// wait for all of the port forwards to be ready
	for _, fw := range portForwards {
		<-fw.Ready
	}

	log.Println("hit Ctrl+C to exit")
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)

	<-signals
	log.Printf("exiting...")
	for _, fw := range portForwards {
		fw.Close()
	}

}
