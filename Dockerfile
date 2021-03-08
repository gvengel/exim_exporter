FROM golang:buster AS builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    DEBIAN_FRONTEND=noninteractive

WORKDIR /src
COPY . .

RUN apt update && apt install -y dpkg-dev git
RUN go test -v .
RUN VERSION="$(dpkg-parsechangelog --show-field Version)"; \
    REVISION="$(git rev-parse --short HEAD)"; \
    BRANCH="$(git rev-parse --abbrev-ref HEAD)"; \
    LDFLAGS="-X github.com/prometheus/common/version.Version=${VERSION} \
             -X github.com/prometheus/common/version.Revision=${REVISION} \
             -X github.com/prometheus/common/version.Branch=${BRANCH}"; \
    go build -v -o exim_exporter -ldflags "$LDFLAGS"

WORKDIR /dist
RUN cp /src/exim_exporter .

FROM scratch
COPY --from=builder /dist/exim_exporter /
ENTRYPOINT ["/exim_exporter"]
EXPOSE 9636