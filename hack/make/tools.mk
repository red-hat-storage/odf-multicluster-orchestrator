# go-get-tool will 'go get' any package $2 and install it to $1.
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin GOFLAGS='' go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

##@ Tools

CONTROLLER_GEN = $(CWD)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.17.2)

KUSTOMIZE = $(CWD)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5@v5.3.0)

.PHONY: opm
OPM = ./bin/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.37.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

.PHONY: operator-sdk
OSDK = ./bin/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
ifeq (,$(wildcard $(OSDK)))
ifeq (,$(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OSDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	OSDK_VERSION=v1.34.1 && \
	curl -sSLo $(OSDK) https://github.com/operator-framework/operator-sdk/releases/download/$${OSDK_VERSION}/operator-sdk_$${OS}_$${ARCH} ;\
	chmod +x $(OSDK) ;\
	}
else
OSDK = $(shell which operator-sdk)
endif
endif

GOLANGCI_URL := https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh
GOLANGCI_VERSION := 1.63.4

.PHONY: golangci-bin
GOLANGCI_BIN := $(CWD)/bin/golangci-lint
GOLANGCI_INSTALLED_VER := $(shell $(GOLANGCI_BIN) version --format=short 2>/dev/null)
golangci-bin: ## Download goloanci-lint locally if necessary.
ifeq (,$(GOLANGCI_INSTALLED_VER))
	$(info Installing golangci-lint (version: $(GOLANGCI_VERSION)) into $(GOLANGCI_BIN))
	curl -sSfL $(GOLANGCI_URL) | sh -s v$(GOLANGCI_VERSION)
else ifneq ($(GOLANGCI_VERSION),$(GOLANGCI_INSTALLED_VER))
	$(error Incorrect version ($(GOLANGCI_INSTALLED_VER)) for golanci-lint found, expecting $(GOLANGCI_VERSION))
endif

.PHONY: kubelinter-bin
KUBELINTER_BIN := $(CWD)/bin/kube-linter
kubelinter-bin: ## Download kube-linter locally if necessary
	$(call go-get-tool,$(KUBELINTER_BIN),golang.stackrox.io/kube-linter/cmd/kube-linter@0.2.2)
