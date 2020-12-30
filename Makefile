.PHONY: all fmt vet test build build-deb clean

all: fmt vet test build build-deb

VERSION = $(shell dpkg-parsechangelog --show-field Version)
REVISION = $(shell git rev-parse --short HEAD)
BRANCH = $(shell git rev-parse --abbrev-ref HEAD)
LDFLAGS = -X github.com/prometheus/common/version.Version=$(VERSION) \
		  -X github.com/prometheus/common/version.Revision=$(REVISION) \
		  -X github.com/prometheus/common/version.Branch=$(BRANCH)
TMP_DIR := $(shell mktemp -d)
BUILD_DIR := $(TMP_DIR)/build

fmt:
	go fmt .

vet:
	go vet -v .

test:
	go test -v .

build:
	GOOS=linux go build -v -o exim_exporter -ldflags "$(LDFLAGS)" .

build-deb:
	mkdir $(BUILD_DIR)
	cp exim_exporter $(BUILD_DIR)/prometheus-exim-exporter
	cp -r debian/ $(BUILD_DIR)
	cd $(BUILD_DIR); debuild -us -uc
	cp $(TMP_DIR)/*.deb .

clean:
	rm -f exim_exporter prometheus-exim-exporter_*.deb

