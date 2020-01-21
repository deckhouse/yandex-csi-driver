# yandex-csi-driver

A Container Storage Interface ([CSI](https://github.com/container-storage-interface/spec)) Driver for [Yandex Compute Disks](https://cloud.yandex.ru/docs/compute/concepts/disk).

## Releases

The Yandex CSI driver follows [semantic versioning](https://semver.org/).
The version will be bumped following the rules below:

* Bug fixes will be released as a `PATCH` update.
* New features (such as CSI spec bumps with no breaking changes) will be released as a `MINOR` update.
* Significant breaking changes makes a `MAJOR` update.

## Features

Below is a list of functionality implemented by the plugin.

### Volume Expansion

Volumes can be expanded by updating the storage request value of the corresponding PVC:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: csi-pvc
  namespace: default
spec:
  [...]
  resources:
    requests:
      # The field below can be increased.
      storage: 10Gi
      [...]
```

After successful expansion, the _status_ section of the PVC object will reflect the actual volume capacity.

Important notes:

* Since Yandex.Cloud Compute Disks **do not** support online resize yet, after resizing a PVC object you have to manually delete a Pod object so that a Disk detaches from Instance, allowing to resize itself.
* Volumes can only be increased in size, not decreased; attempts to do so will lead to an error.
* Expanding a volume that is larger than the target size will have no effect. The PVC object status section will continue to represent the actual volume capacity.
* Resizing volumes other than through the PVC object (e.g., the DigitalOcean cloud control panel) is not recommended as this can potentially cause conflicts. Additionally, size updates will not be reflected in the PVC object status section immediately, and the section will eventually show the actual volume capacity.

### Volume Statistics

Volume statistics are exposed through the CSI-conformant endpoints. Monitoring systems such as Prometheus can scrape metrics and provide insights into volume usage.

## Installing to Kubernetes

### Kubernetes Compatibility

yandex-csi-driver is supported in Kubernetes >=1.15 only.

**Requirements:**

* `--allow-privileged` flag must be set to true for both the API server and the kubelet
* `--feature-gates=VolumeSnapshotDataSource=true,KubeletPluginsWatcher=true,CSINodeInfo=true,CSIDriverRegistry=true` feature gate flags must be set to true for both the API server and the kubelet
* Mount Propagation needs to be enabled. If you use Docker, the Docker daemon of the cluster nodes must allow shared mounts.

### Installing driver

Simply install everyhting from the [deploy path](deploy/1.17). Be sure to replace:
 * [ServiceAccountJSON key](deploy/1.17/secret.yaml) with the appropriate key from your Yandex.Cloud account.
 * [--folder-id](deploy/1.17/csi-controller.yaml) with an appropriate Yandex.Cloud folder ID.