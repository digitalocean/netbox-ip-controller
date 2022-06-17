GITCOMMIT := $(shell git rev-parse --short=10 HEAD 2>/dev/null)
GITCOMMIT_LONG := $(shell git rev-parse HEAD 2>/dev/null)
NAME := netbox-ip-controller
IMAGE := "${NAME}:$(GITCOMMIT)"
# Path to k8s-env-test image on Docker Hub
ENVTEST := digitalocean/k8s-env-test
# Digest of the latest envtest image
ENVTEST_DIGEST := sha256:eea3fd27b7694408915be3686d3f55f69846327e921be9a5b3f93cdaa988f4a2

K8S_VERSION := 1.23.6
ETCD_VERSION := 3.5.0
GO_VERSION := 1.18

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
			git clone --depth 1 --branch kubernetes-${K8S_VERSION} https://github.com/kubernetes/apimachinery /go/src/k8s.io/apimachinery && \
			git clone --depth 1 --branch kubernetes-${K8S_VERSION} https://github.com/kubernetes/code-generator /go/src/k8s.io/code-generator && \
			cd /go/src/k8s.io/code-generator && \
			go mod download && \
			go mod vendor && \
			cd /go/src/github.com/digitalocean/netbox-ip-controller && \
			/go/src/k8s.io/code-generator/generate-groups.sh all github.com/digitalocean/netbox-ip-controller/client github.com/digitalocean/netbox-ip-controller/api 'netbox:v1beta1'"

.PHONY: envtest-image
envtest-image:
	docker build --build-arg GO_VERSION=$(GO_VERSION) --build-arg K8S_VERSION=$(K8S_VERSION) --build-arg ETCD_VERSION=$(ETCD_VERSION) -t "$(ENVTEST):$(GITCOMMIT)" ./test

.PHONY: get-envtest-image-tag
get-envtest-image-tag:
	echo ${ENVTEST}:${ENVTEST_DIGEST}
	
.PHONY:
integration-test:
	TEST_IMAGE=${ENVTEST}@${ENVTEST_DIGEST} ./local/local-integration-test.sh all 

.PHONY:
setup: 
	./local/local-integration-test.sh setup

.PHONY:
execute:
	TEST_IMAGE=${ENVTEST}@${ENVTEST_DIGEST} ./local/local-integration-test.sh execute

.PHONY:
cleanup:
	./local/local-integration-test.sh cleanup
