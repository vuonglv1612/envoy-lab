# SaaS Subscription Rate Limiting - Technical Design

## Problem Statement

Implement rate limiting for a SaaS service with subscription plans:
- **Trial**: 15 days, 5,000 requests total, 50 req/s limit
- **Pro Monthly**: 30 days, 10,000 requests total, 100 req/s limit  
- **Pro Annual**: 365 days, 1,000,000 requests total, 100 req/s limit
- **Custom Plans**: Configurable duration, quotas, and rate limits

Example: User A subscribes to Trial on 2025-06-14, gets 5,000 requests from 06/14 to 06/29 with 50 req/s limit.

## Architecture Overview

### Dual-Level Rate Limiting
```
Request → Plan Validation → Quota Check → Rate Check → Allow/Deny
```

1. **Subscription Quota**: Total requests allowed during subscription period
2. **Rate Limiting**: Requests per second (burst protection)

## Redis Key Management Strategy

### Key Structure Design
```redis
# Plan metadata and configuration
LIMITS:plan:{user_id}:meta          → Plan config (JSON)

# Quota tracking (subscription lifetime)  
LIMITS:plan:{user_id}:quota         → Current usage counter

# Rate limiting (per-second fixed windows)
LIMITS:plan:{user_id}:rate:{second} → Requests in specific second
```

### Detailed Key Examples
```redis
# User A (Trial Plan)
LIMITS:plan:user_a:meta             → {"plan":"trial","start":1718323200,"end":1719619200,"quota_limit":5000,"rate_limit":50}
LIMITS:plan:user_a:quota            → 1247
LIMITS:plan:user_a:rate:1718409600  → 23
LIMITS:plan:user_a:rate:1718409601  → 45
```

### Key Expiration Strategy
- **Plan metadata**: Expires when subscription ends
- **Quota counter**: Expires when subscription ends  
- **Rate keys**: Expire after 1 second (automatic cleanup)

## Lua Scripts Implementation

### 1. Plan Setup Script

```lua
-- setup_plan.lua
-- Sets up a new subscription plan with quota and rate limits
-- KEYS[1]: plan metadata key (LIMITS:plan:{user_id}:meta)
-- KEYS[2]: quota counter key (LIMITS:plan:{user_id}:quota)
-- ARGV[1]: plan configuration JSON
-- ARGV[2]: subscription duration in seconds

local plan_config = ARGV[1]
local duration = tonumber(ARGV[2])

-- Set plan metadata with subscription expiration
redis.call('SET', KEYS[1], plan_config, 'EX', duration)

-- Initialize quota counter (starts at 0)
redis.call('SET', KEYS[2], 0, 'EX', duration)

return "OK"
```

### 2. Multi-Level Limit Check and Consume

```lua
-- consume_request.lua
-- Checks both quota and rate limits, consumes if allowed
-- KEYS[1]: plan metadata key
-- KEYS[2]: quota counter key  
-- KEYS[3]: rate limit key for current second
-- ARGV[1]: current timestamp
-- ARGV[2]: request cost (default 1)

local current_time = tonumber(ARGV[1])
local cost = tonumber(ARGV[2]) or 1

-- 1. Validate plan exists and is not expired
local plan_data = redis.call('GET', KEYS[1])
if not plan_data then
    return {0, "plan_not_found", 0, 0, 0}
end

-- 2. Check if subscription has expired (TTL check)
local plan_ttl = redis.call('TTL', KEYS[1])
if plan_ttl <= 0 then
    return {0, "subscription_expired", 0, 0, 0}
end

-- 3. Parse plan configuration
local plan = cjson.decode(plan_data)
local quota_limit = tonumber(plan.quota_limit)
local rate_limit = tonumber(plan.rate_limit)

-- 4. Check subscription quota
local current_quota = tonumber(redis.call('GET', KEYS[2])) or 0
if current_quota + cost > quota_limit then
    return {0, "quota_exceeded", current_quota, quota_limit, 0}
end

-- 5. Check rate limit (current second)
local current_rate = tonumber(redis.call('GET', KEYS[3])) or 0
if current_rate + cost > rate_limit then
    return {0, "rate_exceeded", current_quota, quota_limit, current_rate}
end

-- 6. Consume from both limits atomically
-- Update quota counter (preserve plan expiration)
redis.call('INCRBY', KEYS[2], cost)
redis.call('EXPIRE', KEYS[2], plan_ttl)

-- Update rate counter (1 second expiration)
redis.call('INCRBY', KEYS[3], cost)
redis.call('EXPIRE', KEYS[3], 1)

-- Return success with updated counters
local new_quota = current_quota + cost
local new_rate = current_rate + cost
return {1, "success", new_quota, quota_limit, new_rate}
```

### 3. Plan Status Check

```lua
-- get_plan_status.lua
-- Returns current plan status without consuming limits
-- KEYS[1]: plan metadata key
-- KEYS[2]: quota counter key
-- KEYS[3]: current rate limit key

local plan_data = redis.call('GET', KEYS[1])
if not plan_data then
    return {0, "plan_not_found"}
end

local plan = cjson.decode(plan_data)
local plan_ttl = redis.call('TTL', KEYS[1])

-- Get current usage
local quota_used = tonumber(redis.call('GET', KEYS[2])) or 0
local rate_used = tonumber(redis.call('GET', KEYS[3])) or 0

return {
    1,                                    -- success
    plan.plan_type,                       -- plan type
    tonumber(plan.quota_limit),           -- quota limit
    quota_used,                           -- quota used
    tonumber(plan.quota_limit) - quota_used, -- quota remaining
    tonumber(plan.rate_limit),            -- rate limit
    rate_used,                            -- rate used in current second
    tonumber(plan.rate_limit) - rate_used,   -- rate remaining
    plan_ttl                              -- seconds until plan expires
}
```

### 4. Plan Renewal/Upgrade

```lua
-- renew_plan.lua
-- Renews or upgrades a subscription plan
-- KEYS[1]: plan metadata key
-- KEYS[2]: quota counter key
-- ARGV[1]: new plan configuration JSON
-- ARGV[2]: new subscription duration in seconds

local new_config = ARGV[1]
local new_duration = tonumber(ARGV[2])

-- Set new plan configuration
redis.call('SET', KEYS[1], new_config, 'EX', new_duration)

-- Reset quota counter for new subscription period
redis.call('SET', KEYS[2], 0, 'EX', new_duration)

-- Rate limit keys will expire naturally (1 second TTL)
return "OK"
```

### 5. Rate Limit Check (Envoy ext_auth)

```lua
-- check_rate_limit.lua
-- Temporarily increases rate counter for Envoy ext_auth
-- Only checks limits without affecting quota
-- KEYS[1]: plan metadata key
-- KEYS[2]: quota counter key  
-- KEYS[3]: rate limit key for current second
-- ARGV[1]: current timestamp
-- ARGV[2]: request cost (default 1)

local current_time = tonumber(ARGV[1])
local cost = tonumber(ARGV[2]) or 1

-- 1. Validate plan exists and is not expired
local plan_data = redis.call('GET', KEYS[1])
if not plan_data then
    return {0, "plan_not_found", 0, 0, 0}
end

local plan_ttl = redis.call('TTL', KEYS[1])
if plan_ttl <= 0 then
    return {0, "subscription_expired", 0, 0, 0}
end

-- 2. Parse plan configuration
local plan = cjson.decode(plan_data)
local quota_limit = tonumber(plan.quota_limit)
local rate_limit = tonumber(plan.rate_limit)

-- 3. Check subscription quota (don't consume yet)
local current_quota = tonumber(redis.call('GET', KEYS[2])) or 0
if current_quota + cost > quota_limit then
    return {0, "quota_exceeded", current_quota, quota_limit, 0}
end

-- 4. Check and temporarily increment rate limit
local current_rate = tonumber(redis.call('GET', KEYS[3])) or 0
if current_rate + cost > rate_limit then
    return {0, "rate_exceeded", current_quota, quota_limit, current_rate}
end

-- 5. Temporarily increment rate counter (will be decremented if request fails)
redis.call('INCRBY', KEYS[3], cost)
redis.call('EXPIRE', KEYS[3], 1)

-- Return success with current state
return {1, "success", current_quota, quota_limit, current_rate + cost}
```

### 6. Usage Tracking Operations

```lua
-- consume_usage.lua
-- Consumes from quota on successful request (200 response)
-- KEYS[1]: plan metadata key
-- KEYS[2]: quota counter key
-- ARGV[1]: request cost (default 1)

local cost = tonumber(ARGV[1]) or 1

-- Validate plan exists and is not expired
local plan_data = redis.call('GET', KEYS[1])
if not plan_data then
    return {0, "plan_not_found"}
end

local plan_ttl = redis.call('TTL', KEYS[1])
if plan_ttl <= 0 then
    return {0, "subscription_expired"}
end

-- Consume from quota
redis.call('INCRBY', KEYS[2], cost)
redis.call('EXPIRE', KEYS[2], plan_ttl)

local new_quota = tonumber(redis.call('GET', KEYS[2]))
return {1, "consumed", new_quota}
```

```lua
-- refund_rate_limit.lua
-- Refunds rate limit on failed request (non-200 response)
-- KEYS[1]: rate limit key for current second
-- ARGV[1]: request cost to refund (default 1)

local cost = tonumber(ARGV[1]) or 1

-- Check if rate key exists
if redis.call('EXISTS', KEYS[1]) == 1 then
    -- Decrement rate counter (but not below 0)
    local current_rate = tonumber(redis.call('GET', KEYS[1])) or 0
    local new_rate = math.max(0, current_rate - cost)
    
    if new_rate > 0 then
        redis.call('SET', KEYS[1], new_rate)
        redis.call('EXPIRE', KEYS[1], 1)
    else
        redis.call('DEL', KEYS[1])
    end
    
    return {1, "refunded", new_rate}
end

return {0, "not_found"}
```

## Python Implementation

### Core SaaS Rate Limiter Class

```python
import json
import time
import redis
from typing import Dict, Tuple, Optional, NamedTuple
from datetime import datetime, timedelta

class PlanConfig(NamedTuple):
    plan_type: str
    start_time: int
    end_time: int  
    duration_days: int
    quota_limit: int
    rate_limit: int

class LimitCheckResult(NamedTuple):
    allowed: bool
    reason: str
    quota_used: int
    quota_limit: int
    quota_remaining: int
    rate_used: int
    rate_limit: int
    expires_in_seconds: int

class SaaSRateLimiter:
    """
    Multi-level rate limiter for SaaS subscriptions
    Handles both subscription quotas and per-second rate limits
    """
    
    # Predefined subscription plans
    PLANS = {
        "trial": {"duration_days": 15, "quota_limit": 5000, "rate_limit": 50},
        "pro_monthly": {"duration_days": 30, "quota_limit": 10000, "rate_limit": 100},
        "pro_annual": {"duration_days": 365, "quota_limit": 1000000, "rate_limit": 100}
    }
    
    def __init__(self, redis_client: redis.Redis, key_prefix: str = "LIMITS"):
        self.redis = redis_client
        self.prefix = key_prefix
        
        # Register Lua scripts
        self.setup_plan_script = self._register_script("setup_plan")
        self.consume_request_script = self._register_script("consume_request") 
        self.get_status_script = self._register_script("get_plan_status")
        self.renew_plan_script = self._register_script("renew_plan")
        self.check_rate_limit_script = self._register_script("check_rate_limit")
        self.consume_usage_script = self._register_script("consume_usage")
        self.refund_rate_limit_script = self._register_script("refund_rate_limit")
    
    def _register_script(self, script_name: str) -> redis.client.Script:
        """Register Lua scripts with Redis"""
        scripts = {
            "setup_plan": """
                local plan_config = ARGV[1]
                local duration = tonumber(ARGV[2])
                redis.call('SET', KEYS[1], plan_config, 'EX', duration)
                redis.call('SET', KEYS[2], 0, 'EX', duration)
                return "OK"
            """,
            
            "consume_request": """
                local current_time = tonumber(ARGV[1])
                local cost = tonumber(ARGV[2]) or 1
                
                local plan_data = redis.call('GET', KEYS[1])
                if not plan_data then
                    return {0, "plan_not_found", 0, 0, 0}
                end
                
                local plan = cjson.decode(plan_data)
                local quota_limit = tonumber(plan.quota_limit)
                local rate_limit = tonumber(plan.rate_limit)
                
                local current_quota = tonumber(redis.call('GET', KEYS[2])) or 0
                if current_quota + cost > quota_limit then
                    return {0, "quota_exceeded", current_quota, quota_limit, 0}
                end
                
                local current_rate = tonumber(redis.call('GET', KEYS[3])) or 0
                if current_rate + cost > rate_limit then
                    return {0, "rate_exceeded", current_quota, quota_limit, current_rate}
                end
                
                local plan_ttl = redis.call('TTL', KEYS[1])
                if plan_ttl > 0 then
                    redis.call('INCRBY', KEYS[2], cost)
                    redis.call('EXPIRE', KEYS[2], plan_ttl)
                end
                
                redis.call('INCRBY', KEYS[3], cost)
                redis.call('EXPIRE', KEYS[3], 1)
                
                local new_quota = current_quota + cost
                local new_rate = current_rate + cost
                return {1, "success", new_quota, quota_limit, new_rate}
            """,
            
            "get_plan_status": """
                local plan_data = redis.call('GET', KEYS[1])
                if not plan_data then
                    return {0, "plan_not_found"}
                end
                
                local plan = cjson.decode(plan_data)
                local plan_ttl = redis.call('TTL', KEYS[1])
                local quota_used = tonumber(redis.call('GET', KEYS[2])) or 0
                local rate_used = tonumber(redis.call('GET', KEYS[3])) or 0
                
                return {
                    1, plan.plan_type, tonumber(plan.quota_limit), quota_used,
                    tonumber(plan.quota_limit) - quota_used, tonumber(plan.rate_limit),
                    rate_used, tonumber(plan.rate_limit) - rate_used, plan_ttl
                }
            """,
            
            "renew_plan": """
                local new_config = ARGV[1]
                local new_duration = tonumber(ARGV[2])
                redis.call('SET', KEYS[1], new_config, 'EX', new_duration)
                redis.call('SET', KEYS[2], 0, 'EX', new_duration)
                return "OK"
            """,
            
            "check_rate_limit": """
                local current_time = tonumber(ARGV[1])
                local cost = tonumber(ARGV[2]) or 1
                
                local plan_data = redis.call('GET', KEYS[1])
                if not plan_data then
                    return {0, "plan_not_found", 0, 0, 0}
                end
                
                local plan_ttl = redis.call('TTL', KEYS[1])
                if plan_ttl <= 0 then
                    return {0, "subscription_expired", 0, 0, 0}
                end
                
                local plan = cjson.decode(plan_data)
                local quota_limit = tonumber(plan.quota_limit)
                local rate_limit = tonumber(plan.rate_limit)
                
                local current_quota = tonumber(redis.call('GET', KEYS[2])) or 0
                if current_quota + cost > quota_limit then
                    return {0, "quota_exceeded", current_quota, quota_limit, 0}
                end
                
                local current_rate = tonumber(redis.call('GET', KEYS[3])) or 0
                if current_rate + cost > rate_limit then
                    return {0, "rate_exceeded", current_quota, quota_limit, current_rate}
                end
                
                redis.call('INCRBY', KEYS[3], cost)
                redis.call('EXPIRE', KEYS[3], 1)
                
                return {1, "success", current_quota, quota_limit, current_rate + cost}
            """,
            
            "consume_usage": """
                local cost = tonumber(ARGV[1]) or 1
                
                local plan_data = redis.call('GET', KEYS[1])
                if not plan_data then
                    return {0, "plan_not_found"}
                end
                
                local plan_ttl = redis.call('TTL', KEYS[1])
                if plan_ttl <= 0 then
                    return {0, "subscription_expired"}
                end
                
                redis.call('INCRBY', KEYS[2], cost)
                redis.call('EXPIRE', KEYS[2], plan_ttl)
                
                local new_quota = tonumber(redis.call('GET', KEYS[2]))
                return {1, "consumed", new_quota}
            """,
            
            "refund_rate_limit": """
                local cost = tonumber(ARGV[1]) or 1
                
                if redis.call('EXISTS', KEYS[1]) == 1 then
                    local current_rate = tonumber(redis.call('GET', KEYS[1])) or 0
                    local new_rate = math.max(0, current_rate - cost)
                    
                    if new_rate > 0 then
                        redis.call('SET', KEYS[1], new_rate)
                        redis.call('EXPIRE', KEYS[1], 1)
                    else
                        redis.call('DEL', KEYS[1])
                    end
                    
                    return {1, "refunded", new_rate}
                end
                
                return {0, "not_found"}
            """
        }
        
        return self.redis.register_script(scripts[script_name])
    
    def _get_redis_keys(self, user_id: str) -> Tuple[str, str, str]:
        """Generate Redis keys for a user"""
        current_second = int(time.time())
        base_key = f"{self.prefix}:plan:{user_id}"
        
        return (
            f"{base_key}:meta",                    # Plan metadata
            f"{base_key}:quota",                   # Quota counter  
            f"{base_key}:rate:{current_second}"    # Rate limit (current second)
        )
    
    def create_subscription(self, user_id: str, plan_type: str, 
                          custom_config: Optional[Dict] = None) -> bool:
        """
        Create a new subscription plan for a user
        
        Args:
            user_id: Unique user identifier
            plan_type: Plan type (trial, pro_monthly, pro_annual, custom)
            custom_config: For custom plans: {duration_days, quota_limit, rate_limit}
        """
        if plan_type in self.PLANS:
            config = self.PLANS[plan_type].copy()
        elif plan_type == "custom" and custom_config:
            config = custom_config
        else:
            raise ValueError(f"Invalid plan type or missing config: {plan_type}")
        
        # Calculate timestamps
        start_time = int(time.time())
        duration_seconds = config["duration_days"] * 24 * 3600
        end_time = start_time + duration_seconds
        
        # Prepare plan metadata
        plan_data = {
            "plan_type": plan_type,
            "start_time": start_time,
            "end_time": end_time,
            "duration_days": config["duration_days"],
            "quota_limit": config["quota_limit"],
            "rate_limit": config["rate_limit"]
        }
        
        # Execute plan setup
        meta_key, quota_key, _ = self._get_redis_keys(user_id)
        result = self.setup_plan_script(
            keys=[meta_key, quota_key],
            args=[json.dumps(plan_data), duration_seconds]
        )
        
        return result == b"OK"
    
    def check_and_consume_request(self, user_id: str, cost: int = 1) -> LimitCheckResult:
        """
        Check limits and consume request if allowed
        
        Args:
            user_id: User identifier
            cost: Request cost (default 1)
            
        Returns:
            LimitCheckResult with detailed information
        """
        current_time = int(time.time())
        meta_key, quota_key, rate_key = self._get_redis_keys(user_id)
        
        result = self.consume_request_script(
            keys=[meta_key, quota_key, rate_key],
            args=[current_time, cost]
        )
        
        return self._parse_consume_result(result)
    
    def get_plan_status(self, user_id: str) -> Optional[LimitCheckResult]:
        """Get current plan status without consuming limits"""
        meta_key, quota_key, rate_key = self._get_redis_keys(user_id)
        
        result = self.get_status_script(keys=[meta_key, quota_key, rate_key])
        
        if not result[0]:  # Plan not found
            return None
            
        return LimitCheckResult(
            allowed=True,
            reason="status_check",
            quota_used=result[3],
            quota_limit=result[2], 
            quota_remaining=result[4],
            rate_used=result[6],
            rate_limit=result[5],
            expires_in_seconds=result[8]
        )
    
    def upgrade_plan(self, user_id: str, new_plan_type: str, 
                    custom_config: Optional[Dict] = None) -> bool:
        """Upgrade or renew a subscription plan"""
        return self.create_subscription(user_id, new_plan_type, custom_config)
    
    def check_rate_limit_only(self, user_id: str, cost: int = 1) -> LimitCheckResult:
        """
        Check rate limits for Envoy ext_auth without consuming quota
        Temporarily increments rate counter (will be refunded if request fails)
        """
        current_time = int(time.time())
        meta_key, quota_key, rate_key = self._get_redis_keys(user_id)
        
        result = self.check_rate_limit_script(
            keys=[meta_key, quota_key, rate_key],
            args=[current_time, cost]
        )
        
        return self._parse_consume_result(result)
    
    def consume_usage_only(self, user_id: str, cost: int = 1) -> bool:
        """
        Consume quota for successful requests (200 response)
        Called by Envoy ext_proc after successful response
        """
        meta_key, quota_key, _ = self._get_redis_keys(user_id)
        
        result = self.consume_usage_script(
            keys=[meta_key, quota_key],
            args=[cost]
        )
        
        return bool(result[0])
    
    def refund_rate_limit(self, user_id: str, cost: int = 1) -> bool:
        """
        Refund rate limit for failed requests (non-200 response)
        Called by Envoy ext_proc after failed response
        """
        _, _, rate_key = self._get_redis_keys(user_id)
        
        result = self.refund_rate_limit_script(
            keys=[rate_key],
            args=[cost]
        )
        
        return bool(result[0])
    
    def _parse_consume_result(self, result) -> LimitCheckResult:
        """Parse Lua script result into LimitCheckResult"""
        allowed = bool(result[0])
        reason = result[1].decode() if isinstance(result[1], bytes) else result[1]
        quota_used = result[2]
        quota_limit = result[3]
        rate_used = result[4] if len(result) > 4 else 0
        
        return LimitCheckResult(
            allowed=allowed,
            reason=reason,
            quota_used=quota_used,
            quota_limit=quota_limit,
            quota_remaining=max(0, quota_limit - quota_used),
            rate_used=rate_used,
            rate_limit=0,  # Not returned in this context
            expires_in_seconds=0  # Can be calculated if needed
        )

# Usage Examples
def main():
    # Initialize Redis connection
    redis_client = redis.Redis(host='localhost', port=6379, db=0)
    limiter = SaaSRateLimiter(redis_client)
    
    # Example 1: Create Trial subscription for User A (June 14, 2025)
    print("Creating trial subscription for user_a...")
    success = limiter.create_subscription("user_a", "trial")
    print(f"Trial subscription created: {success}")
    
    # Example 2: Create custom plan (60 days, 25,000 requests, 80 req/s)
    custom_config = {
        "duration_days": 60,
        "quota_limit": 25000,
        "rate_limit": 80
    }
    limiter.create_subscription("user_b", "custom", custom_config)
    
    # Example 3: Process API requests
    def handle_api_request(user_id: str):
        result = limiter.check_and_consume_request(user_id)
        
        if result.allowed:
            return {
                "success": True,
                "quota_remaining": result.quota_remaining,
                "message": "Request processed successfully"
            }
        else:
            error_messages = {
                "plan_not_found": "No active subscription found",
                "subscription_expired": "Subscription has expired", 
                "quota_exceeded": f"Subscription quota exceeded ({result.quota_used}/{result.quota_limit})",
                "rate_exceeded": "Rate limit exceeded (too many requests per second)"
            }
            
            retry_after = 1 if result.reason == "rate_exceeded" else 3600
            
            return {
                "success": False,
                "error": error_messages.get(result.reason, "Rate limit exceeded"),
                "retry_after": retry_after,
                "quota_used": result.quota_used,
                "quota_limit": result.quota_limit
            }
    
    # Test requests
    print("\nTesting API requests:")
    for i in range(5):
        response = handle_api_request("user_a")
        print(f"Request {i+1}: {response}")
    
    # Example 4: Check plan status
    status = limiter.get_plan_status("user_a")
    if status:
        print(f"\nPlan Status:")
        print(f"Quota: {status.quota_used}/{status.quota_limit}")
        print(f"Remaining: {status.quota_remaining}")
        print(f"Expires in: {status.expires_in_seconds} seconds")

if __name__ == "__main__":
    main()
```

## API Integration Example

```python
from flask import Flask, request, jsonify, g
import jwt

app = Flask(__name__)
limiter = SaaSRateLimiter(redis.Redis())

@app.before_request
def authenticate_and_rate_limit():
    """Apply rate limiting to all API requests"""
    
    # Skip for certain endpoints
    if request.endpoint in ['health', 'login', 'signup']:
        return
    
    # Extract user from JWT token
    try:
        token = request.headers.get('Authorization', '').replace('Bearer ', '')
        payload = jwt.decode(token, 'secret', algorithms=['HS256'])
        user_id = payload['user_id']
        g.user_id = user_id
    except:
        return jsonify({"error": "Invalid authentication"}), 401
    
    # Apply rate limiting
    result = limiter.check_and_consume_request(user_id)
    
    if not result.allowed:
        error_response = {
            "error": "Rate limit exceeded",
            "reason": result.reason,
            "quota_used": result.quota_used,
            "quota_limit": result.quota_limit,
            "retry_after": 1 if result.reason == "rate_exceeded" else 3600
        }
        
        status_code = 429  # Too Many Requests
        return jsonify(error_response), status_code

@app.after_request  
def add_rate_limit_headers(response):
    """Add rate limiting info to response headers"""
    if hasattr(g, 'user_id'):
        status = limiter.get_plan_status(g.user_id)
        if status:
            response.headers['X-RateLimit-Quota-Remaining'] = str(status.quota_remaining)
            response.headers['X-RateLimit-Quota-Limit'] = str(status.quota_limit)
    return response

@app.route('/api/subscription', methods=['POST'])
def create_subscription():
    """Create new subscription"""
    data = request.get_json()
    plan_type = data.get('plan_type')
    user_id = g.user_id
    
    custom_config = None
    if plan_type == 'custom':
        custom_config = {
            'duration_days': data.get('duration_days'),
            'quota_limit': data.get('quota_limit'), 
            'rate_limit': data.get('rate_limit')
        }
    
    success = limiter.create_subscription(user_id, plan_type, custom_config)
    
    if success:
        return jsonify({"message": "Subscription created successfully"})
    else:
        return jsonify({"error": "Failed to create subscription"}), 400

@app.route('/api/usage', methods=['GET'])
def get_usage():
    """Get current usage statistics"""
    user_id = g.user_id
    status = limiter.get_plan_status(user_id)
    
    if not status:
        return jsonify({"error": "No active subscription"}), 404
    
    return jsonify({
        "quota_used": status.quota_used,
        "quota_limit": status.quota_limit,
        "quota_remaining": status.quota_remaining,
        "expires_in_seconds": status.expires_in_seconds
    })
```

## Rate Limiter Operations

### Envoy ext_auth Integration

The Rate Limiter Operations handle the first phase of request processing through Envoy's external authorization (ext_auth) filter. This phase checks rate limits and temporarily increments counters without consuming the subscription quota.

```python
class RateLimiterService:
    """
    Envoy ext_auth service for rate limit checking
    Integrates with Envoy proxy to authorize requests based on subscription limits
    """
    
    def __init__(self, redis_client: redis.Redis):
        self.limiter = SaaSRateLimiter(redis_client)
    
    def check_authorization(self, user_id: str, request_cost: int = 1) -> Tuple[bool, Dict]:
        """
        Check if request should be authorized based on rate limits
        
        Returns:
            (allowed: bool, response_data: dict)
        """
        # Extract user_id from bot request (all bots for a user share same limits)
        result = self.limiter.check_rate_limit_only(user_id, request_cost)
        
        if result.allowed:
            return True, {
                "status": "ALLOWED",
                "quota_remaining": result.quota_remaining,
                "rate_remaining": result.rate_limit - result.rate_used,
                "headers": {
                    "X-RateLimit-Quota-Remaining": str(result.quota_remaining),
                    "X-RateLimit-Quota-Limit": str(result.quota_limit),
                    "X-User-ID": user_id  # Pass user_id to backend for tracking
                }
            }
        else:
            # Determine retry after based on error type
            retry_after = 1 if result.reason == "rate_exceeded" else 3600
            
            error_messages = {
                "plan_not_found": "No active subscription found",
                "subscription_expired": "Subscription has expired",
                "quota_exceeded": "Subscription quota exceeded",
                "rate_exceeded": "Rate limit exceeded"
            }
            
            return False, {
                "status": "DENIED",
                "code": 429,  # Too Many Requests
                "message": error_messages.get(result.reason, "Rate limit exceeded"),
                "headers": {
                    "Retry-After": str(retry_after),
                    "X-RateLimit-Quota-Used": str(result.quota_used),
                    "X-RateLimit-Quota-Limit": str(result.quota_limit)
                }
            }

# Envoy ext_auth gRPC service implementation
import grpc
from envoy.service.auth.v3 import external_auth_pb2
from envoy.service.auth.v3 import external_auth_pb2_grpc

class EnvoyAuthServer(external_auth_pb2_grpc.AuthorizationServicer):
    def __init__(self, redis_client):
        self.rate_limiter_service = RateLimiterService(redis_client)
    
    def Check(self, request, context):
        # Extract user information from request headers
        headers = request.attributes.request.http.headers
        user_id = headers.get('x-user-id')  # From JWT or API key
        
        if not user_id:
            return external_auth_pb2.CheckResponse(
                status=external_auth_pb2.Status(code=401),
                denied_response=external_auth_pb2.DeniedHttpResponse(
                    status=external_auth_pb2.HttpStatus(code=401),
                    body="Missing user identification"
                )
            )
        
        # Check rate limits
        allowed, response_data = self.rate_limiter_service.check_authorization(user_id)
        
        if allowed:
            # Add headers to pass to backend
            response_headers = []
            for key, value in response_data["headers"].items():
                response_headers.append(
                    external_auth_pb2.HeaderValueOption(
                        header=external_auth_pb2.HeaderValue(key=key, value=value)
                    )
                )
            
            return external_auth_pb2.CheckResponse(
                status=external_auth_pb2.Status(code=0),  # OK
                ok_response=external_auth_pb2.OkHttpResponse(
                    headers=response_headers
                )
            )
        else:
            # Add rate limit headers to error response
            response_headers = []
            for key, value in response_data["headers"].items():
                response_headers.append(
                    external_auth_pb2.HeaderValue(key=key, value=value)
                )
            
            return external_auth_pb2.CheckResponse(
                status=external_auth_pb2.Status(code=response_data["code"]),
                denied_response=external_auth_pb2.DeniedHttpResponse(
                    status=external_auth_pb2.HttpStatus(code=response_data["code"]),
                    headers=response_headers,
                    body=response_data["message"]
                )
            )
```

### Envoy Configuration Example

```yaml
# envoy.yaml
static_resources:
  listeners:
  - name: bot_api_listener
    address:
      socket_address:
        address: 0.0.0.0
        port_value: 8080
    filter_chains:
    - filters:
      - name: envoy.filters.network.http_connection_manager
        typed_config:
          "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
          codec_type: AUTO
          stat_prefix: bot_api
          route_config:
            name: bot_api_route
            virtual_hosts:
            - name: bot_api
              domains: ["*"]
              routes:
              - match: { prefix: "/api/" }
                route: { cluster: bot_backend }
          http_filters:
          # Rate limiting check (ext_auth)
          - name: envoy.filters.http.ext_authz
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.filters.http.ext_authz.v3.ExtAuthz
              grpc_service:
                envoy_grpc:
                  cluster_name: rate_limiter_service
              transport_api_version: V3
          # Usage tracking (ext_proc)
          - name: envoy.filters.http.ext_proc
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor
              grpc_service:
                envoy_grpc:
                  cluster_name: usage_tracking_service
              processing_mode:
                response_header_mode: SEND
          - name: envoy.filters.http.router

  clusters:
  - name: bot_backend
    type: LOGICAL_DNS
    dns_lookup_family: V4_ONLY
    load_assignment:
      cluster_name: bot_backend
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: bot-backend
                port_value: 8000
  
  - name: rate_limiter_service
    type: LOGICAL_DNS
    http2_protocol_options: {}
    dns_lookup_family: V4_ONLY
    load_assignment:
      cluster_name: rate_limiter_service
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: rate-limiter
                port_value: 9001
  
  - name: usage_tracking_service
    type: LOGICAL_DNS
    http2_protocol_options: {}
    dns_lookup_family: V4_ONLY
    load_assignment:
      cluster_name: usage_tracking_service
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: usage-tracker
                port_value: 9002
```

## Usage Tracking Service Operations

### Envoy ext_proc Integration

The Usage Tracking Service handles the second phase of request processing through Envoy's external processing (ext_proc) filter. This phase consumes subscription quotas for successful requests and refunds rate limits for failed requests.

```python
class UsageTrackingService:
    """
    Envoy ext_proc service for usage tracking
    Handles quota consumption and rate limit refunds based on response status
    """
    
    def __init__(self, redis_client: redis.Redis):
        self.limiter = SaaSRateLimiter(redis_client)
    
    def process_response(self, user_id: str, status_code: int, request_cost: int = 1) -> Dict:
        """
        Process response and update usage counters
        
        Args:
            user_id: User identifier
            status_code: HTTP status code from backend
            request_cost: Cost of the request (default 1)
        """
        if status_code == 200:
            # Successful request - consume quota
            consumed = self.limiter.consume_usage_only(user_id, request_cost)
            
            if consumed:
                status = self.limiter.get_plan_status(user_id)
                return {
                    "action": "consumed",
                    "quota_used": status.quota_used if status else 0,
                    "quota_remaining": status.quota_remaining if status else 0,
                    "headers": {
                        "X-Usage-Consumed": str(request_cost),
                        "X-Quota-Remaining": str(status.quota_remaining if status else 0)
                    }
                }
            else:
                return {
                    "action": "consume_failed",
                    "error": "Failed to consume quota",
                    "headers": {}
                }
        else:
            # Failed request - refund rate limit
            refunded = self.limiter.refund_rate_limit(user_id, request_cost)
            
            return {
                "action": "refunded" if refunded else "refund_failed",
                "headers": {
                    "X-Rate-Limit-Refunded": str(request_cost) if refunded else "0"
                }
            }

# Envoy ext_proc gRPC service implementation
from envoy.service.ext_proc.v3 import external_processor_pb2
from envoy.service.ext_proc.v3 import external_processor_pb2_grpc
from envoy.config.core.v3 import header_value_pb2

class EnvoyProcessorServer(external_processor_pb2_grpc.ExternalProcessorServicer):
    def __init__(self, redis_client):
        self.usage_service = UsageTrackingService(redis_client)
    
    def Process(self, request_iterator, context):
        for request in request_iterator:
            if request.HasField('response_headers'):
                # Extract user_id and status from response
                headers = request.response_headers.headers.headers
                user_id = None
                status_code = 200
                
                # Find user ID from request headers (set by ext_auth)
                for header in headers:
                    if header.key == 'x-user-id':
                        user_id = header.value
                    elif header.key == ':status':
                        status_code = int(header.value)
                
                if user_id:
                    # Process the response
                    result = self.usage_service.process_response(user_id, status_code)
                    
                    # Add tracking headers to response
                    response_headers = []
                    for key, value in result["headers"].items():
                        response_headers.append(
                            header_value_pb2.HeaderValueOption(
                                header=header_value_pb2.HeaderValue(key=key, value=value)
                            )
                        )
                    
                    yield external_processor_pb2.ProcessingResponse(
                        response_headers=external_processor_pb2.HeadersResponse(
                            response=external_processor_pb2.CommonResponse(
                                header_mutation=external_processor_pb2.HeaderMutation(
                                    set_headers=response_headers
                                )
                            )
                        )
                    )
                else:
                    # No user_id found, continue without processing
                    yield external_processor_pb2.ProcessingResponse(
                        response_headers=external_processor_pb2.HeadersResponse()
                    )
            else:
                # For other request types, continue without processing
                yield external_processor_pb2.ProcessingResponse()
```

### Multi-Bot Usage Tracking

Since users can create multiple bots, all bot requests are tracked under a single user account:

```python
class BotManager:
    """
    Manages bot creation and tracks usage per user
    All bots created by a user share the same rate limits and quota
    """
    
    def __init__(self, redis_client: redis.Redis, db_client):
        self.limiter = SaaSRateLimiter(redis_client)
        self.db = db_client
    
    def create_bot(self, user_id: str, bot_config: Dict) -> Dict:
        """
        Create a new bot for a user
        Bot is added to user's billing list
        """
        # Check if user has an active subscription
        status = self.limiter.get_plan_status(user_id)
        if not status:
            return {
                "success": False,
                "error": "No active subscription found"
            }
        
        # Create bot in database
        bot_id = self._create_bot_in_db(user_id, bot_config)
        
        # Add bot to user's billing list
        self._add_bot_to_billing(user_id, bot_id)
        
        return {
            "success": True,
            "bot_id": bot_id,
            "user_id": user_id,
            "quota_remaining": status.quota_remaining,
            "rate_limit": status.rate_limit
        }
    
    def get_user_bots(self, user_id: str) -> List[Dict]:
        """Get all bots for a user with usage information"""
        bots = self._get_user_bots_from_db(user_id)
        status = self.limiter.get_plan_status(user_id)
        
        return {
            "bots": bots,
            "subscription_status": {
                "quota_used": status.quota_used if status else 0,
                "quota_limit": status.quota_limit if status else 0,
                "quota_remaining": status.quota_remaining if status else 0,
                "expires_in_seconds": status.expires_in_seconds if status else 0
            }
        }
    
    def _create_bot_in_db(self, user_id: str, bot_config: Dict) -> str:
        """Create bot record in database"""
        # Implementation depends on your database choice
        pass
    
    def _add_bot_to_billing(self, user_id: str, bot_id: str):
        """Add bot to user's billing list"""
        # Implementation depends on your billing system
        pass
    
    def _get_user_bots_from_db(self, user_id: str) -> List[Dict]:
        """Get user's bots from database"""
        # Implementation depends on your database choice
        pass
```

### Request Flow Summary

1. **Bot Request** → Envoy Proxy
2. **ext_auth** → Rate Limiter Service
   - Check subscription status (not expired)
   - Check quota limits (within subscription)
   - Check rate limits (requests per second)
   - Temporarily increment rate counter
   - Allow or deny request
3. **Request Processing** → Bot Backend API
4. **ext_proc** → Usage Tracking Service
   - If response is 200: Consume quota
   - If response is not 200: Refund rate limit
   - Add usage headers to response

This design ensures that:
- Expired subscriptions deny all requests immediately
- All bots for a user share the same rate limits and quota
- Failed requests don't consume subscription quota
- Rate limits are properly managed to prevent abuse
- Usage tracking is accurate and real-time

## Key Design Benefits

### 1. **Atomic Operations**
- All limit checks and updates happen in single Lua scripts
- Prevents race conditions in high-concurrency scenarios
- Ensures consistency between quota and rate limits

### 2. **Automatic Expiration**
- Plan metadata expires with subscription
- Rate limit keys auto-expire after 1 second
- No manual cleanup required

### 3. **Flexible Plan Support**
- Predefined plans (trial, pro_monthly, pro_annual)
- Custom plans with any duration/limits/rates
- Easy upgrades and renewals

### 4. **Performance Optimized**
- Fixed window rate limiting (simple, fast)
- Minimal Redis operations per request
- Efficient key structure

### 5. **Production Ready**
- Comprehensive error handling
- Detailed usage tracking
- API integration examples
- Monitoring support

This design provides a robust, scalable solution for SaaS subscription rate limiting with precise quota tracking and effective burst protection using Redis fixed window implementation. 