FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY tools/deploy/slack-stg-deploy-bot/go.mod ./
COPY tools/deploy/slack-stg-deploy-bot/main.go ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/slack-stg-deploy-bot .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/slack-stg-deploy-bot /slack-stg-deploy-bot
USER nonroot:nonroot
ENTRYPOINT ["/slack-stg-deploy-bot"]
