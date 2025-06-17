package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	extproc "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/go-redis/redis/v8"
)

// UsageTrackingService implements the Envoy ext_proc ExternalProcessor service
type UsageTrackingService struct {
	extproc.UnimplementedExternalProcessorServer
	redisClient *redis.Client
	luaScripts  *UsageLuaScripts
	paidStatusCodes map[int]bool
	// Store bot tokens per request to use in response processing
	requestTokens map[string]string // map[requestID]botToken
	// Store quota info per request to add to response headers  
	requestQuotas map[string]map[string]string // map[requestID]map[headerKey]headerValue
}

// NewUsageTrackingService creates a new usage tracking service
func NewUsageTrackingService(redisClient *redis.Client) *UsageTrackingService {
	luaScripts := NewUsageLuaScripts(redisClient)
	
	// Define paid status codes - these are status codes for which we don't reduce usage
	paidStatusCodes := map[int]bool{
		200: true, // OK
		201: true, // Created
		202: true, // Accepted
		204: true, // No Content
		206: true, // Partial Content
		304: true, // Not Modified (cached response)
	}
	
	return &UsageTrackingService{
		redisClient: redisClient,
		luaScripts:  luaScripts,
		paidStatusCodes: paidStatusCodes,
		requestTokens: make(map[string]string),
		requestQuotas: make(map[string]map[string]string),
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
		case *extproc.ProcessingRequest_RequestHeaders:
			resp = s.processRequestHeaders(v.RequestHeaders)
		case *extproc.ProcessingRequest_ResponseHeaders:
			resp = s.processResponseHeaders(v.ResponseHeaders)
		// Note: Other message types available in ext_proc v3 protocol:
		// - ProcessingRequest_RequestBody: for processing request body chunks  
		// - ProcessingRequest_ResponseBody: for processing response body chunks
		// - ProcessingRequest_RequestTrailers: for processing request trailers
		// - ProcessingRequest_ResponseTrailers: for processing response trailers
		// Currently handling request headers (for bot token) and response headers (for status)
		default:
			// For other request types, just continue without processing
			log.Printf("Usage Service: Processing other request type - Type: %T", v)
			resp = &extproc.ProcessingResponse{}
		}

		if err := stream.Send(resp); err != nil {
			return err
		}
	}
}

// processRequestHeaders processes the request headers to extract bot token
func (s *UsageTrackingService) processRequestHeaders(headers *extproc.HttpHeaders) *extproc.ProcessingResponse {
	// Extract bot token and request ID from headers
	botToken := ""
	requestID := ""
	quotaRemaining := ""
	quotaLimit := ""
	rateLimit := ""
	
	for _, header := range headers.Headers.Headers {
		switch header.Key {
		case "x-bot-token":
			botToken = header.Value
		case "x-request-id":
			requestID = header.Value
		case "x-quota-remaining":
			quotaRemaining = header.Value
		case "x-quota-limit":
			quotaLimit = header.Value
		case "x-rate-limit":
			rateLimit = header.Value
		}
	}

	// Store bot token for this request to use in response processing
	if botToken != "" && requestID != "" {
		s.requestTokens[requestID] = botToken
		
		// Store quota information to add to response headers
		s.requestQuotas[requestID] = map[string]string{
			"x-quota-remaining": quotaRemaining,
			"x-quota-limit":     quotaLimit,
			"x-rate-limit":      rateLimit,
		}
		
		botID := strings.Split(botToken, ":")[0]
		log.Printf("Usage Service: Stored bot token for request %s - Bot ID: %s (Quota: %s/%s, Rate: %s)", 
			requestID, botID, quotaRemaining, quotaLimit, rateLimit)
	} else {
		log.Printf("Usage Service: Missing bot token or request ID in request headers")
	}

	// Return empty response (no modifications needed)
	return &extproc.ProcessingResponse{
		Response: &extproc.ProcessingResponse_RequestHeaders{
			RequestHeaders: &extproc.HeadersResponse{},
		},
	}
}

// processResponseHeaders processes the response headers to track usage
func (s *UsageTrackingService) processResponseHeaders(headers *extproc.HttpHeaders) *extproc.ProcessingResponse {
	// Extract status code and request ID from headers
	log.Printf("Usage Service: Processing response headers - Headers: %v", headers.Headers.Headers)
	statusCode := 200
	requestID := ""
	
	for _, header := range headers.Headers.Headers {
		switch header.Key {
		case ":status":
			if code, err := strconv.Atoi(header.Value); err == nil {
				statusCode = code
			}
		case "x-request-id":
			requestID = header.Value
		}
	}

	// Get the bot token stored from request headers
	botToken := ""
	quotaHeaders := make(map[string]string)
	if requestID != "" {
		if token, exists := s.requestTokens[requestID]; exists {
			botToken = token
			// Clean up the stored token
			delete(s.requestTokens, requestID)
		}
		if quotas, exists := s.requestQuotas[requestID]; exists {
			quotaHeaders = quotas
			// Clean up the stored quotas
			delete(s.requestQuotas, requestID)
		}
	}

	// Log incoming response
	if botToken != "" {
		botID := strings.Split(botToken, ":")[0]
		log.Printf("Usage Service: Processing response - Bot ID: %s, Status: %d, Request ID: %s (Adding quota headers: %v)", 
			botID, statusCode, requestID, quotaHeaders)
	} else {
		log.Printf("Usage Service: Processing response with no bot token - Status: %d, Request ID: %s", statusCode, requestID)
	}

	// Process the response
	if botToken != "" {
		s.processResponse(botToken, statusCode)
	}

	// Return empty response (no modifications needed)
	return &extproc.ProcessingResponse{
		Response: &extproc.ProcessingResponse_ResponseHeaders{
			ResponseHeaders: &extproc.HeadersResponse{
				Response: &extproc.CommonResponse{
					HeaderMutation: &extproc.HeaderMutation{
						SetHeaders: []*corev3.HeaderValueOption{
							{
								Header: &corev3.HeaderValue{
									Key:   "x-quota-remaining",
									Value: quotaHeaders["x-quota-remaining"],
								},
							},
							{
								Header: &corev3.HeaderValue{
									Key:   "x-quota-limit",
									Value: quotaHeaders["x-quota-limit"],
								},
							},
							{
								Header: &corev3.HeaderValue{
									Key:   "x-rate-limit",
									Value: quotaHeaders["x-rate-limit"],
								},
							},
						},
					},
				},
			},
		},
	}
}

// processResponse handles the actual usage tracking logic
func (s *UsageTrackingService) processResponse(botToken string, statusCode int) {
	ctx := context.Background()
	requestCost := int64(1)
	
	// Extract bot ID from token
	botID := strings.Split(botToken, ":")[0]
	
	// Check if status code is in paid status list
	if s.paidStatusCodes[statusCode] {
		// Paid status - pass through without changing usage
		log.Printf("Usage Service: Paid status %d for bot %s - NO usage reduction (status in paid list: %v)", 
			statusCode, botID, s.getPaidStatusList())
		return
	}
	
	// Not a paid status - reduce current usage by 1
	log.Printf("Usage Service: Non-paid status %d for bot %s - REDUCING usage by %d", 
		statusCode, botID, requestCost)
	
	keys := s.generateUsageKeys(botID)
	
	result, err := s.luaScripts.ReduceUsage.Run(ctx, s.redisClient, []string{
		keys.UsageTotal,
	}, requestCost).Result()
	
	if err != nil {
		log.Printf("Usage Service: FAILED to reduce usage for bot %s: %v", botID, err)
		return
	}
	
	// Parse the result from Lua script
	if resultSlice, ok := result.([]interface{}); ok && len(resultSlice) >= 2 {
		success, _ := resultSlice[0].(int64)
		newUsage, _ := resultSlice[1].(int64)
		
		if success == 1 {
			log.Printf("Usage Service: SUCCESS - Reduced usage by %d for bot %s (status: %d) - New usage: %d", 
				requestCost, botID, statusCode, newUsage)
		} else {
			log.Printf("Usage Service: NO REDUCTION - Usage key not found for bot %s (status: %d)", 
				botID, statusCode)
		}
	} else {
		log.Printf("Usage Service: Reduced usage by %d for bot %s (status: %d) - Result: %v", 
			requestCost, botID, statusCode, result)
	}
}

// getPaidStatusList returns a list of paid status codes for logging
func (s *UsageTrackingService) getPaidStatusList() []int {
	var statusList []int
	for status := range s.paidStatusCodes {
		statusList = append(statusList, status)
	}
	return statusList
}

// UsageKeys represents the Redis keys for usage tracking
type UsageKeys struct {
	UsageTotal string // usage:{bot_id}
}

// generateUsageKeys generates Redis keys for usage tracking
func (s *UsageTrackingService) generateUsageKeys(botID string) *UsageKeys {
	return &UsageKeys{
		UsageTotal: fmt.Sprintf("usage:%s", botID),
	}
}


// UsageLuaScripts contains Lua scripts for usage tracking
type UsageLuaScripts struct {
	ReduceUsage  *redis.Script
}

// NewUsageLuaScripts initializes Lua scripts for usage tracking
func NewUsageLuaScripts(client *redis.Client) *UsageLuaScripts {
	return &UsageLuaScripts{
		ReduceUsage:  redis.NewScript(reduceUsageScript),
	}
}

// reduceUsageScript reduces current usage by the specified amount
// KEYS[1] = usage:{bot_id}
// ARGV[1] = reduction_amount (default 1)
// Returns: {success, new_usage}
const reduceUsageScript = `
local reduction = tonumber(ARGV[1]) or 1

-- Check if usage key exists
if redis.call('EXISTS', KEYS[1]) == 1 then
    -- Get current usage
    local current_usage = tonumber(redis.call('GET', KEYS[1])) or 0
    
    -- Reduce usage (but not below 0)
    local new_usage = math.max(0, current_usage - reduction)
    
    -- Update the usage counter
    redis.call('SET', KEYS[1], new_usage)
    
    return {1, new_usage}
end

return {0, 0}
` 