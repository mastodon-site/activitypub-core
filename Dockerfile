# Build: docker build -t activitypub-core .
# Runtime command is overridden (e.g. apd, apw) via compose or `docker run ... apw`.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/apd ./cmd/apd && \
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/apw ./cmd/apw && \
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/apadmin ./cmd/apadmin

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/apd /out/apw /out/apadmin /usr/local/bin/
EXPOSE 8080
CMD ["apd"]
