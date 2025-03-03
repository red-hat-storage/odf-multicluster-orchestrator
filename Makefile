PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
CWD := $(shell pwd)

.DEFAULT_GOAL := help

# All variables are defined here
include hack/make/vars.mk

# Install required tools
include hack/make/tools.mk

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

all: build

##@ General

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

golangci-lint: golangci-bin ## Run golangci-lint against code.
	$(GOLANGCI_BIN) run ./...

kube-linter: kubelinter-bin ## Run kube-linter against YAML files
	$(KUBELINTER_BIN) lint ./ --config ./.kube-linter-config.yaml

unit-test: ## Run unit tests
	go test ./... -v -tags unit -coverprofile unit-cover.out

ENVTEST_ASSETS_DIR=$(CWD)/testbin
OPENSHIFT_CI ?= false
test: ## Run integration tests.
ifeq ($(OPENSHIFT_CI), true)
	@echo "Running in OpenShift CI. Syncing vendor"
	go mod tidy && go mod vendor
else
	@echo "Running outside OpenShift CI. Ignoring vendor"
endif
	make manifests generate fmt vet golangci-lint
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.8.3/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); go test ./tests/integration/... -v -tags integration -coverprofile integration-cover.out

##@ Build

build: generate fmt vet golangci-lint kube-linter ## Build manager binary.
	go build -o bin/manager main.go

run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

operator-build: generate fmt vet golangci-lint kube-linter ## Build docker image with the manager.
	${BUILD_TOOL} build -t ${IMG} .

operator-push: ## Push docker image with the manager.
	${BUILD_TOOL} push ${IMG}

##@ Deployment

install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	cd config/console && $(KUSTOMIZE) edit set image odf-multicluster-console=$(MULTICLUSTER_CONSOLE_IMG)
	$(KUSTOMIZE) build config/default | kubectl apply -f -

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -

.PHONY: bundle
bundle: manifests kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	export RAMEN_HUB_PACKAGE_NAME=$(RAMEN_HUB_PACKAGE_NAME) RAMEN_VERSION=$(RAMEN_VERSION) && cat config/dependencies/dependencies.yaml | envsubst > bundle/metadata/dependencies.yaml
	$(OSDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	cd config/console && $(KUSTOMIZE) edit set image odf-multicluster-console=$(MULTICLUSTER_CONSOLE_IMG)
ifeq ($(FUSION) , true)
	cd config/fusion/bases && $(KUSTOMIZE) edit add annotation --force 'olm.skipRange':"$(SKIP_RANGE)" && \
		$(KUSTOMIZE) edit add patch --name odf-multicluster-orchestrator.v0.0.0 --kind ClusterServiceVersion \
		--patch '[{"op": "replace", "path": "/spec/replaces", "value": "$(REPLACES)"}]'
	export IMG=$(IMG) MULTICLUSTER_CONSOLE_IMG=$(MULTICLUSTER_CONSOLE_IMG) && $(KUSTOMIZE) build config/fusion | envsubst | $(OSDK) generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
else
	cd config/manifests/bases && $(KUSTOMIZE) edit add annotation --force 'olm.skipRange':"$(SKIP_RANGE)" && \
		$(KUSTOMIZE) edit add patch --name odf-multicluster-orchestrator.v0.0.0 --kind ClusterServiceVersion \
		--patch '[{"op": "replace", "path": "/spec/replaces", "value": "$(REPLACES)"}]'
	export IMG=$(IMG) MULTICLUSTER_CONSOLE_IMG=$(MULTICLUSTER_CONSOLE_IMG) && $(KUSTOMIZE) build config/manifests | envsubst | $(OSDK) generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
endif
	$(OSDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: bundle ## Build the bundle image.
	${BUILD_TOOL} build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) operator-push IMG=$(BUNDLE_IMG)

.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	$(OPM) index add --container-tool ${BUILD_TOOL} --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT)

.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) operator-push IMG=$(CATALOG_IMG)

##@ Actions

ensure-clean-workdir: ## Ensure all required changes are generated and committed
	go mod tidy
	$(MAKE) manifests generate fmt vet
	git --no-pager diff
	git status --porcelain 2>&1 | tee /dev/stderr | (! read)
