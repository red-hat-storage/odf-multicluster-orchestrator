module github.com/red-hat-storage/odf-multicluster-orchestrator

go 1.16

require (
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.14.0
	github.com/openshift/library-go v0.0.0-20210727084322-8a96c0a97c06
	github.com/spf13/cobra v1.1.3
	golang.org/x/sync v0.0.0-20201207232520-09787c993a3a
	k8s.io/api v0.21.3
	k8s.io/apimachinery v0.21.3
	k8s.io/client-go v0.21.3
	k8s.io/klog/v2 v2.8.0
	open-cluster-management.io/addon-framework v0.0.0-20210709073210-719dbb79d275
	open-cluster-management.io/api v0.0.0-20210727123024-41c7397e9f2d
	sigs.k8s.io/controller-runtime v0.9.5
)

replace (
	github.com/deislabs/oras => github.com/deislabs/oras v0.11.1
	github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.4.1
	github.com/kubevirt/terraform-provider-kubevirt => github.com/nirarg/terraform-provider-kubevirt v0.0.0-20201222125919-101cee051ed3
	github.com/metal3-io/baremetal-operator => github.com/openshift/baremetal-operator v0.0.0-20200715132148-0f91f62a41fe
	github.com/metal3-io/cluster-api-provider-baremetal => github.com/openshift/cluster-api-provider-baremetal v0.0.0-20190821174549-a2a477909c1d
	github.com/openshift/hive/apis => github.com/openshift/hive/apis v0.0.0-20210415080537-ea6f0a2dd76c
	github.com/terraform-providers/terraform-provider-ignition/v2 => github.com/community-terraform-providers/terraform-provider-ignition/v2 v2.1.0
	google.golang.org/grpc => google.golang.org/grpc v1.29.0
	k8s.io/api => k8s.io/api v0.21.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.21.3
	k8s.io/apiserver => k8s.io/apiserver v0.21.3
	k8s.io/client-go => k8s.io/client-go v0.21.3
	k8s.io/component-base => k8s.io/component-base v0.21.3
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20201113171705-d219536bb9fd
	kubevirt.io/client-go => kubevirt.io/client-go v0.29.0
	sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20201022175424-d30c7a274820
	sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.1.0-alpha.3.0.20201016155852-4090a6970205
	sigs.k8s.io/cluster-api-provider-openstack => github.com/openshift/cluster-api-provider-openstack v0.0.0-20201116051540-155384b859c5
)
