# Build
FROM golang:1.19-alpine AS build
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/dispatcher ./cmd/dispatcher

# Run
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/dispatcher /app/dispatcher
EXPOSE 8484
USER nobody
ENTRYPOINT ["/app/dispatcher"]
