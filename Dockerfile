FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/tunnel-server ./cmd/tunnel-server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /out/tunnel-server /app/tunnel-server

EXPOSE 9080 9081 21080

ENTRYPOINT ["/app/tunnel-server"]

