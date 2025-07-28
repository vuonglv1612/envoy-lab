package main

import (
	"context"
	"fmt"
	"log"

	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	typev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	rpc_status "google.golang.org/genproto/googleapis/rpc/status"
)

// SupplierLookupService implements the Envoy ext_authz Authorization service
type SupplierLookupService struct {
	authv3.UnimplementedAuthorizationServer
	redisClient redis.UniversalClient
}

// LookupResult represents the result of a supplier lookup
type LookupResult struct {
	Found          bool
	ConnectionCode string
	Supplier       string
	ErrorMessage   string
}

// NewSupplierLookupService creates a new supplier lookup service
func NewSupplierLookupService(redisClient redis.UniversalClient) *SupplierLookupService {
	return &SupplierLookupService{
		redisClient: redisClient,
	}
}

// Check implements the ext_authz Authorization service Check method
func (s *SupplierLookupService) Check(ctx context.Context, req *authv3.CheckRequest) (*authv3.CheckResponse, error) {
	// Extract request attributes
	httpAttrs := req.GetAttributes().GetRequest().GetHttp()
	if httpAttrs == nil {
		log.Println("Supplier Lookup: No HTTP attributes found in request")
		return s.createDeniedResponse(400, "missing_http_attributes", "No HTTP attributes found"), nil
	}

	// Extract X-Connection-Code from headers
	headers := httpAttrs.GetHeaders()
	connectionCode := ""
	
	if headers != nil {
		if code, exists := headers["x-connection-code"]; exists {
			connectionCode = code
		}
	}

	if connectionCode == "" {
		log.Println("Supplier Lookup: X-Connection-Code header not found or empty")
		return s.createDeniedResponse(400, "missing_connection_code", "X-Connection-Code header is required"), nil
	}

	// Log incoming request
	log.Printf("Supplier Lookup: Incoming request with Connection Code: %s", connectionCode)

	// Perform supplier lookup
	result, err := s.lookupSupplier(ctx, connectionCode)
	if err != nil {
		log.Printf("Supplier Lookup: Error during lookup for connection code %s: %v", connectionCode, err)
		return s.createDeniedResponse(500, "lookup_error", "Internal supplier lookup error"), nil
	}

	// Log the result
	if result.Found {
		log.Printf("Supplier Lookup: Connection code %s belongs to supplier: %s", connectionCode, result.Supplier)
		return s.createAllowedResponse(result), nil
	} else {
		log.Printf("Supplier Lookup: Connection code %s not found - %s", connectionCode, result.ErrorMessage)
		return s.createDeniedResponse(400, "invalid_connection_code", result.ErrorMessage), nil
	}
}

// lookupSupplier performs the actual supplier lookup using Redis
func (s *SupplierLookupService) lookupSupplier(ctx context.Context, connectionCode string) (*LookupResult, error) {
	// Generate Redis key: supplier_mapp:<connection_code>
	redisKey := fmt.Sprintf("supplier_mapp:%s", connectionCode)
	
	// Lookup supplier in Redis
	supplier, err := s.redisClient.Get(ctx, redisKey).Result()
	if err != nil {
		if err == redis.Nil {
			// Key not found
			supplier_name := "vuonglv"
			return &LookupResult{
				Found:          true,
				ConnectionCode: connectionCode,
				Supplier:       supplier_name,
				ErrorMessage:   "Invalid Connection code not belong to any supplier",
			}, nil
		}
		// Redis error
		return nil, fmt.Errorf("redis lookup failed: %w", err)
	}
	
	// Found supplier
	return &LookupResult{
		Found:          true,
		ConnectionCode: connectionCode,
		Supplier:       supplier,
		ErrorMessage:   "",
	}, nil
}

// createAllowedResponse creates a successful authorization response with supplier info
func (s *SupplierLookupService) createAllowedResponse(result *LookupResult) *authv3.CheckResponse {
	headers := []*corev3.HeaderValueOption{
		{
			Header: &corev3.HeaderValue{
				Key:   "x-connection-code",
				Value: result.ConnectionCode,
			},
		},
		{
			Header: &corev3.HeaderValue{
				Key:   "x-supplier",
				Value: result.Supplier,
			},
		},
	}

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
func (s *SupplierLookupService) createDeniedResponse(httpStatus int32, reason string, message string) *authv3.CheckResponse {
	return &authv3.CheckResponse{
		Status: &rpc_status.Status{
			Code: int32(codes.PermissionDenied),
		},
		HttpResponse: &authv3.CheckResponse_DeniedResponse{
			DeniedResponse: &authv3.DeniedHttpResponse{
				Status: &typev3.HttpStatus{
					Code: typev3.StatusCode(httpStatus),
				},
				Headers: []*corev3.HeaderValueOption{
					{
						Header: &corev3.HeaderValue{
							Key:   "content-type",
							Value: "application/json",
						},
					},
				},
				Body: fmt.Sprintf(`{"error": {"code": 400, "message": "%s"}}`, message),
			},
		},
	}
} 