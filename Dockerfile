FROM golang:1.20.6-alpine3.18 AS builder

RUN apk add --no-cache \
    # Important: required for go-sqlite3
    gcc \
    # Required for Alpine
    musl-dev

COPY . /build
WORKDIR /build
ENV CGO_ENABLED=1
RUN go build -ldflags='-s -w -extldflags "-static"' -o feed

FROM alpine:3.18

COPY --from=builder /build/feed /usr/local/feed/feed

ENV PATH="/usr/local/feed:${PATH}"

WORKDIR /usr/local/feed

CMD ["/usr/local/feed/feed", "-config", "config.yaml", "-verbose"]
