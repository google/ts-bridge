# Builder image
FROM golang:alpine AS builder

WORKDIR /go/src/ts-bridge

# Copy go.mod and go.sum first so we can cache this layer unless deps changed or have been added
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Run go generate only on web static assets as there's no need to regen test mocks here
RUN go install github.com/go-bindata/go-bindata/v3/go-bindata && go generate ./web
RUN go build -o /go/bin/ts-bridge ./app

# Runtime image
FROM alpine:3

COPY --from=builder /go/bin/ts-bridge /go/bin/ts-bridge

# Important note, alpine uses busybox, so `adduser` and `addgroup` syntax is different,
# see: https://busybox.net/downloads/BusyBox.html#adduser
RUN addgroup -g 1000 ts-bridge \
    && adduser -D -H -g '' -u 1000 -G ts-bridge ts-bridge \
    && mkdir -p /etc/ts-bridge /ts-bridge \
    && chown -R ts-bridge:ts-bridge /etc/ts-bridge /ts-bridge

WORKDIR /ts-bridge
USER ts-bridge

EXPOSE 8080

ENTRYPOINT ["/go/bin/ts-bridge"]
