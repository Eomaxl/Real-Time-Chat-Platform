package chatservice

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"real-time-chat-system/internal/chat"
	"real-time-chat-system/internal/config"
	"real-time-chat-system/internal/database"
	"real-time-chat-system/internal/discovery"
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
	db, err := database.NewPostgreDB(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

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

	// Initialize Chat Service
	chatService, err := chat.New(&cfg.Chat, healthChecker, db, redisClient)
	if err != nil {
		log.Fatalf("Failed to initialize chat service: %v", err)
	}

	// Register service
	if err := serviceDiscovery.Register("chat-service", cfg.Chat.Port); err != nil {
		log.Fatalf("Failed to register service: %v", err)
	}

	// Start server
	server := &http.Server{
		Addr:    cfg.Chat.Port,
		Handler: chatService.Router(),
	}

	// Graceful shutdown
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	log.Printf("Chat Service started on %s", cfg.Chat.Port)

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	// Deregister service
	serviceDiscovery.Deregister("chat-service")

	log.Println("Server exited")
}
