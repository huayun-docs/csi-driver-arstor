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

package main

import (
	"flag"
	"fmt"
	"os"
	"path"

	"github.com/huayun-docs/csi-driver-arstor/pkg/arstor"

	"k8s.io/klog"
)

const (
	// Number of worker threads
	threads = 5
)

var (
	// Set by the build process
	version = "1.0.0"

	endpoint    = flag.String("endpoint", "unix://csi/csi.sock", "CSI endpoint")
	driverName  = flag.String("drivername", "arstor.csi.huayun.io", "name of the driver")
	nodeID      = flag.String("nodeid", "", "kubernetes node id or name")
	ephemeral   = flag.Bool("ephemeral", true, "publish volumes in ephemeral mode even if kubelet did not ask for it (only needed for Kubernetes 1.15)")
	showVersion = flag.Bool("version", false, "Show version.")

	arstorMountPoint = flag.String("arstorMountPoint", "/arstor", "Location where MFS is loaded.")
	arstorShares     = flag.String("arstorShares", "", "the arstor shares connetion.")
	mountAttamps     = flag.Int("mountAttamps", 3, "the number of retry times.")
	arstorContainer  = flag.String("arstorContainer", "mxsp", "ArStor docker container name or id")
	dockerUrl        = flag.String("dockerUrl", "unix:///var/run/docker.sock", "the client url of docker")
	mountHashDir     = flag.Bool("mountHashDir", false, "if True, device will mount on a directory with hash directory, such as /arstor/XXX")

	mountOption           = flag.String("mountOption", "", "the option of arstor mount.")
	arstorAlternateServer = flag.String("arstorAlternateServer", "", "ArStor Nodes in the cluster.")

	maxVolumesPerNode = flag.Int64("maxvolumespernode", 0, "limit of volumes per node")
)

func main() {
	klog.InitFlags(nil)
	flag.Set("logtostderr", "true")
	flag.Parse()

	if *showVersion {
		baseName := path.Base(os.Args[0])
		fmt.Println(baseName, version)
		return
	}

	if *ephemeral {
		fmt.Fprintln(os.Stderr, "Deprecation warning: The ephemeral flag is deprecated and should only be used when deploying on Kubernetes 1.15. It will be removed in the future.")
	}

	handle()
	os.Exit(0)
}

func handle() {
	arstorConfig := arstor.ArStorConfig{
		ArStorMountPoint:      *arstorMountPoint,
		ArStorShares:          *arstorShares,
		MountAttamps:          *mountAttamps,
		ArStorContainer:       *arstorContainer,
		MountHashDir:          *mountHashDir,
		MountOption:           *mountOption,
		ArStorAlternateServer: *arstorAlternateServer,
		DockerUrl:             *dockerUrl,
	}

	arstorClient, err := arstor.NewArStorClient(arstorConfig)
	if err != nil {
		fmt.Printf("Failed to initialize driver: %s", err.Error())
		os.Exit(1)
	}
	// setup to load arstor data
	arstorClient.Setup()

	klog.Infof("NewArStorDriver drivername %s, nodeID %s, endpoint %s", *driverName, *nodeID, *endpoint)
	driver, err := arstor.NewArStorDriver(*driverName, *nodeID, *endpoint, version, *ephemeral, *maxVolumesPerNode)
	if err != nil {
		fmt.Printf("Failed to initialize driver: %s\n", err.Error())
		os.Exit(1)
	}
	driver.SetupDriver(arstorClient)
	driver.Run()

}
