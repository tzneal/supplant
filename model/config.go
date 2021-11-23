package model

import (
	"log"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type Config struct {
	Supplant []SupplantService
	External []ExternalService
}

type SupplantService struct {
	Namespace string
	Name      string
	Enabled   bool
	Ports     []SupplantPortConfig
}
type SupplantPortConfig struct {
	Name       string `yaml:"name,omitempty"`
	Protocol   v1.Protocol
	Port       int32
	ListenPort int32
}

type ExternalService struct {
	Namespace string
	Name      string
	Enabled   bool
	Ports     []ExternalPortConfig
}
type ExternalPortConfig struct {
	Name       string `yaml:"name,omitempty"`
	Protocol   v1.Protocol
	TargetPort int32
	LocalPort  int32
}

func MapSupplantService(svc v1.Service) SupplantService {
	ret := SupplantService{
		Name:      svc.Name,
		Namespace: svc.Namespace,
		Enabled:   false,
	}
	for _, port := range svc.Spec.Ports {
		ret.Ports = append(ret.Ports, SupplantPortConfig{
			Name:       port.Name,
			Port:       port.Port,
			Protocol:   port.Protocol,
			ListenPort: decodePort(port.TargetPort),
		})
	}
	return ret
}

func MapExternalService(svc v1.Service) ExternalService {
	ret := ExternalService{
		Name:      svc.Name,
		Namespace: svc.Namespace,
		Enabled:   false,
	}
	for _, port := range svc.Spec.Ports {
		ret.Ports = append(ret.Ports, ExternalPortConfig{
			Name:       port.Name,
			TargetPort: decodePort(port.TargetPort),
			Protocol:   port.Protocol,
			LocalPort:  0,
		})
	}
	return ret
}

func decodePort(port intstr.IntOrString) int32 {
	if port.Type == intstr.Int {
		return port.IntVal
	}
	log.Fatalf("TODO: support parsing port names")
	return -1
}
