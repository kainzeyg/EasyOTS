FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o secret-app

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/secret-app .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/static ./static 

# Устанавливаем права на статические файлы
RUN chmod -R 644 /app/static/* && \
    chown -R 1000:1000 /app/static

EXPOSE 8080
CMD ["./secret-app"]