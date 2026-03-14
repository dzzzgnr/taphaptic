FROM golang:1.24-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/agentwatch-api ./cmd/agentwatch-api

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=build /out/agentwatch-api /app/agentwatch-api

ENV PORT=8080
ENV AGENTWATCH_DATA_DIR=/data

EXPOSE 8080

ENTRYPOINT ["/app/agentwatch-api"]
