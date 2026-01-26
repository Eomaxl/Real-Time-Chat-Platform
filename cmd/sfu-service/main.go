package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"real-time-chat-system/internal/config"
	"real-time-chat-system/internal/discovery"
	"real-time-chat-system/internal/health"
	"real-time-chat-system/internal/sfu"
	"syscall"
	"time"

	"github.com/pion/webrtc/v3"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize service discovery
	serviceDiscovery, err := discovery.New(&cfg.ServiceDiscovery)
	if err != nil {
		log.Fatalf("Failed to initialize service discovery: %v", err)
	}

	// Initialize health checker
	healthChecker := health.NewChecker()
	healthChecker.SetVersion("1.0.0")

	// Configure SFU
	sfuConfig := &sfu.SFUConfig{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
		Bandwidth: sfu.BandwidthConfig{
			MaxBitrate:     2_000_000, // 2 Mbps
			MinBitrate:     100_000,   // 100 Kbps
			StartBitrate:   500_000,   // 500 Kbps
			EnableAdaptive: true,
		},
		Quality: sfu.QualityConfig{
			Level:           sfu.QualityMedium,
			MaxWidth:        1280,
			MaxHeight:       720,
			MaxFrameRate:    30,
			EnableSimulcast: false,
		},
		MaxPublishers:  50,
		MaxSubscribers: 500,
		SessionTimeout: 5 * time.Minute,
	}

	// Initialize SFU Service
	sfuService, err := sfu.New(sfuConfig, healthChecker)
	if err != nil {
		log.Fatalf("Failed to initialize SFU service: %v", err)
	}

	// Start background tasks
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sfuService.Start(ctx)

	// Register service
	port := cfg.SFU.Port
	if port == "" {
		port = ":8084"
	}

	if err := serviceDiscovery.Register("sfu-service", port); err != nil {
		log.Fatalf("Failed to register service: %v", err)
	}

	// Start server
	server := &http.Server{
		Addr:    port,
		Handler: sfuService.Router(),
	}

	// Graceful shutdown
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	log.Printf("SFU Service started on %s", port)

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	// Deregister service
	serviceDiscovery.Deregister("sfu-service")

	log.Println("Server exited")
}
