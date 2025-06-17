package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	extproc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	"github.com/go-redis/redis/v8"
)

// UsageTrackingService implements the Envoy ext_proc ExternalProcessor service
type UsageTrackingService struct {
	extproc.UnimplementedExternalProcessorServer
	redisClient *redis.Client
	luaScripts  *UsageLuaScripts
}

// NewUsageTrackingService creates a new usage tracking service
func NewUsageTrackingService(redisClient *redis.Client) *UsageTrackingService {
	luaScripts := NewUsageLuaScripts(redisClient)
	
	return &UsageTrackingService{
		redisClient: redisClient,
		luaScripts:  luaScripts,
	}
}

// Process implements the ext_proc ExternalProcessor service Process method
func (s *UsageTrackingService) Process(stream extproc.ExternalProcessor_ProcessServer) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			return err
		}

		var resp *extproc.ProcessingResponse

		switch v := req.Request.(type) {
		case *extproc.ProcessingRequest_ResponseHeaders:
			resp = s.processResponseHeaders(v.ResponseHeaders)
		default:
			// For other request types, just continue without processing
			resp = &extproc.ProcessingResponse{}
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

// processResponseHeaders processes the response headers to track usage
func (s *UsageTrackingService) processResponseHeaders(headers *extproc.HttpHeaders) *extproc.ProcessingResponse {
	// Extract bot token and status code from headers
	botToken := ""
	statusCode := 200
	
	for _, header := range headers.Headers.Headers {
		switch header.Key {
		case "x-bot-token":
			botToken = header.Value
		case ":status":
			if code, err := strconv.Atoi(header.Value); err == nil {
				statusCode = code
			}
		}
	}

	// Process the response
	if botToken != "" {
		s.processResponse(botToken, statusCode)
	}

	// Return empty response (no modifications needed)
	return &extproc.ProcessingResponse{
		Response: &extproc.ProcessingResponse_ResponseHeaders{
			ResponseHeaders: &extproc.HeadersResponse{},
		},
	}
}

// processResponse handles the actual usage tracking logic
func (s *UsageTrackingService) processResponse(botToken string, statusCode int) {
	ctx := context.Background()
	currentTime := time.Now().Unix()
	requestCost := int64(1)
	
	// Extract bot ID from token
	botID := strings.Split(botToken, ":")[0]
	
	if statusCode == 200 {
		// Successful request - consume quota
		keys := s.generateUsageKeys(botID, currentTime)
		
		_, err := s.luaScripts.ConsumeUsage.Run(ctx, s.redisClient, []string{
			keys.UsageTotal,
		}, requestCost).Result()
		
		if err != nil {
			log.Printf("Failed to consume usage for bot %s: %v", botID, err)
		} else {
			log.Printf("Consumed usage for bot %s (status: %d)", botID, statusCode)
		}
	} else {
		// Failed request - refund rate limit
		keys := s.generateRateKeys(botID, currentTime)
		
		_, err := s.luaScripts.RefundRate.Run(ctx, s.redisClient, []string{
			keys.Counter,
		}, requestCost).Result()
		
		if err != nil {
			log.Printf("Failed to refund rate limit for bot %s: %v", botID, err)
		} else {
			log.Printf("Refunded rate limit for bot %s (status: %d)", botID, statusCode)
		}
	}
}

// UsageKeys represents the Redis keys for usage tracking
type UsageKeys struct {
	UsageTotal string // usage_total:{bot_id}:{period}
}

// RateKeys represents the Redis keys for rate limiting
type RateKeys struct {
	Counter string // counter:{bot_id}:{timestamp}
}

// generateUsageKeys generates Redis keys for usage tracking
func (s *UsageTrackingService) generateUsageKeys(botID string, currentTime int64) *UsageKeys {
	return &UsageKeys{
		UsageTotal: fmt.Sprintf("usage_total:%s:%d", botID, s.getPeriodStart(currentTime)),
	}
}

// generateRateKeys generates Redis keys for rate limiting
func (s *UsageTrackingService) generateRateKeys(botID string, currentTime int64) *RateKeys {
	return &RateKeys{
		Counter: fmt.Sprintf("counter:%s:%d", botID, currentTime),
	}
}

// getPeriodStart calculates the billing period start timestamp
func (s *UsageTrackingService) getPeriodStart(currentTime int64) int64 {
	// For simplicity, use monthly periods starting from the beginning of the month
	t := time.Unix(currentTime, 0).UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC).Unix()
}

// UsageLuaScripts contains Lua scripts for usage tracking
type UsageLuaScripts struct {
	ConsumeUsage *redis.Script
	RefundRate   *redis.Script
}

// NewUsageLuaScripts initializes Lua scripts for usage tracking
func NewUsageLuaScripts(client *redis.Client) *UsageLuaScripts {
	return &UsageLuaScripts{
		ConsumeUsage: redis.NewScript(consumeUsageScript),
		RefundRate:   redis.NewScript(refundRateScript),
	}
}

// consumeUsageScript consumes from quota on successful request (200 response)
// KEYS[1] = usage_total:{bot_id}:{period}
// ARGV[1] = request_cost (default 1)
// Returns: {success, new_quota}
const consumeUsageScript = `
local cost = tonumber(ARGV[1]) or 1

-- Increment usage counter
local new_quota = redis.call('INCRBY', KEYS[1], cost)

-- Set expiration to end of month (30 days for simplicity)
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
    end
    
    return {1, new_rate}
end

return {0, 0}
` 