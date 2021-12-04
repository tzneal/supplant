package kube

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/tzneal/supplant/util"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
)

type PortForwarder struct {
	Namespace string
	Name      string
	Port      int32
	LocalPort int32
	Forwarder *portforward.PortForwarder
}

// PortForward opens up a socket for the given local IP address and port and forwards it to the specified service and target port.
func PortForward(f cmdutil.Factory, namespace string, svcName string, targetPort int32, localIP net.IP, localPort int32) (PortForwarder, error) {
	builder := f.NewBuilder().WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		ContinueOnError().NamespaceParam(namespace)
	builder.ResourceNames("pods", fmt.Sprintf("service/%s", svcName))
	obj, err := builder.Do().Object()
	if err != nil {
		return PortForwarder{}, err
	}

	getPodTimeout := 10 * time.Second
	forwardablePod, err := polymorphichelpers.AttachablePodForObjectFn(f, obj, getPodTimeout)
	if err != nil {
		return PortForwarder{}, fmt.Errorf("unable to find pod for service: %w", err)
	}

	stop := make(chan struct{}, 1)
	ready := make(chan struct{})

	restClient, err := f.RESTClient()
	if err != nil {
		return PortForwarder{}, fmt.Errorf("unable to create rest client: %w", err)
	}

	req := restClient.Post().
		Resource("pods").
		Namespace(namespace).
		Name(forwardablePod.Name).
		SubResource("portforward")

	restCfg, err := f.ToRESTConfig()
	if err != nil {
		return PortForwarder{}, fmt.Errorf("error creating rest model: %w", err)
	}

	transport, upgrader, err := spdy.RoundTripperFor(restCfg)
	if err != nil {
		return PortForwarder{}, fmt.Errorf("error creating round tripper: %w", err)
	}

	var strm genericclioptions.IOStreams
	ports := []string{fmt.Sprintf("%d:%d", localPort, targetPort)}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())
	address := []string{localIP.String()}
	fw, err := portforward.NewOnAddresses(dialer, address, ports, stop, ready, strm.Out, strm.ErrOut)
	if err != nil {
		return PortForwarder{}, fmt.Errorf("error creating targetPort forward: %w", err)
	}

	go func() {
		err := fw.ForwardPorts()
		if err != nil {
			util.LogError("error forwarding ports for %s: %s", svcName, err)
		}
	}()

	return PortForwarder{
		Namespace: namespace,
		Name:      svcName,
		Port:      targetPort,
		LocalPort: localPort,
		Forwarder: fw,
	}, nil
}
