# Standalone image for the cost alert bridge only. It ships on its own
# Cloud Build trigger (cloudbuild.cost-alert-bridge.yaml) so a Slack alert fix
# never waits for, or rides along with, a Goat OS STG release.
FROM golang:1.25.13-alpine AS build

WORKDIR /src/backend
RUN apk add --no-cache ca-certificates

COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY backend/ ./

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
      -o /out/cost-alert-bridge ./cmd/cost-alert-bridge

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S goatos && adduser -S -G goatos goatos

WORKDIR /app
COPY --from=build /out/cost-alert-bridge /app/bin/cost-alert-bridge

ENV GOATOS_HTTP_ADDR=":8080"
EXPOSE 8080
USER goatos
ENTRYPOINT ["/app/bin/cost-alert-bridge"]
