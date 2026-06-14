FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
COPY vendor ./vendor
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -trimpath -ldflags="-s -w" -o /out/tunnel-server ./cmd/tunnel-server

FROM alpine:3.22

WORKDIR /app
COPY --from=builder /out/tunnel-server /app/tunnel-server

EXPOSE 9080 9081 21080

ENTRYPOINT ["/app/tunnel-server"]
