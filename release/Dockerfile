FROM --platform=amd64 ubuntu:focal
WORKDIR /opt/exim_exporter
RUN apt-get update; \
    DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates make libsystemd-dev docker.io
RUN echo 'deb [trusted=yes] https://repo.goreleaser.com/apt/ /' > /etc/apt/sources.list.d/goreleaser.list; \
    apt-get update; \
    DEBIAN_FRONTEND=noninteractive apt-get install -y goreleaser
CMD ["goreleaser", "release", "--snapshot", "--clean"]