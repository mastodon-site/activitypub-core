# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w" \
    -o /out/apd ./cmd/apd && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w" \
    -o /out/apw ./cmd/apw

# API daemon
FROM alpine:3.21 AS apd

RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -g 1001 -S activitypub && \
    adduser -u 1001 -S activitypub -G activitypub

WORKDIR /app

COPY --from=builder /out/apd /app/apd
RUN chown activitypub:activitypub /app/apd

USER activitypub
EXPOSE 8080

LABEL org.opencontainers.image.source="https://github.com/mastodon-site/activitypub-core" \
    org.opencontainers.image.description="activitypub-core API daemon (apd)"

ENTRYPOINT ["/app/apd"]

# Background worker
FROM alpine:3.21 AS apw

RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -g 1001 -S activitypub && \
    adduser -u 1001 -S activitypub -G activitypub

WORKDIR /app

COPY --from=builder /out/apw /app/apw
RUN chown activitypub:activitypub /app/apw

USER activitypub

LABEL org.opencontainers.image.source="https://github.com/mastodon-site/activitypub-core" \
    org.opencontainers.image.description="activitypub-core worker (apw)"

ENTRYPOINT ["/app/apw"]
