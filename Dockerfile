# Default: docker build -t activitypub-core .  → all binaries on PATH, CMD apd
# Per-binary: docker build --target apd|apw -t ...  → /app/apd or /app/apw (see CI pr-checks.yml)
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/apd ./cmd/apd && \
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/apw ./cmd/apw && \
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/apadmin ./cmd/apadmin

FROM alpine:3.24 AS apd
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/apd /app/apd
COPY --from=build /out/apadmin /app/apadmin
COPY --from=build /src/db/migrations /app/db/migrations
EXPOSE 8080
CMD ["/app/apd"]

FROM alpine:3.24 AS apw
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/apw /app/apw
COPY --from=build /src/db/migrations /app/db/migrations
CMD ["/app/apw"]

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/apd /out/apw /out/apadmin /usr/local/bin/
COPY --from=build /src/db/migrations /usr/local/share/activitypub-core/db/migrations
WORKDIR /usr/local/share/activitypub-core
EXPOSE 8080
CMD ["apd"]
