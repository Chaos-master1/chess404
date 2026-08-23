FROM golang:1.25-bookworm AS build

WORKDIR /src/services/realtime

COPY services/realtime/go.mod services/realtime/go.sum ./
RUN go mod download -x

COPY services/realtime ./

RUN go vet -tags pgx5driver ./cmd/platform-service/... ./cmd/matchmaking-service/... && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags pgx5driver -ldflags="-s -w" -o /out/platform-service ./cmd/platform-service && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags pgx5driver -ldflags="-s -w" -o /out/matchmaking-service ./cmd/matchmaking-service

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=build /out/platform-service /usr/local/bin/platform-service
COPY --from=build /out/matchmaking-service /usr/local/bin/matchmaking-service
COPY deploy/railway/platform-suite-entrypoint.sh /usr/local/bin/platform-suite-entrypoint.sh
RUN chmod +x /usr/local/bin/platform-suite-entrypoint.sh

RUN adduser -D -g '' -u 1001 chess404
USER chess404

# platform-service listens on 0.0.0.0:$PORT (Railway healthcheck target).
# matchmaking-service listens on 0.0.0.0:8084 inside the same container;
# peers reach it via http://<private-domain>:8084.
ENV MATCHMAKING_ADDR=0.0.0.0:8084

CMD ["/usr/local/bin/platform-suite-entrypoint.sh"]
