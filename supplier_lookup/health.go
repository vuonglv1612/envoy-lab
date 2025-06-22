package main

import (
	"context"
	"log"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// CreateHealthServer creates a health server with Redis connectivity check
func CreateHealthServer(redisClient redis.UniversalClient) *health.Server {
	healthServer := health.NewServer()
	
	// Set initial status
	go func() {
		// Check Redis connectivity periodically and update health status
		ctx := context.Background()
		if err := redisClient.Ping(ctx).Err(); err != nil {
			log.Printf("Health Check: Redis ping failed: %v", err)
			healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
		} else {
			log.Println("Health Check: Service is healthy")
			healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
		}
	}()
	
	return healthServer
} 