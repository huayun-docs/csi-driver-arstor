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

	"bytes"
	docker "github.com/fsouza/go-dockerclient"
	"k8s.io/klog"
	"os"
)

type dockerClient struct {
	client      *docker.Client
	containerId string
	executor    utilexec.Interface
}

func getDockerClient(dockerUrl string) (*docker.Client, error) {
	klog.Infof("Connect docker: %s", dockerUrl)

	client, err := docker.NewClient(dockerUrl)
	if err != nil {
		return nil, fmt.Errorf("can not get docker client from url %s: %v", dockerUrl, err)
	}

	return client, nil
}

func NewDockerClient(dockerUrl string, containerId string) (*dockerClient, error) {
	client, err := getDockerClient(dockerUrl)
	if err != nil {
		return nil, err
	}

	executor := utilexec.New()
	return &dockerClient{
		client:      client,
		containerId: containerId,
		executor:    executor,
	}, nil
}

func (dc *dockerClient) CheckArStorContainer() error {
	klog.V(4).Infof("CheckArStorContainer %s", dc.containerId)
	container, err := dc.client.InspectContainer(dc.containerId)
	if err != nil {
		return err
	}
	//klog.V(4).Infof("CheckArStorContainer the container %s info :%v", dc.containerId, *container)

	status := container.State.Running

	if !status {
		message := fmt.Sprintf("CheckArStorContainer: the container %s is not running", dc.containerId)
		return errors.New(message)
	}

	return nil
}

func (dc *dockerClient) dockerExec(args []string) (string, error) {
	// create exec
	klog.V(4).Infof("debug dockerClient: docker exec %s %s", dc.containerId, args)
	createOpts := docker.CreateExecOptions{}
	createOpts.AttachStdin = true
	createOpts.AttachStdout = true
	createOpts.Tty = true
	createOpts.Cmd = args
	createOpts.Container = dc.containerId
	exec, err := dc.client.CreateExec(createOpts)
	if err != nil {
		return "", fmt.Errorf("failed to create exec %v", err)
	}
	// start exec
	errorBuf := &bytes.Buffer{}
	outputBuf := &bytes.Buffer{}
	startOpts := docker.StartExecOptions{}
	startOpts.Tty = true
	startOpts.RawTerminal = true
	startOpts.Detach = false
	startOpts.ErrorStream = errorBuf
	//startOpts.InputStream = os.Stdin
	startOpts.OutputStream = outputBuf
	err = dc.client.StartExec(exec.ID, startOpts)
	if err != nil {
		message := fmt.Sprintf("failed to start exec %v", err)
		klog.Error(message)
		return "", fmt.Errorf("%s", message)
	}

	info, err := dc.client.InspectExec(exec.ID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect exec %v", err)
	}

	message := fmt.Sprintf("result: %s, reason: docker exec %s %s, its exit code is %d, error %v,",
		outputBuf.String(), dc.containerId, args, info.ExitCode, errorBuf.String())
	klog.V(4).Infof("debug dockerClient result: %s", message)

	if info.ExitCode != 0 {
		return "", fmt.Errorf("failed: %s", message)
	}

	return fmt.Sprintf("%s", outputBuf.String()), nil
}

func (dc *dockerClient) executeMxzklist(args []string) (string, error) {
	klog.V(4).Infof("dockerClient zklist command: %s", args)

	options := []string{"zklist"}
	options = append(options, args...)

	out, err := dc.dockerExec(options)
	if err != nil {
		message := fmt.Sprintf("dockerClient executeMxzklist error: %s, %v", out, err)
		return "", errors.New(message)
	}

	return string(out[:]), nil
}

func (dc *dockerClient) GetVolumeAttribute(volumeInode uint64) (string, error) {
	klog.V(4).Infof("dockerClient GetVolumeAttribute volumeinode %d", volumeInode)
	args := []string{}
	args = append(args, "-i")
	args = append(args, strconv.FormatUint(volumeInode, 10))
	args = append(args, "-r")

	result, err := dc.executeMxzklist(args)
	if err != nil {
		return "", err
	}
	klog.V(4).Infof("dockerClient GetVolumeAttribute result: %s", result)
	return result, nil
}

func (dc *dockerClient) GetSnapshotAttribute(snapshotInode uint64) (string, error) {
	klog.V(4).Infof("dockerClient GetSnapshotAttribute snapshotInode %s", snapshotInode)
	args := []string{}
	args = append(args, "-i")
	args = append(args, strconv.FormatUint(snapshotInode, 10))
	args = append(args, "-r")

	result, err := dc.executeMxzklist(args)
	if err != nil {
		return "", err
	}
	klog.V(4).Infof("dockerClient GetSnapshotAttribute result: %s", result)
	return result, nil
}

func (mt *dockerClient) GetSnapshotSrcvolumeId(snapshotInode uint64) (string, error) {
	//	volume-f61ff4d8-6fac-4445-b319-589ada9c5927 : 113766 (0x1bc66) : UPTODATE
	//snapshots:
	//	|-> NoName-126834 : 126834 (0x1ef72) : UPTODATE
	//	|	clones:
	//	|	|-> pfytest_clone_ok : 89044 (0x15bd4) : UPTODATE
	klog.V(4).Infof("dockerClient GetMxSnapshotSrcvolume snapshotInode %s", snapshotInode)
	args := []string{}
	args = append(args, "-i")
	args = append(args, strconv.FormatUint(snapshotInode, 10))
	args = append(args, "--strand")

	result, err := mt.executeMxzklist(args)
	if err != nil {
		return "", err
	}

	arr := strings.Split(result, ":")
	klog.V(4).Infof("dockerClient GetMxSnapshotSrcvolume response %v", arr)

	idName := strings.Split(arr[0], ".")
	return idName[0], nil
}

func (dc *dockerClient) executeMxzkcli(args []string) (string, error) {
	klog.V(4).Infof("dockerClient mxzkcli.sh command: %s", args)

	options := []string{"mxzkcli.sh"}
	options = append(options, args...)

	out, err := dc.dockerExec(options)
	if err != nil {
		message := fmt.Sprintf("arstorClient executeMxzkcli command error: %s, %v", out, err)
		return "", errors.New(message)
	}

	return string(out[:]), nil
}

func (dc *dockerClient) execute(command string, args []string) (string, error) {
	klog.V(4).Infof("dockerClient exec command: %s %s", command, args)
	options := []string{"mxTool", "-c", command}
	options = append(options, args...)

	out, err := dc.dockerExec(options)
	if err != nil {
		message := fmt.Sprintf("arstorClient exec command error: %s, %v", out, err)
		return "", errors.New(message)
	}

	return string(out[:]), nil
}

// bash truncate
func (dc *dockerClient) Truncate(volumeFile string, newSize int64) (string, error) {
	klog.V(4).Infof("dockerClient truncate volumeFile %s, newSize %d", volumeFile, newSize)

	if len(volumeFile) == 0 {
		message := fmt.Sprintf("Cloning volume args error.")
		return "", errors.New(message)
	}

	args := []string{}
	args = append(args, volumeFile)
	args = append(args, "-s")
	args = append(args, strconv.FormatInt(newSize, 10))

	result, err := dc.executor.Command("truncate", args...).CombinedOutput()
	klog.V(4).Infof("dockerClient truncate result: %s", result)
	if err != nil {
		return "", fmt.Errorf("failed to truncate file %s: %v", volumeFile, err)
	}
	return string(result[:]), nil
}

func (dc *dockerClient) Clone(srcPath string, destPath string) (string, error) {
	klog.V(4).Infof("dockerClient clone srcPath %s, destPath %s", srcPath, destPath)

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
	klog.V(4).Infof("dockerClient clone result: %s", result)
	return result, nil
}

func (dc *dockerClient) Snapshot(srcPath string, snapshotPath string) (string, error) {
	klog.V(4).Infof("dockerClient snapshot srcPath %s, snapshotPath %s", srcPath, snapshotPath)

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
	klog.V(4).Infof("dockerClient snapshot result: %s", result)
	return result, nil
}

func (dc *dockerClient) DeleteSnapshot(path string) error {
	klog.Infof("dockerClient DeleteSnapshot %s", path)

	if len(path) == 0 {
		message := fmt.Sprintf("Snapshoting args error, path is empty.")
		return errors.New(message)
	}

	args := []string{}
	args = append(args, path)

	result, err := dc.execute("deletesnapshot", args)
	if err != nil {
		return err
	}

	// remove file
	err = DeleteFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			message := fmt.Sprintf("failed to do DeleteSnapshot file %s, err: %v", path, err)
			return errors.New(message)
		}
	}

	klog.V(4).Infof("dockerClient DeleteSnapshot result: %s", result)

	return nil
}

//id <string>: name of creating file
//size <long int>: the size of creating file
//dir_inode <long int>: the inode of directory where file put
//pagesize <int>: file page size, default: 8196
//compression <string>: the compression algorithm, default:lz4 'None' is closed compression
//read_cache: the switch of turning on or off read_cache
//mirroring: the replicas of creating file, default: 2
func (dc *dockerClient) Create(id string, size int64, dirInode uint64,
	pagesize int, compression string, readCache bool, mirroring int) (string, error) {
	klog.V(4).Infof("dockerClient CreateVolume")

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

	policy := fmt.Sprintf("localFS: {pageSize: %d, compression: %s, "+
		"readCache: %s, compressionAlgorithm: %s}, mirroring: {numberOfMirrors: %d}",
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

func (dc *dockerClient) Delete(path string) error {
	klog.Infof("dockerClient DeleteVolume %s", path)

	err := DeleteFile(path)
	if err != nil {
		return err
	}

	return nil
}

func (dc *dockerClient) GetVolumeObject(inode uint64) (string, error) {
	klog.V(4).Infof("dockerClient GetVolumeObject")

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
