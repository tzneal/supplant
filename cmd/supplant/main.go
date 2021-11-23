package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type port struct {
	listenPort  int
	servicePort int
}

func parseInt(full, num, name string) int {
	i, err := strconv.ParseInt(num, 10, 32)
	if err != nil {
		log.Fatalf("error parsing %s from %s: %s", name, full, err)
	}

	if i < 0 || i > 65535 {
		log.Fatalf("%s %d is out of range", name, i)
	}

	return int(i)
}

func parsePorts(ports []string) []port {
	ret := []port{}
	for _, p := range ports {
		parsed := port{}
		idx := strings.Index(p, ":")
		if idx != -1 {
			parsed.listenPort = parseInt(p, p[:idx], "listen port")
			parsed.servicePort = parseInt(p, p[idx+1:], "service port")
		} else {
			parsed.listenPort = parseInt(p, p, "listen port")
			parsed.servicePort = parsed.listenPort
		}
		ret = append(ret, parsed)
	}
	return ret
}

func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatalf("unable to determine outbound IP: %s", err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

func main() {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	namespace := flag.String("namespace", v1.NamespaceDefault, "namespace of service")
	ip := flag.String("ip", "<autodetect>", "IP address to redirect service to")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage %s: [OPTION]... SERVICE PORT [PORT]...\n", os.Args[0])
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
		panic(err.Error())
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	ctx := context.Background()

	svc, err := clientset.CoreV1().Services(*namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		log.Fatalf("error retrieving service %s: %s", serviceName, err)
	}

	if len(ports) != len(svc.Spec.Ports) {
		log.Fatalf("service %s has %d ports, but only %d ports provided on command line", svc.Name, len(svc.Spec.Ports), len(ports))
	}

	svcPorts := map[int]v1.ServicePort{}
	for _, port := range svc.Spec.Ports {
		svcPorts[int(port.Port)] = port
	}

	if *ip == "<autodetect>" {
		*ip = getOutboundIP()
	}

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
		newPort.TargetPort = intstr.FromInt(port.listenPort)
		newPort.Protocol = svcPorts[port.servicePort].Protocol
		svc.Spec.Ports = append(svc.Spec.Ports, newPort)
	}

	if svc.ObjectMeta.Labels == nil {
		svc.ObjectMeta.Labels = map[string]string{}
	}
	svc.ObjectMeta.Labels["supplant"] = "true"
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

	ep := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceName,
			Labels: map[string]string{
				"supplant": "true",
			},
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{
					{
						IP: *ip,
					},
				},
			},
		},
	}
	for _, port := range ports {
		ep.Subsets[0].Ports = append(ep.Subsets[0].Ports, v1.EndpointPort{
			Port: int32(port.listenPort),
		})
	}
	endpoints.Create(ctx, ep, metav1.CreateOptions{})
}
