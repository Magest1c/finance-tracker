# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/finance-tracker ./cmd/api

FROM alpine:3.23
RUN addgroup -S app && adduser -S -G app app
COPY --from=build /out/finance-tracker /usr/local/bin/finance-tracker

USER app
EXPOSE 8080
ENV HTTP_ADDR=:8080

ENTRYPOINT ["finance-tracker"]
