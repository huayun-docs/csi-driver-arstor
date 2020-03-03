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
	"errors"
	"fmt"
	"strings"

	"bytes"
	"github.com/pborman/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
	utilexec "k8s.io/utils/exec"
	"os"

	"k8s.io/apimachinery/pkg/util/wait"
	"time"
)

type deviceType int

const (
	losetupPath = "losetup"

	modeBlock deviceType = iota
	modeFile
	modeUnsupported

	//ErrDeviceNotFound defines "device not found"
	ErrDeviceNotFound = "device not found"
	//ErrDeviceNotSupported defines "device not supported"
	ErrDeviceNotSupported = "device not supported"
	//ErrNotAvailable defines "not available"
	ErrNotAvailable = "not available"

	partitionPrefix = "p"
	mapperPrefix    = "/dev/mapper/"
	loopPrefix      = "/dev/"
)

type LoopDeviceManager struct {
	executor utilexec.Interface
}

func NewLoopDeviceManager() (*LoopDeviceManager, error) {
	executor := utilexec.New()
	return &LoopDeviceManager{
		executor: executor,
	}, nil
}

func (ldm *LoopDeviceManager) RunDetachLostLoopDevice(workers int, period time.Duration, stopCh <-chan struct{}) {

	klog.Infof("Starting DetachLostLoopDevice")
	defer klog.Infof("Shutting DetachLostLoopDevice")

	for i := 0; i < workers; i++ {
		go wait.Until(ldm.DetachLostLoopDevice, period, stopCh)
	}

	<-stopCh
}

func (ldm *LoopDeviceManager) DetachLostLoopDevice() {
	klog.Infof("PERIOD: DetachLostLoopDevice for arstor driver")

	pathPrefix := KubernetesPath + VolumePath + "/" + VolumePrefix
	lostPrefix := "/.nfs"

	options := []string{}
	options = append(options, "-l")
	output, err := ldm.executor.Command("losetup", options...).CombinedOutput()
	if err != nil {
		klog.Errorf("PERIOD: AttachLoopDevice output: %s error: %v", string(output[:]), err)
		return
	}
	klog.Infof("PERIOD: list loop device result: %s", string(output[:]))

	lostLoopDevices := make(map[string]string)
	lines := strings.Split(string(output[:]), "\n")
	for _, line := range lines {
		klog.Infof("PERIOD: check %s", line)
		if index := strings.Index(line, pathPrefix); index < 0 {
			continue
		}
		if index := strings.Index(line, lostPrefix); index > 0 {
			info := strings.Split(line, " ")
			for _, file := range info {
				if index := strings.Index(file, lostPrefix); index > 0 {
					lostLoopDevices[info[0]] = file
				}
			}
		}
	}
	if len(lostLoopDevices) == 0 {
		klog.Infof("PERIOD: there is no lost loop device.")
		return
	}

	klog.Warningf("PERIOD: the loop device map file: %s", lostLoopDevices)

	// detach
	errMessage := ""
	successKeys := []string{}
	failedKeys := []string{}
	for lostLoopDevice, file := range lostLoopDevices {
		err := ldm.DetachLoopDevice(lostLoopDevice)
		if err != nil {
			message := fmt.Sprintf("failed to datach lost loop device %s err: %v\n", lostLoopDevice, err)
			errMessage = errMessage + message
			failedKeys = append(failedKeys, lostLoopDevice)
			continue
		}

		err = DeleteFile(file)
		if err != nil {
			if !os.IsNotExist(err) {
				message := fmt.Sprintf("failed to delete file %s err: %v\n", file, err)
				errMessage = errMessage + message
				failedKeys = append(failedKeys, lostLoopDevice)
				continue
			}
		}

		successKeys = append(successKeys, lostLoopDevice)
	}

	if len(successKeys) != 0 {
		klog.Infof("PERIOD: detach lost loop device (%s) successfully", successKeys)
	}
	if len(errMessage) != 0 {
		klog.Errorf("PERIOD: failed to detach lost loop devices (%s) err:\n %s", failedKeys, errMessage)
	}

	return
}

func (ldm *LoopDeviceManager) DetachLostLoopDeviceByVolumeId(volumeId string) error {
	klog.Infof("DetachLostLoopDeviceByVolumeId volumeId %s", volumeId)

	pathPrefix := KubernetesPath + VolumePath + "/" + VolumePrefix

	options := []string{}
	options = append(options, "-l")
	output, err := ldm.executor.Command("losetup", options...).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("AttachLoopDevice output: %s error: %v", string(output[:]), err)
		return errors.New(message)
	}
	klog.Infof("list loop device result: %s", string(output[:]))

	loopDevice := ""
	lines := strings.Split(string(output[:]), "\n")
	for _, line := range lines {
		if index := strings.Index(line, pathPrefix); index < 0 {
			continue
		}
		if index := strings.Index(line, volumeId); index > 0 {
			info := strings.Split(line, " ")
			loopDevice = info[0]
			break
		}
	}
	if len(loopDevice) == 0 {
		klog.Infof("the loop device of volumeid %s has been deleted.", volumeId)
		return nil
	}

	klog.Infof("get loop device %s by volumeid %s", loopDevice, volumeId)
	err = ldm.DetachLoopDevice(loopDevice)
	if err != nil {
		return err
	}

	return nil
}

func (ldm *LoopDeviceManager) GetFreeLoopDevice() (string, error) {
	klog.Infof("GetFreeLoopDevice")
	// get free loop device
	options := []string{"-f"}
	out, err := ldm.executor.Command("losetup", options...).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("GetFreeLoopDevice output: %s error: %v", string(out[:]), err)
		return "", errors.New(message)
	}

	klog.Infof("GetFreeLoopDevice result: %s", string(out[:]))
	str := strings.Trim(string(out[:]), "\n")
	return str, nil
}

func (ldm *LoopDeviceManager) AttachLoopDevice(loopDevice string, volumeFile string) error {
	klog.Infof("AttachLoopDevice loopDevice %s volumeFile %s", loopDevice, volumeFile)
	// mount
	options := []string{}
	options = append(options, loopDevice)
	options = append(options, volumeFile)
	output, err := ldm.executor.Command("losetup", options...).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("AttachLoopDevice output: %s error: %v", string(output[:]), err)
		return errors.New(message)
	}
	klog.Infof("AttachLoopDevice result: %s %s %s", loopDevice, volumeFile, string(output[:]))
	return nil
}

func (ldm *LoopDeviceManager) DetachLoopDevice(loopDevice string) error {
	klog.Infof("DetachLoopDevice loopDevice %s", loopDevice)
	// unmount
	options := []string{}
	options = append(options, "-d")
	options = append(options, loopDevice)
	output, err := ldm.executor.Command("losetup", options...).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("DetachLoopDevice output: %s error: %v", string(output[:]), err)
		return errors.New(message)
	}
	klog.Infof("DetachLoopDevice result: %s", string(output[:]))
	return nil
}

func (ldm *LoopDeviceManager) ListLoopDeviceByFile(volumeFile string) ([]string, error) {
	klog.Infof("ListLoopDeviceByFile volumeFile %s", volumeFile)
	//test
	//out, err := ldm.executor.Command("ls", volumeFile).CombinedOutput()
	//klog.Infof("ListDevice ls result: %s, %v", string(out[:]), err)
	//out, err = ldm.executor.Command("losetup", "-h").CombinedOutput()
	//klog.Infof("ListDevice losetup result: %s, %v", string(out[:]), err)

	// list loop device
	options := []string{}
	options = append(options, "-j")
	options = append(options, volumeFile)

	output, err := ldm.executor.Command("losetup", options...).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("ListLoopDeviceByFile output: %s error: %v", string(output[:]), err)
		return []string{}, errors.New(message)
	}
	klog.Infof("ListDevice file %s result: %s", volumeFile, string(output[:]))
	if len(output) == 0 {
		return []string{}, nil
	}

	loopDevices := strings.Split(string(output[:]), ":")
	names := []string{}
	for _, v := range loopDevices {
		if index := strings.Index(v, "loop"); index > 0 {
			name := strings.Split(v, " ")[0]
			names = append(names, strings.Trim(name, "\n"))
		}
	}

	klog.Infof("ListDevice file %s result device names: %s", volumeFile, names)
	return names, nil
}

func parseLosetupOutputForDevice(output []byte) (string, error) {
	if len(output) == 0 {
		return "", errors.New(ErrDeviceNotFound)
	}

	// losetup returns device in the format:
	// /dev/loop1: [0073]:148662 (/volumes/308f14af-cf0a-08ff-c9c3-b48104318e05)
	device := strings.TrimSpace(strings.SplitN(string(output), ":", 2)[0])
	if len(device) == 0 {
		return "", errors.New(ErrDeviceNotFound)
	}
	return device, nil
}

func (ldm *LoopDeviceManager) GetDevicePartition(loopDevice string) (string, error) {
	klog.Infof("GetDevicePartition loopDevice %s", loopDevice)

	// get device partition by fdisk -l

	option := "fdisk -l " + loopDevice + " | grep  '^/dev/loop' | awk '{print $1}'"
	output, err := ldm.executor.Command("sh", "-c", option).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("GetDevicePartition  output: %s error: %v", string(output[:]), err)
		return "", errors.New(message)
	}
	klog.Infof("GetDevicePartition result: %s", string(output[:]))

	if len(string(output[:])) == 0 {
		klog.Infof("the device %s has no partition", loopDevice)
		return "", nil
	}

	arr := strings.Split(string(output[:]), " ")
	if len(arr) == 0 {
		klog.Infof("the device %s has no partition", loopDevice)
		return "", nil
	}
	if len(arr) > 1 {
		klog.Warningf("GetDevicePartition loopDevice %s has multiple partition, just use the first partition", loopDevice)
	}

	klog.Infof("GetDevicePartition get partition: %s", arr[0])
	return strings.Trim(arr[0], "\n"), nil
}

func (ldm *LoopDeviceManager) FdiskDevice(loopDevice string) error {
	klog.Infof("FdiskDevice loopDevice %s", loopDevice)
	// args just for shell
	tmpFielPath := "/tmp/" + uuid.NewUUID().String()
	buf := bytes.Buffer{}
	buf.WriteString("n\n")
	buf.WriteString("p\n")
	buf.WriteString("1\n")
	buf.WriteString("\n")
	buf.WriteString("\n")
	buf.WriteString("w\n")
	err := WriteFile(tmpFielPath, buf)
	if err != nil {
		return err
	}
	defer DeleteFile(tmpFielPath)

	// check
	output, err := ldm.executor.Command("cat", tmpFielPath).CombinedOutput()
	klog.Infof("FdiskDevice cat %s result: %s err:%v", tmpFielPath, string(output[:]), err)

	// partition
	option := "cat " + tmpFielPath + " | fdisk " + loopDevice
	output, err = ldm.executor.Command("sh", "-c", option).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("FdiskDevice  output: %s error: %v", string(output[:]), err)
		klog.Warningf("%s", message)

		skip := "Created a new partition"
		if index := strings.Index(string(output[:]), skip); index > 0 {
			return nil
		}

		return errors.New(message)
	}
	klog.Infof("FdiskDevice %s result: %s", loopDevice, string(output[:]))

	return nil
}

func (ldm *LoopDeviceManager) AddDevmapping(loopDevice string) (string, error) {
	klog.Infof("AddDevmapping volumeFile %s", loopDevice)
	// mount

	option := "kpartx -av " + loopDevice + " | awk '{print $3}' "
	output, err := ldm.executor.Command("sh", "-c", option).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("AddDevmapping  output: %s error: %v", string(output[:]), err)
		return "", errors.New(message)
	}
	klog.Infof("AddDevmapping result: %s", string(output[:]))

	return strings.Trim(string(output[:]), "\n"), nil
}

func (ldm *LoopDeviceManager) DeleteDevmapping(loopDevice string) error {
	klog.Infof("DeleteDevmapping volumeFile %s", loopDevice)
	// mount
	options := []string{}
	options = append(options, "-d")
	options = append(options, loopDevice)
	output, err := ldm.executor.Command("kpartx", options...).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("DeleteDevmapping  output: %s error: %v", string(output[:]), err)
		return errors.New(message)
	}
	klog.Infof("DeleteDevmappingresult: %s", string(output[:]))
	return nil
}

func isLoopDevice(device string) bool {
	return strings.HasPrefix(device, "/dev/loop")
}

func isMapperDevice(device string) bool {
	return strings.HasPrefix(device, "/dev/mapper")
}

func (ldm *LoopDeviceManager) CheckPathDeviceType(path string, dt deviceType) error {
	klog.Infof("CheckPathDeviceType path %s", path)
	result, err := ldm.GetPathDeviceType(path)
	if err != nil {
		return err
	}

	if result != dt {
		message := fmt.Sprintf("the device type of path %s is %v, but args is %v", path, result, dt)
		return errors.New(message)
	}

	return nil
}

// pathMode returns the FileMode for a path.
func (ldm *LoopDeviceManager) GetPathDeviceType(path string) (deviceType, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return modeUnsupported, err
	}
	klog.Infof("GetPathDeviceType path %s info: %v", path, fi)
	switch mode := fi.Mode(); {
	case mode&os.ModeDevice != 0:
		klog.Infof("GetPathDeviceType path %s is block device", path)
		return modeBlock, nil
	case mode.IsRegular():
		klog.Infof("GetPathDeviceType path %s is file device", path)
		return modeFile, nil
	default:
		klog.Infof("GetPathDeviceType path %s is invalid device", path)
		return modeUnsupported, nil
	}
}

func (ldm *LoopDeviceManager) GetMapperDevice(name string) (string, error) {
	klog.Infof("GetMapperDevice name %s", name)
	devicePath := mapperPrefix + name

	option := "ls -l " + devicePath
	output, err := ldm.executor.Command("sh", "-c", option).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("GetMapperDevice  output: %s error: %v", string(output[:]), err)
		return "", errors.New(message)
	}
	//check block
	info := strings.Split(string(output[:]), " ")

	if index := strings.Index(info[0], "b"); index < 0 {
		message := fmt.Sprintf("the file %s is not a block device", devicePath)
		return "", errors.New(message)
	}

	return devicePath, nil
}

func (ldm *LoopDeviceManager) EnsureFsType(mapperDevice string, fsType string) error {
	klog.Infof("EnsureFsType mapperDevice %s", mapperDevice)

	// get fsType
	fs, err := ldm.GetFsType(mapperDevice)
	if err != nil {
		return err
	}

	if len(fs) == 0 {
		klog.Infof("EnsureFsType mapperDevice %s, Current it has no fsType", mapperDevice)
	} else {
		klog.Infof("EnsureFsType mapperDevice %s, Current fsType: %s", mapperDevice, fs)
	}
	if fs == fsType {
		return nil
	}

	klog.Warningf("the fstype of device %s should be changed: %s --> %s", mapperDevice, fs, fsType)

	err = ldm.SetFsType(mapperDevice, fsType)
	if err != nil {
		return err
	}

	return nil
}

func (ldm *LoopDeviceManager) GetFsType(mapperDevice string) (string, error) {
	klog.Infof("GetFsType mapperDevice %s", mapperDevice)

	option := "file -s " + mapperDevice
	output, err := ldm.executor.Command("sh", "-c", option).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("GetFsType  output: %s error: %v", string(output[:]), err)
		return "", errors.New(message)
	}
	// # file -s /dev/mapper/loop18p1
	// /dev/mapper/loop18p1: SGI XFS filesystem data (blksz 4096, inosz 512, v2 dirs)
	// # file -s /dev/mapper/loop18p1
	// /dev/mapper/loop18p1: Linux rev 1.0 ext4 filesystem data, UUID=7c661838-4d65-4455-92df-505089f941ce (extents) (64bit) (large files) (huge files)
	// # file -s /dev/mapper/loop18p1
	// /dev/mapper/loop18p1: Linux rev 1.0 ext3 filesystem data, UUID=a2591ec1-ccc6-45f6-a70e-502c710587f0 (large files)

	result := string(output[:])
	if index := strings.Index(result, "ext3"); index > 0 {
		return "ext3", nil
	}
	if index := strings.Index(result, "ext4"); index > 0 {
		return "ext4", nil
	}
	if index := strings.Index(result, "XFS"); index > 0 {
		return "xfs", nil
	}

	message := fmt.Sprintf("the fstype of device %s is not ext3/ext4/xfs", mapperDevice)
	klog.Infof("%s", message)
	return "", nil
}

func (ldm *LoopDeviceManager) SetFsType(mapperDevice string, fsType string) error {
	klog.Infof("SetFsType mapperDevice %s fsType %s", mapperDevice, fsType)

	message := ""

	switch fsType {
	case "ext3":
		option := "mkfs.ext3 -F -m0 " + mapperDevice
		output, err := ldm.executor.Command("sh", "-c", option).CombinedOutput()
		if err != nil {
			message = fmt.Sprintf("SetFsType  output: %s error: %v", string(output[:]), err)
		}
	case "ext4":
		option := "mkfs.ext4 -F -m0 " + mapperDevice
		output, err := ldm.executor.Command("sh", "-c", option).CombinedOutput()
		if err != nil {
			message = fmt.Sprintf("SetFsType  output: %s error: %v", string(output[:]), err)
		}
	case "xfs":
		option := "mkfs.xfs -f " + mapperDevice
		output, err := ldm.executor.Command("sh", "-c", option).CombinedOutput()
		if err != nil {
			message = fmt.Sprintf("SetFsType  output: %s error: %v", string(output[:]), err)
		}
	default:
		message = fmt.Sprintf("invalid fstype %s", fsType)
	}

	if len(message) != 0 {
		return errors.New(message)
	}

	return nil
}

func (ldm *LoopDeviceManager) ListDevmapperName(loopDevice string) ([]string, error) {
	klog.Infof("ListDevmapping volumeFile %s", loopDevice)
	// mount
	option := "kpartx -lv " + loopDevice + " | awk '{print $1}' "
	output, err := ldm.executor.Command("sh", "-c", option).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("ListDevmapping  output: %s error: %v", string(output[:]), err)
		return []string{}, errors.New(message)
	}
	klog.Infof("ListDevmapping result: %s", string(output[:]))

	devices := strings.Split(string(output[:]), "\n")
	names := []string{}
	for _, v := range devices {
		name := strings.Trim(v, "\n")
		names = append(names, strings.TrimSpace(name))
	}
	klog.Infof("ListDevmapping result loopdevicesname: %s", names)
	return names, nil
}

func (ldm *LoopDeviceManager) GetLoopDeviceByMapperDevice(mapperDevice string) (string, error) {
	klog.V(4).Infof("GetLoopDeviceByMapperDevice mapperDevice %s", mapperDevice)

	mapperName := strings.TrimLeft(mapperDevice, mapperPrefix)
	index := strings.LastIndex(mapperName, partitionPrefix)
	loopName := mapperName[0:index]
	loopDevice := loopPrefix + loopName
	klog.V(4).Infof("get loop device %s from mapper device %s", loopDevice, mapperDevice)

	// just check
	names, err := ldm.ListDevmapperName(loopDevice)
	if err != nil {
		return "", err
	}
	if names[0] != mapperName {
		message := fmt.Sprintf("failed to get loop device by mapper Device %s", mapperDevice)
		return "", status.Error(codes.Internal, message)
	}

	return loopDevice, nil
}

func (ldm *LoopDeviceManager) GetLoopDeviceByMountPoint(stagingTargetPath string) (string, error) {
	klog.V(4).Infof("GetMapperDeviceByMountPoint stagingTargetPath %s", stagingTargetPath)

	option := " findmnt -o source --noheadings --target " + stagingTargetPath
	output, err := ldm.executor.Command("sh", "-c", option).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("GetMapperDeviceByMountPoint findmnt output: %s error: %v", string(output[:]), err)
		return "", status.Errorf(codes.Internal, "Could not determine device path: %s", message)
	}

	devicePath := strings.TrimSpace(string(output))
	klog.V(4).Infof("GetMapperDeviceByMountPoint result %s", devicePath)

	return devicePath, nil
}

func (ldm *LoopDeviceManager) ExpandLoopDevice(loopDevice string) error {
	klog.V(4).Infof("ResizeMapperDevice mapperDevice %s", loopDevice)

	option := " losetup -c " + loopDevice
	output, err := ldm.executor.Command("sh", "-c", option).CombinedOutput()
	if err != nil {
		message := fmt.Sprintf("ResizeMapperDevice losetup output: %s error: %v", string(output[:]), err)
		return status.Errorf(codes.Internal, "Could not resize device path: %s", message)
	}

	return nil
}
