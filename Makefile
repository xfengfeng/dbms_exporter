
GO_SRC := $(shell find . -type f -name "*.go")

CONTAINER_NAME = ncabatoff/dbms_exporter:latest
BUILD_CONTAINER_NAME = ncabatoff/dbms_exporter_builder:latest
TAG_VERSION ?= $(shell git describe --tags --abbrev=0)

# Possible BUILDTAGS settings are postgres, freetds, and odbc.
DRIVERS = postgres freetds
# Use make LDFLAGS= if you want to build with tag ODBC.
LDFLAGS = -extldflags=-static

GOX = gox -os="linux"

all: vet test dbms_exporter

# Simple go build
dbms_exporter: $(GO_SRC)
	go build -ldflags '$(LDFLAGS) -X main.Version=$(TAG_VERSION)' -o dbms_exporter -tags '$(DRIVERS)' .

# Take a go build and turn it into a minimal container
docker: dbms_exporter Dockerfile
	docker build -t $(CONTAINER_NAME) .

vet:
	go vet . ./config ./common ./db ./recipes

test:
	go test -v . ./config ./common ./db ./recipes

test-integration:
	tests/test-smoke

# Do a self-contained docker build - we pull the official upstream container
# and do a self-contained build.
docker-build: $(GO_SRC) Dockerfile-buildexporter Dockerfile
	docker build -f Dockerfile-buildexporter -t $(BUILD_CONTAINER_NAME) .
	docker run --rm -v $(shell pwd):/go/src/github.com/ncabatoff/dbms_exporter \
	    -v $(shell pwd):/real_src \
	    -e SHELL_UID=$(shell id -u) -e SHELL_GID=$(shell id -g) \
	    -w /go/src/github.com/ncabatoff/dbms_exporter \
	    $(BUILD_CONTAINER_NAME) \
	    /bin/bash -c 'make LDFLAGS="$(LDFLAGS)" DRIVERS="$(DRIVERS)" >&2 && chown $$SHELL_UID:$$SHELL_GID ./dbms_exporter'
	docker build -t $(CONTAINER_NAME) .

.PHONY: docker-build docker test vet
