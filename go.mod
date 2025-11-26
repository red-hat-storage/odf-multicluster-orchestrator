module github.com/red-hat-storage/odf-multicluster-orchestrator

go 1.24.6

toolchain go1.24.10

require (
	github.com/blang/semver/v4 v4.0.0
	github.com/csi-addons/kubernetes-csi-addons v0.12.0
	github.com/go-logr/zapr v1.3.0
	github.com/google/uuid v1.6.1-0.20241114170450-2d3c2a9cc518
	github.com/joho/godotenv v1.5.1
	github.com/kube-object-storage/lib-bucket-provisioner v0.0.0-20221122204822-d1a8c34382f1
	github.com/kubernetes-csi/external-snapshotter/client/v8 v8.2.0
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.36.1
	github.com/openshift/api v0.0.0-20250303104811-f587fb60f627
	github.com/openshift/library-go v0.0.0-20240124134907-4dfbf6bc7b11
	github.com/ramendr/ramen/api v0.0.0-20241001141243-29d6f22ad237
	github.com/red-hat-storage/ocs-operator/api/v4 v4.0.0-20250721221552-03d2581333b7
	github.com/rook/rook/pkg/apis v0.0.0-20250627200304-5094b1f5e6a6
	github.com/spf13/cobra v1.9.1
	github.com/stolostron/multicloud-operators-foundation v0.0.0-20220824091202-e9cd9710d009
	github.com/stretchr/testify v1.10.0
	go.uber.org/zap v1.27.0
	go.uber.org/zap/exp v0.2.0
	golang.org/x/sync v0.15.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.33.1
	k8s.io/apiextensions-apiserver v0.33.1
	k8s.io/apimachinery v0.33.1
	k8s.io/client-go v0.33.1
	k8s.io/klog/v2 v2.130.1
	k8s.io/utils v0.0.0-20250604170112-4c0f3b243397
	open-cluster-management.io/addon-framework v0.9.2
	open-cluster-management.io/api v0.13.0
	sigs.k8s.io/controller-runtime v0.21.0
	sigs.k8s.io/yaml v1.4.0
)

require (
	cel.dev/expr v0.23.0 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containernetworking/cni v1.2.3 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.12.2 // indirect
	github.com/evanphx/json-patch v5.9.11+incompatible // indirect
	github.com/evanphx/json-patch/v5 v5.9.11 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/fxamacker/cbor/v2 v2.8.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.2 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v0.21.1 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/cel-go v0.23.2 // indirect
	github.com/google/gnostic-models v0.6.9 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.3 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.7 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/hashicorp/go-secure-stdlib/parseutil v0.1.8 // indirect
	github.com/hashicorp/go-secure-stdlib/strutil v0.1.2 // indirect
	github.com/hashicorp/go-sockaddr v1.0.7 // indirect
	github.com/hashicorp/hcl v1.0.1-vault-7 // indirect
	github.com/hashicorp/vault/api v1.16.0 // indirect
	github.com/hashicorp/vault/api/auth/approle v0.8.0 // indirect
	github.com/hashicorp/vault/api/auth/kubernetes v0.8.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/k8snetworkplumbingwg/network-attachment-definition-client v1.7.6 // indirect
	github.com/libopenstorage/secrets v0.0.0-20240416031220-a17cf7f72c6c // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/noobaa/noobaa-operator/v5 v5.0.0-20250415145404-9b448831a834 // indirect
	github.com/nxadm/tail v1.4.8 // indirect
	github.com/openshift/custom-resource-status v1.1.3-0.20220503160415-f2fdb4999d87 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.22.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.64.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/stoewer/go-strcase v1.3.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.59.0 // indirect
	go.opentelemetry.io/otel v1.36.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.36.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.36.0 // indirect
	go.opentelemetry.io/otel/metric v1.36.0 // indirect
	go.opentelemetry.io/otel/sdk v1.36.0 // indirect
	go.opentelemetry.io/otel/trace v1.36.0 // indirect
	go.opentelemetry.io/proto/otlp v1.6.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.39.0 // indirect
	golang.org/x/exp v0.0.0-20240719175910-8a7402abbf56 // indirect
	golang.org/x/net v0.41.0 // indirect
	golang.org/x/oauth2 v0.30.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	golang.org/x/term v0.32.0 // indirect
	golang.org/x/text v0.26.0 // indirect
	golang.org/x/time v0.12.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250519155744-55703ea1f237 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250519155744-55703ea1f237 // indirect
	google.golang.org/grpc v1.73.0 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiserver v0.33.1 // indirect
	k8s.io/component-base v0.33.1 // indirect
	k8s.io/kube-openapi v0.0.0-20250610211856-8b98d1ed966a // indirect
	open-cluster-management.io/sdk-go v0.13.1-0.20240416030555-aa744f426379 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.31.2 // indirect
	sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect
	sigs.k8s.io/kube-storage-version-migrator v0.0.6-0.20230721195810-5c8923c5ff96 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.7.0 // indirect
)

replace (
	github.com/IBM-Cloud/terraform-provider-ibm => github.com/openshift/terraform-provider-ibm v1.26.2-openshift-2
	github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.4.1
	github.com/kubernetes-incubator/external-storage => github.com/libopenstorage/external-storage v0.20.4-openstorage-rc3
	// To remove dependency on github.com/hashicorp/vault/sdk
	github.com/libopenstorage/secrets => github.com/libopenstorage/secrets v0.0.0-20231011182615-5f4b25ceede1
	github.com/metal3-io/baremetal-operator => github.com/openshift/baremetal-operator v0.0.0-20211201170610-92ffa60c683d
	github.com/metal3-io/baremetal-operator/apis => github.com/openshift/baremetal-operator/apis v0.0.0-20211201170610-92ffa60c683d
	github.com/metal3-io/baremetal-operator/pkg/hardwareutils => github.com/openshift/baremetal-operator/pkg/hardwareutils v0.0.0-20211201170610-92ffa60c683d
	github.com/noobaa/noobaa-operator/v5 => github.com/noobaa/noobaa-operator/v5 v5.0.0-20220412093540-8972af85cac4
	github.com/open-cluster-management-io/api => open-cluster-management.io/api v0.12.0
	github.com/openshift/api => github.com/openshift/api v0.0.0-20240323003854-2252c7adfb79
	github.com/openshift/hive => github.com/openshift/hive v1.1.17-0.20220223000051-b1c8fa5853b1
	github.com/openshift/hive/apis => github.com/openshift/hive/apis v0.0.0-20220221165319-b389a65758da
	github.com/portworx/sched-ops => github.com/portworx/sched-ops v0.20.4-openstorage-rc3
	github.com/terraform-providers/terraform-provider-aws => github.com/openshift/terraform-provider-aws v1.60.1-0.20211215220004-24df6d73af46
	github.com/terraform-providers/terraform-provider-ignition/v2 => github.com/community-terraform-providers/terraform-provider-ignition/v2 v2.1.0
	sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20210121023454-5ffc5f422a80
	sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.1.0-alpha.3.0.20210626224711-5d94c794092f
	sigs.k8s.io/cluster-api-provider-openstack => github.com/openshift/cluster-api-provider-openstack v0.0.0-20211111204942-611d320170af
)

exclude (
	// This tag doesn't exist, but is imported by github.com/portworx/sched-ops.
	github.com/kubernetes-incubator/external-storage v0.20.4-openstorage-rc2
	github.com/noobaa/noobaa-operator/v5 v5.0.0-20210912161037-7eb9969404e4
	github.com/noobaa/noobaa-operator/v5 v5.9.0
	github.com/noobaa/noobaa-operator/v5 v5.10.1
	github.com/openshift/api v3.9.0+incompatible
	github.com/openshift/api v3.9.1-0.20190924102528-32369d4db2ad+incompatible
	github.com/openshift/api v3.9.1-0.20191111211345-a27ff30ebf09+incompatible
	// Exclude pre-go-mod kubernetes tags, because they are older
	// than v0.x releases but are picked when updating dependencies.
	k8s.io/client-go v1.4.0
	k8s.io/client-go v1.5.0
	k8s.io/client-go v1.5.1
	k8s.io/client-go v1.5.2
	k8s.io/client-go v2.0.0-alpha.1+incompatible
	k8s.io/client-go v2.0.0+incompatible
	k8s.io/client-go v3.0.0-beta.0+incompatible
	k8s.io/client-go v3.0.0+incompatible
	k8s.io/client-go v4.0.0-beta.0+incompatible
	k8s.io/client-go v4.0.0+incompatible
	k8s.io/client-go v5.0.0+incompatible
	k8s.io/client-go v5.0.1+incompatible
	k8s.io/client-go v6.0.0+incompatible
	k8s.io/client-go v7.0.0+incompatible
	k8s.io/client-go v8.0.0+incompatible
	k8s.io/client-go v9.0.0-invalid+incompatible
	k8s.io/client-go v9.0.0+incompatible
	k8s.io/client-go v10.0.0+incompatible
	k8s.io/client-go v11.0.0+incompatible
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/client-go v12.0.0+incompatible
)

exclude cloud.google.com/go/compute v1.6.1
