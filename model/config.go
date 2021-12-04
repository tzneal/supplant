package model

import (
	"context"
	"fmt"

	"github.com/tzneal/supplant/util"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

type Config struct {
	Supplant []SupplantService
	External []ExternalService
}

type SupplantService struct {
	Name      string
	Namespace string
	Enabled   bool
	Ports     []SupplantPortConfig
}
type SupplantPortConfig struct {
	Name      string `yaml:"name,omitempty"`
	Protocol  v1.Protocol
	Port      int32
	LocalPort int32
}

type ExternalService struct {
	Name      string
	Namespace string
	Enabled   bool
	Ports     []ExternalPortConfig
}
type ExternalPortConfig struct {
	Name       string `yaml:"name,omitempty"`
	Protocol   v1.Protocol
	TargetPort int32
	LocalPort  int32
}

type PortLookup struct {
	cs    *kubernetes.Clientset
	cache map[string]int32
}

func NewPortLookup(cs *kubernetes.Clientset) *PortLookup {
	return &PortLookup{
		cs:    cs,
		cache: make(map[string]int32),
	}
}

func MapSupplantService(pl *PortLookup, svc v1.Service) SupplantService {
	ret := SupplantService{
		Name:      svc.Name,
		Namespace: svc.Namespace,
		Enabled:   false,
	}
	for _, port := range svc.Spec.Ports {
		// doesn't support UDP port forwarding yet see https://github.com/kubernetes/kubernetes/issues/47862
		if port.Protocol != "TCP" {
			continue
		}
		ret.Ports = append(ret.Ports, SupplantPortConfig{
			Name:      port.Name,
			Port:      port.Port,
			Protocol:  port.Protocol,
			LocalPort: 0,
		})
	}
	return ret
}

func MapExternalService(pl *PortLookup, svc v1.Service) ExternalService {
	ret := ExternalService{
		Name:      svc.Name,
		Namespace: svc.Namespace,
		Enabled:   false,
	}
	for _, port := range svc.Spec.Ports {
		// doesn't support UDP port forwarding yet see https://github.com/kubernetes/kubernetes/issues/47862
		if port.Protocol != "TCP" {
			continue
		}
		ret.Ports = append(ret.Ports, ExternalPortConfig{
			Name:       port.Name,
			TargetPort: pl.LookupPort(svc, port.TargetPort),
			Protocol:   port.Protocol,
			LocalPort:  0,
		})
	}
	return ret
}

func (pl *PortLookup) LookupPort(svc v1.Service, port intstr.IntOrString) int32 {
	if port.Type == intstr.Int {
		return port.IntVal
	}
	key := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)
	if port, ok := pl.cache[key]; ok {
		return port
	}

	ctx := context.Background()
	listOpts := metav1.ListOptions{
		LabelSelector: labels.FormatLabels(svc.Spec.Selector),
	}

	pods, err := pl.cs.CoreV1().Pods("").List(ctx, listOpts)
	if err != nil {
		util.LogError("error looking up named port %s: %s", port.StrVal, err)
		return -1
	}
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			for _, cport := range container.Ports {
				if cport.Name == port.StrVal {
					pl.cache[key] = cport.ContainerPort
					return cport.ContainerPort
				}
			}
		}
	}

	util.LogError("unable to find named port %s for service %s", port.StrVal, svc.Name)
	return -1
}
