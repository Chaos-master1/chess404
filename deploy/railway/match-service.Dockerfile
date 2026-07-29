FROM golang:1.25-bookworm AS build

WORKDIR /src/services/realtime

COPY services/realtime/go.mod services/realtime/go.sum ./
RUN go mod download -x

COPY services/realtime ./

RUN go vet -tags pgx5driver ./cmd/match-service/... && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags pgx5driver -ldflags="-s -w" -o /out/match-service ./cmd/match-service

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata curl

COPY --from=build /out/match-service /usr/local/bin/match-service

# NNUE is opt-in (CHESS404_ENGINE_NNUE=1) and off by default -- see
# internal/engine/nnue.go -- because the committed weights predate a fix to
# the trainer/inference contract and need retraining before they're useful.
# This still ships the file and points the loader at an absolute path so that
# whenever a corrected retrain lands, enabling it in production is a one-line
# env var change, not another silent "file not found from an unexpected
# working directory" bug -- which is exactly what previously happened: the
# runtime image never shipped the file at all, and even if it had, this
# stage's CMD runs from /, which none of the loader's relative fallback paths
# resolve from.
COPY services/realtime/nnue_weights.bin /usr/local/share/chess404/nnue_weights.bin
ENV CHESS404_NNUE_WEIGHTS_PATH=/usr/local/share/chess404/nnue_weights.bin

RUN adduser -D -g '' -u 1001 chess404
USER chess404

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:8080/readyz || exit 1

CMD ["/usr/local/bin/match-service"]
