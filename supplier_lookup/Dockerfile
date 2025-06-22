FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o supplier_lookup .

# Final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates curl
WORKDIR /root/

# Install grpc_health_probe for health checks
RUN curl -L https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.19/grpc_health_probe-linux-amd64 -o /usr/local/bin/grpc_health_probe && \
    chmod +x /usr/local/bin/grpc_health_probe

COPY --from=builder /app/supplier_lookup .

EXPOSE 9002

CMD ["./supplier_lookup"] 