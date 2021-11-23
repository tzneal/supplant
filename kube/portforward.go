package kube

import (
	"fmt"
	"net/http"
	"time"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
)

func PortForward(cs *kubernetes.Clientset, namespace string, resourceName string) (*portforward.PortForwarder, error) {
	kubeconfigFlags := genericclioptions.NewConfigFlags(true)
	f := cmdutil.NewFactory(kubeconfigFlags)
	builder := f.NewBuilder().WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		ContinueOnError().NamespaceParam(namespace).DefaultNamespace()

	builder.ResourceNames("pods", resourceName)
	obj, err := builder.Do().Object()
	if err != nil {
		return nil, err
	}

	getPodTimeout := 10 * time.Second
	forwardablePod, err := polymorphichelpers.AttachablePodForObjectFn(f, obj, getPodTimeout)
	if err != nil {
		return nil, fmt.Errorf("unable to find pod for service: %w", err)
	}

	stop := make(chan struct{}, 1)
	ready := make(chan struct{})

	restClient, err := f.RESTClient()
	if err != nil {
		return nil, fmt.Errorf("unable to create rest client: %w", err)
	}

	req := restClient.Post().
		Resource("pods").
		Namespace(namespace).
		Name(forwardablePod.Name).
		SubResource("portforward")

	restCfg, err := f.ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("error creating rest config: %w", err)
	}

	transport, upgrader, err := spdy.RoundTripperFor(restCfg)
	if err != nil {
		return nil, fmt.Errorf("error creating round tripper: %w", err)
	}

	var strm genericclioptions.IOStreams
	ports := []string{"0:80"}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())
	address := []string{"0.0.0.0"}
	fw, err := portforward.NewOnAddresses(dialer, address, ports, stop, ready, strm.Out, strm.ErrOut)
	if err != nil {
		return nil, fmt.Errorf("error creating port forward: %w", err)
	}

	go func() {
		fw.ForwardPorts()
	}()

	return fw, nil
}
