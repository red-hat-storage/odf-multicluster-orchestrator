## Tool Binaries
LOCALBIN ?= $(CWD)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_BIN ?= $(LOCALBIN)/golangci-lint
KUBELINTER_BIN ?= $(LOCALBIN)/kube-linter
OPM ?=  $(LOCALBIN)/opm
OSDK ?= $(LOCALBIN)/operator-sdk

## Tool Versions
KUSTOMIZE_VERSION ?= v5.6.0
CONTROLLER_TOOLS_VERSION ?= v0.17.2
#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell go list -m -f "{{ .Version }}" sigs.k8s.io/controller-runtime | awk -F'[v.]' '{printf "release-%d.%d", $$2, $$3}')
#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell go list -m -f "{{ .Version }}" k8s.io/api | awk -F'[v.]' '{printf "1.%d", $$3}')
GOLANGCI_LINT_VERSION ?= v1.64.8
KUBELINTER_VERSION ?= 0.2.2
OPM_VERSION ?= v1.51.0
OSDK_VERSION ?= v1.39.2

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-bin
golangci-bin: $(GOLANGCI_BIN) ## Download golangci-lint locally if necessary.
$(GOLANGCI_BIN): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_BIN),github.com/golangci/golangci-lint/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))


.PHONY: kubelinter-bin
kubelinter-bin: $(KUBELINTER_BIN) ## Download kube-linter locally if necessary
$(KUBELINTER_BIN): $(LOCALBIN)
	$(call go-install-tool,$(KUBELINTER_BIN),golang.stackrox.io/kube-linter/cmd/kube-linter,$(KUBELINTER_VERSION))

.PHONY: opm
opm:$(OPM) ## Download opm locally if necessary.
$(OPM): $(LOCALBIN)
	$(call go-install-tool,$(OPM),github.com/operator-framework/operator-registry/cmd/opm,$(OPM_VERSION))

.PHONY: operator-sdk
operator-sdk: $(OSDK) ## Download operator-sdk locally if necessary.
$(OSDK) : $(LOCALBIN)
	$(call go-install-tool,$(OSDK),github.com/operator-framework/operator-sdk/cmd/operator-sdk,$(OSDK_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f $(1) || true ;\
GOBIN=$(LOCALBIN) go install $${package} ;\
mv $(1) $(1)-$(3) ;\
} ;\
ln -sf $(1)-$(3) $(1)
endef