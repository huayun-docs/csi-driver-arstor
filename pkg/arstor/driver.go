/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package arstor

import (
	"fmt"
	"k8s.io/klog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type arstor struct {
	name              string
	nodeID            string
	version           string
	endpoint          string
	ephemeral         bool
	maxVolumesPerNode int64

	ids *identityServer
	ns  *nodeServer
	cs  *controllerServer
}

const threads = 1

var vendorVersion = "dev"

func NewArStorDriver(driverName, nodeID, endpoint, version string, ephemeral bool, maxVolumesPerNode int64) (*arstor, error) {
	if driverName == "" {
		return nil, fmt.Errorf("No driver name provided")
	}

	if nodeID == "" {
		return nil, fmt.Errorf("No node id provided")
	}

	if endpoint == "" {
		return nil, fmt.Errorf("No driver endpoint provided")
	}
	if version != "" {
		vendorVersion = version
	}

	klog.Infof("Driver: %v ", driverName)
	klog.Infof("Version: %s", vendorVersion)

	return &arstor{
		name:              driverName,
		version:           vendorVersion,
		nodeID:            nodeID,
		endpoint:          endpoint,
		ephemeral:         ephemeral,
		maxVolumesPerNode: maxVolumesPerNode,
	}, nil
}

func (arstor *arstor) SetupDriver(arstorClient *ArStorClient) {

	arstor.ids = NewIdentityServer(arstor.name, arstor.version)
	arstor.ns = NewNodeServer(arstor.nodeID, arstor.ephemeral, arstor.maxVolumesPerNode, arstorClient)
	arstor.cs = NewControllerServer(arstor.ephemeral, arstor.nodeID, arstorClient)
}

func (arstor *arstor) Run() {

	// start clear
	//test
	//period := 5 * time.Second
	period := 5 * time.Minute
	stopCh := make(chan struct{})
	go arstor.ns.loopDeviceManager.RunDetachLostLoopDevice(threads, period, stopCh)

	// Create GRPC servers
	s := NewNonBlockingGRPCServer()
	s.Start(arstor.endpoint, arstor.ids, arstor.cs, arstor.ns)

	// ...until SIGINT
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Kill, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	<-sc
	close(stopCh)

	// wait GRPC server
	s.Wait()
}
