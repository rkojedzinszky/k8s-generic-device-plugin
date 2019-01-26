package main

import (
	"fmt"
	"net"
	"os"
	"path"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	pluginapi "k8s.io/kubernetes/pkg/kubelet/apis/deviceplugin/v1beta1"
)

// serverSock		= pluginapi.DevicePluginPath + "spidev.sock"

type genericDevicePlugin struct {
	Config *Resource
	socket string

	devs    []*pluginapi.Device
	devices map[string]*pluginapi.ContainerAllocateResponse

	stop   chan interface{}
	health chan *pluginapi.Device

	server *grpc.Server
}

func newGenericDevicePlugin(resource *Resource) *genericDevicePlugin {
	var devs []*pluginapi.Device
	devMap := make(map[string]*pluginapi.ContainerAllocateResponse)

	for _, set := range resource.Sets {
		devs = append(devs, &pluginapi.Device{
			ID:     set.ID,
			Health: pluginapi.Healthy,
		})
		devMap[set.ID] = set.Spec
	}

	serverSock := pluginapi.DevicePluginPath + strings.Replace(resource.Name, "/", "--", -1) + ".sock"

	return &genericDevicePlugin{
		Config:  resource,
		socket:  serverSock,
		devs:    devs,
		devices: devMap,
		stop:    make(chan interface{}),
		health:  make(chan *pluginapi.Device),
	}
}

func (m *genericDevicePlugin) deviceExists(id string) bool {
	_, ok := m.devices[id]
	return ok
}

// dial establishes the gRPC communication with the registered device plugin.
func dial(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
	c, err := grpc.Dial(unixSocketPath, grpc.WithInsecure(), grpc.WithBlock(),
		grpc.WithTimeout(timeout),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	)

	if err != nil {
		return nil, err
	}

	return c, nil
}

// Start starts the gRPC server of the device plugin
func (m *genericDevicePlugin) Start() error {
	err := m.cleanup()
	if err != nil {
		return err
	}

	sock, err := net.Listen("unix", m.socket)
	if err != nil {
		return err
	}

	m.server = grpc.NewServer([]grpc.ServerOption{}...)
	pluginapi.RegisterDevicePluginServer(m.server, m)

	go func() {
		m.server.Serve(sock)
		log.Errorf("m.server.Serve() exited")
	}()

	// Wait for server to start by launching a blocking connection
	conn, err := dial(m.socket, 5*time.Second)
	if err != nil {
		return err
	}
	conn.Close()

	go m.healthcheck()

	return nil
}

// Stop stops the gRPC server
func (m *genericDevicePlugin) Stop() error {
	if m.server == nil {
		return nil
	}

	m.server.Stop()
	m.server = nil
	close(m.stop)

	return m.cleanup()
}

// Register registers the device plugin for the given resourceName with Kubelet.
func (m *genericDevicePlugin) Register(kubeletEndpoint, resourceName string) error {
	conn, err := dial(kubeletEndpoint, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)
	reqt := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     path.Base(m.socket),
		ResourceName: resourceName,
	}

	_, err = client.Register(context.Background(), reqt)
	if err != nil {
		return err
	}
	return nil
}

// ListAndWatch lists devices and update that list according to the health status
func (m *genericDevicePlugin) ListAndWatch(e *pluginapi.Empty, s pluginapi.DevicePlugin_ListAndWatchServer) error {
	s.Send(&pluginapi.ListAndWatchResponse{Devices: m.devs})

	for {
		select {
		case <-m.stop:
			return nil
		case d := <-m.health:
			// FIXME: there is no way to recover from the Unhealthy state.
			d.Health = pluginapi.Unhealthy
			s.Send(&pluginapi.ListAndWatchResponse{Devices: m.devs})
		}
	}
}

func (m *genericDevicePlugin) unhealthy(dev *pluginapi.Device) {
	m.health <- dev
}

// Allocate which return list of devices.
func (m *genericDevicePlugin) Allocate(ctx context.Context, r *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	ContainerResponses := make([]*pluginapi.ContainerAllocateResponse, 0)

	for _, contReq := range r.ContainerRequests {
		if len(contReq.DevicesIDs) != 1 {
			return nil, fmt.Errorf("invalid allocation request: more than one device requested")
		}

		devId := contReq.DevicesIDs[0]

		log.Debugf("Requested device ID: %v", devId)

		if !m.deviceExists(devId) {
			return nil, fmt.Errorf("invalid allocation request: unknown device: %s", devId)
		}

		if spec, ok := m.devices[devId]; ok {
			ContainerResponses = append(ContainerResponses, spec)
		} else {
			ContainerResponses = append(ContainerResponses, &pluginapi.ContainerAllocateResponse{})
		}
	}

	return &pluginapi.AllocateResponse{ContainerResponses: ContainerResponses}, nil
}

func (m *genericDevicePlugin) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

func (m *genericDevicePlugin) PreStartContainer(context.Context, *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

func (m *genericDevicePlugin) cleanup() error {
	if err := os.Remove(m.socket); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func (m *genericDevicePlugin) healthcheck() {
	ctx, cancel := context.WithCancel(context.Background())

	xids := make(chan *pluginapi.Device)
	go watchXIDs(ctx, m.devs, xids)

	for {
		select {
		case <-m.stop:
			cancel()
			return
		case dev := <-xids:
			m.unhealthy(dev)
		}
	}
}

// Serve starts the gRPC server and register the device plugin to Kubelet
func (m *genericDevicePlugin) Serve() error {
	err := m.Start()
	if err != nil {
		log.Errorf("Could not start device plugin: %v", err)
		return err
	}
	log.Infof("Starting to serve on %s", m.socket)

	err = m.Register(pluginapi.KubeletSocket, m.Config.Name)
	if err != nil {
		log.Errorf("Could not register device plugin: %v", err)
		m.Stop()
		return err
	}
	log.Infof("Registered device plugin with Kubelet")

	return nil
}

func watchXIDs(ctx context.Context, devs []*pluginapi.Device, xids chan<- *pluginapi.Device) {
	for {
		select {
		case <-ctx.Done():
			return
		}
	}
}
