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
	"github.com/golang/protobuf/ptypes"

	"github.com/pborman/uuid"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/klog"
)

const (
	deviceID                    = "deviceID"
	maxStorageCapacity          = tib
	maxEphemeralStorageCapacity = gib
)

type accessType int

const (
	mountAccess accessType = iota
	blockAccess
)

type controllerServer struct {
	caps         []*csi.ControllerServiceCapability
	nodeID       string
	arstorClient *ArStorClient

	loopDeviceManager *LoopDeviceManager
}

func NewControllerServer(ephemeral bool, nodeID string, arstorClient *ArStorClient) *controllerServer {
	if ephemeral {
		return &controllerServer{caps: getControllerServiceCapabilities(nil), nodeID: nodeID}
	}
	loopDeviceManager, _ := NewLoopDeviceManager()
	return &controllerServer{
		//specify capabilities for controller server
		caps: getControllerServiceCapabilities(
			[]csi.ControllerServiceCapability_RPC_Type{
				csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
				csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
				csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
				csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
				csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
				csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
			}),
		nodeID:            nodeID,
		arstorClient:      arstorClient,
		loopDeviceManager: loopDeviceManager,
	}
}

func (cs *controllerServer) ValidateControllerServiceRequest(c csi.ControllerServiceCapability_RPC_Type) error {
	if c == csi.ControllerServiceCapability_RPC_UNKNOWN {
		return nil
	}

	for _, cap := range cs.caps {
		if c == cap.GetRpc().GetType() {
			return nil
		}
	}
	return status.Error(codes.InvalidArgument, fmt.Sprintf("%s", c))
}

func (cs *controllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if err := cs.validateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		klog.V(3).Infof("invalid create volume req: %v", req)
		return nil, err
	}

	// Check arguments
	if len(req.GetName()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Name missing in request")
	}
	caps := req.GetVolumeCapabilities()
	if caps == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume Capabilities missing in request")
	}

	// Keep a record of the requested access types.
	var accessTypeMount, accessTypeBlock bool

	for _, cap := range caps {
		if cap.GetBlock() != nil {
			accessTypeBlock = true
		}
		if cap.GetMount() != nil {
			accessTypeMount = true
		}
	}
	// A real driver would also need to check that the other
	// fields in VolumeCapabilities are sane. The check above is
	// just enough to pass the "[Testpattern: Dynamic PV (block
	// volmode)] volumeMode should fail in binding dynamic
	// provisioned PV to PVC" storage E2E test.

	if accessTypeBlock && accessTypeMount {
		return nil, status.Error(codes.InvalidArgument, "cannot have both block and mount access type")
	}

	var requestedAccessType accessType

	if accessTypeBlock {
		requestedAccessType = blockAccess
	} else {
		// Default to mount.
		requestedAccessType = mountAccess
	}

	// Check for maximum available capacity
	capacity := int64(req.GetCapacityRange().GetRequiredBytes())
	if capacity >= maxStorageCapacity {
		return nil, status.Errorf(codes.OutOfRange, "Requested capacity %d exceeds maximum allowed %d", capacity, maxStorageCapacity)
	}

	topologies := []*csi.Topology{&csi.Topology{
		Segments: map[string]string{
			TopologyKeyNode:          cs.nodeID,
			TopologyKeyArStorEnabled: "true"},
	}}

	// Need to check for already existing volume name, and if found
	//check for the requested capacity and already allocated capacity
	exVol := GetVolumeByName(cs.arstorClient.volumes, req.GetName())
	if exVol != nil {
		// Since err is nil, it means the volume with the same name already exists
		// need to check if the size of exisiting volume is the same as in new
		// request
		if exVol.Size >= int64(req.GetCapacityRange().GetRequiredBytes()) {
			// exisiting volume is compatible with new request and should be reused.
			return &csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					VolumeId:           exVol.Id,
					CapacityBytes:      int64(exVol.Size),
					VolumeContext:      req.GetParameters(),
					AccessibleTopology: topologies,
				},
			}, nil
		}
		return nil, status.Errorf(codes.AlreadyExists, "Volume with the same name: %s but with different size already exist", req.GetName())
	}

	volumeID := uuid.NewUUID().String()
	ephemeral := false
	arstorVol := &arstorCreateVolumeRequest{
		VolID:         volumeID,
		VolName:       req.GetName(),
		VolSize:       capacity,
		VolPath:       "",
		VolAccessType: requestedAccessType,
		Ephemeral:     ephemeral,
	}

	// create volume by the content source of pvc,
	// nil        --> new volume
	// snapshotId --> create volume by snapshot, restore
	// vlumeId    --> clone volume
	contentSource := req.GetVolumeContentSource()
	klog.Infof("GetVolumeContentSource %v", contentSource)
	if contentSource == nil {
		err := cs.arstorClient.CreateVolume(arstorVol)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create volume %v: %v", arstorVol, err)
		}
		klog.V(4).Infof("created volume %s at path %s", arstorVol.VolID, arstorVol.VolPath)
	} else {
		if snapshot := contentSource.GetSnapshot(); snapshot != nil {
			snapshotId := snapshot.GetSnapshotId()
			err := cs.arstorClient.RestoreSnapshot(snapshotId, arstorVol)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create volume %v form snapshot: %v", arstorVol, err)
			}
			klog.V(4).Infof("created volume %s from snapshot %s", arstorVol.VolPath, snapshotId)
		}
		if srcVolume := contentSource.GetVolume(); srcVolume != nil {
			volumeId := srcVolume.GetVolumeId()
			err := cs.arstorClient.CloneVolume(volumeId, arstorVol)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create volume %v from volume: %v", arstorVol, err)
			}
			klog.V(4).Infof("created volume %s from volume %s", arstorVol.VolPath, volumeId)
		}
		klog.V(4).Infof("successfully populated volume %s", arstorVol.VolID)
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:           volumeID,
			CapacityBytes:      req.GetCapacityRange().GetRequiredBytes(),
			VolumeContext:      req.GetParameters(),
			ContentSource:      req.GetVolumeContentSource(),
			AccessibleTopology: topologies,
		},
	}, nil
}

func (cs *controllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	if err := cs.validateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		klog.V(3).Infof("invalid delete volume req: %v", req)
		return nil, err
	}

	volId := req.GetVolumeId()
	// check volume
	volume, ok := cs.arstorClient.volumes[volId]
	if !ok {
		klog.Infof("the volume %s has been deleted.", volId)
		return &csi.DeleteVolumeResponse{}, nil
	}
	volumeFile := cs.arstorClient.mountPath + volume.Path
	devices, err := cs.loopDeviceManager.ListLoopDeviceByFile(volumeFile)
	if err != nil {
		return nil, err
	}
	if len(devices) > 0 {
		message := fmt.Sprintf("the volume %s has loop devices, wait for node server deatch loopdevice", volumeFile)
		return nil, errors.New(message)
	}

	if err := cs.arstorClient.DeleteVolume(volId); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete volume %v: %v", volId, err)
	}

	klog.V(4).Infof("volume %v successfully deleted", volId)

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *controllerServer) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: cs.caps,
	}, nil
}

func (cs *controllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {

	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID cannot be empty")
	}
	if len(req.VolumeCapabilities) == 0 {
		return nil, status.Error(codes.InvalidArgument, req.VolumeId)
	}

	//if _, err := getVolumeByID(req.GetVolumeId()); err != nil {
	//	return nil, status.Error(codes.NotFound, req.GetVolumeId())
	//}

	for _, cap := range req.GetVolumeCapabilities() {
		if cap.GetMount() == nil && cap.GetBlock() == nil {
			return nil, status.Error(codes.InvalidArgument, "cannot have both mount and block access type be undefined")
		}

		// A real driver would check the capabilities of the given volume with
		// the set of requested capabilities.
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeContext:      req.GetVolumeContext(),
			VolumeCapabilities: req.GetVolumeCapabilities(),
			Parameters:         req.GetParameters(),
		},
	}, nil
}

// arstor volume is a file which created on the node, so do not need publish volume
func (cs *controllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	volumeID := req.GetVolumeId()

	err := cs.arstorClient.WaitVolumeReady(volumeID)
	if err != nil {
		klog.V(3).Infof("Failed to WaitVolumeReady: %v", err)
		return nil, status.Error(codes.Internal, fmt.Sprintf("Failed to WaitVolumeReady: %v", err))
	}

	devicePath, _ := cs.arstorClient.GetVolumeDevicePath(volumeID)

	// Publish Volume Info
	pvInfo := map[string]string{}
	pvInfo["DevicePath"] = devicePath

	return &csi.ControllerPublishVolumeResponse{
		PublishContext: pvInfo,
	}, nil
}

// arstor volume is a file which created on the node, so do not need unpublish volume
func (cs *controllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	instanceID := req.GetNodeId()
	volumeID := req.GetVolumeId()
	klog.V(4).Infof("ControllerUnpublishVolume %s on %s", volumeID, instanceID)

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (cs *controllerServer) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *controllerServer) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {

	var ventries []*csi.ListVolumesResponse_Entry
	for _, volume := range cs.arstorClient.volumes {
		ventry := csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      volume.Id,
				CapacityBytes: volume.Size,
			},
		}
		ventries = append(ventries, &ventry)
	}
	return &csi.ListVolumesResponse{
		Entries: ventries,
	}, nil
}

// CreateSnapshot uses tar command to create snapshot for arstor volume. The tar command can quickly create
// archives of entire directories. The host image must have "tar" binaries in /bin, /usr/sbin, or /usr/bin.
func (cs *controllerServer) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	klog.Infof("CreateSnapshot %v", req)

	if err := cs.validateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT); err != nil {
		klog.V(3).Infof("invalid create snapshot req: %v", req)
		return nil, err
	}

	if len(req.GetName()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Name missing in request")
	}
	// Check arguments
	// currently req.GetSourceVolumeId() == volume.id
	if len(req.GetSourceVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "SourceVolumeId missing in request")
	}

	// Need to check for already existing snapshot name, and if found check for the
	// requested sourceVolumeId and sourceVolumeId of snapshot that has been created.
	exSnap := GetSnapshotByName(cs.arstorClient.snapshots, req.GetName())
	if exSnap != nil {
		if len(exSnap.Path) == 0 {
			exSnap.ReadyToUse = false
		}
		// Since err is nil, it means the snapshot with the same name already exists need
		// to check if the sourceVolumeId of existing snapshot is the same as in new request.
		// check snapshot every time

		return &csi.CreateSnapshotResponse{
			Snapshot: &csi.Snapshot{
				SnapshotId:     exSnap.Id,
				SourceVolumeId: exSnap.VolumeId,
				CreationTime:   &exSnap.CreationTime,
				SizeBytes:      exSnap.Size,
				ReadyToUse:     exSnap.ReadyToUse,
			},
		}, nil

	}

	volumeID := req.GetSourceVolumeId()

	snapshotID := uuid.NewUUID().String()
	creationTime := ptypes.TimestampNow()

	klog.V(4).Infof("create snapshot from volume %s", volumeID)
	snapshot := arstorSnapshot{}
	snapshot.Name = req.GetName()
	snapshot.Id = snapshotID
	snapshot.VolumeId = volumeID
	snapshot.Path = ""
	snapshot.CreationTime = *creationTime
	snapshot.ReadyToUse = false

	err := cs.arstorClient.CreateSnapshot(volumeID, &snapshot)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create snapshot %v form volume: %v", snapshot, err)
	}

	return &csi.CreateSnapshotResponse{
		Snapshot: &csi.Snapshot{
			SnapshotId:     snapshot.Id,
			SourceVolumeId: snapshot.VolumeId,
			CreationTime:   &snapshot.CreationTime,
			SizeBytes:      snapshot.Size,
			ReadyToUse:     snapshot.ReadyToUse,
		},
	}, nil
}

func (cs *controllerServer) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	// Check arguments
	if len(req.GetSnapshotId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Snapshot ID missing in request")
	}

	if err := cs.validateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT); err != nil {
		klog.V(3).Infof("invalid delete snapshot req: %v", req)
		return nil, err
	}

	snapshotID := req.GetSnapshotId()
	klog.V(4).Infof("deleting snapshot %s", snapshotID)
	if err := cs.arstorClient.DeleteSnapshot(snapshotID); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete snapshot %v: %v", snapshotID, err)
	}

	return &csi.DeleteSnapshotResponse{}, nil
}

func (cs *controllerServer) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {

	var ventries []*csi.ListSnapshotsResponse_Entry
	for _, snapshot := range cs.arstorClient.snapshots {
		ventry := csi.ListSnapshotsResponse_Entry{
			Snapshot: &csi.Snapshot{
				SizeBytes:      snapshot.Size,
				SnapshotId:     snapshot.Id,
				SourceVolumeId: snapshot.VolumeId,
				CreationTime:   &snapshot.CreationTime,
				ReadyToUse:     true,
			},
		}
		ventries = append(ventries, &ventry)
	}
	return &csi.ListSnapshotsResponse{
		Entries: ventries,
	}, nil
}

func (cs *controllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {

	volID := req.GetVolumeId()
	if len(volID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	capRange := req.GetCapacityRange()
	if capRange == nil {
		return nil, status.Error(codes.InvalidArgument, "Capacity range not provided")
	}

	capacity := int64(capRange.GetRequiredBytes())
	if capacity >= maxStorageCapacity {
		return nil, status.Errorf(codes.OutOfRange, "Requested capacity %d exceeds maximum allowed %d", capacity, maxStorageCapacity)
	}

	exVol := GetVolumeById(cs.arstorClient.volumes, volID)
	if exVol == nil {
		// Assume not found error
		return nil, status.Errorf(codes.NotFound, "Could not get volume %s", volID)
	}

	if capacity < exVol.Size {
		return nil, status.Errorf(codes.Internal, fmt.Sprintf("Could not expand volume %q from size %d to size %d", volID, exVol.Size, capacity))
	}

	err := cs.arstorClient.ExpandVolume(volID, capacity)
	if err != nil {
		return nil, status.Errorf(codes.Internal, fmt.Sprintf("Could not resize volume %q to size %v: %v", volID, capacity, err))
	}

	klog.V(4).Infof("ControllerExpandVolume resized volume %v to size %v", volID, capacity)

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         capacity,
		NodeExpansionRequired: true,
	}, nil
	return nil, nil
}

func (cs *controllerServer) validateControllerServiceRequest(c csi.ControllerServiceCapability_RPC_Type) error {
	if c == csi.ControllerServiceCapability_RPC_UNKNOWN {
		return nil
	}

	for _, cap := range cs.caps {
		if c == cap.GetRpc().GetType() {
			return nil
		}
	}
	return status.Errorf(codes.InvalidArgument, "unsupported capability %s", c)
}

func getControllerServiceCapabilities(cl []csi.ControllerServiceCapability_RPC_Type) []*csi.ControllerServiceCapability {
	var csc []*csi.ControllerServiceCapability

	for _, cap := range cl {
		klog.Infof("Enabling controller service capability: %v", cap.String())
		csc = append(csc, &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		})
	}

	return csc
}
