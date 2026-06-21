FROM golang:1.23-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/get-a-channel-bot .

FROM alpine:3.21

RUN addgroup -S bot && adduser -S bot -G bot && mkdir -p /data && chown bot:bot /data

USER bot
WORKDIR /app

COPY --from=build /out/get-a-channel-bot /app/get-a-channel-bot

ENV DATABASE_PATH=/data/bot.db
ENV CLEANUP_INTERVAL_SECONDS=300

VOLUME ["/data"]

ENTRYPOINT ["/app/get-a-channel-bot"]
