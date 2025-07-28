# Envoy Access Log Analytics with Loki and Grafana

This document describes the logging infrastructure setup that collects Envoy access logs and makes them available for analysis in Grafana via Loki.

## Architecture Overview

```
Envoy → Docker Logs → Fluent Bit → Loki → Grafana
```

- **Envoy**: Produces structured JSON access logs
- **Fluent Bit**: Collects Docker container logs and forwards them to Loki
- **Loki**: Stores and indexes the log data (log aggregation system)
- **Grafana**: Provides visualization and analysis interface

## Services

### Loki
- **Image**: `grafana/loki:3.0.0`
- **Port**: `3100`
- **Data**: Persisted in `loki_data` volume
- **Configuration**: Uses default local configuration for development

### Grafana
- **Image**: `grafana/grafana:10.2.0`
- **Port**: `3000`
- **Dependencies**: Loki
- **Credentials**: admin/admin (default)
- **Data**: Persisted in `grafana_data` volume

### Fluent Bit
- **Image**: `fluent/fluent-bit:3.0`
- **Configuration**: `fluent-bit-envoy.conf`
- **Function**: Collects Envoy container logs and sends to Loki

## Getting Started

### 1. Start the Services

```bash
docker-compose up -d
```

This will start all services including:
- Redis
- Supplier lookup service
- Backend service
- Envoy proxy
- Loki
- Grafana
- Fluent Bit

### 2. Wait for Services to be Ready

Check service health:
```bash
docker-compose ps
```

### 3. Access Grafana Dashboard

1. Open Grafana at http://localhost:3000
2. Login with username: `admin`, password: `admin`
3. The Loki datasource is automatically configured via provisioning
4. Navigate to "Dashboards" to see the pre-configured "Envoy Access Logs" dashboard

### 4. Generate Some Traffic

Send requests to Envoy to generate access logs:
```bash
curl http://localhost:10000/
curl http://localhost:10000/api/health
```

### 5. View Logs in Grafana

1. Go to "Explore" in Grafana
2. Select Loki as the datasource
3. Use LogQL queries like `{job="envoy-proxy"}` to view access logs
4. Or use the pre-configured dashboard for visualizations

## Log Fields

The structured access logs include the following fields:

| Field | Description | Example |
|-------|-------------|---------|
| `timestamp` | Request timestamp | `2024-01-15T10:30:15.123Z` |
| `method` | HTTP method | `GET`, `POST` |
| `path` | Request path | `/api/users` |
| `response_code` | HTTP status code | `200`, `404`, `500` |
| `duration` | Request duration (ms) | `45` |
| `bytes_received` | Request body size | `1024` |
| `bytes_sent` | Response body size | `2048` |
| `supplier` | Supplier header value | `supplier-123` |
| `remote_address` | Client IP address | `192.168.1.100:54321` |
| `user_agent` | User agent string | `curl/7.68.0` |
| `request_id` | Unique request ID | `abc-123-def` |
| `protocol` | HTTP protocol version | `HTTP/1.1` |

## Dashboard Features

The pre-configured "Envoy Access Logs" dashboard includes:

### Response Code Distribution
- Pie chart showing HTTP status code breakdown
- Query: `sum by (http_status) (count_over_time({job="envoy-proxy"} [$__range]))`

### Request Rate
- Time series showing requests per second
- Query: `sum(rate({job="envoy-proxy"} [$__rate_interval]))`

### Response Time Percentiles
- Time series showing 95th and 50th percentile response times
- Queries using `quantile_over_time()` with unwrapped `response_time_ms`

### Top Suppliers by Request Count
- Bar chart showing most active suppliers
- Query: `topk(10, sum by (supplier_id) (count_over_time({job="envoy-proxy"} [$__range])))`

### Top Request Paths
- Table showing most frequently accessed endpoints
- Query: `topk(20, sum by (http_path) (count_over_time({job="envoy-proxy"} [$__range])))`

### Raw Access Logs
- Log panel for detailed log inspection with filtering capabilities

## Monitoring and Maintenance

### Check Loki Health
```bash
curl http://localhost:3100/ready
```

### View Loki Metrics
```bash
curl http://localhost:3100/metrics
```

### Fluent Bit Status
Check Fluent Bit logs:
```bash
docker logs fluent-bit
```

### Storage Management
Loki data is stored in Docker volumes. To clean up old logs:

1. Configure retention policies in Loki configuration
2. Or restart with clean volumes:
   ```bash
   docker-compose down -v
   docker-compose up -d
   ```

## Troubleshooting

### No Logs Appearing in Grafana
1. Check if Envoy is generating traffic
2. Verify Fluent Bit is running: `docker logs fluent-bit`
3. Check Loki health: `curl http://localhost:3100/ready`
4. Verify Loki datasource configuration in Grafana

### Loki Memory Issues
If Loki crashes due to memory:
1. Increase Docker memory allocation
2. Configure Loki limits in configuration file
3. Consider using external Loki for production

### Performance Tuning
- Adjust Fluent Bit buffer sizes in `fluent-bit-envoy.conf`
- Configure Loki retention policies
- Set up log compaction for better storage efficiency

### LogQL Query Examples
- All logs: `{job="envoy-proxy"}`
- Errors only: `{job="envoy-proxy"} |= "5"`
- Specific supplier: `{job="envoy-proxy"} | json | supplier_id="supplier-123"`
- Response time filter: `{job="envoy-proxy"} | json | response_time_ms > 1000`

## Security Considerations

⚠️ **Important**: This setup uses default configurations for development convenience. For production:

1. Enable Grafana authentication and authorization
2. Configure Loki authentication if needed
3. Use TLS/SSL encryption for all communications
4. Restrict network access using firewalls
5. Set up proper user roles and permissions in Grafana
6. Consider using external authentication providers (LDAP, OAuth, etc.) 