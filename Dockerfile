FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY server /build/server
COPY simulator /build/simulator
WORKDIR /build/simulator
RUN go mod edit -replace=github.com/fookiejs/fookie=/build/server && \
    go mod tidy && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/simulator ./cmd/simulator

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/simulator ./simulator
ENV SIMULATOR_SCHEMA_PATH=/schema/main.fql
ENV SIMULATOR_GRAPHQL_URL=http://127.0.0.1:8080/graphql
CMD ["/bin/sh", "-c", "merge_opt=; case \"${SIMULATOR_MERGE_ROOMS}\" in 1|true|yes) merge_opt=\"--merge-rooms\" ;; esac; while true; do ./simulator -schema \"${SIMULATOR_SCHEMA_PATH}\" -url \"${SIMULATOR_GRAPHQL_URL}\" -n \"${SIMULATOR_STEPS:-35}\" -valid \"${SIMULATOR_VALID:-0.72}\" ${merge_opt} -seed $(date +%s) || true; sleep \"${SIMULATOR_SLEEP_SEC:-15}\"; done"]
