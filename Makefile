GITCOMMIT := $(shell git rev-parse --short=10 HEAD 2>/dev/null)
GITCOMMIT_LONG := $(shell git rev-parse HEAD 2>/dev/null)
NAME := netbox-ip-controller
IMAGE := "${NAME}:$(GITCOMMIT)"

ifeq ($(strip $(shell git status --porcelain 2>/dev/null)),)
	GIT_TREE_STATE=clean
else
	GIT_TREE_STATE=dirty
endif
export GIT_TREE_STATE

all: ${NAME} build-image clean

${NAME}: cmd/${NAME}/main.go
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
