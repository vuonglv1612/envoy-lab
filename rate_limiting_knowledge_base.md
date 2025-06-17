# Rate Limiting Algorithms - Comprehensive Knowledge Base

## Table of Contents
1. [Overview](#overview)
2. [Core Concepts](#core-concepts)
3. [Algorithm Implementations](#algorithm-implementations)
4. [Redis-Based Implementation](#redis-based-implementation)
5. [Best Practices](#best-practices)
6. [Use Case Decision Matrix](#use-case-decision-matrix)
7. [Performance Considerations](#performance-considerations)
8. [Code Examples](#code-examples)

## Overview

Rate limiting is a critical technique for controlling the rate of requests sent or received by a system. It prevents resource exhaustion, ensures fair usage, and protects against abuse or denial-of-service attacks.

### Key Benefits
- **Resource Protection**: Prevents system overload
- **Fair Usage**: Ensures equitable access to resources
- **Security**: Mitigates brute force and DDoS attacks
- **Cost Control**: Manages usage-based billing
- **Quality of Service**: Maintains consistent performance

## Core Concepts

### Rate Limiting Components
1. **Identifier**: Who/what is being limited (user, IP, API key)
2. **Limit**: Maximum allowed requests (e.g., 100 requests)
3. **Window**: Time period for the limit (e.g., per minute, hour, day)
4. **Action**: What happens when limit is exceeded (block, queue, throttle)

### Common Rate Limit Expressions
```
1000/hour    - 1000 requests per hour
10/minute    - 10 requests per minute
1/second     - 1 request per second
100/day      - 100 requests per day
```

## Algorithm Implementations

### 1. Fixed Window Rate Limiting

**Description**: Uses a counter that resets at fixed intervals.

**How It Works**:
- Maintains a single counter per limit
- Increments counter for each request
- Resets counter at fixed intervals (e.g., every minute)
- Allows requests if counter < limit

**Redis Implementation**:
```lua
-- incr_expire.lua
local current
local amount = tonumber(ARGV[2])
current = redis.call("incrby", KEYS[1], amount)

if tonumber(current) == amount then
    redis.call("expire", KEYS[1], ARGV[1])
end

return current
```

**Key Characteristics**:
- ✅ Memory efficient (single counter)
- ✅ High performance
- ✅ Simple implementation
- ❌ Allows burst traffic at boundaries
- ❌ Sudden cutoffs

**Best Use Cases**:
- API rate limiting with clear billing periods
- Login attempt protection
- SaaS tier enforcement
- High-volume systems where performance matters
- DDoS protection layers

**Example Implementation**:
```python
class FixedWindowRateLimiter:
    def hit(self, item, *identifiers, cost=1):
        return (
            self.storage.incr(
                item.key_for(*identifiers),
                item.get_expiry(),
                amount=cost,
            ) <= item.amount
        )
    
    def test(self, item, *identifiers, cost=1):
        return self.storage.get(item.key_for(*identifiers)) < item.amount - cost + 1
```

### 2. Moving Window (Sliding Log) Rate Limiting

**Description**: Tracks individual request timestamps in a precise sliding window.

**How It Works**:
- Stores each request timestamp in a Redis list
- For new requests, checks timestamps within the window
- Removes expired timestamps
- Allows request if count + new_requests ≤ limit

**Redis Implementation**:
```lua
-- acquire_moving_window.lua
local timestamp = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local expiry = tonumber(ARGV[3])
local amount = tonumber(ARGV[4])

-- Check if adding amount would exceed limit
local entry = redis.call('lindex', KEYS[1], limit - amount)
if entry and tonumber(entry) >= timestamp - expiry then
    return false
end

-- Add new timestamps
local entries = {}
for i = 1, amount do
    entries[i] = timestamp
end

for i=1,#entries,5000 do
    redis.call('lpush', KEYS[1], unpack(entries, i, math.min(i+4999, #entries)))
end
redis.call('ltrim', KEYS[1], 0, limit - 1)
redis.call('expire', KEYS[1], expiry)

return true
```

**Optimization**: Uses position-based checking instead of scanning entire list.

**Key Characteristics**:
- ✅ Precise rate limiting
- ✅ No burst traffic issues
- ✅ Smooth enforcement
- ❌ Memory intensive (stores all timestamps)
- ❌ More complex operations

**Best Use Cases**:
- Critical systems where precision matters
- Financial API limits
- High-value resource protection
- Systems with strict SLA requirements

### 3. Sliding Window Counter Rate Limiting

**Description**: Approximates moving window using two counters with weighted calculation.

**How It Works**:
- Maintains two counters: current and previous window
- Calculates weighted count based on time elapsed
- Formula: `weighted_count = previous_count * (remaining_time/window_duration) + current_count`
- Automatically shifts windows when current expires

**Redis Implementation**:
```lua
-- acquire_sliding_window.lua
local limit = tonumber(ARGV[1])
local expiry = tonumber(ARGV[2]) * 1000
local amount = tonumber(ARGV[3])

-- Handle window shifting
local current_ttl = tonumber(redis.call('pttl', KEYS[2]))
if current_ttl > 0 and current_ttl < expiry then
    redis.call('rename', KEYS[2], KEYS[1])
    redis.call('set', KEYS[2], 0, 'PX', current_ttl + expiry)
end

-- Get counts and calculate weighted total
local previous_count = tonumber(redis.call('get', KEYS[1])) or 0
local previous_ttl = tonumber(redis.call('pttl', KEYS[1])) or 0
local current_count = tonumber(redis.call('get', KEYS[2])) or 0

local weighted_count = math.floor(previous_count * previous_ttl / expiry) + current_count

if (weighted_count + amount) > limit then
    return false
end

-- Update current counter
if redis.call('exists', KEYS[2]) == 1 then
    redis.call('incrby', KEYS[2], amount)
else
    redis.call('set', KEYS[2], amount, 'PX', expiry * 2)
end

return true
```

**Key Characteristics**:
- ✅ Good balance of accuracy and efficiency
- ✅ Smooth rate limiting
- ✅ Low memory usage
- ✅ Handles window transitions well
- ❌ Approximation (not perfectly precise)
- ❌ More complex than fixed window

**Best Use Cases**:
- General-purpose API rate limiting
- User-facing applications
- Systems requiring smooth experience
- Resource-constrained environments

## Redis-Based Implementation

### Storage Architecture

**Key Structure**:
```
LIMITS:{prefix}:{identifier}:{resource}
```

**Examples**:
```
LIMITS:api_key:abc123:requests     - API key rate limit
LIMITS:user:12345:uploads          - User upload limit
LIMITS:ip:192.168.1.1:login        - IP-based login limit
```

### Lua Scripts for Atomicity

All operations use Lua scripts to ensure atomicity and prevent race conditions:

1. **incr_expire.lua**: Fixed window increment with expiry
2. **acquire_moving_window.lua**: Moving window request processing
3. **acquire_sliding_window.lua**: Sliding window counter processing
4. **moving_window.lua**: Moving window state retrieval
5. **sliding_window.lua**: Sliding window state retrieval

### Cluster Support

Uses Redis hash tags `{key}` to ensure related keys are on the same cluster node:
```python
def _current_window_key(self, key):
    return f"{{{key}}}"

def _previous_window_key(self, key):
    return f"{self._current_window_key(key)}/-1"
```

## Best Practices

### 1. Algorithm Selection

**Choose Fixed Window when**:
- Memory efficiency is critical
- High performance required (>10K requests/second)
- Clear billing/quota periods needed
- Burst traffic is acceptable

**Choose Moving Window when**:
- Precision is critical
- Burst traffic must be prevented
- Memory usage is not a constraint
- Complex rate limiting logic needed

**Choose Sliding Window Counter when**:
- Balance of accuracy and efficiency needed
- User experience is important
- Resource constraints exist
- General-purpose rate limiting required

### 2. Implementation Strategies

**Multi-layered Rate Limiting**:
```python
# Layer 1: Global capacity protection
global_limit = parse("100000/minute")

# Layer 2: Per-user limits
user_limit = parse("1000/hour")

# Layer 3: Per-endpoint limits
api_limit = parse("100/minute")

def check_all_limits(user_id, endpoint):
    return (
        limiter.test(global_limit, "global") and
        limiter.test(user_limit, "user", user_id) and
        limiter.test(api_limit, "endpoint", endpoint, user_id)
    )
```

**Graceful Degradation**:
```python
def handle_rate_limit_exceeded(limit_type, reset_time):
    if limit_type == "tier_limit":
        return {"upgrade_url": "/pricing", "message": "Upgrade for higher limits"}
    elif limit_type == "burst_protection":
        return {"retry_after": 60, "message": "Please wait a moment"}
    else:
        return {"retry_after": reset_time, "message": "Rate limit exceeded"}
```

### 3. Monitoring and Alerting

**Key Metrics to Track**:
- Rate limit hit rate by user/endpoint
- 429 response rate
- Average requests per window
- Memory usage (especially for moving window)
- Lua script execution time

**Alert Conditions**:
- Rate limit hit rate > 10%
- Sudden spikes in 429 responses
- Memory usage growth (moving window)
- Unusual traffic patterns

## Use Case Decision Matrix

| Use Case | Fixed Window | Moving Window | Sliding Counter | Reasoning |
|----------|--------------|---------------|-----------------|-----------|
| **API Rate Limiting** | ✅ Primary | ⚠️ High-value APIs | ✅ Alternative | Performance + clear billing |
| **Login Protection** | ✅ Ideal | ❌ Overkill | ⚠️ Optional | Simple, effective |
| **DDoS Protection** | ✅ Ideal | ❌ Too expensive | ⚠️ Optional | High performance needed |
| **SaaS Billing** | ✅ Perfect | ❌ Unnecessary | ❌ Confusing | Clear billing periods |
| **Financial APIs** | ❌ Too risky | ✅ Required | ⚠️ Acceptable | Precision critical |
| **User-facing Apps** | ⚠️ UX issues | ✅ Smooth | ✅ Good balance | User experience matters |
| **IoT Rate Limiting** | ✅ Efficient | ❌ Memory cost | ✅ Good option | Resource constraints |
| **Critical Infrastructure** | ❌ Burst risk | ✅ Precise | ⚠️ Acceptable | Reliability critical |

## Performance Considerations

### Memory Usage Comparison

**Fixed Window**:
- 1 integer per limit
- ~8 bytes per active limit
- Scales linearly with unique limits

**Moving Window**:
- 1 timestamp per request
- ~8 bytes × limit × active_windows
- Can be very memory intensive

**Sliding Counter**:
- 2 integers per limit
- ~16 bytes per active limit
- Predictable memory usage

### Performance Benchmarks

Based on typical Redis operations:

| Algorithm | Operations/Request | Typical Latency | Memory per 1000 req/min |
|-----------|-------------------|-----------------|-------------------------|
| Fixed Window | 1-2 | <1ms | 8 bytes |
| Moving Window | 3-5 | 1-3ms | 8KB |
| Sliding Counter | 2-4 | 1-2ms | 16 bytes |

## Code Examples

### Basic Usage

```python
from limits import strategies, storage, parse

# Initialize storage and limiter
backend = storage.RedisStorage("redis://localhost:6379")
limiter = strategies.FixedWindowRateLimiter(backend)

# Define limits
api_limit = parse("1000/hour")
user_limit = parse("100/minute")

# Check and consume limits
def api_handler(request):
    user_id = request.user_id
    
    # Test before processing
    if not limiter.test(api_limit, "api", user_id):
        return {"error": "API limit exceeded", "retry_after": 3600}
    
    if not limiter.test(user_limit, "user", user_id):
        return {"error": "User limit exceeded", "retry_after": 60}
    
    # Process request
    limiter.hit(api_limit, "api", user_id)
    limiter.hit(user_limit, "user", user_id)
    
    return process_request(request)
```

### Advanced Multi-tier Limiting

```python
class TieredRateLimiter:
    def __init__(self, storage):
        self.limiter = strategies.FixedWindowRateLimiter(storage)
        self.tiers = {
            "free": {"requests": parse("100/day"), "burst": parse("10/minute")},
            "pro": {"requests": parse("10000/day"), "burst": parse("100/minute")},
            "enterprise": {"requests": parse("1000000/day"), "burst": parse("1000/minute")}
        }
    
    def check_limits(self, user_id, tier):
        limits = self.tiers[tier]
        
        # Check daily quota
        if not self.limiter.test(limits["requests"], "daily", user_id):
            return False, "daily_quota_exceeded"
        
        # Check burst protection
        if not self.limiter.test(limits["burst"], "burst", user_id):
            return False, "burst_limit_exceeded"
        
        return True, "allowed"
    
    def consume(self, user_id, tier, cost=1):
        allowed, reason = self.check_limits(user_id, tier)
        if not allowed:
            return False, reason
        
        limits = self.tiers[tier]
        self.limiter.hit(limits["requests"], "daily", user_id, cost=cost)
        self.limiter.hit(limits["burst"], "burst", user_id, cost=cost)
        
        return True, "consumed"
```

### Async Implementation

```python
import asyncio
from limits import aio

async def async_rate_limiting():
    storage = aio.storage.RedisStorage("redis://localhost:6379")
    limiter = aio.strategies.FixedWindowRateLimiter(storage)
    
    limit = parse("10/minute")
    
    async def process_request(user_id):
        if await limiter.hit(limit, "user", user_id):
            return f"Request processed for {user_id}"
        else:
            return f"Rate limit exceeded for {user_id}"
    
    # Process multiple requests concurrently
    tasks = [process_request(f"user_{i}") for i in range(20)]
    results = await asyncio.gather(*tasks)
    
    return results
```

### Custom Error Handling

```python
class RateLimitHandler:
    def __init__(self, limiter):
        self.limiter = limiter
    
    def check_with_details(self, limit, *identifiers):
        """Check limit and return detailed response"""
        if self.limiter.test(limit, *identifiers):
            return {"allowed": True}
        
        # Get window stats for retry information
        stats = self.limiter.get_window_stats(limit, *identifiers)
        
        return {
            "allowed": False,
            "retry_after": int(stats.reset_time - time.time()),
            "limit": limit.amount,
            "remaining": stats.remaining,
            "reset_time": stats.reset_time
        }
```

---

This knowledge base provides a comprehensive understanding of rate limiting algorithms, their implementations, and practical applications. The Redis-based implementation offers production-ready solutions with atomic operations, cluster support, and excellent performance characteristics suitable for modern distributed systems. 