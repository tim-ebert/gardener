# Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

REGISTRY                           := eu.gcr.io/gardener-project/gardener
APISERVER_IMAGE_REPOSITORY         := $(REGISTRY)/apiserver
CONROLLER_MANAGER_IMAGE_REPOSITORY := $(REGISTRY)/controller-manager
SCHEDULER_IMAGE_REPOSITORY         := $(REGISTRY)/scheduler
SEED_ADMISSION_IMAGE_REPOSITORY    := $(REGISTRY)/seed-admission-controller
GARDENLET_IMAGE_REPOSITORY         := $(REGISTRY)/gardenlet
IMAGE_TAG                          := $(shell cat VERSION)
PUSH_LATEST_TAG                    := true

REPO_ROOT                          := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))))
LOCAL_GARDEN_LABEL                 := local-garden

#########################################
# Rules for local development scenarios #
#########################################

.PHONY: dev-setup
dev-setup:
	@./hack/local-development/dev-setup

.PHONY: dev-setup-register-gardener
dev-setup-register-gardener:
	@./hack/local-development/dev-setup-register-gardener

.PHONY: local-garden-up
local-garden-up:
	# Remove old containers and create the docker user network
	@-./hack/local-development/local-garden/cleanup
	@-docker network create gardener-dev --label $(LOCAL_GARDEN_LABEL)

	# Start the nodeless kubernetes environment
	@./hack/local-development/local-garden/run-kube-etcd $(LOCAL_GARDEN_LABEL)
	@./hack/local-development/local-garden/run-kube-apiserver $(LOCAL_GARDEN_LABEL)
	@./hack/local-development/local-garden/run-kube-controller-manager $(LOCAL_GARDEN_LABEL)

	# This etcd will be used to storge gardener resources (e.g., seeds, shoots)
	@./hack/local-development/local-garden/run-gardener-etcd $(LOCAL_GARDEN_LABEL)

	# Applying proxy RBAC for the extension controller
	# After this step, you can start using the cluster at hack/local-garden/kubeconfigs/admin.conf
	@./hack/local-development/local-garden/apply-rbac-garden-ns

	# Now you can start using the cluster at with `export KUBECONFIG=hack/local-garden/kubeconfigs/default-admin.conf`
	# Then you need to run `make dev-setup` to setup config and certificates files for gardener's components and to register the gardener-apiserver.
	# Finally, run `make start-apiserver,start-controller-manager,start-scheduler,start-gardenlet` to start the gardener components as usual.

.PHONY: local-garden-down
local-garden-down:
	@-./hack/local-development/local-garden/cleanup

.PHONY: start-apiserver
start-apiserver:
	@./hack/local-development/start-apiserver

.PHONY: start-controller-manager
start-controller-manager:
	@./hack/local-development/start-controller-manager

.PHONY: start-scheduler
start-scheduler:
	@./hack/local-development/start-scheduler

.PHONY: start-seed-admission-controller
start-seed-admission-controller:
	@./hack/local-development/start-seed-admission-controller

.PHONY: start-gardenlet
start-gardenlet:
	@./hack/local-development/start-gardenlet

#################################################################
# Rules related to binary build, Docker image build and release #
#################################################################

.PHONY: install
install:
	@./hack/install.sh ./...

.PHONY: docker-images
docker-images:
	@docker build -t $(APISERVER_IMAGE_REPOSITORY):$(IMAGE_TAG)         -t $(APISERVER_IMAGE_REPOSITORY):latest         -f Dockerfile --target apiserver .
	@docker build -t $(CONROLLER_MANAGER_IMAGE_REPOSITORY):$(IMAGE_TAG) -t $(CONROLLER_MANAGER_IMAGE_REPOSITORY):latest -f Dockerfile --target controller-manager .
	@docker build -t $(SCHEDULER_IMAGE_REPOSITORY):$(IMAGE_TAG)         -t $(SCHEDULER_IMAGE_REPOSITORY):latest         -f Dockerfile --target scheduler .
	@docker build -t $(SEED_ADMISSION_IMAGE_REPOSITORY):$(IMAGE_TAG)    -t $(SEED_ADMISSION_IMAGE_REPOSITORY):latest    -f Dockerfile --target seed-admission-controller .
	@docker build -t $(GARDENLET_IMAGE_REPOSITORY):$(IMAGE_TAG)         -t $(GARDENLET_IMAGE_REPOSITORY):latest         -f Dockerfile --target gardenlet .

.PHONY: docker-login
docker-login:
	@gcloud auth activate-service-account --key-file .kube-secrets/gcr/gcr-readwrite.json

.PHONY: docker-push
docker-push:
	@if ! docker images $(APISERVER_IMAGE_REPOSITORY) | awk '{ print $$2 }' | grep -q -F $(IMAGE_TAG); then echo "$(APISERVER_IMAGE_REPOSITORY) version $(IMAGE_TAG) is not yet built. Please run 'make docker-images'"; false; fi
	@if ! docker images $(CONROLLER_MANAGER_IMAGE_REPOSITORY) | awk '{ print $$2 }' | grep -q -F $(IMAGE_TAG); then echo "$(CONROLLER_MANAGER_IMAGE_REPOSITORY) version $(IMAGE_TAG) is not yet built. Please run 'make docker-images'"; false; fi
	@if ! docker images $(SCHEDULER_IMAGE_REPOSITORY) | awk '{ print $$2 }' | grep -q -F $(IMAGE_TAG); then echo "$(SCHEDULER_IMAGE_REPOSITORY) version $(IMAGE_TAG) is not yet built. Please run 'make docker-images'"; false; fi
	@if ! docker images $(SEED_ADMISSION_IMAGE_REPOSITORY) | awk '{ print $$2 }' | grep -q -F $(IMAGE_TAG); then echo "$(SEED_ADMISSION_IMAGE_REPOSITORY) version $(IMAGE_TAG) is not yet built. Please run 'make docker-images'"; false; fi
	@if ! docker images $(GARDENLET_IMAGE_REPOSITORY) | awk '{ print $$2 }' | grep -q -F $(IMAGE_TAG); then echo "$(GARDENLET_IMAGE_REPOSITORY) version $(IMAGE_TAG) is not yet built. Please run 'make docker-images'"; false; fi
	@gcloud docker -- push $(APISERVER_IMAGE_REPOSITORY):$(IMAGE_TAG)
	@if [[ "$(PUSH_LATEST_TAG)" == "true" ]]; then gcloud docker -- push $(APISERVER_IMAGE_REPOSITORY):latest; fi
	@gcloud docker -- push $(CONROLLER_MANAGER_IMAGE_REPOSITORY):$(IMAGE_TAG)
	@if [[ "$(PUSH_LATEST_TAG)" == "true" ]]; then gcloud docker -- push $(CONROLLER_MANAGER_IMAGE_REPOSITORY):latest; fi
	@gcloud docker -- push $(SCHEDULER_IMAGE_REPOSITORY):$(IMAGE_TAG)
	@if [[ "$(PUSH_LATEST_TAG)" == "true" ]]; then gcloud docker -- push $(SCHEDULER_IMAGE_REPOSITORY):latest; fi
	@gcloud docker -- push $(SEED_ADMISSION_IMAGE_REPOSITORY):$(IMAGE_TAG)
	@if [[ "$(PUSH_LATEST_TAG)" == "true" ]]; then gcloud docker -- push $(SEED_ADMISSION_IMAGE_REPOSITORY):latest; fi
	@gcloud docker -- push $(GARDENLET_IMAGE_REPOSITORY):$(IMAGE_TAG)
	@if [[ "$(PUSH_LATEST_TAG)" == "true" ]]; then gcloud docker -- push $(GARDENLET_IMAGE_REPOSITORY):latest; fi

#####################################################################
# Rules for verification, formatting, linting, testing and cleaning #
#####################################################################

.PHONY: install-requirements
install-requirements:
	@go install -mod=vendor github.com/onsi/ginkgo/ginkgo
	@go install -mod=vendor github.com/ahmetb/gen-crd-api-reference-docs
	@go install -mod=vendor github.com/golang/mock/mockgen
	@GO111MODULE=off go get github.com/prometheus/prometheus/cmd/promtool
	@go get golang.org/x/tools/cmd/goimports
	@./hack/install-requirements.sh

.PHONY: revendor
revendor:
	@GO111MODULE=on go mod vendor
	@GO111MODULE=on go mod tidy

.PHONY: clean
clean:
	@hack/clean.sh ./cmd/... ./extensions/... ./pkg/... ./plugin/... ./test/...

.PHONY: check-generate
check-generate:
	@hack/check-generate.sh $(REPO_ROOT)

.PHONY: check
check:
	@hack/check.sh --golangci-lint-config=./.golangci.yaml ./cmd/... ./extensions/... ./pkg/... ./plugin/... ./test/...
	@hack/check-charts.sh ./charts

.PHONY: generate
generate:
	@hack/generate.sh ./cmd/... ./extensions/... ./pkg/... ./plugin/... ./test/...
	@hack/update-flow-viz.sh

.PHONY: format
format:
	@./hack/format.sh ./cmd ./extensions ./pkg ./plugin ./test

.PHONY: test
test:
	@./hack/test.sh -r ./cmd/... ./extensions/... ./pkg/... ./plugin/...
	@./hack/test-prometheus.sh

.PHONY: test-cov
test-cov:
	@./hack/test-cover.sh -r ./cmd/... ./extensions/... ./pkg/... ./plugin/...

.PHONY: test-clean
test-clean:
	@./hack/test-cover-clean.sh

.PHONY: verify
verify: check format test

.PHONY: verify-extended
verify-extended: install-requirements check-generate check format test-cov test-clean
