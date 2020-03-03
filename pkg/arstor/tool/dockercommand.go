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
	"errors"
	"fmt"

	utilexec "k8s.io/utils/exec"

	"strconv"
	"strings"

	"k8s.io/klog"
)

type dockerCommand struct {
	containerId string
	executor    utilexec.Interface
}

func NewDockerCommand(containerId string) (*dockerCommand, error) {
	executor := utilexec.New()
	return &dockerCommand{
		containerId: containerId,
		executor:    executor,
	}, nil
}

func (dc *dockerCommand) CheckArStorContainer() error {
	option := "docker inspect --format '{{.State.Running}}' " + dc.containerId
	output, err := dc.executor.Command("sh", "-c", option).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("CheckArStorContainer output: %s error: %v", string(output[:]), err)
		return errors.New(message)
	}
	klog.Infof("CheckArStorContainer result: %s", string(output[:]))

	str := string(output[:])
	if strings.ToLower(str) != "true" {
		message := fmt.Sprintf("CheckArStorContainer: the container %s is not running", dc.containerId)
		return errors.New(message)
	}

	return nil
}

func (dc *dockerCommand) executeMxzklist(args []string) (string, error) {
	klog.V(4).Infof("dockerCommand zklist command: %s", args)

	options := []string{"exec", dc.containerId, "zklist"}
	options = append(options, args...)

	out, err := dc.executor.Command("docker", options...).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("dockerCommand exec command error: %s", out)
		return "", errors.New(message)
	}

	return string(out[:]), nil
}

func (dc *dockerCommand) GetVolumeAttribute(volumeInode uint64) (string, error) {
	klog.V(4).Infof("dockerCommand GetVolumeAttribute volumeinode %s", volumeInode)
	args := []string{}
	args = append(args, "-i")
	args = append(args, strconv.FormatUint(volumeInode, 10))
	args = append(args, "-r")

	result, err := dc.executeMxzklist(args)
	if err != nil {
		return "", err
	}
	klog.V(4).Infof("dockerCommand GetVolumeAttribute result: %s", result)
	return result, nil
}

func (dc *dockerCommand) GetSnapshotAttribute(snapshotInode uint64) (string, error) {
	klog.V(4).Infof("dockerCommand GetSnapshotAttribute snapshotInode %s", snapshotInode)
	args := []string{}
	args = append(args, "-i")
	args = append(args, strconv.FormatUint(snapshotInode, 10))
	args = append(args, "-r")

	result, err := dc.executeMxzklist(args)
	if err != nil {
		return "", err
	}
	klog.V(4).Infof("dockerCommand GetSnapshotAttribute result: %s", result)
	return result, nil
}

func (mt *dockerCommand) GetSnapshotSrcvolumeId(snapshotInode uint64) (string, error) {
	//	volume-f61ff4d8-6fac-4445-b319-589ada9c5927 : 113766 (0x1bc66) : UPTODATE
	//snapshots:
	//	|-> NoName-126834 : 126834 (0x1ef72) : UPTODATE
	//	|	clones:
	//	|	|-> pfytest_clone_ok : 89044 (0x15bd4) : UPTODATE
	klog.V(4).Infof("arstorTool GetMxSnapshotSrcvolume snapshotInode %s", snapshotInode)
	args := []string{}
	args = append(args, "-i")
	args = append(args, strconv.FormatUint(snapshotInode, 10))
	args = append(args, "--strand")

	result, err := mt.executeMxzklist(args)
	if err != nil {
		return "", err
	}

	arr := strings.Split(result, ":")
	klog.V(4).Infof("arstorTool GetMxSnapshotSrcvolume response %v", arr)

	idName := strings.Split(arr[0], ".")
	return idName[0], nil
}

func (dc *dockerCommand) executeMxzkcli(args []string) (string, error) {
	klog.V(4).Infof("dockerCommand mxzkcli.sh command: %s", args)

	options := []string{"exec", dc.containerId, "mxzkcli.sh"}
	options = append(options, args...)

	out, err := dc.executor.Command("docker", options...).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("arstorClient exec command error: %s", out)
		return "", errors.New(message)
	}

	return string(out[:]), nil
}

func (dc *dockerCommand) execute(command string, args []string) (string, error) {
	klog.V(4).Infof("dockerCommand exec command: %s %s", command, args)
	options := []string{"exec", dc.containerId, "mxTool", "-c", command}
	options = append(options, args...)

	out, err := dc.executor.Command("docker", options...).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("arstorClient exec command error: %s", out)
		return "", errors.New(message)
	}

	return string(out[:]), nil
}

func (dc *dockerCommand) Truncate(volumeFile string, newSize int64) (string, error) {
	klog.V(4).Infof("dockerCommand truncate volumeFile %s, newSize %s", volumeFile, newSize)

	if len(volumeFile) == 0 {
		message := fmt.Sprintf("Cloning volume args error.")
		return "", errors.New(message)
	}

	args := []string{}
	args = append(args, volumeFile)
	args = append(args, "-s")
	args = append(args, strconv.FormatInt(newSize, 10))

	result, err := dc.executor.Command("truncate", args...).CombinedOutput()
	if err != nil {
		return "", err
	}
	klog.V(4).Infof("dockerCommand truncate result: %s", result)
	return string(result[:]), nil
}

func (dc *dockerCommand) Clone(srcPath string, destPath string) (string, error) {
	klog.V(4).Infof("dockerCommand clone srcPath %s, destPath %s", srcPath, destPath)

	if len(srcPath) == 0 || len(destPath) == 0 {
		message := fmt.Sprintf("Cloning volume args error.")
		return "", errors.New(message)
	}

	args := []string{}
	arg := srcPath + ":" + destPath
	args = append(args, arg)

	result, err := dc.execute("createclone", args)
	if err != nil {
		return "", err
	}
	klog.V(4).Infof("dockerCommand clone result: %s", result)
	return result, nil
}

func (dc *dockerCommand) Snapshot(srcPath string, snapshotPath string) (string, error) {
	klog.V(4).Infof("dockerCommand snapshot srcPath %s, snapshotPath %s", srcPath, snapshotPath)

	if len(srcPath) == 0 || len(snapshotPath) == 0 {
		message := fmt.Sprintf("Snapshoting volume args error.")
		return "", errors.New(message)
	}

	args := []string{}
	arg := srcPath + ":" + snapshotPath
	args = append(args, arg)

	result, err := dc.execute("createsnapshot", args)
	if err != nil {
		return "", err
	}
	klog.V(4).Infof("dockerCommand snapshot result: %s", result)
	return result, nil
}

func (dc *dockerCommand) DeleteSnapshot(path string) error {
	klog.Infof("dockerCommand DeleteSnapshot %s", path)

	args := []string{}
	args = append(args, path)

	result, err := dc.execute("deletesnapshot", args)
	if err != nil {
		return err
	}
	klog.V(4).Infof("dockerCommand snapshot result: %s", result)

	return nil
}

//id <string>: name of creating file
//size <long int>: the size of creating file
//dir_inode <long int>: the inode of directory where file put
//pagesize <int>: file page size, default: 8196
//compression <string>: the compression algorithm, default:lz4 'None' is closed compression
//read_cache: the switch of turning on or off read_cache
//mirroring: the replicas of creating file, default: 2
func (dc *dockerCommand) Create(id string, size int64, dirInode uint64,
	pagesize int, compression string, readCache bool, mirroring int) (string, error) {
	klog.V(4).Infof("dockerCommand CreateVolume")

	if pagesize == 0 {
		pagesize = 8192
	} else {
		exist, _ := Contains(pagesize, PageSize)
		if !exist {
			message := fmt.Sprintf("Creating volume args error: pagesize %s", pagesize)
			return "", errors.New(message)
		}
	}

	if len(compression) == 0 {
		compression = "lz4"
	} else {
		exist, _ := Contains(compression, CompressionMap)
		if !exist {
			message := fmt.Sprintf("Creating volume args error: compression %s", compression)
			return "", errors.New(message)
		}
	}

	compressionAlgorithm := CompressionMap[compression]

	readCacheStr := strings.ToLower(strconv.FormatBool(readCache))
	if mirroring == 0 {
		mirroring = 2
	} else {
		exist, _ := Contains(mirroring, Mirroring)
		if !exist {
			message := fmt.Sprintf("Creating volume args error: mirroring %s", mirroring)
			return "", errors.New(message)
		}
	}

	policy := fmt.Sprintf("localFS: {{pageSize: %d, compression: %s, "+
		"readCache: %s, compressionAlgorithm: %s}}, mirroring: {{numberOfMirrors: %d}}",
		pagesize, "true", readCacheStr, compressionAlgorithm, mirroring)

	args := []string{}
	args = append(args, id)
	args = append(args, strconv.FormatInt(size, 10))
	args = append(args, strconv.FormatUint(dirInode, 10))
	args = append(args, policy)

	file, err := dc.execute("createfile", args)
	if err != nil {
		return "", err
	}
	return file, nil
}

func (dc *dockerCommand) Delete(path string) error {
	klog.Infof("dockerCommand DeleteVolume %s", path)

	err := DeleteFile(path)
	if err != nil {
		return err
	}

	return nil
}

func (dc *dockerCommand) GetVolumeObject(inode uint64) (string, error) {
	klog.V(4).Infof("dockerCommand truncate")

	prefix := "/ArStor/Namespace/NSROOT/iBucket-"
	bucket := fmt.Sprintf("%x", inode%1000)
	bInode := fmt.Sprintf("%x", inode)
	prefix = prefix + bucket + "/" + bInode

	args := []string{}
	args = append(args, "cat")
	args = append(args, prefix)

	result, err := dc.executeMxzkcli(args)
	if err != nil {
		return "", err
	}
	return result, nil
}
