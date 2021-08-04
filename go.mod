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
	github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.4.1
	k8s.io/api => k8s.io/api v0.21.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.21.3
	k8s.io/apiserver => k8s.io/apiserver v0.21.3
	k8s.io/client-go => k8s.io/client-go v0.21.3
	k8s.io/component-base => k8s.io/component-base v0.21.3
)
