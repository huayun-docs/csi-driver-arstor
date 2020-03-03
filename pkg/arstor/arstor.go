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

	"github.com/golang/protobuf/ptypes/timestamp"
	"k8s.io/apimachinery/pkg/util/wait"
	"os"

	"github.com/golang/protobuf/ptypes"
	"github.com/huayun-docs/csi-driver-arstor/pkg/arstor/tool"
	"k8s.io/klog"
	"time"
)

// arstor volume path: /arstor_root_path/kubernetes/volumes/volume_xxx
// arstor snapshot path: /arstor_root_path/kubernetes/snapshots/snapshot_xxx
const (
	KubernetesPath string = "/kubernetes"
	VolumePath     string = "/volumes"
	SnapshotPath   string = "/snapshots"
	VolumePrefix   string = "volume_"
	SnapshotPrefix string = "snapshot_"

	FileNameMAX = 255
	MaxDirCount = 1000
)

// arstor config options
type ArStorConfig struct {
	ArStorMountPoint string // root mount point for arstor client
	ArStorShares     string
	MountAttamps     int
	ArStorContainer  string // the container id/name for arstor client
	MountHashDir     bool

	MountOption           string
	ArStorAlternateServer string

	DockerUrl string //csi driver connect arstor by docker
}

// the options for request of creating volume
type arstorCreateVolumeRequest struct {
	VolName        string     `json:"volName"`
	VolID          string     `json:"volID"`
	VolSize        int64      `json:"volSize"`
	VolPath        string     `json:"volPath"`
	VolAccessType  accessType `json:"volAccessType"`
	Ephemeral      bool       `json:"ephemeral"`
	VolInode       uint64     `json:"volInode"`
	VolPageSize    int        `json:"volPageSize"`
	VolCompression string     `json:"volCompression"`
	VolReadCache   bool       `json:"volReadCache"`
	VolMirroring   int        `json:"volMirroring"`
}

// snapshot option
type arstorSnapshot struct {
	Id           string              `json:"id"`
	Name         string              `json:"name"`
	VolumeId     string              `json:"volumeId"`
	Path         string              `json:"path"`
	CreationTime timestamp.Timestamp `json:"creationTime"`
	Size         int64               `json:"sizeBytes"`
	ReadyToUse   bool                `json:"readyToUse"`
}

type arstorVolume struct {
	Id   string `json:"id"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	Path string `json:"path"`
}

type ArStorClient struct {
	containerId string
	mountPath   string
	volumeDir   string
	snapshotDir string
	mountRetry  int

	// volume id -> volume
	volumes map[string]arstorVolume

	// snapshot id -> snapshot
	snapshots map[string]arstorSnapshot

	// store the full dir which managed by arstor, actually a dir cound have <1000 file
	fullDirs   map[string]bool
	arstorTool tool.Tool
}

func NewArStorClient(arstorConfig ArStorConfig) (*ArStorClient, error) {

	mountRetry := 3
	if arstorConfig.MountAttamps > 3 {
		mountRetry = arstorConfig.MountAttamps
	}

	mountPath := arstorConfig.ArStorMountPoint
	if arstorConfig.MountHashDir {
		if len(arstorConfig.ArStorShares) == 0 {
			message := fmt.Sprintf("Creating volume args error: mountHashDir %v arstorShares %s",
				arstorConfig.MountHashDir, arstorConfig.ArStorShares)
			return nil, errors.New(message)
		}
		mountPath = arstorConfig.ArStorMountPoint + "/" + arstorConfig.ArStorShares
	}

	exist, err := PathExists(mountPath)
	if err != nil {
		return nil, err
	}
	if !exist {
		message := fmt.Sprintf("the arstor mount point %s is not exist", mountPath)
		return nil, errors.New(message)
	}

	// ensure dir for volume and snapshot
	volumeDir := KubernetesPath + VolumePath
	snapshotDir := KubernetesPath + SnapshotPath
	err = EnsureDir(mountPath + volumeDir)
	if err != nil {
		return nil, err
	}
	err = EnsureDir(mountPath + snapshotDir)
	if err != nil {
		return nil, err
	}

	fullDirs := make(map[string]bool, MaxDirCount)

	arstorTool, err := tool.NewTool(tool.ToolParameters{
		ContainerId: arstorConfig.ArStorContainer,
		DockerUrl:   arstorConfig.DockerUrl,
	})
	if err != nil {
		return nil, err
	}

	return &ArStorClient{
		containerId: arstorConfig.ArStorContainer,
		mountPath:   mountPath,
		volumeDir:   volumeDir,
		snapshotDir: snapshotDir,
		mountRetry:  mountRetry,

		volumes:    make(map[string]arstorVolume, MaxDirCount),
		snapshots:  make(map[string]arstorSnapshot, MaxDirCount),
		fullDirs:   fullDirs,
		arstorTool: arstorTool,
	}, nil
}

func (mc *ArStorClient) Setup() {
	mc.LoadArStorData()
}

// load exist volumes and snaphosts
func (mc *ArStorClient) LoadArStorData() {

	volumeFiles := make(map[string]string)
	err := GetAllFile(mc.mountPath, mc.volumeDir, volumeFiles)
	if err != nil {
		klog.Errorf("Failed to initialize arstor volumes: %s", err.Error())
		os.Exit(1)
	}
	for file, path := range volumeFiles {
		// file = id . name
		arr := strings.Split(file, "_")
		if len(arr) != 2 {
			klog.Infof("the file(%s) is not a arstor volume, skip...", file)
			continue
		}
		id := arr[0]
		_, ok := mc.volumes[id]
		if ok {
			klog.Infof("the volume(%s) file(%s) is exist, skip...", id, file)
			continue
		}
		name := arr[1]
		size, err := GetFileSize(mc.mountPath + path)
		if err != nil {
			klog.Errorf("Failed to get file size: %s : %s", mc.mountPath+path, err.Error())
			os.Exit(1)
		}
		if size == 0 {
			klog.Infof("the size of file(%s) is 0, delete it", file)
			DeleteFile(file)
			continue
		}
		mc.volumes[id] = arstorVolume{
			Id:   id,
			Name: name,
			Size: size,
			Path: path,
		}
	}
	klog.Infof("arstorClient init volumes result %v", mc.volumes)

	// init snapshot
	snapshotFiles := make(map[string]string)
	err = GetAllFile(mc.mountPath, mc.snapshotDir, snapshotFiles)
	if err != nil {
		klog.Errorf("Failed to initialize arstor snapshots: %s", err.Error())
		os.Exit(1)
	}

	for file, path := range snapshotFiles {
		// file = id . name
		arr := strings.Split(file, "_")
		if len(arr) != 2 {
			klog.Infof("the file %s is not a snapshot file", mc.mountPath+path)
			continue
		}
		id := arr[0]
		_, ok := mc.snapshots[id]
		if ok {
			klog.Infof("the snapshot(%s) file(%s) is exist, skip...", id, file)
			continue
		}

		name := arr[1]

		size, err := GetFileSize(mc.mountPath + path)
		if err != nil {
			klog.Errorf("Failed to get file size: %s : %s", mc.mountPath+path, err.Error())
			os.Exit(1)
		}
		creationTime, err := GetFileCreationTime(mc.mountPath + path)
		if err != nil {
			klog.Errorf("Failed to get file created time: %s : %s", mc.mountPath+path, err.Error())
			os.Exit(1)
		}
		inode, err := GetPathInode(mc.mountPath + path)
		if err != nil {
			klog.Errorf("Failed to get file inode: %s : %s", mc.mountPath+path, err.Error())
			os.Exit(1)
		}
		volumeId, err := mc.arstorTool.GetSnapshotSrcvolumeId(inode)
		if err != nil {
			klog.Errorf("Failed to get snapshot file's volume id %s : %s", mc.mountPath+path, err.Error())
			os.Exit(1)
		}
		mc.snapshots[id] = arstorSnapshot{
			Id:           id,
			Name:         name,
			VolumeId:     volumeId,
			Path:         path,
			CreationTime: *creationTime,
			Size:         size,
			ReadyToUse:   true,
		}
	}
	klog.Infof("arstorClient init snapshot result %v", mc.snapshots)
}

func (mc *ArStorClient) addFullDir(mxDir string) {
	mc.fullDirs[mxDir] = true
}

func (mc *ArStorClient) removeFullDir(mxDir string) {
	if _, ok := mc.fullDirs[mxDir]; ok {
		mc.fullDirs[mxDir] = false
		delete(mc.fullDirs, mxDir)
	}
}

func (mc *ArStorClient) GetVolumeDevicePath(volumeId string) (string, bool) {

	srcVolumeFile, ok := mc.volumes[volumeId]
	if !ok {
		klog.Errorf("can not find the arstor path for volume %s", volumeId)
		return "", ok
	}

	klog.Infof("arstorClient GetVolumeDevicePath %s", mc.mountPath+srcVolumeFile.Path)
	return mc.mountPath + srcVolumeFile.Path, ok
}

func (mc *ArStorClient) GetSnapshotDevicePath(snapshotId string) (string, bool) {
	snapshot, ok := mc.snapshots[snapshotId]
	if !ok {
		klog.Errorf("can not find the arstor path for snapshot %s", snapshotId)
		return "", ok
	}

	klog.Infof("arstorClient GetSnapshotDevicePath %s", mc.mountPath+snapshot.Path)
	return mc.mountPath + snapshot.Path, ok
}

func (mc *ArStorClient) createVolume(volume *arstorCreateVolumeRequest, localDir string) (string, error) {
	var err error
	volume.VolInode, err = GetPathInode(localDir)
	if err != nil {
		return "", err
	}

	name := volume.VolID + "_" + volume.VolName
	volume.VolName = name
	klog.Infof("arstor CreateVolume request volume %v", volume)

	result, err := mc.arstorTool.Create(name, volume.VolSize, volume.VolInode, volume.VolPageSize,
		volume.VolCompression, volume.VolReadCache, volume.VolMirroring)
	if err != nil {
		return "", err
	}

	filePath := localDir + "/" + name
	exist, err := PathExists(filePath)
	if err != nil {
		return "", err
	}
	if !exist {
		message := fmt.Sprintf("arstor created file successfully, but the volume(%s) is not exist", filePath)
		return "", errors.New(message)
	}
	_, err = mc.arstorTool.Truncate(filePath, volume.VolSize)
	if err != nil {
		return "", err
	}
	klog.Infof("arstorClient CreateVolume result %s", result)
	return filePath, nil
}

func (mc *ArStorClient) CreateVolume(volume *arstorCreateVolumeRequest) error {
	klog.Infof("arstorClient CreateVolume %v", volume)

	switch volume.VolAccessType {
	case mountAccess:
		klog.Infof("arstorClient CreateVolume mountAccess type")
		break
	case blockAccess:
		klog.Infof("arstorClient CreateVolume blockAccess type")
		//return fmt.Errorf("unsupported access type %v", volume.VolAccessType)
	default:
		return fmt.Errorf("unsupported access type %v", volume.VolAccessType)
	}

	prefix := mc.volumeDir + "/" + VolumePrefix
	localDir, mxDir, err := mc.GenerateArStorDir(mc.mountPath, prefix, volume.VolID)
	if err != nil {
		return err
	}

	name := volume.VolID + "_" + volume.VolName
	if len(name) >= FileNameMAX {
		return fmt.Errorf("the volume name is too long %v", volume.VolName)
	}
	volumeCache := arstorVolume{
		Id:   volume.VolID,
		Name: volume.VolName,
		Size: volume.VolSize,
		Path: "",
	}
	mc.volumes[volume.VolID] = volumeCache
	_, err = mc.createVolume(volume, localDir)
	if err != nil {
		DeleteFile(localDir + "/" + name)
		delete(mc.volumes, volume.VolID)
		if IsFileFullErr(err) {
			mc.addFullDir(mxDir)
			localDir, mxDir, err = mc.ReGenerateArStorDir(mc.mountPath, prefix, volume.VolID)
			if err != nil {
				return err
			}
			_, err = mc.createVolume(volume, localDir)
			if err != nil {
				DeleteFile(localDir + "/" + name)
				delete(mc.volumes, volume.VolID)
				return err
			}
		} else {
			return err
		}
	}
	volumeCache.Path = mxDir + "/" + name
	mc.volumes[volume.VolID] = volumeCache

	klog.Infof("arstorClient CreateVolume result %v", volumeCache)
	volume.VolPath = mxDir + "/" + name

	return nil
}

func (mc *ArStorClient) WaitVolumeReady(volumeID string) error {
	backoff := wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   1.2,
		Steps:    10,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		ready, err := mc.volumeIsReady(volumeID)
		if err != nil {
			return false, err
		}
		return ready, nil
	})

	if err == wait.ErrWaitTimeout {
		err = fmt.Errorf("Timeout, Snapshot  %s is still not Ready %v", volumeID, err)
	}

	return err
}

func (mc *ArStorClient) volumeIsReady(volumeID string) (bool, error) {
	volume, ok := mc.volumes[volumeID]
	if !ok {
		return false, nil
	}
	if len(volume.Path) == 0 {
		return false, nil
	}

	return true, nil
}

func (mc *ArStorClient) CreateSnapshot(volumeId string, snapshotRequest *arstorSnapshot) error {
	klog.Infof("arstorClient CreateSnapshot %v", snapshotRequest)

	volumeFile, ok := mc.volumes[volumeId]
	if !ok {
		return fmt.Errorf("can not find the arstor path for volume %s", volumeId)
	}

	prefix := mc.snapshotDir + "/" + SnapshotPrefix
	localDir, mxDir, err := mc.GenerateArStorDir(mc.mountPath, prefix, snapshotRequest.Id)
	if err != nil {
		return err
	}

	name := snapshotRequest.Id + "_" + snapshotRequest.Name
	if len(name) >= FileNameMAX {
		return fmt.Errorf("the volume name is too long %v", snapshotRequest.Name)
	}

	mc.snapshots[snapshotRequest.Id] = *snapshotRequest
	_, err = mc.arstorTool.Snapshot(volumeFile.Path, mxDir+"/"+name)
	if err != nil {
		DeleteFile(localDir + "/" + name)
		delete(mc.snapshots, snapshotRequest.Id)
		if IsFileFullErr(err) {
			mc.addFullDir(mxDir)
			localDir, mxDir, err = mc.ReGenerateArStorDir(mc.mountPath, prefix, snapshotRequest.Id)
			if err != nil {
				return err
			}
			_, err = mc.arstorTool.Snapshot(volumeFile.Path, mxDir+"/"+name)
			if err != nil {
				DeleteFile(localDir + "/" + name)
				delete(mc.snapshots, snapshotRequest.Id)
				return err
			}
		} else {
			return err
		}
	}
	snapshotRequest.Path = mxDir + "/" + name
	snapshotRequest.ReadyToUse = true
	snapshotFile := mc.mountPath + mxDir + "/" + name
	if err = mc.WaitSnapshotReady(snapshotFile); err != nil {
		message := fmt.Sprintf("Failed to WaitSnapshotReady: %v", err)
		return errors.New(message)
	}
	snapshotRequest.Size, err = GetFileSize(snapshotFile)
	if err != nil {
		klog.Infof("arstorClient CreateSnapshot cat not get size from file %s: %v", snapshotFile, err)
	}

	mc.snapshots[snapshotRequest.Id] = *snapshotRequest
	klog.Infof("arstorClient CreateSnapshot result %v", snapshotRequest)
	return nil
}

func (mc *ArStorClient) ExpandVolume(volumeId string, newSize int64) error {
	klog.Infof("arstorClient ResizeVolume %s newSize %d", volumeId, newSize)

	volume, _ := mc.volumes[volumeId]

	filePath := mc.mountPath + "/" + volume.Path
	result, err := mc.arstorTool.Truncate(filePath, newSize)
	if err != nil {
		return err
	}

	volume.Size = newSize
	klog.Infof("arstorClient ResizeVolume result %v", result)

	return nil
}

func (mc *ArStorClient) WaitSnapshotReady(snapshotPath string) error {
	klog.Infof("WaitSnapshotReady snapshotPath %s", snapshotPath)
	backoff := wait.Backoff{
		Duration: 1 * time.Second,
		Factor:   1.2,
		Steps:    10,
	}

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		ready, err := PathExists(snapshotPath)
		if err != nil {
			return false, err
		}
		return ready, nil
	})

	if err == wait.ErrWaitTimeout {
		err = fmt.Errorf("Timeout, Snapshot  %s is still not exist %v", snapshotPath, err)
	}

	return err
}

func (mc *ArStorClient) DeleteSnapshot(snapshotId string) error {
	klog.Infof("arstorClient DeleteSnapshot %s", snapshotId)
	// no need to check snapshot volume

	snapshot, ok := mc.snapshots[snapshotId]
	if !ok || len(snapshot.Path) == 0 {
		klog.Infof("the snapshot %s has been deleted.", snapshotId)
		delete(mc.snapshots, snapshotId)
		return nil
	}

	// check file from fullDirs
	needRemove := false
	localDir, err := GetFileDir(mc.mountPath + snapshot.Path)
	if err == nil {
		full, err := mc.isFullArStorDir(localDir)
		if err == nil && full {
			needRemove = true
		}
	}

	err = mc.arstorTool.DeleteSnapshot(mc.mountPath + snapshot.Path)
	if err != nil {
		return err
	}
	delete(mc.snapshots, snapshotId)

	// delete file from fullDirs
	if needRemove {
		mc.removeFullDir(strings.TrimPrefix(localDir, mc.mountPath))
	}

	return nil
}

func (mc *ArStorClient) RestoreSnapshot(snapshotId string, volume *arstorCreateVolumeRequest) error {
	klog.Infof("arstorClient RestoreSnapshot %s", snapshotId)

	snapshot, ok := mc.snapshots[snapshotId]
	if !ok {
		if len(snapshot.Path) == 0 {
			return fmt.Errorf("can not find the arstor path for snapshot %s", snapshotId)
		}
		return fmt.Errorf("can not find the arstor path for snapshot %s", snapshotId)
	}

	klog.Infof("arstorClient RestoreSnapshot %s", snapshot.Path)

	prefix := mc.volumeDir + "/" + VolumePrefix
	localDir, mxDir, err := mc.GenerateArStorDir(mc.mountPath, prefix, volume.VolID)
	if err != nil {
		return err
	}

	name := volume.VolID + "_" + volume.VolName
	volumeCache := arstorVolume{
		Id:   volume.VolID,
		Name: volume.VolName,
		Size: volume.VolSize,
		Path: "",
	}
	mc.volumes[volume.VolID] = volumeCache
	_, err = mc.arstorTool.Clone(snapshot.Path, mxDir+"/"+name)
	if err != nil {
		DeleteFile(localDir + "/" + name)
		delete(mc.volumes, volume.VolID)
		if IsFileFullErr(err) {
			mc.addFullDir(mxDir)
			localDir, mxDir, err = mc.ReGenerateArStorDir(mc.mountPath, prefix, volume.VolID)
			if err != nil {
				return err
			}
			_, err = mc.arstorTool.Clone(snapshot.Path, mxDir+"/"+name)
			if err != nil {
				DeleteFile(localDir + "/" + name)
				delete(mc.volumes, volume.VolID)
				return err
			}
		} else {
			return err
		}
	}

	volumeCache.Path = mxDir + "/" + name
	mc.volumes[volume.VolID] = volumeCache

	klog.Infof("arstorClient RestoreSnapshot result %v", volumeCache)
	volume.VolPath = volumeCache.Path
	return nil
}

func (mc *ArStorClient) CloneVolume(srcVolumeId string, volume *arstorCreateVolumeRequest) error {
	klog.Infof("arstorClient CloneVolume %s", srcVolumeId)

	srcVolume, ok := mc.volumes[srcVolumeId]
	if !ok {
		return fmt.Errorf("can not find the arstor path for volume %s", srcVolumeId)
	}
	srcVolumeFile := srcVolume.Path
	klog.Infof("arstorClient CloneVolume %s", srcVolumeFile)

	// 1. create tmp clone snapshot
	creationTime := ptypes.TimestampNow()
	klog.V(4).Infof("create snapshot from volume %s", srcVolumeId)
	snapshot := arstorSnapshot{}
	snapshot.Name = volume.VolName
	snapshot.Id = volume.VolID
	snapshot.VolumeId = srcVolumeId
	snapshot.Path = ""
	snapshot.CreationTime = *creationTime
	snapshot.ReadyToUse = false

	err := mc.CreateSnapshot(srcVolumeId, &snapshot)
	if err != nil {
		return err
	}

	// 2. create volume
	err = mc.RestoreSnapshot(snapshot.Id, volume)
	if err != nil {
		return err
	}

	return nil
}

func (mc *ArStorClient) GetSnaphostByVolume(volId string) []string {
	var snapshotIds []string
	for _, snapshot := range mc.snapshots {
		if snapshot.VolumeId == volId {
			snapshotIds = append(snapshotIds, snapshot.Id)
		}
	}

	return snapshotIds
}

func (mc *ArStorClient) DeleteVolume(volId string) error {
	klog.Infof("arstorClient DeleteVolume %s", volId)
	volumeFile, ok := mc.volumes[volId]
	if !ok || len(volumeFile.Path) == 0 {
		klog.Infof("the volume %s has been deleted.", volId)
		delete(mc.volumes, volId)
		return nil
	}

	snapshotIds := mc.GetSnaphostByVolume(volId)
	if len(snapshotIds) != 0 {
		message := fmt.Sprintf("the volume %s has snapshot %v, can not delete it.", volId, snapshotIds)
		return errors.New(message)
	}

	// check file from fullDirs
	needRemove := false
	localDir, err := GetFileDir(mc.mountPath + volumeFile.Path)
	if err == nil {
		full, err := mc.isFullArStorDir(localDir)
		if err == nil && full {
			needRemove = true
		}
	}

	err = mc.arstorTool.Delete(mc.mountPath + volumeFile.Path)
	if err != nil {
		return err
	}

	delete(mc.volumes, volId)

	// delete file from fullDirs
	if needRemove {
		mc.removeFullDir(strings.TrimPrefix(localDir, mc.mountPath))
	}

	//delete tmp clone snapshot
	_, ok = mc.snapshots[volId]
	if ok {
		err = mc.DeleteSnapshot(volId)
		if err != nil {
			return err
		}
	}

	return nil
}

func (mt *ArStorClient) GenerateArStorDir(localPrefix string, mxPrefix string, volumeId string) (string, string, error) {
	klog.Infof("arstorClient GenerateArStorDir volumeid %s", volumeId)
	hash := StringToHash(volumeId) % MaxDirCount
	localDir := localPrefix + mxPrefix + fmt.Sprintf("%d", hash)
	mxDir := mxPrefix + fmt.Sprintf("%d", hash)
	if full, ok := mt.fullDirs[mxDir]; ok && full {
		i := 0
		for i = 0; i < MaxDirCount; i++ {
			hash = hash + i
			localDir = localPrefix + mxPrefix + fmt.Sprintf("%d", hash)
			mxDir = mxPrefix + fmt.Sprintf("%d", hash)
			if full, ok := mt.fullDirs[mxDir]; ok && full {
				continue
			} else {
				break
			}
		}
		if i == MaxDirCount {
			message := fmt.Sprintf("Creating volume args error: can not generate arstor dir for volume %s", volumeId)
			return "", "", errors.New(message)
		}
	}

	err := EnsureDir(localDir)
	if err != nil {
		return "", "", err
	}
	klog.Infof("arstorClient GenerateArStorDir result: localDir(%s) mxDir(%s)", localDir, mxDir)

	return localDir, mxDir, nil
}

func (mt *ArStorClient) isFullArStorDir(localDir string) (bool, error) {
	klog.Infof("arstorTool run isFullArStorDir()")

	inode, err := GetPathInode(localDir)
	if err != nil {
		return true, err
	}

	result, err := mt.arstorTool.GetVolumeAttribute(inode)
	if err != nil {
		return true, err
	}
	if len(result) == 0 {
		return false, nil
	}

	klog.Infof("arstorTool  isFullArStorDir result: %v", result)

	dirListStr := strings.Split(result, "dirList")[1]
	dirList := strings.Split(dirListStr, "dEntries")

	volumeCount := len(dirList) - 2

	//objs_len = len(ret_dir[0].split('dirList')[1].split("name")) - 3
	// if objs_len > 1000: return True

	if volumeCount > 1000 {
		return true, nil
	}
	return false, nil
}

func (mt *ArStorClient) checkArStorDir(localDir string, mxDir string) bool {
	klog.Infof("arstorTool  checkArStorDir %s", localDir)
	if full, ok := mt.fullDirs[mxDir]; ok && full {
		return false
	}

	full, err := mt.isFullArStorDir(localDir)
	if err != nil {
		return false
	}
	if full {
		return false
	}
	return true
}

func (mt *ArStorClient) ReGenerateArStorDir(localPrefix string, mxPrefix string, volumeId string) (string, string, error) {
	klog.Infof("arstorTool  ReGenerateArStorDir volumeid %s", volumeId)
	hash := StringToHash(volumeId) % MaxDirCount
	hash = hash + 1
	localDir := localPrefix + mxPrefix + fmt.Sprintf("%d", hash)
	mxDir := mxPrefix + fmt.Sprintf("%d", hash)

	if ok := mt.checkArStorDir(localDir, mxDir); !ok {
		i := 2
		for i = 2; i < MaxDirCount; i++ {
			hash = hash + i
			localDir = localPrefix + mxPrefix + fmt.Sprintf("%d", hash)
			mxDir = mxPrefix + fmt.Sprintf("%d", hash)
			if ok := mt.checkArStorDir(localDir, mxDir); !ok {
				continue
			} else {
				break
			}
		}
		if i == MaxDirCount {
			message := fmt.Sprintf("Creating volume args error: can not generate arstor dir for volume %s", volumeId)
			return "", "", errors.New(message)
		}
	}

	err := EnsureDir(localDir)
	if err != nil {
		return "", "", err
	}
	klog.Infof("arstorTool  ReGenerateArStorDir %s", localDir)
	return localDir, mxDir, nil
}
