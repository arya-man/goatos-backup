FROM golang:1.25.13-alpine AS build

WORKDIR /src/backend
RUN apk add --no-cache ca-certificates

COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY backend/ ./
COPY contracts /src/contracts

ARG TARGETOS
ARG TARGETARCH
# GIT_SHA stamps internal/platform/buildinfo.SHA into the api binary only, so
# GET /version (see internal/bootstrap/api.go) can report which commit is
# actually running alongside its migration-drift status
# (docs/decisions/stale-binary-migration-drift-guard.md). Defaults to
# "unknown" - passing --build-arg GIT_SHA=<sha> is a deliberate follow-up for
# whichever CI/CD pipeline builds this image, not wired here. buildinfo.
# Current() also accepts a GOATOS_BUILD_SHA runtime env var as a no-rebuild
# fallback.
ARG GIT_SHA=unknown
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    set -e && \
    mkdir -p /out && \
    for cmd in api mcp outbox-relay outbox-dlq domain-event-consumer domain-event-processed-sweeper generate-vaccination-obligations vaccination-eligibility-rollup-recompute obligation-sweeper kernel-worker notification-dispatcher backfill-goat-created idempotency-key-sweeper inventory-batch-reconciler sop-review-fanout-retry partition-maintainer legacy-god-sheet-sync feed-direction-issue feed-transport-issue seed-dev-grant seed-dev-email-grants seed-vaccination-trigger mint-dev-token analytics-rollup; do \
      ldflags="-s -w"; \
      if [ "$cmd" = "api" ]; then \
        ldflags="$ldflags -X github.com/vgoats/goatos/backend/internal/platform/buildinfo.SHA=${GIT_SHA}"; \
      fi; \
      GOOS="${TARGETOS:-linux}" GOARCH="${TARGETARCH:-amd64}" CGO_ENABLED=0 go build -trimpath -ldflags="$ldflags" -o "/out/${cmd}" "./cmd/${cmd}"; \
    done

FROM alpine:3.20

RUN apk add --no-cache ca-certificates git tzdata && \
    addgroup -S goatos && adduser -S -G goatos goatos

WORKDIR /app
COPY --from=build /out/ /app/bin/
COPY contracts/jsonschema /app/contracts/jsonschema

ENV PATH="/app/bin:${PATH}" \
    GOATOS_HTTP_ADDR=":8080"

EXPOSE 8080
USER goatos
ENTRYPOINT ["/app/bin/api"]
