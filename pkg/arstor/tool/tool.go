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

package tool

import (
	"k8s.io/klog"
)

type Tool interface {
	CheckArStorContainer() error
	Create(id string, size int64, dirInode uint64, pagesize int, compression string, readCache bool, mirroring int) (string, error)
	GetVolumeAttribute(volumeInode uint64) (string, error)
	Truncate(volumeFile string, newSize int64) (string, error)
	Snapshot(srcPath string, snapshotPath string) (string, error)
	GetSnapshotAttribute(volumeInode uint64) (string, error)
	GetSnapshotSrcvolumeId(snapshotInode uint64) (string, error)
	Clone(srcPath string, destPath string) (string, error)
	Delete(Path string) error
	DeleteSnapshot(Path string) error
}

type ToolParameters struct {
	ContainerId string
	DockerUrl   string
}

func NewTool(params ToolParameters) (Tool, error) {
	klog.Infof("NewTool %s", params)

	var tool Tool
	var err error
	if len(params.DockerUrl) != 0 {
		klog.Infof("arstor tool use docker url %s for DockerClient.", params.DockerUrl)
		tool, err = NewDockerClient(params.DockerUrl, params.ContainerId)
		if err != nil {
			return nil, err
		}
	} else {
		klog.Infof("arstor tool use DockerCommand.")
		tool, err = NewDockerCommand(params.ContainerId)
		if err != nil {
			return nil, err
		}
	}

	err = tool.CheckArStorContainer()
	if err != nil {
		return nil, err
	}

	return tool, nil
}
