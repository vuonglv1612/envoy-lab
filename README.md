# Bot Rate Limiting System

A comprehensive rate limiting solution for bot APIs using Envoy Proxy, Redis, and Go services. This system implements the SaaS subscription rate limiting design with dual-level rate limiting: per-second rate limits and subscription-based quotas.

## Architecture Overview

```mermaid
graph TD
    A[Bot Request] --> B[Envoy Proxy]
    B --> C[Rate Limiter Service<br/>ext_authz]
    C --> D[Redis<br/>Lua Scripts]
    C --> E{Rate Check}
    E -->|Allow| F[Backend API]
    E -->|Deny| G[429 Too Many Requests]
    F --> H[Usage Service<br/>ext_proc]
    H --> D
    H --> I[Response]
```

## Key Features

### 🔒 **Dual-Level Rate Limiting**
- **Rate Limiting**: Requests per second (burst protection)
- **Quota Limiting**: Total requests per billing period

### 🚀 **High Performance**
- Redis Lua scripts for atomic operations
- Fixed window rate limiting algorithm
- Automatic key expiration (1 second for rate counters)

### 🎯 **Bot Token Extraction**
- Parses bot tokens from URL paths: `/bot{TOKEN}/endpoint`
- Example: `/bot6148450508:AAFl4_SrGS-wh4wiO70XFb3dY_74ikigA1M/getUpdates`

### 📊 **Usage Tracking**
- Tracks successful requests (200 responses) against quotas
- Refunds rate limits for failed requests (non-200 responses)
- Monthly billing period support

## Redis Key Patterns

The system uses a simplified Redis key structure based on the design document:

```redis
# Rate Limits (per bot)
rate_limit:{bot_id} → 50 (requests per second)

# Rate Counters (per second window)
counter:{bot_id}:{timestamp} → 23 (requests in current second)
TTL: 1 second (auto-cleanup)

# Usage Totals (per billing period)
usage_total:{bot_id}:{period_start} → 1247 (total requests this period)
TTL: 30 days (configurable)
```

### Example for Bot `6148450508`:
```redis
rate_limit:6148450508 → 50
counter:6148450508:1718409600 → 23
usage_total:6148450508:1718236800 → 1247
```

## Quick Start

### 1. **Start the Backend Services**
```bash
# Start Redis, rate limiter, usage service, and backend API
docker-compose up -d

# Check service health
docker-compose ps
```

### 2. **Start Envoy Proxy (Separate Service)**
```bash
# Option 1: Use the provided script (recommended)
./scripts/run_envoy.sh

# Option 2: Run Envoy with Docker
docker run --rm -it -p 10000:10000 -p 9901:9901 \
  -v $(pwd)/envoy.yaml:/etc/envoy/envoy.yaml \
  --network host \
  envoyproxy/envoy:v1.28-latest \
  envoy -c /etc/envoy/envoy.yaml --service-cluster front-proxy

# Option 3: Run Envoy binary directly (if installed)
envoy -c envoy.yaml --service-cluster front-proxy
```

### 3. **Configure Bot Limits**
```bash
# Set rate limit for bot 6148450508 (50 req/sec)
uv run scripts/setup_bot_limits.py --bot 6148450508 --rate 50 --action set

# Check bot status
uv run scripts/setup_bot_limits.py --bot 6148450508 --action get
```

### 4. **Test the System**
```bash
# Run comprehensive tests
./scripts/test_rate_limiting.sh

# Test specific scenarios
./scripts/test_rate_limiting.sh --flood 100
./scripts/test_rate_limiting.sh --setup 25
```

### 4. **Make API Requests**
```bash
# Valid request (should work)
curl "http://localhost:10000/bot6148450508:AAFl4_SrGS-wh4wiO70XFb3dY_74ikigA1M/getUpdates"

# Check rate limit headers
curl -I "http://localhost:10000/bot6148450508:AAFl4_SrGS-wh4wiO70XFb3dY_74ikigA1M/getMe"
```

## Service Components

### 🛡️ **Rate Limiter Service** (Port 9001)
- **Purpose**: Envoy ext_authz integration
- **Function**: Checks rate limits before request processing
- **Technology**: Go gRPC service
- **Key Features**:
  - Bot token extraction from URL
  - Redis Lua script execution
  - Rate limit and quota validation
  - Temporary rate counter increments

### 📈 **Usage Tracking Service** (Port 9002)
- **Purpose**: Envoy ext_proc integration  
- **Function**: Tracks usage after request completion
- **Technology**: Go gRPC service
- **Key Features**:
  - Consumes quotas for successful requests (200)
  - Refunds rate limits for failed requests (non-200)
  - Real-time usage tracking

### 🚪 **Envoy Proxy** (Port 10000) - Separate Service
- **Purpose**: Request routing and filtering
- **Configuration**: `envoy.yaml`
- **Deployment**: Runs separately from Docker Compose
- **Filters**:
  - `ext_authz`: Rate limit checking
  - `ext_proc`: Usage tracking
  - `router`: Request forwarding

### 🗄️ **Redis** (Port 6379)
- **Purpose**: Rate limit storage and Lua script execution
- **Key Features**:
  - Atomic operations with Lua scripts
  - Automatic key expiration
  - High-performance counters

### 🔧 **Backend API** (Port 8000)
- **Purpose**: Bot API simulation
- **Technology**: FastAPI
- **Endpoints**:
  - `POST /sendMessage`: Test endpoint with configurable responses

## API Endpoints

### Bot API Format
```
http://localhost:10000/bot{BOT_TOKEN}/{ENDPOINT}
```

### Examples
```bash
# Get bot updates
GET /bot6148450508:AAFl4_SrGS-wh4wiO70XFb3dY_74ikigA1M/getUpdates

# Get bot info  
GET /bot6148450508:AAFl4_SrGS-wh4wiO70XFb3dY_74ikigA1M/getMe

# Send message (POST)
POST /bot6148450508:AAFl4_SrGS-wh4wiO70XFb3dY_74ikigA1M/sendMessage
Content-Type: application/json
{
  "message": "Hello World",
  "status": 200
}
```

## Rate Limiting Responses

### ✅ **Successful Request** (200)
```json
{
  "message": "Message received: Hello World",
  "status": 200,
  "success": true
}
```

**Headers:**
- `x-bot-token`: Bot token that was processed
- `x-quota-remaining`: Remaining quota for billing period
- `x-quota-limit`: Total quota limit
- `x-rate-limit`: Rate limit per second

### 🚫 **Rate Limited** (429)
```json
{
  "error": "rate_exceeded", 
  "message": "Rate limit exceeded - too many requests per second"
}
```

**Headers:**
- `retry-after`: 1 (seconds to wait)
- `x-rate-limit-reason`: Specific reason for limit

### 🚫 **Quota Exceeded** (429)
```json
{
  "error": "quota_exceeded",
  "message": "Bot quota exceeded (5000/5000 requests used)"
}
```

**Headers:**
- `retry-after`: 3600 (seconds until next billing period)

### ❌ **Invalid Bot Token** (401)
```json
{
  "error": "invalid_bot_token",
  "message": "Invalid or missing bot token in URL"
}
```

## Configuration

### Environment Variables

#### Rate Limiter Service
```bash
REDIS_ADDR=localhost:6379    # Redis connection string
GRPC_PORT=:9001             # gRPC server port
```

#### Usage Service
```bash
REDIS_ADDR=localhost:6379    # Redis connection string  
GRPC_PORT=:9002             # gRPC server port
```

### Default Rate Limits
- **Trial**: 50 requests/second, 5,000 requests/month
- **Pro**: 100 requests/second, 10,000 requests/month
- **Custom**: Configurable via management tools

## Management Tools

### Bot Limit Management
```bash
# Set bot rate limit
uv run scripts/setup_bot_limits.py --bot 6148450508 --rate 100 --action set

# Get bot status
uv run scripts/setup_bot_limits.py --bot 6148450508 --action get

# Delete bot limits
uv run scripts/setup_bot_limits.py --bot 6148450508 --action delete
```

### Testing Tools
```bash
# Run all tests
./scripts/test_rate_limiting.sh

# Flood test with 100 requests
./scripts/test_rate_limiting.sh --flood 100

# Setup custom rate limit
./scripts/test_rate_limiting.sh --setup 25

# Check Redis keys
./scripts/test_rate_limiting.sh --keys
```

## Monitoring

### Health Checks
```bash
# Check all services
docker-compose ps

# Check Redis connectivity
docker-compose exec redis redis-cli ping

# Check Envoy admin interface
curl http://localhost:9901/stats
```

### Redis Monitoring
```bash
# Check current keys
docker-compose exec redis redis-cli KEYS "*"

# Monitor real-time commands
docker-compose exec redis redis-cli MONITOR

# Check memory usage
docker-compose exec redis redis-cli INFO memory
```

### Logs
```bash
# View all logs
docker-compose logs -f

# Service-specific logs
docker-compose logs -f rate_limiter
docker-compose logs -f usage_service
docker-compose logs -f envoy
```

## Development

### Building Services
```bash
# Build rate limiter
cd rate_limiter
go build -o rate_limiter .

# Build usage service
cd usage_service  
go build -o usage_service .
```

### Running Locally
```bash
# Start Redis
docker run -d -p 6379:6379 redis:7-alpine

# Start rate limiter
cd rate_limiter
REDIS_ADDR=localhost:6379 go run .

# Start usage service
cd usage_service
REDIS_ADDR=localhost:6379 go run .

# Start backend
uvicorn main:app --port 8000

# Start Envoy (separate terminal)
./scripts/run_envoy.sh
# OR
envoy -c envoy.yaml --service-cluster front-proxy
```

## Troubleshooting

### Common Issues

#### 1. **Services Not Starting**
```bash
# Check Docker Compose logs
docker-compose logs

# Restart services
docker-compose down && docker-compose up -d
```

#### 2. **Redis Connection Failed**
```bash
# Check Redis health
docker-compose exec redis redis-cli ping

# Verify Redis port
docker-compose ps redis
```

#### 3. **Rate Limits Not Working**
```bash
# Check if bot limits are set
docker-compose exec redis redis-cli GET "rate_limit:6148450508"

# Set default limits
docker-compose exec redis redis-cli SET "rate_limit:6148450508" 50
```

#### 4. **Invalid Bot Token Error**
- Verify URL format: `/bot{TOKEN}/endpoint`
- Check token format: `{BOT_ID}:{SECRET}`
- Example: `/bot6148450508:AAFl4_SrGS-wh4wiO70XFb3dY_74ikigA1M/getUpdates`

### Debug Mode
```bash
# Enable verbose logging
export LOG_LEVEL=debug

# Check Envoy configuration
curl http://localhost:9901/config_dump
```

## Performance Tuning

### Redis Optimization
```bash
# Increase Redis memory limit
docker-compose exec redis redis-cli CONFIG SET maxmemory 256mb

# Check Redis performance
docker-compose exec redis redis-cli --latency-history
```

### Rate Limiter Tuning
- Adjust rate limits based on subscription tiers
- Monitor Redis memory usage for high-traffic bots
- Consider sharding for very high-volume deployments

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) file for details.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Submit a pull request

---

For more information, see the [SaaS Rate Limiting Design Document](saas_rate_limiting_design.md) and [Rate Limiting Knowledge Base](rate_limiting_knowledge_base.md).
