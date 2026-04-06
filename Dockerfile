# Build
FROM golang:1.19-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/outbox-dispatcher ./cmd/outbox-dispatcher

# Run
FROM alpine:3.19
ENV APP_ENV=production
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/outbox-dispatcher /app/outbox-dispatcher
EXPOSE 8484
USER nobody
ENTRYPOINT ["/app/outbox-dispatcher"]
