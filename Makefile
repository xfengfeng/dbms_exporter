
GO_DIRS := $(shell go list ./... |sed -e 1d -e s,github.com/ncabatoff/dbms_exporter/,,)
GO_SRC := dbms_exporter.go $(shell find ${GO_DIRS} -name '*.go')

CONTAINER_NAME = ncabatoff/dbms_exporter:latest
FREETDS_VERSION = 1.1.5
BUILD_CONTAINER_NAME = ncabatoff/dbms_exporter_builder:${FREETDS_VERSION}
# Possible BUILDTAGS settings are postgres, freetds, and odbc.
DRIVERS = postgres freetds
# Use make LDFLAGS= if you want to build with tag ODBC.
LDFLAGS = -extldflags=-static

all: vet test dbms_exporter

# Simple go build
dbms_exporter: $(GO_SRC)
	go build -ldflags '$(LDFLAGS) -X main.Version=git:$(shell git rev-parse HEAD)' -o dbms_exporter -tags '$(DRIVERS)' .

docker: Dockerfile $(GO_SRC)
	docker build --build-arg drivers="$(DRIVERS)" --build-arg ldflags="$(LDFLAGS)" -t $(CONTAINER_NAME) .

vet:
	go vet . ./config ./common ./db ./recipes

test:
	go test -v . ./config ./common ./db ./recipes
 
test-integration:
	tests/test-smoke

docker-build-pre: Dockerfile-buildexporter
	docker build --build-arg FREETDS_VERSION=${FREETDS_VERSION} -f Dockerfile-buildexporter -t $(BUILD_CONTAINER_NAME) .

# Do a self-contained build of dbms_exporter using Docker.
build-with-docker: $(GO_SRC) docker-build-pre Dockerfile
	docker run --rm -v $(shell pwd):/work \
	    -w /work \
	    $(BUILD_CONTAINER_NAME) \
	    make DRIVERS="$(DRIVERS)" LDFLAGS="$(LDFLAGS)"

.PHONY: docker-build docker test vet
