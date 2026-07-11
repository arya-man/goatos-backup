FROM golang:1.23-alpine AS build

WORKDIR /src/backend
RUN apk add --no-cache ca-certificates

COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY backend/ ./
COPY contracts /src/contracts

ARG TARGETOS
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /out && \
    for cmd in api outbox-relay outbox-dlq domain-event-consumer domain-event-processed-sweeper generate-vaccination-obligations vaccination-eligibility-rollup-recompute process-integrity-projection-recompute obligation-sweeper calendar-vaccination-projector calendar-reminder-sweeper calendar-escalation-sweeper notification-dispatcher backfill-goat-created idempotency-key-sweeper inventory-batch-reconciler sop-review-fanout-retry partition-maintainer legacy-god-sheet-sync seed-dev-grant seed-dev-email-grants seed-vaccination-trigger mint-dev-token; do \
      GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-amd64}" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "/out/${cmd}" "./cmd/${cmd}"; \
    done

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S goatos && adduser -S -G goatos goatos

WORKDIR /app
COPY --from=build /out/ /app/bin/
COPY contracts/jsonschema /app/contracts/jsonschema

ENV PATH="/app/bin:${PATH}" \
    GOATOS_HTTP_ADDR=":8080"

EXPOSE 8080
USER goatos
ENTRYPOINT ["/app/bin/api"]
