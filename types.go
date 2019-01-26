package main

import (
	pluginapi "k8s.io/kubernetes/pkg/kubelet/apis/deviceplugin/v1beta1"
)

type Resource struct {
	Name string `json:"name"`
	Sets []*ResourceSet `json:"sets"`
}

type ResourceSet struct {
	ID string `json:"id"`
	Spec *pluginapi.ContainerAllocateResponse
}

