package kube

import (
	v1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AppendLabel appends or replaces a label on the ObjectMeta, creating the label map if necessary
func AppendLabel(meta metav1.ObjectMeta, key string, value string) {
	if meta.Labels == nil {
		meta.Labels = map[string]string{}
	}
	meta.Labels[key] = value
}

func NewEndpoint(serviceName string, ip string) *v1.Endpoints {
	return &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{Name: serviceName},
		Subsets: []v1.EndpointSubset{{
			Addresses: []v1.EndpointAddress{{
				IP: ip,
			}}}},
	}
}
