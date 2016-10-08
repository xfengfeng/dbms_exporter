
GO_SRC := $(shell find . -type f -name "*.go")

CONTAINER_NAME ?= ncabatoff/dbms_exporter:latest

all: vet test dbms_exporter

# Simple go build
dbms_exporter: $(GO_SRC)
	CGO_ENABLED=0 go build -a -ldflags "-extldflags '-static' -X main.Version=git:$(shell git rev-parse HEAD)" -o dbms_exporter .

# Take a go build and turn it into a minimal container
docker: dbms_exporter
	docker build -t $(CONTAINER_NAME) .

vet:
	go vet . ./config ./common

test:
	go test -v . ./config ./common

test-integration:
	tests/test-smoke

# Do a self-contained docker build - we pull the official upstream container
# and do a self-contained build.
docker-build: dbms_exporter
	docker run -v $(shell pwd):/go/src/github.com/ncabatoff/dbms_exporter \
	    -v $(shell pwd):/real_src \
	    -e SHELL_UID=$(shell id -u) -e SHELL_GID=$(shell id -g) \
	    -w /go/src/github.com/ncabatoff/dbms_exporter \
		golang:1.7-wheezy \
		/bin/bash -c "make >&2 && chown $$SHELL_UID:$$SHELL_GID ./dbms_exporter"
	docker build -t $(CONTAINER_NAME) .

.PHONY: docker-build docker test vet
