package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/go-redis/redis/v8"
	"google.golang.org/grpc/codes"
	rpc_status "google.golang.org/genproto/googleapis/rpc/status"
)

// RateLimiterService implements the Envoy ext_authz Authorization service
type RateLimiterService struct {
	authv3.UnimplementedAuthorizationServer
	redisClient *redis.Client
	luaScripts  *LuaScripts
}

// CheckResult represents the result of a rate limit check
type CheckResult struct {
	Allowed        bool
	Reason         string
	QuotaUsed      int64
	QuotaLimit     int64
	RateUsed       int64
	RateLimit      int64
	RetryAfter     int64
	BotToken       string
}

// Bot token regex pattern to extract token from URL path
var botTokenRegex = regexp.MustCompile(`^/bot([0-9]+:[A-Za-z0-9_-]+)/`)

// NewRateLimiterService creates a new rate limiter service
func NewRateLimiterService(redisClient *redis.Client) *RateLimiterService {
	luaScripts := NewLuaScripts(redisClient)
	
	service := &RateLimiterService{
		redisClient: redisClient,
		luaScripts:  luaScripts,
	}
	
	return service
}

// Check implements the ext_authz Authorization service Check method
func (s *RateLimiterService) Check(ctx context.Context, req *authv3.CheckRequest) (*authv3.CheckResponse, error) {
	// Extract request attributes
	httpAttrs := req.GetAttributes().GetRequest().GetHttp()
	if httpAttrs == nil {
		log.Println("No HTTP attributes found in request")
		return s.createDeniedResponse(401, "missing_http_attributes", "No HTTP attributes found"), nil
	}

	// Extract bot token from URL path
	path := httpAttrs.GetPath()
	botToken := s.extractBotToken(path)
	
	if botToken == "" {
		log.Printf("Invalid bot token in path: %s", path)
		return s.createDeniedResponse(401, "invalid_bot_token", "Invalid or missing bot token in URL"), nil
	}

	// Check rate limits
	result, err := s.checkRateLimit(ctx, botToken)
	if err != nil {
		log.Printf("Rate limit check error for bot %s: %v", botToken, err)
		return s.createDeniedResponse(500, "rate_limit_error", "Internal rate limiting error"), nil
	}

	if result.Allowed {
		return s.createAllowedResponse(result), nil
	} else {
		return s.createDeniedResponse(429, result.Reason, s.getErrorMessage(result)), nil
	}
}

// extractBotToken extracts bot token from URL path
func (s *RateLimiterService) extractBotToken(path string) string {
	matches := botTokenRegex.FindStringSubmatch(path)
	if len(matches) >= 2 {
		return matches[1]
	}
	return ""
}

// checkRateLimit performs the actual rate limit check using Redis Lua scripts
func (s *RateLimiterService) checkRateLimit(ctx context.Context, botToken string) (*CheckResult, error) {
	currentTime := time.Now().Unix()
	requestCost := int64(1)
	
	// Generate Redis keys
	keys := s.generateKeys(botToken, currentTime)
	
	// Execute rate limit check Lua script
	result, err := s.luaScripts.CheckRateLimit.Run(ctx, s.redisClient, []string{
		keys.RateLimit,
		keys.Counter,
		keys.UsageTotal,
		keys.QuotaLimit,
	}, currentTime, requestCost).Result()
	
	if err != nil {
		return nil, fmt.Errorf("lua script execution failed: %w", err)
	}
	
	// Parse Lua script result
	return s.parseLuaResult(result, botToken)
}

// RedisKeys represents the Redis keys used for rate limiting
type RedisKeys struct {
	RateLimit  string // rate_limit:{bot_token}
	Counter    string // counter:{bot_token}:{timestamp}
	UsageTotal string // usage:{bot_token}
	QuotaLimit string // quota:{bot_token}
}

// generateKeys generates Redis keys for a bot token
func (s *RateLimiterService) generateKeys(botToken string, currentTime int64) *RedisKeys {
	// Extract bot ID from token for user identification
	botID := strings.Split(botToken, ":")[0]
	
	return &RedisKeys{
		RateLimit:  fmt.Sprintf("rate_limit:%s", botID),
		Counter:    fmt.Sprintf("counter:%s:%d", botID, currentTime),
		UsageTotal: fmt.Sprintf("usage:%s", botID),
		QuotaLimit: fmt.Sprintf("quota:%s", botID),
	}
}

// parseLuaResult parses the result from Lua script execution
func (s *RateLimiterService) parseLuaResult(result interface{}, botToken string) (*CheckResult, error) {
	resultSlice, ok := result.([]interface{})
	if !ok || len(resultSlice) < 5 {
		return nil, fmt.Errorf("unexpected lua result format: %v", result)
	}
	
	allowed, _ := resultSlice[0].(int64)
	reason, _ := resultSlice[1].(string)
	quotaUsed, _ := resultSlice[2].(int64)
	quotaLimit, _ := resultSlice[3].(int64)
	rateUsed, _ := resultSlice[4].(int64)
	
	// Default values
	rateLimit := int64(50) // Default rate limit
	retryAfter := int64(1) // 1 second for rate limit, 3600 for quota
	
	if reason == "quota_exceeded" {
		retryAfter = 3600
	}
	
	return &CheckResult{
		Allowed:     allowed == 1,
		Reason:      reason,
		QuotaUsed:   quotaUsed,
		QuotaLimit:  quotaLimit,
		RateUsed:    rateUsed,
		RateLimit:   rateLimit,
		RetryAfter:  retryAfter,
		BotToken:    botToken,
	}, nil
}

// createAllowedResponse creates a successful authorization response
func (s *RateLimiterService) createAllowedResponse(result *CheckResult) *authv3.CheckResponse {
	headers := []*corev3.HeaderValueOption{
		{
			Header: &corev3.HeaderValue{
				Key:   "x-bot-token",
				Value: result.BotToken,
			},
		},
		{
			Header: &corev3.HeaderValue{
				Key:   "x-quota-remaining",
				Value: strconv.FormatInt(result.QuotaLimit-result.QuotaUsed, 10),
			},
		},
		{
			Header: &corev3.HeaderValue{
				Key:   "x-quota-limit",
				Value: strconv.FormatInt(result.QuotaLimit, 10),
			},
		},
		{
			Header: &corev3.HeaderValue{
				Key:   "x-rate-limit",
				Value: strconv.FormatInt(result.RateLimit, 10),
			},
		},
	}
	// add json response content type
	headers = append(headers, &corev3.HeaderValueOption{
		Header: &corev3.HeaderValue{
			Key:   "content-type",
			Value: "application/json",
		},
	})
	return &authv3.CheckResponse{
		Status: &rpc_status.Status{
			Code: int32(codes.OK),
		},
		HttpResponse: &authv3.CheckResponse_OkResponse{
			OkResponse: &authv3.OkHttpResponse{
				Headers: headers,
			},
		},
	}
}

// createDeniedResponse creates a denied authorization response
func (s *RateLimiterService) createDeniedResponse(httpStatus int32, reason string, message string) *authv3.CheckResponse {
	headers := []*corev3.HeaderValueOption{
		{
			Header: &corev3.HeaderValue{
				Key:   "x-rate-limit-reason",
				Value: reason,
			},
		},
	}
	
	// Add retry-after header for rate limit responses
	if httpStatus == 429 {
		retryAfter := "1"
		if reason == "quota_exceeded" {
			retryAfter = "3600"
		}
		headers = append(headers, &corev3.HeaderValueOption{
			Header: &corev3.HeaderValue{
				Key:   "retry-after",
				Value: retryAfter,
			},
		})
	}
	// add json response content type
	headers = append(headers, &corev3.HeaderValueOption{
		Header: &corev3.HeaderValue{
			Key:   "content-type",
			Value: "application/json",
		},
	})
	
	return &authv3.CheckResponse{
		Status: &rpc_status.Status{
			Code:    int32(codes.PermissionDenied),
			Message: message,
		},
		HttpResponse: &authv3.CheckResponse_DeniedResponse{
			DeniedResponse: &authv3.DeniedHttpResponse{
				Status: &typev3.HttpStatus{
					Code: typev3.StatusCode(httpStatus),
				},
				Headers: headers,
				Body:    fmt.Sprintf(`{"error": "%s", "message": "%s"}`, reason, message),
			},
		},
	}
}

// getErrorMessage returns a user-friendly error message based on the check result
func (s *RateLimiterService) getErrorMessage(result *CheckResult) string {
	switch result.Reason {
	case "plan_not_found":
		return "No active subscription found for this bot"
	case "subscription_expired":
		return "Bot subscription has expired"
	case "quota_exceeded":
		return fmt.Sprintf("Bot quota exceeded (%d/%d requests used)", result.QuotaUsed, result.QuotaLimit)
	case "rate_exceeded":
		return "Rate limit exceeded - too many requests per second"
	default:
		return "Rate limit exceeded"
	}
} 