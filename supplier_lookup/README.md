# Supplier Lookup Service

This service implements an Envoy ext_authz filter that performs supplier lookups based on connection codes. It uses Redis v9 and supports both standalone and cluster configurations.

## Purpose

The service extracts the `X-Connection-Code` header from incoming requests and looks up the corresponding supplier in Redis. It returns both the original connection code and the supplier name if found, or rejects the request with a 400 error if the connection code is not mapped to any supplier.

## Configuration

The service can be configured using environment variables:

### Redis Configuration

- `REDIS_MODE`: Redis mode - "standalone" (default) or "cluster"
- `REDIS_ADDR`: Redis address for standalone mode (default: "localhost:6379")
- `REDIS_CLUSTER_ADDRS`: Comma-separated Redis cluster addresses (default: "localhost:7000,localhost:7001,localhost:7002")
- `REDIS_PASSWORD`: Redis password (optional)

### Server Configuration

- `GRPC_PORT`: gRPC server port (default: ":9002")

## Redis Key Format

The service looks up suppliers using the following Redis key pattern:
```
supplier_mapp:<connection_code>
```

The value should be the supplier name (e.g., "tbo", "hotelbed").

## Example Redis Data

```bash
# Set supplier mappings
redis-cli SET supplier_mapp:conn123 "tbo"
redis-cli SET supplier_mapp:conn456 "hotelbed"
```

## API Response

### Success Response (200 OK)
```
Headers:
- x-connection-code: <original connection code>
- x-supplier: <supplier name>
```

### Error Response (400 Bad Request)
```json
{
  "error": "invalid_connection_code",
  "message": "Invalid Connection code not belong to any supplier"
}
```

## Building and Running

### Local Development
```bash
go mod tidy
go build .
./supplier_lookup
```

### Docker
```bash
docker build -t supplier_lookup .
docker run -p 9002:9002 \
  -e REDIS_ADDR=redis:6379 \
  -e REDIS_MODE=standalone \
  supplier_lookup
```

## Health Check

The service implements gRPC health checking protocol and supports both direct health checks and `grpc_health_probe`:

### Direct gRPC Health Check
```bash
grpc_health_probe -addr=localhost:9002
```

### Kubernetes Health Checks
The service includes proper health check implementation for Kubernetes deployment:

- **Liveness Probe**: Checks if the service is running
- **Readiness Probe**: Checks if the service is ready to serve traffic  
- **Startup Probe**: Allows time for the service to start up

The health check verifies:
- gRPC server is responsive
- Redis connectivity is working

## Kubernetes Deployment

### Prerequisites
- Kubernetes cluster
- Redis instance or cluster
- Docker registry access

### Deploy with Standalone Redis
```bash
kubectl apply -f k8s-deployment.yaml
```

### Deploy with Redis Cluster
```bash
kubectl apply -f k8s-deployment-cluster.yaml
```

### Environment Variables for Kubernetes
The deployment supports the following environment variables:

- `REDIS_MODE`: "standalone" or "cluster"
- `REDIS_ADDR`: Redis address for standalone mode
- `REDIS_CLUSTER_ADDRS`: Comma-separated cluster addresses
- `REDIS_PASSWORD`: Redis password (from secret)
- `GRPC_PORT`: gRPC server port

### Monitoring and Scaling
The deployment includes:
- **Resource limits**: CPU and memory constraints
- **HorizontalPodAutoscaler**: Automatic scaling based on CPU/memory usage
- **Health probes**: Liveness, readiness, and startup probes
- **Service discovery**: ClusterIP service for internal communication 