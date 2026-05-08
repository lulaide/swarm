FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /swarm .

FROM alpine:latest
LABEL org.opencontainers.image.source="https://github.com/lulaide/swarm"
LABEL org.opencontainers.image.description="High-concurrency multi-exit proxy pool"

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /swarm /usr/local/bin/swarm

EXPOSE 7890 9090

VOLUME ["/etc/swarm"]

ENTRYPOINT ["swarm"]
CMD ["-c", "/etc/swarm/config.yaml"]
