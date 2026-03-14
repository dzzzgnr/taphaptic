FROM golang:1.24-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/taphaptic-api ./cmd/taphaptic-api

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=build /out/taphaptic-api /app/taphaptic-api

ENV PORT=8080
ENV TAPHAPTIC_DATA_DIR=/data

EXPOSE 8080

ENTRYPOINT ["/app/taphaptic-api"]
