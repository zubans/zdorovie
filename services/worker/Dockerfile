FROM golang:1.27.4-alpine AS builder

WORKDIR /app
COPY go.mod go.sum* ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/bin/worker ./cmd/worker

FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata curl
WORKDIR /app
COPY --from=builder /app/bin/worker /app/worker

CMD ["/app/worker"]
