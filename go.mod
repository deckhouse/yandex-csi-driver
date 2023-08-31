module github.com/deckhouse/yandex-csi-driver

require (
	github.com/container-storage-interface/spec v1.2.0
	github.com/golang/protobuf v1.5.2
	github.com/sirupsen/logrus v1.4.2
	github.com/yandex-cloud/go-genproto v0.0.0-20230828085742-c924b3bfee75
	github.com/yandex-cloud/go-sdk v0.0.0-20230828093337-d401bc1eeaba
	golang.org/x/sync v0.0.0-20190911185100-cd5d95a43a6e
	golang.org/x/sys v0.0.0-20210510120138-977fb7262007
	google.golang.org/genproto v0.0.0-20211021150943-2b146023228c
	google.golang.org/grpc v1.41.0
	google.golang.org/protobuf v1.31.0
	k8s.io/apimachinery v0.17.1
	k8s.io/klog v1.0.0
	k8s.io/kubernetes v1.17.1
	k8s.io/utils v0.0.0-20191114184206-e782cd3c129f
)

go 1.15

replace k8s.io/api => k8s.io/api v0.17.1

replace k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.17.1

replace k8s.io/apimachinery => k8s.io/apimachinery v0.17.1

replace k8s.io/apiserver => k8s.io/apiserver v0.17.1

replace k8s.io/cli-runtime => k8s.io/cli-runtime v0.17.1

replace k8s.io/client-go => k8s.io/client-go v0.17.1

replace k8s.io/cloud-provider => k8s.io/cloud-provider v0.17.1

replace k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.17.1

replace k8s.io/code-generator => k8s.io/code-generator v0.17.1-beta.0

replace k8s.io/component-base => k8s.io/component-base v0.17.1

replace k8s.io/cri-api => k8s.io/cri-api v0.17.1

replace k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.17.1

replace k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.17.1

replace k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.17.1

replace k8s.io/kube-proxy => k8s.io/kube-proxy v0.17.1

replace k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.17.1

replace k8s.io/kubectl => k8s.io/kubectl v0.17.1

replace k8s.io/kubelet => k8s.io/kubelet v0.17.1

replace k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.17.1

replace k8s.io/metrics => k8s.io/metrics v0.17.1

replace k8s.io/node-api => k8s.io/node-api v0.17.1

replace k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.17.1

replace k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.17.1

replace k8s.io/sample-controller => k8s.io/sample-controller v0.17.1
