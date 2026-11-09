FROM golang:1.25-bookworm AS build

WORKDIR /src/services/realtime

COPY services/realtime/go.mod services/realtime/go.sum ./
RUN go mod download

COPY services/realtime ./

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags pgx5driver -ldflags="-s -w" -o /out/platform-service ./cmd/platform-service && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags pgx5driver -ldflags="-s -w" -o /out/matchmaking-service ./cmd/matchmaking-service && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags pgx5driver -ldflags="-s -w" -o /out/match-service ./cmd/match-service && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags pgx5driver -ldflags="-s -w" -o /out/gateway ./cmd/gateway && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/proxy ./cmd/proxy

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata curl

WORKDIR /app

COPY --from=build /out/platform-service /usr/local/bin/platform-service
COPY --from=build /out/matchmaking-service /usr/local/bin/matchmaking-service
COPY --from=build /out/match-service /usr/local/bin/match-service
COPY --from=build /out/gateway /usr/local/bin/gateway
COPY --from=build /out/proxy /usr/local/bin/proxy
COPY deploy/render/entrypoint.sh /usr/local/bin/entrypoint.sh

RUN chmod +x /usr/local/bin/*

RUN adduser -D -g '' -u 1001 chess404 && \
    mkdir -p /app/data && \
    chown -R chess404:chess404 /app

USER chess404

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:${PORT:-8080}/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
