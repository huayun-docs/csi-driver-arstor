package arstor

import (
	"context"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
)

var fakeNs *nodeServer

const maxVolume = 1000

// Init Node Server
func init() {
	if fakeNs == nil {
		fakeNs = NewNodeServer(FakeNodeID, false, maxVolume, nil)
	}
}

// Test NodeGetInfo
func TestNodeGetInfo(t *testing.T) {

	// Init assert
	assert := assert.New(t)

	// Expected Result
	topology := &csi.Topology{
		Segments: map[string]string{
			TopologyKeyNode:          FakeNodeID,
			TopologyKeyArStorEnabled: "true"},
	}
	expectedRes := &csi.NodeGetInfoResponse{
		NodeId:             FakeNodeID,
		AccessibleTopology: topology,
		MaxVolumesPerNode:  maxVolume,
	}

	// Fake request
	fakeReq := &csi.NodeGetInfoRequest{}

	// Invoke NodeGetId
	var FakeCtx = context.Background()
	actualRes, err := fakeNs.NodeGetInfo(FakeCtx, fakeReq)
	if err != nil {
		t.Errorf("failed to NodeGetInfo: %v", err)
	}

	// Assert
	assert.Equal(expectedRes, actualRes)
}
