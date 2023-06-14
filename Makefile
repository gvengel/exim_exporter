.PHONY: all fmt vet test build clean

all: fmt vet test build

GOOS ?= linux
ARCH ?= amd64
TAGS ?= systemd
LDFLAGS = -X github.com/prometheus/common/version.Version=$(VERSION) \
		  -X github.com/prometheus/common/version.Revision=$(REVISION) \
		  -X github.com/prometheus/common/version.Branch=$(BRANCH)
TMP_DIR := $(shell mktemp -d)
BUILD_DIR := $(TMP_DIR)/build

fmt:
	go fmt .

vet:
	go vet .

test:
	go test -v .

build:
	GOOS=$(GOOS) GOARCH=$(ARCH) go build -v -o exim_exporter -tags $(TAGS) -ldflags "$(LDFLAGS)" .

clean:
	rm -f exim_exporter