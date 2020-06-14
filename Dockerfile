FROM golang:alpine AS builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

WORKDIR /src
COPY . .

RUN go mod download
RUN go build -o exim_exporter .

WORKDIR /dist
RUN cp /src/exim_exporter .

FROM scratch
COPY --from=builder /dist/exim_exporter /
ENTRYPOINT ["/exim_exporter"]