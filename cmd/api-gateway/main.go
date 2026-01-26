package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"real-time-chat-system/internal/config"
	"real-time-chat-system/internal/database"
	"real-time-chat-system/internal/discovery"
	"real-time-chat-system/internal/gateway"
	"real-time-chat-system/internal/health"
	redisclient "real-time-chat-system/internal/redis"
	"syscall"
	"time"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize database
	db, err := database.NewPostgresDB(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Initialize database schema
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.InitSchema(ctx); err != nil {
		log.Fatalf("Failed to initialize database schema: %v", err)
	}

	// Initialize Redis
	redisClient, err := redisclient.NewClient(&cfg.Redis)
	if err != nil {
		log.Fatalf("Failed to initialize Redis: %v", err)
	}
	defer redisClient.Close()

	// Initialize service discovery
	serviceDiscovery, err := discovery.New(&cfg.ServiceDiscovery)
	if err != nil {
		log.Fatalf("Failed to initialize service discovery: %v", err)
	}

	// Initialize health checker
	healthChecker := health.NewChecker()
	healthChecker.SetVersion("1.0.0")

	// Initialize API Gateway
	gateway, err := gateway.New(&cfg.Gateway, serviceDiscovery, healthChecker, db, redisClient)
	if err != nil {
		log.Fatalf("Failed to initialize API gateway: %v", err)
	}

	// Start server
	server := &http.Server{
		Addr:         cfg.Gateway.Port,
		Handler:      gateway.Router(),
		ReadTimeout:  cfg.Gateway.GetReadTimeout(),
		WriteTimeout: cfg.Gateway.GetWriteTimeout(),
	}

	// Graceful shutdown
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	log.Printf("API Gateway started on %s", cfg.Gateway.Port)

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop gateway components
	gateway.Stop()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
