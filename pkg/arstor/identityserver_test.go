package arstor

import (
	"context"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
)

var FakeDriverName = "arstor.csi.huayun.io"
var FakeVersion = "1.0.0"

func TestGetPluginInfo(t *testing.T) {
	ids := NewIdentityServer(FakeDriverName, FakeVersion)

	req := csi.GetPluginInfoRequest{}
	resp, err := ids.GetPluginInfo(context.Background(), &req)
	assert.NoError(t, err)
	assert.Equal(t, resp.GetName(), FakeDriverName)
	assert.Equal(t, resp.GetVendorVersion(), FakeVersion)
}

func TestProbe(t *testing.T) {
	ids := NewIdentityServer(FakeDriverName, FakeVersion)

	req := csi.ProbeRequest{}
	_, err := ids.Probe(context.Background(), &req)
	assert.NoError(t, err)
}
