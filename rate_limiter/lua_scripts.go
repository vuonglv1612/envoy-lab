package main

import (
	"github.com/go-redis/redis/v8"
)

// LuaScripts contains all the Lua scripts used for rate limiting
type LuaScripts struct {
	CheckRateLimit *redis.Script
}

// NewLuaScripts initializes and registers all Lua scripts with Redis
func NewLuaScripts(client *redis.Client) *LuaScripts {
	return &LuaScripts{
		CheckRateLimit: redis.NewScript(checkRateLimitScript),
	}
}

// checkRateLimitScript checks both quota and rate limits, temporarily increments counters
// Based on the simplified Redis patterns from your design
// KEYS[1] = rate_limit:{bot_id}
// KEYS[2] = counter:{bot_id}:{timestamp}
// KEYS[3] = usage:{bot_id}
// KEYS[4] = quota:{bot_id}
// ARGV[1] = current_timestamp
// ARGV[2] = request_cost (default 1)
// Returns: {allowed, reason, quota_used, quota_limit, rate_used}
const checkRateLimitScript = `
local current_time = tonumber(ARGV[1])
local cost = tonumber(ARGV[2]) or 1

-- Get rate limit from Redis, create with 0 if not found
local rate_limit = tonumber(redis.call('GET', KEYS[1]))
if not rate_limit then
	return {0, "rate_exceeded", 0, 0, 0}
end

-- Get quota limit from Redis, create with 0 if not found
local quota_limit = tonumber(redis.call('GET', KEYS[4]))
if not quota_limit then
    return {0, "quota_exceeded", 0, 0, 0}
end
local quota_used = tonumber(redis.call('GET', KEYS[3])) or 0

-- Check current rate usage before incrementing
local current_rate = tonumber(redis.call('GET', KEYS[2])) or 0
if current_rate + cost > rate_limit then
    return {0, "rate_exceeded", quota_used, quota_limit, current_rate}
end
redis.call('INCRBY', KEYS[2], cost)
redis.call('EXPIRE', KEYS[2], 1)
current_rate = current_rate + cost

-- Check current quota usage before incrementing
if quota_used + cost > quota_limit then
    return {0, "quota_exceeded", quota_used, quota_limit, current_rate}
end
-- Increment usage_total (will be decremented if request fails)
quota_used = redis.call('INCRBY', KEYS[3], cost)

-- Return success with current state
return {1, "success", quota_used, quota_limit, current_rate}
`
