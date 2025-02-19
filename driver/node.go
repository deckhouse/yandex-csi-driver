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
	"path/filepath"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/sirupsen/logrus"
	"github.com/yandex-cloud/go-genproto/yandex/cloud/compute/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog"
	mountutil "k8s.io/mount-utils"
	"k8s.io/utils/exec"
	"k8s.io/utils/mount"
)

const (
	diskIDPath = "/dev/disk/by-id"

	volumeModeBlock      = "block"
	volumeModeFilesystem = "filesystem"

	HasFilesystemErrors MountErrorType = "HasFilesystemErrors"
	// 'fsck' found errors and corrected them
	fsckErrorsCorrected = 1
	// 'fsck' found errors but exited without correcting them
	fsckErrorsUncorrected = 4
)

type MountErrorType string

type MountError struct { // nolint: golint
	Type    MountErrorType
	Message string
}

func (mountError MountError) String() string {
	return mountError.Message
}

func (mountError MountError) Error() string {
	return mountError.Message
}

func NewMountError(mountErrorValue MountErrorType, format string, args ...interface{}) error {
	mountError := MountError{
		Type:    mountErrorValue,
		Message: fmt.Sprintf(format, args...),
	}
	return mountError
}

var (
	// This annotation is added to a PV to indicate that the volume should be
	// not formatted. Useful for cases if the user wants to reuse an existing
	// volume.
	annsNoFormatVolume = []string{
		"com.flant.csi.yandex/noformat",
	}
)

// checkAndRepairFileSystem checks and repairs filesystems using command fsck.
// ported from original k8s mount-utils library https://github.com/kubernetes/mount-utils/blob/master/mount_linux.go#L450
func checkAndRepairFilesystem(source string) error {
	klog.V(4).Infof("Checking for issues with fsck on disk: %s", source)
	args := []string{"-a", source}
	executor := exec.New()
	out, err := executor.Command("fsck", args...).CombinedOutput()
	if err != nil {
		ee, isExitError := err.(exec.ExitError)
		switch {
		case err == exec.ErrExecutableNotFound:
			klog.Warningf("'fsck' not found on system; continuing mount without running 'fsck'.")
		case isExitError && ee.ExitStatus() == fsckErrorsCorrected:
			klog.Infof("Device %s has errors which were corrected by fsck.", source)
		case isExitError && ee.ExitStatus() == fsckErrorsUncorrected:
			return NewMountError(HasFilesystemErrors, "'fsck' found errors on device %s but could not correct them: %s", source, string(out))
		case isExitError && ee.ExitStatus() > fsckErrorsUncorrected:
			klog.Infof("`fsck` error %s", string(out))
		default:
			klog.Warningf("fsck on device %s failed with error %v, output: %v", source, err, string(out))
		}
	}
	return nil
}

// NodeStageVolume mounts the volume to a staging path on the node. This is
// called by the CO before NodePublishVolume and is used to temporary mount the
// volume to a staging path. Once mounted, NodePublishVolume will make sure to
// mount it to the appropriate path
func (d *Driver) NodeStageVolume(_ context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume Volume ID must be provided")
	}

	if req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume Staging Target Path must be provided")
	}

	if req.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume Volume Capability must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"volume_id":           req.VolumeId,
		"staging_target_path": req.StagingTargetPath,
		"method":              "node_stage_volume",
	})
	log.Info("node stage volume called")

	// If it is a block volume, we do nothing for stage volume
	// because we bind mount the absolute device path to a file
	switch req.VolumeCapability.GetAccessType().(type) {
	case *csi.VolumeCapability_Block:
		return &csi.NodeStageVolumeResponse{}, nil
	}

	source := getDeviceByIDPath(req.VolumeId)
	target := req.StagingTargetPath

	mnt := req.VolumeCapability.GetMount()
	options := mnt.MountFlags

	fsType := "ext4"
	if mnt.FsType != "" {
		fsType = mnt.FsType
	}

	log = d.log.WithFields(logrus.Fields{
		"volume_mode":     volumeModeFilesystem,
		"volume_name":     req.VolumeId,
		"volume_context":  req.VolumeContext,
		"publish_context": req.PublishContext,
		"source":          source,
		"fs_type":         fsType,
		"mount_options":   options,
	})

	var noFormat bool
	for _, ann := range annsNoFormatVolume {
		_, noFormat = req.VolumeContext[ann]
		if noFormat {
			break
		}
	}
	if noFormat {
		log.Info("skipping formatting the source device")
	} else {
		formatted, err := d.mounter.IsFormatted(source)
		if err != nil {
			return nil, err
		}

		if !formatted {
			log.Info("formatting the volume for staging")
			if err := d.mounter.Format(source, fsType); err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
		} else {
			log.Info("source device is already formatted")
		}
	}

	err := checkAndRepairFilesystem(source)
	if err != nil {
		return nil, err
	}

	log.Info("mounting the volume for staging")

	mounted, err := d.mounter.IsMounted(target)
	if err != nil {
		return nil, err
	}

	if !mounted {
		if err := d.mounter.Mount(source, target, fsType, options...); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	} else {
		log.Info("source device is already mounted to the target path")
	}

	log.Info("formatting and mounting stage volume is finished")
	return &csi.NodeStageVolumeResponse{}, nil
}

// NodeUnstageVolume unstages the volume from the staging path
func (d *Driver) NodeUnstageVolume(_ context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume Volume ID must be provided")
	}

	if req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume Staging Target Path must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"volume_id":           req.VolumeId,
		"staging_target_path": req.StagingTargetPath,
		"method":              "node_unstage_volume",
	})
	log.Info("node unstage volume called")

	mounted, err := d.mounter.IsMounted(req.StagingTargetPath)
	if err != nil {
		return nil, err
	}

	if mounted {
		log.Info("unmounting the staging target path")
		err := d.mounter.Unmount(req.StagingTargetPath)
		if err != nil {
			return nil, err
		}
	} else {
		log.Info("staging target path is already unmounted")
	}

	log.Info("unmounting stage volume is finished")
	return &csi.NodeUnstageVolumeResponse{}, nil
}

// NodePublishVolume mounts the volume mounted to the staging path to the target path
func (d *Driver) NodePublishVolume(_ context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Volume ID must be provided")
	}

	if req.StagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Staging Target Path must be provided")
	}

	if req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Target Path must be provided")
	}

	if req.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume Volume Capability must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"volume_id":           req.VolumeId,
		"staging_target_path": req.StagingTargetPath,
		"target_path":         req.TargetPath,
		"method":              "node_publish_volume",
	})
	log.Info("node publish volume called")

	options := []string{"bind"}
	if req.Readonly {
		options = append(options, "ro")
	}

	var err error
	switch req.GetVolumeCapability().GetAccessType().(type) {
	case *csi.VolumeCapability_Block:
		err = d.nodePublishVolumeForBlock(req, options, log)
	case *csi.VolumeCapability_Mount:
		err = d.nodePublishVolumeForFileSystem(req, options, log)
	default:
		return nil, status.Error(codes.InvalidArgument, "Unknown access type")
	}

	if err != nil {
		return nil, err
	}

	log.Info("bind mounting the volume is finished")
	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume unmounts the volume from the target path
func (d *Driver) NodeUnpublishVolume(_ context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeUnpublishVolume Volume ID must be provided")
	}

	if req.TargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeUnpublishVolume Target Path must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"volume_id":   req.VolumeId,
		"target_path": req.TargetPath,
		"method":      "node_unpublish_volume",
	})
	log.Info("node unpublish volume called")

	mounted, err := d.mounter.IsMounted(req.TargetPath)
	if err != nil {
		return nil, err
	}

	if mounted {
		log.Info("unmounting the target path")
		err := d.mounter.Unmount(req.TargetPath)
		if err != nil {
			return nil, err
		}
	} else {
		log.Info("target path is already unmounted")
	}

	log.Info("unmounting volume is finished")
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeGetCapabilities returns the supported capabilities of the node server
func (d *Driver) NodeGetCapabilities(_ context.Context, _ *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	nscaps := []*csi.NodeServiceCapability{
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
		{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
				},
			},
		},
	}

	d.log.WithFields(logrus.Fields{
		"node_capabilities": nscaps,
		"method":            "node_get_capabilities",
	}).Info("node get capabilities called")
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: nscaps,
	}, nil
}

// NodeGetInfo returns the supported capabilities of the node server. This
// should eventually return the droplet ID if possible. This is used so the CO
// knows where to place the workload. The result of this function will be used
// by the CO in ControllerPublishVolume.
func (d *Driver) NodeGetInfo(_ context.Context, _ *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	d.log.WithField("method", "node_get_info").Info("node get info called")

	instance, err := d.sdk.Compute().Instance().Get(context.TODO(), &compute.GetInstanceRequest{
		InstanceId: d.hostID,
		View:       0,
	})
	if err != nil {
		return nil, err
	}

	const bootDisksCount = 1
	networkInterfacesCount := int64(len(instance.GetNetworkInterfaces()))
	secondaryDisksCount := int64(len(instance.GetSecondaryDisks()))
	localDisksCount := int64(len(instance.GetLocalDisks()))
	filesystemsCount := int64(len(instance.GetFilesystems()))

	var nonCSISecondaryDisksCount = localDisksCount + filesystemsCount
	for _, disk := range instance.GetSecondaryDisks() {
		if !strings.HasPrefix(disk.DeviceName, "csi-") {
			nonCSISecondaryDisksCount++
		}
	}

	// See: https://cloud.yandex.ru/docs/compute/concepts/limits
	// When a VM is starting, a maximum of 14 devices, including the boot disk and a NIC, can be connected to it. Other devices must be connected to a running VM. Please note: If you restart a VM with more than 14 devices connected, it will not be able to load and run.
	var disksLimit int64

	switch instance.GetPlatformId() {
	// Intel Broadwell, Intel Broadwell with NVIDIA® Tesla® V100
	case "standard-v1", "gpu-standard-v1":
		if instance.GetResources().Cores > 18 {
			disksLimit = 14
		} else {
			disksLimit = 8
		}
	// Intel Cascade Lake, Intel Cascade Lake with NVIDIA® Tesla® V100
	case "standard-v2", "gpu-standard-v2":
		if instance.GetResources().Cores > 20 {
			disksLimit = 14
		} else {
			disksLimit = 8
		}
	// Intel Ice Lake, Ice Lake Compute-optimized
	case "standard-v3", "highfreq-v3":
		if instance.GetResources().Cores > 32 {
			disksLimit = 14
		} else {
			disksLimit = 8
		}
	// other
	default:
		disksLimit = 8
	}

	var maxVolumesPerNode int64

	if bootDisksCount+secondaryDisksCount+networkInterfacesCount < 14 {
		maxVolumesPerNode = disksLimit - bootDisksCount - nonCSISecondaryDisksCount
	} else {
		maxVolumesPerNode = disksLimit - bootDisksCount - nonCSISecondaryDisksCount - networkInterfacesCount
	}

	d.log.WithFields(logrus.Fields{
		"secondaryDisksCount":       secondaryDisksCount,
		"localDisksCount":           localDisksCount,
		"filesystemsCount":          filesystemsCount,
		"networkInterfacesCount":    networkInterfacesCount,
		"nonCSISecondaryDisksCount": nonCSISecondaryDisksCount,
		"disksLimit":                disksLimit,
		"maxVolumesPerNode":         maxVolumesPerNode,
	}).Info()

	return &csi.NodeGetInfoResponse{
		NodeId:            d.hostID,
		MaxVolumesPerNode: maxVolumesPerNode,

		// make sure that the driver works on this particular region only
		AccessibleTopology: &csi.Topology{
			Segments: map[string]string{
				regionTopologyKey: d.region,
				zoneTopologyKey:   d.zone,
			},
		},
	}, nil
}

// NodeGetVolumeStats returns the volume capacity statistics available for the
// the given volume.
func (d *Driver) NodeGetVolumeStats(_ context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	if req.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeGetVolumeStats Volume ID must be provided")
	}

	volumePath := req.VolumePath
	if volumePath == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeGetVolumeStats Volume Path must be provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"volume_id":   req.VolumeId,
		"volume_path": req.VolumePath,
		"method":      "node_get_volume_stats",
	})
	log.Info("node get volume stats called")

	mounted, err := d.mounter.IsMounted(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to check if volume path %q is mounted: %s", volumePath, err)
	}

	if !mounted {
		return nil, status.Errorf(codes.NotFound, "volume path %q is not mounted", volumePath)
	}

	isBlock, err := d.mounter.IsBlockDevice(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to determine if %q is block device: %s", volumePath, err)
	}

	stats, err := d.mounter.GetStatistics(volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to retrieve capacity statistics for volume path %q: %s", volumePath, err)
	}

	// only can retrieve total capacity for a block device
	if isBlock {
		log.WithFields(logrus.Fields{
			"volume_mode": volumeModeBlock,
			"bytes_total": stats.totalBytes,
		}).Info("node capacity statistics retrieved")

		return &csi.NodeGetVolumeStatsResponse{
			Usage: []*csi.VolumeUsage{
				{
					Unit:  csi.VolumeUsage_BYTES,
					Total: stats.totalBytes,
				},
			},
		}, nil
	}

	log.WithFields(logrus.Fields{
		"volume_mode":      volumeModeFilesystem,
		"bytes_available":  stats.availableBytes,
		"bytes_total":      stats.totalBytes,
		"bytes_used":       stats.usedBytes,
		"inodes_available": stats.availableInodes,
		"inodes_total":     stats.totalInodes,
		"inodes_used":      stats.usedInodes,
	}).Info("node capacity statistics retrieved")

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: stats.availableBytes,
				Total:     stats.totalBytes,
				Used:      stats.usedBytes,
				Unit:      csi.VolumeUsage_BYTES,
			},
			{
				Available: stats.availableInodes,
				Total:     stats.totalInodes,
				Used:      stats.usedInodes,
				Unit:      csi.VolumeUsage_INODES,
			},
		},
	}, nil
}

func (d *Driver) NodeExpandVolume(_ context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeExpandVolume volume ID not provided")
	}

	volumePath := req.GetVolumePath()
	if len(volumePath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeExpandVolume volume path not provided")
	}

	log := d.log.WithFields(logrus.Fields{
		"volume_id":   req.VolumeId,
		"volume_path": req.VolumePath,
		"method":      "node_expand_volume",
	})
	log.Info("node expand volume called")

	if req.GetVolumeCapability() != nil {
		switch req.GetVolumeCapability().GetAccessType().(type) {
		case *csi.VolumeCapability_Block:
			log.Info("filesystem expansion is skipped for block volumes")
			return &csi.NodeExpandVolumeResponse{}, nil
		}
	}

	mounter := mount.New("")
	devicePath, _, err := mount.GetDeviceNameFromMount(mounter, volumePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "NodeExpandVolume unable to get device path for %q: %v", volumePath, err)
	}

	r := mountutil.NewResizeFs(exec.New())

	log = log.WithFields(logrus.Fields{
		"device_path": devicePath,
	})
	log.Info("resizing volume")
	if _, err := r.Resize(devicePath, volumePath); err != nil {
		return nil, status.Errorf(codes.Internal, "NodeExpandVolume could not resize volume %q (%q):  %v", volumeID, req.GetVolumePath(), err)
	}

	log.Info("volume was resized")
	return &csi.NodeExpandVolumeResponse{}, nil
}

func (d *Driver) nodePublishVolumeForFileSystem(req *csi.NodePublishVolumeRequest, mountOptions []string, log *logrus.Entry) error {
	source := req.StagingTargetPath
	target := req.TargetPath

	mnt := req.VolumeCapability.GetMount()
	mountOptions = append(mountOptions, mnt.MountFlags...)

	fsType := "ext4"
	if mnt.FsType != "" {
		fsType = mnt.FsType
	}

	mounted, err := d.mounter.IsMounted(target)
	if err != nil {
		return err
	}

	log = log.WithFields(logrus.Fields{
		"source_path":   source,
		"volume_mode":   volumeModeFilesystem,
		"fs_type":       fsType,
		"mount_options": mountOptions,
	})

	if !mounted {
		log.Info("mounting the volume")
		if err := d.mounter.Mount(source, target, fsType, mountOptions...); err != nil {
			return status.Error(codes.Internal, err.Error())
		}
	} else {
		log.Info("volume is already mounted")
	}

	return nil
}

func (d *Driver) nodePublishVolumeForBlock(req *csi.NodePublishVolumeRequest, mountOptions []string, log *logrus.Entry) error {
	source, err := findAbsoluteDeviceByIDPath(req.VolumeId)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to find device path for volume %s. %v", req.VolumeId, err)
	}

	target := req.TargetPath

	mounted, err := d.mounter.IsMounted(target)
	if err != nil {
		return err
	}

	log = log.WithFields(logrus.Fields{
		"source_path":   source,
		"volume_mode":   volumeModeBlock,
		"mount_options": mountOptions,
	})

	if !mounted {
		log.Info("mounting the volume")
		if err := d.mounter.Mount(source, target, "", mountOptions...); err != nil {
			return status.Error(codes.Internal, err.Error())
		}
	} else {
		log.Info("volume is already mounted")
	}

	return nil
}

func getDeviceByIDPath(volumeName string) string {
	return filepath.Join(diskIDPath, "virtio-"+genDiskID(volumeName))
}

// findAbsoluteDeviceByIDPath follows the /dev/disk/by-id symlink to find the absolute path of a device
func findAbsoluteDeviceByIDPath(volumeName string) (string, error) {
	path := getDeviceByIDPath(volumeName)

	// EvalSymlinks returns relative link if the file is not a symlink
	// so we do not have to check if it is symlink prior to evaluation
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("could not resolve symlink %q: %v", path, err)
	}

	if !strings.HasPrefix(resolved, "/dev") {
		return "", fmt.Errorf("resolved symlink %q for %q was unexpected", resolved, path)
	}

	return resolved, nil
}
