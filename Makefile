.PHONY: all fmt vet test build build-deb clean

all: fmt vet test build build-deb

VERSION = $(shell dpkg-parsechangelog --show-field Version)
REVISION = $(shell git rev-parse --short HEAD)
BRANCH = $(shell git rev-parse --abbrev-ref HEAD)
LDFLAGS = -X github.com/prometheus/common/version.Version=$(VERSION) \
		  -X github.com/prometheus/common/version.Revision=$(REVISION) \
		  -X github.com/prometheus/common/version.Branch=$(BRANCH)

fmt:
	go fmt .

vet:
	go vet -v .

test:
	go test -v .

build:
	CGO_ENABLED=0 GOOS=linux go build -v -o exim_exporter -ldflags "$(LDFLAGS)" .

build-deb:
	mkdir build
	cp exim_exporter build/prometheus-exim-exporter
	cp -r debian/ build/
	cd build; debuild -us -uc

clean:
	rm -fr exim_exporter build

