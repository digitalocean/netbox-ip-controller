# Copyright 2022 DigitalOcean
# 
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at:
# 
# http://www.apache.org/licenses/LICENSE-2.0
# 
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

GITCOMMIT := $(shell git rev-parse --short=10 HEAD 2>/dev/null)
GITCOMMIT_LONG := $(shell git rev-parse HEAD 2>/dev/null)
NAME := netbox-ip-controller
IMAGE ?= "${NAME}:$(GITCOMMIT)"
# Path to k8s-env-test image on Docker Hub
ENVTEST := digitalocean/k8s-env-test

K8S_VERSION := 1.30.11
ETCD_VERSION := 3.5.0
GO_VERSION := 1.22.0

ifeq ($(strip $(shell git status --porcelain 2>/dev/null)),)
	GIT_TREE_STATE=clean
else
	GIT_TREE_STATE=dirty
endif
export GIT_TREE_STATE

all: ${NAME} build-image clean

.PHONY: ${NAME}
${NAME}:
	env GOOS=linux GOARCH=amd64 go build -o ./cmd/${NAME}/${NAME} ./cmd/${NAME}

.PHONY: build-image
build-image: ${NAME}
	docker build -t $(IMAGE) cmd/${NAME}

.PHONY: clean
clean:
	-rm ./cmd/${NAME}/${NAME}

.PHONY: test
test:
	go test -v ./...

.PHONY: crd
crd:
	docker run \
		--user=$(shell id -u) \
		--interactive \
		--tty \
		--env "GOPRIVATE=*.internal.digitalocean.com,github.com/digitalocean" \
		--volume $(shell pwd):/go/src/github.com/digitalocean/netbox-ip-controller \
		--volume $(shell go env GOCACHE):/.cache/go-build \
		golang:${GO_VERSION} bash -c "\
			cd /go/src/github.com/digitalocean/netbox-ip-controller && \
			git clone --depth 1 --branch kubernetes-${K8S_VERSION} https://github.com/kubernetes/code-generator vendor/k8s.io/code-generator && \
			pushd vendor/k8s.io/code-generator && \
			go mod download && \
			go mod vendor && \
			popd && \
			source vendor/k8s.io/code-generator/kube_codegen.sh && \
			kube::codegen::gen_client --output-dir client --output-pkg github.com/digitalocean/netbox-ip-controller/client --boilerplate vendor/k8s.io/code-generator/examples/hack/boilerplate.go.txt api" && \
			go mod tidy && \
			go mod vendor

.PHONY: envtest-image
envtest-image:
	docker build --build-arg GO_VERSION=$(GO_VERSION) --build-arg K8S_VERSION=$(K8S_VERSION) --build-arg ETCD_VERSION=$(ETCD_VERSION) -t "$(ENVTEST):$(GITCOMMIT)" ./test

.PHONY: get-envtest-image-tag
get-envtest-image-tag:
	echo $(ENVTEST):$(GITCOMMIT)

.PHONY:
envtest-image-push:
	docker push digitalocean/k8s-env-test:$(GITCOMMIT)
	
.PHONY:
integration-test:
	TEST_IMAGE=$(ENVTEST):$(GITCOMMIT) ./local/local-integration-test.sh all

.PHONY:
setup: 
	./local/local-integration-test.sh setup

.PHONY:
execute:
	TEST_IMAGE=$(ENVTEST):$(GITCOMMIT) ./local/local-integration-test.sh execute

.PHONY:
cleanup:
	./local/local-integration-test.sh cleanup
