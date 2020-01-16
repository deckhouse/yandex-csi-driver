package driver

import (
	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/sirupsen/logrus"
)

// GetPluginInfo returns metadata of the plugin
func (d *Driver) GetPluginInfo(_ context.Context, _ *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	resp := &csi.GetPluginInfoResponse{
		Name:          d.name,
		VendorVersion: version,
	}

	d.log.WithFields(logrus.Fields{
		"response": resp,
		"method":   "get_plugin_info",
	}).Info("get plugin info called")
	return resp, nil
}

// GetPluginCapabilities returns available capabilities of the plugin
func (d *Driver) GetPluginCapabilities(_ context.Context, _ *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	resp := &csi.GetPluginCapabilitiesResponse{
		Capabilities: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
			{
				Type: &csi.PluginCapability_VolumeExpansion_{
					VolumeExpansion: &csi.PluginCapability_VolumeExpansion{
						Type: csi.PluginCapability_VolumeExpansion_OFFLINE,
					},
				},
			},
		},
	}

	d.log.WithFields(logrus.Fields{
		"response": resp,
		"method":   "get_plugin_capabilities",
	}).Info("get plugin capabitilies called")
	return resp, nil
}

// Probe returns the health and readiness of the plugin
func (d *Driver) Probe(_ context.Context, _ *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	d.log.WithField("method", "probe").Info("probe called")
	d.readyMu.Lock()
	defer d.readyMu.Unlock()

	return &csi.ProbeResponse{
		Ready: &wrappers.BoolValue{
			Value: d.ready,
		},
	}, nil
}
