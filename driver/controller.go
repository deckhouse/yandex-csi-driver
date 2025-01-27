/*
Copyright 2020 DigitalOcean
Copyright 2020 Flant

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

package driver

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/genproto/protobuf/field_mask"

	"github.com/yandex-cloud/go-genproto/yandex/cloud/operation"

	"github.com/deckhouse/yandex-csi-driver/ychelpers"

	compute "github.com/yandex-cloud/go-genproto/yandex/cloud/compute/v1"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	_   = iota
	kiB = 1 << (10 * iota)
	miB
	giB
	tiB

	defaultListPageSize = 100
)

const (
	// minimumVolumeSizeInBytes is used to validate that the user is not trying
	// to create a volume that is smaller than what we support
	minimumVolumeSizeInBytes int64 = 4194304

	// maximumVolumeSizeInBytes is used to validate that the user is not trying
	// to create a volume that is larger than what we support
	maximumVolumeSizeInBytes int64 = 4398046511104

	// defaultVolumeSizeInBytes is used when the user did not provide a size or
	// the size they provided did not satisfy our requirements
	defaultVolumeSizeInBytes int64 = 5 * giB

	// createdByYandex is used to tag volumes that are created by this CSI plugin
	createdByYandex = "Created by Yandex CSI driver"

	regionTopologyKey = "failure-domain.beta.kubernetes.io/region"
	zoneTopologyKey   = "failure-domain.beta.kubernetes.io/zone"
)

var supportedAccessMode = &csi.VolumeCapability_AccessMode{
	Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
}

// CreateVolume creates a new volume from the given request. The function is
// idempotent.
func (d *Driver) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume Name must be provided")
	}

	if len(req.VolumeCapabilities) == 0 {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume Volume capabilities must be provided")
	}

	if violations := validateCapabilities(req.VolumeCapabilities); len(violations) > 0 {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("volume capabilities cannot be satisified: %s", strings.Join(violations, "; ")))
	}

	var typeID string
	if id, ok := req.Parameters["typeID"]; !ok {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume Volume parameter \"typeID\" must be provided")
	} else {
		typeID = id
	}

	size, err := extractStorage(req.CapacityRange)
	if err != nil {
		return nil, status.Errorf(codes.OutOfRange, "invalid capacity range: %v", err)
	}

	zone := d.zone
	region := d.region
	if req.AccessibilityRequirements != nil {
		for _, t := range req.AccessibilityRequirements.Requisite {
			regionStr, ok := t.Segments[regionTopologyKey]
			if ok {
				region = regionStr
			}

			zoneStr, ok := t.Segments[zoneTopologyKey]
			if ok {
				zone = zoneStr
			}
		}
	}

	volumeName := req.Name

	log := d.log.WithFields(logrus.Fields{
		"volume_name":            volumeName,
		"storage_size_gibibytes": size / giB,
		"method":                 "create_volume",
		"volume_capabilities":    req.VolumeCapabilities,
		"region":                 region,
		"zone":                   zone,
	})
	log.Info("create volume called")

	listDiskRequest := &compute.ListDisksRequest{
		FolderId: d.folderID,
		Filter:   "name=" + strconv.Quote(volumeName),
	}
	listDiskResp, err := d.sdk.Compute().Disk().List(ctx, listDiskRequest)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	disks := listDiskResp.Disks

	// volume already exists, do nothing
	if len(disks) != 0 {
		if len(disks) > 1 {
			return nil, fmt.Errorf("fatal issue: duplicate volume %q exists\n%+v", volumeName, disks)
		}
		vol := disks[0]

		if vol.Size != size {
			return nil, status.Error(codes.AlreadyExists, fmt.Sprintf("invalid option requested size: %d", size))
		}

		log.Info("volume already created")
		return &csi.CreateVolumeResponse{
			Volume: &csi.Volume{
				VolumeId:      vol.Id,
				CapacityBytes: vol.Size,
				AccessibleTopology: []*csi.Topology{
					{
						Segments: map[string]string{
							regionTopologyKey: region,
							zoneTopologyKey:   vol.ZoneId,
						},
					},
				},
			},
		}, nil
	}

	diskCreateRequest := &compute.CreateDiskRequest{
		FolderId:    d.folderID,
		Name:        volumeName,
		Description: createdByYandex,
		Labels:      map[string]string{"cluster_uuid": d.clusterUUID},
		TypeId:      typeID,
		ZoneId:      zone,
		Size:        size,
	}

	// TODO: Snapshots with GetVolumeContentSource

	log.WithField("volume_req", diskCreateRequest).Info("creating volume")
	result, _, err := ychelpers.WaitForResult(ctx, d.sdk, func() (*operation.Operation, error) {
		return d.sdk.Compute().Disk().Create(ctx, diskCreateRequest)
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	newDisk := result.(*compute.Disk)

	resp := &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      newDisk.Id,
			CapacityBytes: newDisk.Size,
			AccessibleTopology: []*csi.Topology{
				{
					Segments: map[string]string{
						regionTopologyKey: region,
						zoneTopologyKey:   zone,
					},
				},
			},
		},
	}

	log.WithField("response", resp).Info("volume was created")
	return resp, nil
}

// DeleteVolume deletes the given volume. The function is idempotent.
func (d *Driver) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "DeleteVolume Volume ID must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"volume_id": req.VolumeId,
		"method":    "delete_volume",
	})
	log.Info("delete volume called")

	response, _, err := ychelpers.WaitForResult(ctx, d.sdk, func() (*operation.Operation, error) {
		return d.sdk.Compute().Disk().Delete(ctx, &compute.DeleteDiskRequest{
			DiskId: req.VolumeId,
		})
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			// we assume it's deleted already for idempotency
			log.WithFields(logrus.Fields{
				"error": err,
				"resp":  err.Error(),
			}).Warn("assuming volume is deleted because it does not exist")
			return &csi.DeleteVolumeResponse{}, nil
		}

		return nil, status.Error(codes.Internal, err.Error())
	}

	log.WithField("response", response).Info("volume was deleted")
	return &csi.DeleteVolumeResponse{}, nil
}

// ControllerPublishVolume attaches the given volume to the node
func (d *Driver) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume Volume ID must be provided")
	}

	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume Node ID must be provided")
	}

	if req.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "ControllerPublishVolume Volume capability must be provided")
	}

	if req.Readonly {
		return nil, status.Error(codes.AlreadyExists, "read only Volumes are not supported")
	}

	log := d.log.WithFields(logrus.Fields{
		"volume_id":   req.VolumeId,
		"device_name": genDiskID(req.VolumeId),
		"node_id":     req.NodeId,
		"method":      "controller_publish_volume",
	})
	log.Info("controller publish volume called")

	if ok := d.resizeLocks.VolIdExists(req.VolumeId); ok {
		return nil, status.Errorf(codes.Internal, "Resize required, not publishing")
	}

	// check if volume exist before trying to attach it
	vol, err := d.sdk.Compute().Disk().Get(ctx, &compute.GetDiskRequest{DiskId: req.VolumeId})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, status.Errorf(codes.NotFound, "volume %q does not exist", req.VolumeId)
		}
		return nil, err
	}

	// check if droplet exist before trying to attach the volume to the droplet
	_, err = d.sdk.Compute().Instance().Get(ctx, &compute.GetInstanceRequest{InstanceId: req.NodeId})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, status.Errorf(codes.NotFound, "instance %q does not exist", req.NodeId)
		}
		return nil, err
	}

	for _, id := range vol.InstanceIds {
		if id == req.NodeId {
			log.Info("volume is already attached")
			return &csi.ControllerPublishVolumeResponse{
				PublishContext: map[string]string{
					d.publishInfoVolumeName: vol.Name,
				},
			}, nil
		}
	}

	// droplet is attached to a different node, return an error
	if len(vol.InstanceIds) != 0 {
		return nil, status.Errorf(codes.FailedPrecondition,
			"Disk %q is attached to the wrong Instance(s) (%v), detach the Disk to fix it",
			req.VolumeId, vol.InstanceIds)
	}

	// attach the volume to the correct node
	_, _, err = ychelpers.WaitForResult(ctx, d.sdk, func() (*operation.Operation, error) {
		return d.sdk.Compute().Instance().AttachDisk(ctx, &compute.AttachInstanceDiskRequest{
			InstanceId: req.NodeId,
			AttachedDiskSpec: &compute.AttachedDiskSpec{
				Mode:       compute.AttachedDiskSpec_READ_WRITE,
				DeviceName: genDiskID(req.VolumeId),
				AutoDelete: false,
				Disk:       &compute.AttachedDiskSpec_DiskId{DiskId: req.VolumeId},
			},
		})
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	log.Info("volume was attached")
	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			d.publishInfoVolumeName: vol.Name,
		},
	}, nil
}

// ControllerUnpublishVolume deattaches the given volume from the node
func (d *Driver) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "ControllerUnpublishVolume Volume ID must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"volume_id": req.VolumeId,
		"node_id":   req.NodeId,
		"method":    "controller_unpublish_volume",
	})
	log.Info("controller unpublish volume called")

	// check if volume exist before trying to detach it
	disk, err := d.sdk.Compute().Disk().Get(ctx, &compute.GetDiskRequest{DiskId: req.VolumeId})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			log.Info("assuming Disk is detached because it does not exist")
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, err
	}

	if len(disk.InstanceIds) == 0 {
		log.Info("assuming Disk is detached it's not attached to any Instance")
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	// check if Instance exist before trying to attach the volume to the droplet
	_, err = d.sdk.Compute().Instance().Get(ctx, &compute.GetInstanceRequest{InstanceId: req.NodeId})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			log.Info("assuming Disk is detached because Instance does not exist")
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, err
	}

	_, _, err = ychelpers.WaitForResult(ctx, d.sdk, func() (*operation.Operation, error) {
		return d.sdk.Compute().Instance().DetachDisk(ctx, &compute.DetachInstanceDiskRequest{
			InstanceId: req.NodeId,
			Disk:       &compute.DetachInstanceDiskRequest_DiskId{DiskId: req.VolumeId},
		})
	},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	log.Info("volume was detached")
	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

// ValidateVolumeCapabilities checks whether the volume capabilities requested
// are supported.
func (d *Driver) ValidateVolumeCapabilities(_ context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "ValidateVolumeCapabilities Volume ID must be provided")
	}

	if req.VolumeCapabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "ValidateVolumeCapabilities Volume Capabilities must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"volume_id":              req.VolumeId,
		"volume_capabilities":    req.VolumeCapabilities,
		"supported_capabilities": supportedAccessMode,
		"method":                 "validate_volume_capabilities",
	})
	log.Info("validate volume capabilities called")

	// if it's not supported (i.e: wrong region), we shouldn't override it
	resp := &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: supportedAccessMode,
				},
			},
		},
	}

	log.WithField("confirmed", resp.Confirmed).Info("supported capabilities")
	return resp, nil
}

// ListVolumes returns a list of all requested volumes
func (d *Driver) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	var err error

	var pageSize int64
	if req.MaxEntries != 0 {
		pageSize = int64(req.MaxEntries)
	} else {
		pageSize = defaultListPageSize
	}

	listOpts := &compute.ListDisksRequest{
		FolderId:  d.folderID,
		PageSize:  pageSize,
		PageToken: req.StartingToken,
	}

	log := d.log.WithFields(logrus.Fields{
		"list_opts":          listOpts,
		"req_starting_token": req.StartingToken,
		"method":             "compu",
	})
	log.Info("list volumes called")

	listRes, err := d.sdk.Compute().Disk().List(ctx, listOpts)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	var entries []*csi.ListVolumesResponse_Entry
	for _, disk := range listRes.Disks {
		entries = append(entries, &csi.ListVolumesResponse_Entry{
			Volume: &csi.Volume{
				VolumeId:      disk.Id,
				CapacityBytes: disk.Size,
			},
		})
	}

	resp := &csi.ListVolumesResponse{
		Entries:   entries,
		NextToken: listRes.NextPageToken,
	}

	log.WithField("response", resp).Info("volumes listed")
	return resp, nil
}

// GetCapacity returns the capacity of the storage pool
func (d *Driver) GetCapacity(_ context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	d.log.WithFields(logrus.Fields{
		"params": req.Parameters,
		"method": "get_capacity",
	}).Warn("get capacity is not implemented")
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerGetCapabilities returns the capabilities of the controller service.
func (d *Driver) ControllerGetCapabilities(_ context.Context, _ *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	newCap := func(cap csi.ControllerServiceCapability_RPC_Type) *csi.ControllerServiceCapability {
		return &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		}
	}

	var caps []*csi.ControllerServiceCapability
	for _, capability := range []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
	} {
		caps = append(caps, newCap(capability))
	}

	resp := &csi.ControllerGetCapabilitiesResponse{
		Capabilities: caps,
	}

	d.log.WithFields(logrus.Fields{
		"response": resp,
		"method":   "controller_get_capabilities",
	}).Info("controller get capabilities called")
	return resp, nil
}

// ControllerExpandVolume is called from the resizer to increase the volume size.
func (d *Driver) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	volID := req.GetVolumeId()

	if len(volID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "ControllerExpandVolume volume ID missing in request")
	}
	disk, err := d.sdk.Compute().Disk().Get(ctx, &compute.GetDiskRequest{DiskId: req.VolumeId})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "ControllerExpandVolume could not retrieve existing volume: %v", err)
	}

	resizeBytes, err := extractStorage(req.GetCapacityRange())
	if err != nil {
		return nil, status.Errorf(codes.OutOfRange, "ControllerExpandVolume invalid capacity range: %v", err)
	}

	log := d.log.WithFields(logrus.Fields{
		"volume_id": req.VolumeId,
		"method":    "controller_expand_volume",
	})

	log.Info("controller expand volume called")

	if resizeBytes <= disk.Size {
		log.WithFields(logrus.Fields{
			"current_volume_size":   disk.Size,
			"requested_volume_size": resizeBytes,
		}).Info("skipping volume resize because current volume size exceeds requested volume size")
		// even if the volume is resized independently from the control panel, we still need to resize the node fs when resize is requested
		// in this case, the claim capacity will be resized to the volume capacity, requested capcity will be ignored to make the PV and PVC capacities consistent
		return &csi.ControllerExpandVolumeResponse{CapacityBytes: disk.Size, NodeExpansionRequired: true}, nil
	}

	_, _, err = ychelpers.WaitForResult(ctx, d.sdk, func() (o *operation.Operation, err error) {
		return d.sdk.Compute().Disk().Update(ctx, &compute.UpdateDiskRequest{
			DiskId: req.VolumeId,
			UpdateMask: &field_mask.FieldMask{
				Paths: []string{"size"},
			},
			Size: resizeBytes,
		})
	})
	if err != nil {
		if status.Code(err) == codes.FailedPrecondition && strings.Contains(status.Convert(err).Message(), "attached to non-stopped instance") {
			d.resizeLocks.PutVolId(req.VolumeId)
			return nil, status.Errorf(codes.Internal, "cannot resize volume %s: %s", req.GetVolumeId(), err.Error())
		}
		return nil, status.Errorf(codes.Internal, "cannot resize volume %s: %s", req.GetVolumeId(), err.Error())
	}
	d.resizeLocks.RemoveVolId(req.VolumeId)

	log = log.WithField("new_volume_size", resizeBytes)
	log.Info("volume was resized")

	nodeExpansionRequired := true
	if req.GetVolumeCapability() != nil {
		switch req.GetVolumeCapability().GetAccessType().(type) {
		case *csi.VolumeCapability_Block:
			log.Info("node expansion is not required for block volumes")
			nodeExpansionRequired = false
		}
	}

	return &csi.ControllerExpandVolumeResponse{CapacityBytes: resizeBytes, NodeExpansionRequired: nodeExpansionRequired}, nil
}

// extractStorage extracts the storage size in bytes from the given capacity
// range. If the capacity range is not satisfied it returns the default volume
// size. If the capacity range is below or above supported sizes, it returns an
// error.
func extractStorage(capRange *csi.CapacityRange) (int64, error) {
	if capRange == nil {
		return defaultVolumeSizeInBytes, nil
	}

	requiredBytes := capRange.GetRequiredBytes()
	requiredSet := 0 < requiredBytes
	limitBytes := capRange.GetLimitBytes()
	limitSet := 0 < limitBytes

	if !requiredSet && !limitSet {
		return defaultVolumeSizeInBytes, nil
	}

	if requiredSet && limitSet && limitBytes < requiredBytes {
		return 0, fmt.Errorf("limit (%v) can not be less than required (%v) size", formatBytes(limitBytes), formatBytes(requiredBytes))
	}

	if requiredSet && !limitSet && requiredBytes < minimumVolumeSizeInBytes {
		return 0, fmt.Errorf("required (%v) can not be less than minimum supported volume size (%v)", formatBytes(requiredBytes), formatBytes(minimumVolumeSizeInBytes))
	}

	if limitSet && limitBytes < minimumVolumeSizeInBytes {
		return 0, fmt.Errorf("limit (%v) can not be less than minimum supported volume size (%v)", formatBytes(limitBytes), formatBytes(minimumVolumeSizeInBytes))
	}

	if requiredSet && requiredBytes > maximumVolumeSizeInBytes {
		return 0, fmt.Errorf("required (%v) can not exceed maximum supported volume size (%v)", formatBytes(requiredBytes), formatBytes(maximumVolumeSizeInBytes))
	}

	if !requiredSet && limitSet && limitBytes > maximumVolumeSizeInBytes {
		return 0, fmt.Errorf("limit (%v) can not exceed maximum supported volume size (%v)", formatBytes(limitBytes), formatBytes(maximumVolumeSizeInBytes))
	}

	if requiredSet && limitSet && requiredBytes == limitBytes {
		return requiredBytes, nil
	}

	if requiredSet {
		return requiredBytes, nil
	}

	if limitSet {
		return limitBytes, nil
	}

	return defaultVolumeSizeInBytes, nil
}

func formatBytes(inputBytes int64) string {
	output := float64(inputBytes)
	unit := ""

	switch {
	case inputBytes >= tiB:
		output = output / tiB
		unit = "Ti"
	case inputBytes >= giB:
		output = output / giB
		unit = "Gi"
	case inputBytes >= miB:
		output = output / miB
		unit = "Mi"
	case inputBytes >= kiB:
		output = output / kiB
		unit = "Ki"
	case inputBytes == 0:
		return "0"
	}

	result := strconv.FormatFloat(output, 'f', 1, 64)
	result = strings.TrimSuffix(result, ".0")
	return result + unit
}

// validateCapabilities validates the requested capabilities. It returns a list
// of violations which may be empty if no violatons were found.
func validateCapabilities(caps []*csi.VolumeCapability) []string {
	violations := sets.NewString()
	for _, capability := range caps {
		if capability.GetAccessMode().GetMode() != supportedAccessMode.GetMode() {
			violations.Insert(fmt.Sprintf("unsupported access mode %s", capability.GetAccessMode().GetMode().String()))
		}

		accessType := capability.GetAccessType()
		switch accessType.(type) {
		case *csi.VolumeCapability_Block:
		case *csi.VolumeCapability_Mount:
		default:
			violations.Insert("unsupported access type")
		}
	}

	return violations.List()
}
