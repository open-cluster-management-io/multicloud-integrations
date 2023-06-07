# Copyright 2022 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

LOCAL_OS := $(shell uname)
ifeq ($(LOCAL_OS),Linux)
    TARGET_OS ?= linux
    XARGS_FLAGS="-r"
else ifeq ($(LOCAL_OS),Darwin)
    TARGET_OS ?= darwin
    XARGS_FLAGS=
else
    $(error "This system's OS $(LOCAL_OS) isn't recognized/supported")
endif

FINDFILES=find . \( -path ./.git -o -path ./.github \) -prune -o -type f
XARGS = xargs -0 ${XARGS_FLAGS}
CLEANXARGS = xargs ${XARGS_FLAGS}

REGISTRY = quay.io/open-cluster-management
VERSION = latest
IMAGE_NAME_AND_VERSION ?= $(REGISTRY)/multicloud-integrations:$(VERSION)
export GOPACKAGES   = $(shell go list ./... | grep -v /manager | grep -v /bindata  | grep -v /vendor | grep -v /internal | grep -v /build | grep -v /test | grep -v /e2e )

TEST_TMP :=/tmp
export KUBEBUILDER_ASSETS ?=$(TEST_TMP)/kubebuilder/bin
K8S_VERSION ?=1.19.2
GOHOSTOS ?=$(shell go env GOHOSTOS)
GOHOSTARCH ?= $(shell go env GOHOSTARCH)
KB_TOOLS_ARCHIVE_NAME :=kubebuilder-tools-$(K8S_VERSION)-$(GOHOSTOS)-$(GOHOSTARCH).tar.gz
KB_TOOLS_ARCHIVE_PATH := $(TEST_TMP)/$(KB_TOOLS_ARCHIVE_NAME)

.PHONY: build

build:
	@common/scripts/gobuild.sh build/_output/bin/gitopscluster ./cmd/gitopscluster
	@common/scripts/gobuild.sh build/_output/bin/gitopssyncresc ./cmd/gitopssyncresc
	@common/scripts/gobuild.sh build/_output/bin/multiclusterstatusaggregation ./cmd/multiclusterstatusaggregation
	@common/scripts/gobuild.sh build/_output/bin/propagation ./cmd/propagation

local:
	@GOOS=darwin common/scripts/gobuild.sh build/_output/bin/gitopscluster ./cmd/gitopscluster
	@GOOS=darwin common/scripts/gobuild.sh build/_output/bin/gitopssyncresc ./cmd/gitopssyncresc
	@GOOS=darwin common/scripts/gobuild.sh build/_output/bin/multiclusterstatusaggregation ./cmd/multiclusterstatusaggregation
	@GOOS=darwin common/scripts/gobuild.sh build/_output/bin/propagation ./cmd/propagation

.PHONY: build-images

build-images: build
	@docker build -t ${IMAGE_NAME_AND_VERSION} -f build/Dockerfile .

.PHONY: lint

lint: lint-all

.PHONY: lint-all

lint-all:lint-go

.PHONY: lint-go

lint-go:
	@${FINDFILES} -name '*.go' \( ! \( -name '*.gen.go' -o -name '*.pb.go' \) \) -print0 | ${XARGS} common/scripts/lint_go.sh

.PHONY: test

# download the kubebuilder-tools to get kube-apiserver binaries from it
ensure-kubebuilder-tools:
ifeq "" "$(wildcard $(KUBEBUILDER_ASSETS))"
	$(info Downloading kube-apiserver into '$(KUBEBUILDER_ASSETS)')
	mkdir -p '$(KUBEBUILDER_ASSETS)'
	curl -s -f -L https://storage.googleapis.com/kubebuilder-tools/$(KB_TOOLS_ARCHIVE_NAME) -o '$(KB_TOOLS_ARCHIVE_PATH)'
	tar -C '$(KUBEBUILDER_ASSETS)' --strip-components=2 -zvxf '$(KB_TOOLS_ARCHIVE_PATH)'
else
	$(info Using existing kube-apiserver from "$(KUBEBUILDER_ASSETS)")
endif
.PHONY: ensure-kubebuilder-tools

test: ensure-kubebuilder-tools
	go test -timeout 300s -v ./pkg/... -coverprofile=coverage.out
	go test -timeout 300s -v ./propagation-controller/... -coverprofile=prop_coverage.out

.PHONY: manifests
manifests: controller-gen
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

CONTROLLER_TOOLS_VERSION ?= v0.9.2

CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: deploy-ocm
deploy-ocm:
	deploy/ocm/install.sh

.PHONY: test-e2e
test-e2e: deploy-ocm
	e2e/run_e2e.sh
