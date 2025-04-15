# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o secret-app

# Runtime stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/secret-app .
COPY --from=builder /app/templates ./templates

EXPOSE 8080

CMD ["./secret-app"]