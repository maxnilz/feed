FROM golang:1.24-alpine AS builder

RUN apk add --no-cache \
    # Important: required for go-sqlite3
    gcc \
    # Required for Alpine
    musl-dev

COPY . /build
WORKDIR /build
ENV CGO_ENABLED=1
RUN go build -ldflags='-s -w -extldflags "-static"' -o feed

FROM alpine:latest

COPY --from=builder /build/feed /usr/local/feed/feed

ENV PATH="/usr/local/feed:${PATH}"

WORKDIR /usr/local/feed

CMD ["/usr/local/feed/feed", "-config", "config.yaml", "-verbose"]
