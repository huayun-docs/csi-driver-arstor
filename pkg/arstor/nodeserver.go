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
	"os"
	"strings"

	"golang.org/x/net/context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	utilexec "k8s.io/utils/exec"
	"k8s.io/utils/mount"

	"k8s.io/klog"
	"path/filepath"
)

const TopologyKeyNode = "topology.arstor.csi/node"

// arstor node has installed mxsp container
const TopologyKeyArStorEnabled = "topology.arstor.csi/arstorenabled"
const EphemeralPrefix = "ephemeral-"

// the PvcPrefix is volume-name-prefix of csi-provisioner, default is pvc
const PvcPrefix = "pvc-"

type nodeServer struct {
	nodeID            string
	ephemeral         bool
	maxVolumesPerNode int64
	arstorClient      *ArStorClient

	loopDeviceManager *LoopDeviceManager
}

func NewNodeServer(nodeId string, ephemeral bool, maxVolumesPerNode int64, arstorClient *ArStorClient) *nodeServer {
	loopDeviceManager, _ := NewLoopDeviceManager()
	return &nodeServer{
		nodeID:            nodeId,
		ephemeral:         ephemeral,
		maxVolumesPerNode: maxVolumesPerNode,
		arstorClient:      arstorClient,
		loopDeviceManager: loopDeviceManager,
	}
}

func (ns *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {

	klog.Infof("NodePublishVolume %v", req)

	// Check arguments
	volID := req.GetVolumeId()
	source := req.GetStagingTargetPath()
	targetPath := req.GetTargetPath()
	volumeCapability := req.GetVolumeCapability()

	if len(volID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Volume ID must be provided")
	}
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Target Path must be provided")
	}
	// if ephemeral is specified, create volume here to avoid errors
	ephemeralVolume := req.GetVolumeContext()["csi.storage.k8s.io/ephemeral"] == "true" ||
		req.GetVolumeContext()["csi.storage.k8s.io/ephemeral"] == "" && ns.ephemeral
	if ephemeralVolume {
		return ns.NodePublishEphemeralVolume(ctx, req)
	}

	if len(source) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Staging Target Path must be provided")
	}
	if volumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Volume Capability must be provided")
	}

	if req.GetVolumeCapability().GetBlock() != nil &&
		req.GetVolumeCapability().GetMount() != nil {
		return nil, status.Error(codes.InvalidArgument, "cannot have both block and mount access type")
	}

	// check volume
	_, ok := ns.arstorClient.volumes[volID]
	if !ok {
		ns.arstorClient.LoadArStorData()
		_, ok = ns.arstorClient.volumes[volID]
		if !ok {
			messge := fmt.Sprintf("the volume %s is not found,", volID)
			return nil, status.Error(codes.NotFound, messge)
		}
	}
	// get device path
	err := ns.arstorClient.WaitVolumeReady(volID)
	if err != nil {
		klog.V(3).Infof("Failed to WaitVolumeReady: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to WaitVolumeReady: %v", err))
	}

	vol, ok := ns.arstorClient.volumes[volID]

	mountOptions := []string{"bind"}
	if req.GetReadonly() {
		mountOptions = append(mountOptions, "ro")
	} else {
		mountOptions = append(mountOptions, "rw")
	}

	if req.GetVolumeCapability().GetBlock() != nil {
		//if vol.VolAccessType != blockAccess {
		//	return nil, status.Error(codes.InvalidArgument, "cannot publish a non-block volume as block volume")
		//}
		loopDevice, err := ns.mountToLoopDevice(ns.arstorClient.mountPath + vol.Path)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		klog.V(4).Infof("the block volume %s is mount to loop device %s", ns.arstorClient.mountPath+vol.Path, loopDevice)

		source = loopDevice

		mounter := mount.New("")

		// Check if the target path exists. Create if not present.
		_, err = os.Lstat(targetPath)
		if os.IsNotExist(err) {
			targetDir := filepath.Dir(targetPath)
			err := EnsureDir(targetDir)
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			err = Ensurefile(targetPath)
			if err != nil {
				return nil, status.Error(codes.Internal, fmt.Sprintf("failed to create target path: %s: %v", targetPath, err))
			}
		}
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to check if the target block file exists: %v", err)
		}

		// Check if the target path is already mounted. Prevent remounting.
		notMount, err := mounter.IsLikelyNotMountPoint(targetPath)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, status.Errorf(codes.Internal, "error checking path %s for mount: %s", targetPath, err)
			}
			notMount = true
		}
		if !notMount {
			// It's already mounted.
			klog.V(5).Infof("Skipping bind-mounting subpath %s: already mounted", targetPath)
			return &csi.NodePublishVolumeResponse{}, nil
		}

		if err := mount.New("").Mount(source, targetPath, "", mountOptions); err != nil {
			if removeErr := os.Remove(targetPath); removeErr != nil {
				if !os.IsNotExist(err) {
					return nil, status.Errorf(codes.Internal, "Could not remove mount target %q: %v", targetPath, err)
				}
			}
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to mount block device: %s at %s: %v", source, targetPath, err))
		}

		klog.V(4).Infof("success to mount block volume %s to targetPath %s, mountOptions: %s",
			source, targetPath, mountOptions)

	} else if req.GetVolumeCapability().GetMount() != nil {
		//if vol.VolAccessType != mountAccess {
		//	return nil, status.Error(codes.InvalidArgument, "cannot publish a non-mount volume as mount volume")
		//}

		notMnt, err := IsLikelyNotMountPointAttach(targetPath)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}

		// Volume Mount
		if notMnt {
			fsType := "xfs"
			if mnt := volumeCapability.GetMount(); mnt != nil {
				if mnt.FsType != "" {
					fsType = mnt.FsType
				}
			}
			// Mount
			diskMounter := &mount.SafeFormatAndMount{Interface: mount.New(""), Exec: utilexec.New()}
			err = diskMounter.Mount(source, targetPath, fsType, mountOptions)
			if err != nil {
				os.Remove(targetPath)
				return nil, status.Error(codes.Internal, err.Error())
			}
		}

		klog.V(4).Infof("success to mount volume %s to targetPath %s, mountOptions: %s",
			source, targetPath, mountOptions)
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodePublishEphemeralVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	klog.V(4).Infof("NodePublishEphemeralVolume: called with args %+v", *req)
	volID := req.GetVolumeId()
	targetPath := req.GetTargetPath()
	volumeCapability := req.GetVolumeCapability()

	volName := fmt.Sprintf("%s%s", EphemeralPrefix, volID)
	arstorVol := &arstorCreateVolumeRequest{
		VolID:         volID,
		VolName:       volName,
		VolSize:       1 * gib,
		VolPath:       "",
		VolAccessType: mountAccess,
		Ephemeral:     true,
	}
	err := ns.arstorClient.CreateVolume(arstorVol)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create volume %v: %v", arstorVol, err)
	}

	klog.V(4).Infof("ephemeral mode: created volume: %s", ns.arstorClient.mountPath+arstorVol.VolPath)
	// check volume
	_, ok := ns.arstorClient.volumes[volID]
	if !ok {
		ns.arstorClient.LoadArStorData()
		_, ok = ns.arstorClient.volumes[volID]
		if !ok {
			messge := fmt.Sprintf("the ephemeral volume %s is not found,", volID)
			return nil, status.Error(codes.NotFound, messge)
		}
	}
	// get device path
	err = ns.arstorClient.WaitVolumeReady(volID)
	if err != nil {
		klog.V(3).Infof("Failed to WaitVolumeReady: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to WaitVolumeReady: %v", err))
	}

	vol, ok := ns.arstorClient.volumes[volID]

	// Verify whether mounted
	notMnt, err := IsLikelyNotMountPointAttach(targetPath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Volume Mount
	if notMnt {

		// set default fstype is xfs
		fsType := "xfs"
		var options []string
		if mnt := volumeCapability.GetMount(); mnt != nil {
			if mnt.FsType != "" {
				fsType = mnt.FsType
			}
			mountFlags := mnt.GetMountFlags()
			options = append(options, mountFlags...)
		}
		attrib := req.GetVolumeContext()
		mountFlags := req.GetVolumeCapability().GetMount().GetMountFlags()

		// loop mount
		loopDevice, err := ns.mountToLoopDevice(ns.arstorClient.mountPath + vol.Path)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		klog.V(4).Infof("mount ephemeral volume args:\ndevice %v\ntarget %v\nfstype %v\nvolumeId %v\nattributes %v\nmountflags %v\nmountOptions %v\n",
			loopDevice, targetPath, fsType, volID, attrib, mountFlags, options)

		// Mount
		err = ns.FormatAndMount(loopDevice, targetPath, fsType, options)
		if err != nil {
			os.Remove(targetPath)
			message := fmt.Sprintf("failed to mount volume %s to targetPath %s, mountOptions: %s,err: %v",
				loopDevice, targetPath, options, err)
			klog.Errorf("%s", message)

			unmountErr := ns.unmountLoopDevice(vol.Id)
			if err != nil {
				message = message + fmt.Sprintf(fmt.Sprintf("failed to unmount loop device %s :%s", loopDevice, unmountErr.Error()))
				klog.Errorf("%s", message)
			}

			if rmErr := ns.arstorClient.DeleteVolume(volID); rmErr != nil {
				message = message + fmt.Sprintf(fmt.Sprintf("failed to delete volume path %s :%s", ns.arstorClient.mountPath+vol.Path, rmErr.Error()))
				klog.Errorf("%s", message)
			}

			return nil, status.Error(codes.Internal, message)
		}
		klog.V(4).Infof("success to mount ephemeral volume %s to targetPath %s, mountOptions: %s",
			loopDevice, targetPath, options)
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {

	klog.V(4).Infof("NodeUnPublishVolume: called with args %+v", *req)

	volID := req.GetVolumeId()
	targetPath := req.GetTargetPath()
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeUnpublishVolume Target Path must be provided")
	}
	if len(volID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeUnpublishVolume volumeID must be provided")
	}

	notMnt, err := IsLikelyNotMountPointDetach(targetPath)
	if err != nil && !mount.IsCorruptedMnt(err) {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if notMnt && !mount.IsCorruptedMnt(err) {
		//return nil, status.Error(codes.NotFound, "Volume not mounted")
		klog.Warningf("the targetPath %s not mounted", targetPath)
		if err = os.Remove(targetPath); err != nil {
			if !os.IsNotExist(err) {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	err = UnmountPath(targetPath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	vol, ok := ns.arstorClient.volumes[volID]
	if !ok {
		klog.Infof("the volume(%s) is deleted by others, skip...", volID)
	} else {
		pvcIndex := strings.Index(vol.Name, PvcPrefix)
		ephemeralIndex := strings.Index(vol.Name, EphemeralPrefix)
		if pvcIndex < 0 && ephemeralIndex == 0 {
			klog.Infof("delete ephemeral volume file %s", ns.arstorClient.mountPath+vol.Path)
			unmountErr := ns.unmountLoopDevice(vol.Id)
			if err != nil {
				message := fmt.Sprintf(fmt.Sprintf("failed to unmount the loop device of ephemeral volume%s :%s", vol.Id, unmountErr.Error()))
				return nil, status.Error(codes.Internal, message)
			}

			if rmErr := ns.arstorClient.DeleteVolume(volID); rmErr != nil {
				message := fmt.Sprintf(fmt.Sprintf("failed to delete ephemeral volume path %s :%s", ns.arstorClient.mountPath+vol.Path, rmErr.Error()))
				return nil, status.Error(codes.Internal, message)
			}
			klog.V(4).Infof("success to delete ephemeral volume %s ", ns.arstorClient.mountPath+vol.Path)
		}
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *nodeServer) mountToLoopDevice(volumeFile string) (string, error) {
	klog.V(4).Infof("mountToLoopDevice volumeFile %s", volumeFile)

	loopDevice := ""
	devices, err := ns.loopDeviceManager.ListLoopDeviceByFile(volumeFile)
	if err != nil {
		return "", err
	}
	if len(devices) > 0 {
		if len(devices) == 1 {
			loopDevice = devices[0]
		} else {
			message := fmt.Sprintf("the volume %s has multiple loop devices: %v", volumeFile, devices)
			return "", errors.New(message)
		}
	}
	if len(devices) == 0 {
		loopDevice, err = ns.loopDeviceManager.GetFreeLoopDevice()
		if err != nil {
			return "", err
		}
		// 1. attach loop device
		err = ns.loopDeviceManager.AttachLoopDevice(loopDevice, volumeFile)
		if err != nil {
			return "", err
		}
	}

	return loopDevice, nil
	//	// 2. fdisk partition
	//	partition, err := ns.loopDeviceManager.GetDevicePartition(loopDevice)
	//	if err != nil {
	//		return "", err
	//	}
	//	if len(partition) == 0 {
	//		err = ns.loopDeviceManager.FdiskDevice(loopDevice)
	//		if err != nil {
	//			return "", err
	//		}
	//		partition, err = ns.loopDeviceManager.GetDevicePartition(loopDevice)
	//		if err != nil {
	//			return "", err
	//		}
	//	}
	//
	//	// 3. add device mapper, can add again
	//	deviceName, err := ns.loopDeviceManager.AddDevmapping(loopDevice)
	//	if err != nil {
	//		return "", err
	//	}
	//	if index := strings.Index(partition, deviceName); index < 0 {
	//		message := fmt.Sprintf("the file %s: mapper device(%s) is not right, partition: %s", volumeFile, deviceName, partition)
	//		return "", errors.New(message)
	//	}
	//
	//	mapperDevice, err := ns.loopDeviceManager.GetMapperDevice(deviceName)
	//	if err != nil {
	//		return "", err
	//	}
	//
	//	// 4. mkfs
	//	err = ns.loopDeviceManager.EnsureFsType(mapperDevice, fsType)
	//	if err != nil {
	//		return "", err
	//	}
	//
	//	return mapperDevice, nil
}

func (ns *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	klog.V(4).Infof("NodeStageVolume: called with args %+v", *req)

	stagingTarget := req.GetStagingTargetPath()
	volumeCapability := req.GetVolumeCapability()
	volID := req.GetVolumeId()

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(req.GetStagingTargetPath()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume Capability missing in request")
	}

	// check volume
	_, ok := ns.arstorClient.volumes[volID]
	if !ok {
		ns.arstorClient.LoadArStorData()
		_, ok = ns.arstorClient.volumes[volID]
		if !ok {
			messge := fmt.Sprintf("the volume %s is not found,", volID)
			return nil, status.Error(codes.NotFound, messge)
		}
	}

	// get device path
	err := ns.arstorClient.WaitVolumeReady(volID)
	if err != nil {
		klog.V(3).Infof("Failed to WaitVolumeReady: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to WaitVolumeReady: %v", err))
	}

	vol, _ := ns.arstorClient.volumes[volID]

	if blk := volumeCapability.GetBlock(); blk != nil {
		// If block volume, do nothing
		//err := ns.loopDeviceManager.CheckPathDeviceType(ns.arstorClient.mountPath+vol.Path, modeBlock)
		//if err != nil {
		//	return nil, err
		//}
		return &csi.NodeStageVolumeResponse{}, nil
	}

	// Verify whether mounted
	notMnt, err := IsLikelyNotMountPointAttach(stagingTarget)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Volume Mount
	if notMnt {

		// set default fstype is xfs
		fsType := "xfs"
		var options []string
		if mnt := volumeCapability.GetMount(); mnt != nil {
			if mnt.FsType != "" {
				fsType = mnt.FsType
			}
			mountFlags := mnt.GetMountFlags()
			options = append(options, mountFlags...)
		}
		attrib := req.GetVolumeContext()
		mountFlags := req.GetVolumeCapability().GetMount().GetMountFlags()

		// loop mount
		loopDevice, err := ns.mountToLoopDevice(ns.arstorClient.mountPath + vol.Path)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		klog.V(4).Infof("mount args:\ndevice %v\ntarget %v\nfstype %v\nvolumeId %v\nattributes %v\nmountflags %v\nmountOptions %v\n",
			loopDevice, stagingTarget, fsType, volID, attrib, mountFlags, options)

		// Mount
		err = ns.FormatAndMount(loopDevice, stagingTarget, fsType, options)
		if err != nil {
			os.Remove(stagingTarget)
			message := fmt.Sprintf("failed to mount volume %s to targetPath %s, mountOptions: %s,err: %v",
				loopDevice, stagingTarget, options, err)
			klog.Errorf("%s", message)

			return nil, status.Error(codes.Internal, message)
		}
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (ns *nodeServer) FormatAndMount(source string, target string, fsType string, mountOptions []string) error {
	diskMounter := &mount.SafeFormatAndMount{Interface: mount.New(""), Exec: utilexec.New()}

	err := diskMounter.FormatAndMount(source, target, fsType, mountOptions)
	if err != nil {
		errMessage := fmt.Sprintf("FormatAndMount is failed with err %s\n", err.Error())
		resizefs := NewResizeFs()
		format, err := resizefs.GetDiskFormat(source)
		if err != nil {
			formatErr := fmt.Sprintf("ResizeFS.Resize - error checking format for device %s: %v", source, err)
			return errors.New(errMessage + formatErr)
		}

		if format != fsType {
			typeErr := fmt.Sprintf("the volume fsType is %s, but mount fsType is %s", format, fsType)
			return errors.New(errMessage + typeErr)
		}

		err = diskMounter.Mount(source, target, fsType, mountOptions)
		if err != nil {
			return errors.New(errMessage + fmt.Sprintf("Mount is failed with err %s", err))
		}
	}

	return nil
}

func (ns *nodeServer) unmountLoopDevice(volumeID string) error {
	klog.V(4).Infof("unmountLoopDevice volumeID %s", volumeID)
	// check volume
	_, ok := ns.arstorClient.volumes[volumeID]
	if !ok {
		ns.arstorClient.LoadArStorData()
		_, ok = ns.arstorClient.volumes[volumeID]
		if !ok {
			message := fmt.Sprintf("ERROR: the volume %s is not found, should clear its loop device and mapper device", volumeID)
			klog.Warningf("%s", message)

			// clear loop device mapper device
			err := ns.loopDeviceManager.DetachLostLoopDeviceByVolumeId(volumeID)
			if err != nil {
				return err
			}
			return nil
		}
	}
	vol, ok := ns.arstorClient.volumes[volumeID]
	volumeFile := ns.arstorClient.mountPath + vol.Path

	klog.V(4).Infof("unmountLoopDevice volumeFile %s", volumeFile)
	loopDevice := ""
	devices, err := ns.loopDeviceManager.ListLoopDeviceByFile(volumeFile)
	if err != nil {
		return err
	}
	if len(devices) > 0 {
		if len(devices) == 1 {
			loopDevice = devices[0]
		} else {
			message := fmt.Sprintf("the volume %s has multiple loop devices: %v", volumeFile, devices)
			return errors.New(message)
		}
	} else {
		message := fmt.Sprintf("the volume %s has not loop devices", volumeFile)
		klog.Infof("%s", message)
		return nil
	}

	//// 1. delete device mapper
	//err = ns.loopDeviceManager.DeleteDevmapping(loopDevice)
	//if err != nil {
	//	return err
	//}

	// 2. detach loop device
	err = ns.loopDeviceManager.DetachLoopDevice(loopDevice)
	if err != nil {
		return err
	}

	return nil
}

func (ns *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	klog.V(4).Infof("NodeUnstageVolume: called with args %+v", *req)

	volID := req.GetVolumeId()
	if len(volID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume Id not provided")
	}

	stagingTargetPath := req.GetStagingTargetPath()
	if len(stagingTargetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume Staging Target Path must be provided")
	}

	notMnt, err := IsLikelyNotMountPointDetach(stagingTargetPath)
	if err != nil && !mount.IsCorruptedMnt(err) {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if notMnt && !mount.IsCorruptedMnt(err) {
		klog.Warningf("the stagingTargetPath %s not mounted", stagingTargetPath)
		if err = os.Remove(stagingTargetPath); err != nil {
			if !os.IsNotExist(err) {
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
		err = ns.unmountLoopDevice(volID)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	// unmount staging
	err = UnmountPath(stagingTargetPath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	err = ns.unmountLoopDevice(volID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (ns *nodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {

	topology := &csi.Topology{
		Segments: map[string]string{
			TopologyKeyNode:          ns.nodeID,
			TopologyKeyArStorEnabled: "true"},
	}

	return &csi.NodeGetInfoResponse{
		NodeId:             ns.nodeID,
		MaxVolumesPerNode:  ns.maxVolumesPerNode,
		AccessibleTopology: topology,
	}, nil
}

func (ns *nodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
					},
				},
			},
		},
	}, nil
}

func (ns *nodeServer) NodeGetVolumeStats(ctx context.Context, in *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, fmt.Sprintf("NodeGetVolumeStats is not yet implemented"))
}

// NodeExpandVolume is only implemented so the driver can be used for e2e testing.
func (ns *nodeServer) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	klog.V(4).Infof("NodeExpandVolume: called with args %+v", *req)

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}
	volumePath := req.GetVolumePath()

	// get loop device
	loopDevice, err := ns.loopDeviceManager.GetLoopDeviceByMountPoint(volumePath)
	if err != nil {
		return nil, err
	}
	if loopDevice == "" {
		return nil, status.Error(codes.Internal, "Unable to find Device path for volume")
	}
	// 1. resize loop device. # lsblk  | grep loop3
	err = ns.loopDeviceManager.ExpandLoopDevice(loopDevice)
	if err != nil {
		return nil, err
	}

	// 2. resize volume
	r := NewResizeFs()
	if _, err := r.Resize(loopDevice, volumePath); err != nil {
		return nil, status.Errorf(codes.Internal, "Could not resize volume %q:  %v", volumeID, err)
	}
	return &csi.NodeExpandVolumeResponse{}, nil
}
