package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"github.com/redis/go-redis/v9"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	// Initialize Redis client (supports both standalone and cluster)
	var redisClient redis.UniversalClient
	
	redisMode := getEnv("REDIS_MODE", "standalone") // "standalone" or "cluster"
	
	if redisMode == "cluster" {
		// Initialize Redis cluster client
		addrs := strings.Split(getEnv("REDIS_CLUSTER_ADDRS", "localhost:7000,localhost:7001,localhost:7002"), ",")
		redisClient = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    addrs,
			Password: getEnv("REDIS_PASSWORD", ""),
		})
		log.Printf("Initializing Redis cluster client with addresses: %v", addrs)
	} else {
		// Initialize Redis standalone client
		redisClient = redis.NewClient(&redis.Options{
			Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       0,
		})
		log.Printf("Initializing Redis standalone client with address: %s", getEnv("REDIS_ADDR", "localhost:6379"))
	}

	// Test Redis connection
	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("Connected to Redis successfully")

	// Initialize supplier lookup service
	supplierLookupService := NewSupplierLookupService(redisClient)

	// Initialize health check service
	healthServer := CreateHealthServer(redisClient)

	// Setup gRPC server
	lis, err := net.Listen("tcp", getEnv("GRPC_PORT", ":9002"))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	authv3.RegisterAuthorizationServer(grpcServer, supplierLookupService)
	healthpb.RegisterHealthServer(grpcServer, healthServer)

	// Enable reflection for debugging
	reflection.Register(grpcServer)

	log.Printf("Supplier Lookup gRPC server starting on %s", lis.Addr().String())

	// Handle graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down gRPC server...")
		grpcServer.GracefulStop()
		redisClient.Close()
	}()

	// Start server
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
} 