# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS builder
WORKDIR /src

# `-mod=mod` lets the build resolve and record checksums when go.sum is absent,
# so the image builds straight from a fresh checkout.
ENV GOFLAGS=-mod=mod CGO_ENABLED=0 GOOS=linux

COPY go.mod ./
COPY go.sum* ./
RUN go mod download

COPY . .
# Both binaries ship in the image. The gateway is the entrypoint; a worker
# deployment overrides the command instead of needing an image of its own.
RUN go build -ldflags="-s -w" -o /out/gateway ./cmd/server  && go build -ldflags="-s -w" -o /out/worker ./cmd/worker

FROM alpine:3.20 AS runner
RUN apk add --no-cache ca-certificates wget \
  && adduser -D -u 10001 app
USER app

COPY --from=builder /out/gateway /usr/local/bin/gateway
COPY --from=builder /out/worker /usr/local/bin/worker

EXPOSE 8080
ENTRYPOINT ["gateway"]
