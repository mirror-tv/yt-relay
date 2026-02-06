FROM golang:1.15-alpine3.13 AS build

WORKDIR /yt-relay

COPY . .

RUN apk add --update --no-cache make && \
    go get ./... && \
    make all

FROM alpine:3.13

WORKDIR /yt-relay

COPY --from=build /yt-relay/bin/ .
COPY --from=build /yt-relay/configs ./configs

EXPOSE 8080
CMD ["./yt-relay", "serve", "-config", "./configs/config.yml", "-address", "0.0.0.0", "-port", "8080"]
