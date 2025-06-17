package main

import (
	"github.com/go-redis/redis/v8"
)

// LuaScripts contains all the Lua scripts used for rate limiting
type LuaScripts struct {
	CheckRateLimit *redis.Script
	ConsumeUsage   *redis.Script
	RefundRate     *redis.Script
	SetRateLimit   *redis.Script
	GetStatus      *redis.Script
}

// NewLuaScripts initializes and registers all Lua scripts with Redis
func NewLuaScripts(client *redis.Client) *LuaScripts {
	return &LuaScripts{
		CheckRateLimit: redis.NewScript(checkRateLimitScript),
		ConsumeUsage:   redis.NewScript(consumeUsageScript),
		RefundRate:     redis.NewScript(refundRateScript),
		SetRateLimit:   redis.NewScript(setRateLimitScript),
		GetStatus:      redis.NewScript(getStatusScript),
	}
}

// checkRateLimitScript checks both quota and rate limits, temporarily increments rate counter
// Based on the simplified Redis patterns from your design
// KEYS[1] = rate_limit:{bot_id}
// KEYS[2] = counter:{bot_id}:{timestamp}
// KEYS[3] = usage_total:{bot_id}:{period}
// ARGV[1] = current_timestamp
// ARGV[2] = request_cost (default 1)
// Returns: {allowed, reason, quota_used, quota_limit, rate_used}
const checkRateLimitScript = `
local current_time = tonumber(ARGV[1])
local cost = tonumber(ARGV[2]) or 1

-- Get rate limit (default 50 for trial)
local rate_limit = tonumber(redis.call('GET', KEYS[1])) or 50

-- Check current usage in this second
local current_rate = tonumber(redis.call('GET', KEYS[2])) or 0
if current_rate + cost > rate_limit then
    local quota_used = tonumber(redis.call('GET', KEYS[3])) or 0
    return {0, "rate_exceeded", quota_used, 5000, current_rate}
end

-- Check quota (default 5000 for trial)
local quota_used = tonumber(redis.call('GET', KEYS[3])) or 0
local quota_limit = 5000  -- Default trial quota

-- You can extend this to get quota_limit from a separate key if needed
-- local quota_limit = tonumber(redis.call('GET', KEYS[1] .. ':quota_limit')) or 5000

if quota_used + cost > quota_limit then
    return {0, "quota_exceeded", quota_used, quota_limit, current_rate}
end

-- Temporarily increment rate counter (will be decremented if request fails)
redis.call('INCRBY', KEYS[2], cost)
redis.call('EXPIRE', KEYS[2], 1)

-- Return success with current state
return {1, "success", quota_used, quota_limit, current_rate + cost}
`

// consumeUsageScript consumes from quota on successful request (200 response)
// KEYS[1] = usage_total:{bot_id}:{period}
// ARGV[1] = request_cost (default 1)
// Returns: {success, new_quota}
const consumeUsageScript = `
local cost = tonumber(ARGV[1]) or 1

-- Increment usage counter
local new_quota = redis.call('INCRBY', KEYS[1], cost)

-- Set expiration to end of month (30 days for simplicity)
-- You can make this more sophisticated based on subscription end dates
redis.call('EXPIRE', KEYS[1], 30 * 24 * 3600)

return {1, new_quota}
`

// refundRateScript refunds rate limit on failed request (non-200 response)
// KEYS[1] = counter:{bot_id}:{timestamp}
// ARGV[1] = request_cost to refund (default 1)
// Returns: {success, new_rate}
const refundRateScript = `
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
    endif
    
    return {1, new_rate}
end

return {0, 0}
`

// setRateLimitScript sets or updates rate limit for a bot
// KEYS[1] = rate_limit:{bot_id}
// ARGV[1] = new_rate_limit
// Returns: {success, old_limit, new_limit}
const setRateLimitScript = `
local new_limit = tonumber(ARGV[1])

-- Get old limit
local old_limit = tonumber(redis.call('GET', KEYS[1])) or 50

-- Set new limit (permanent)
redis.call('SET', KEYS[1], new_limit)

return {1, old_limit, new_limit}
`

// getStatusScript gets current status without consuming limits
// KEYS[1] = rate_limit:{bot_id}
// KEYS[2] = counter:{bot_id}:{timestamp}
// KEYS[3] = usage_total:{bot_id}:{period}
// Returns: {success, rate_limit, rate_used, quota_used, quota_limit}
const getStatusScript = `
-- Get current limits and usage
local rate_limit = tonumber(redis.call('GET', KEYS[1])) or 50
local rate_used = tonumber(redis.call('GET', KEYS[2])) or 0
local quota_used = tonumber(redis.call('GET', KEYS[3])) or 0
local quota_limit = 5000  -- Default trial quota

return {1, rate_limit, rate_used, quota_used, quota_limit}
` 