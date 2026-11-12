FROM golang:1.27.4-alpine AS builder

WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/bin/server ./cmd/server

FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata curl wget
WORKDIR /app
COPY --from=builder /app/bin/server /app/server

EXPOSE 8080
CMD ["/app/server"]
