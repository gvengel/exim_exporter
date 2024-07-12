FROM --platform=amd64 ubuntu:focal
ENV GO_VERSION=1.22.5
ENV PATH="${PATH}:/opt/go/bin"
WORKDIR /opt/exim_exporter
RUN apt-get update && apt-get install -y ca-certificates wget docker.io
RUN wget -q -O /tmp/go.tar.gz https://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz && \
    tar -C /opt -xzf /tmp/go.tar.gz && rm /tmp/go.tar.gz
COPY install-deps.sh .
RUN ./install-deps.sh && rm ./install-deps.sh
RUN echo 'deb [trusted=yes] https://repo.goreleaser.com/apt/ /' > /etc/apt/sources.list.d/goreleaser.list; \
    apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends goreleaser
CMD ["goreleaser", "release", "--snapshot", "--clean"]