# Generic device plugin for Kubernetes

## Introduction

`k8s-generic-device-plugin` is a [device plugin](https://github.com/kubernetes/community/blob/master/contributors/design-proposals/resource-management/device-plugin.md) for Kubernetes to manage resources allocated for containers.

It was forked from RDMA plugin and generalized.

You can specify resources exactly as in [ContainerAllocateResponse](https://github.com/kubernetes/kubelet/blob/master/pkg/apis/deviceplugin/v1beta1/api.pb.go#L654) objects.

It does no checking for the devices' real existence, just serves them to Kubernetes deviceplugin.

For examples, check samples/.

## Quick Start

### Build

```
$ go get -d .
$ go build .
```

### Use it

* Run device plugin daemon process

```
# ./k8s-generic-device-plugin <resource-config.yaml>
```
