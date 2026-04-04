# Default: docker build -t activitypub-core .  → all binaries on PATH, CMD apd
# Per-binary: docker build --target apd|apw -t ...  → /app/apd or /app/apw (see CI pr-checks.yml)
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/apd ./cmd/apd && \
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/apw ./cmd/apw && \
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/apadmin ./cmd/apadmin

FROM alpine:3.21 AS apd
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/apd /app/apd
EXPOSE 8080
CMD ["/app/apd"]

FROM alpine:3.21 AS apw
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/apw /app/apw
CMD ["/app/apw"]

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/apd /out/apw /out/apadmin /usr/local/bin/
EXPOSE 8080
CMD ["apd"]
