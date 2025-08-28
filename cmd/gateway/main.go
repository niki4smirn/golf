package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/niki4smirn/golf/internal/database"
	"github.com/niki4smirn/golf/internal/gateway"
)

func main() {
	// Command line flags
	var (
		port          = flag.String("port", "8080", "Port to run the server on")
		dbPath        = flag.String("db", "audit.db", "Path to SQLite database file")
		targetURL     = flag.String("target", "", "Target URL for JSON-RPC forwarding (required)")
		tinybirdToken = flag.String("tinybird-token", "", "Tinybird authentication token (optional)")
	)
	flag.Parse()

	// Initialize SQLite database (primary storage)
	db, err := database.New(*dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize SQLite database: %v", err)
	}
	defer db.Close()

	// Initialize Tinybird if token provided
	var tinybirdDB *database.TinybirdDatabase
	if *tinybirdToken != "" {
		log.Printf("Initializing Tinybird integration")
		tinybirdDB = database.NewTinybirdDatabase(*tinybirdToken)
	}

	// Create gateway
	gw := gateway.New(db, *targetURL)

	// Add Tinybird logging to gateway if available
	if tinybirdDB != nil {
		gw.SetTinybirdLogger(tinybirdDB)
	}

	// Set up router
	router := gw.SetupRoutes()

	// Configure server
	server := &http.Server{
		Addr:         ":" + *port,
		Handler:      loggingMiddleware(router),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Validate target URL is provided
	if *targetURL == "" {
		log.Fatal("Target URL is required. Use -target flag to specify the JSON-RPC server URL.")
	}

	// Start server in goroutine
	go func() {
		log.Printf("Starting JSON-RPC Gateway on port %s", *port)
		log.Printf("Database: %s", *dbPath)
		log.Printf("Forwarding to: %s", *targetURL)
		log.Printf("Endpoints:")
		log.Printf("  POST /rpc           - JSON-RPC proxy")
		log.Printf("  GET  /audit/logs    - View audit logs")
		log.Printf("  GET  /audit/stats   - View statistics")
		log.Printf("  GET  /health        - Health check")
		log.Printf("  GET  /              - Dashboard")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	if err := server.Close(); err != nil {
		log.Printf("Error shutting down server: %v", err)
	}
	log.Println("Server stopped")
}

// loggingMiddleware logs HTTP requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.RequestURI, time.Since(start))
	})
}
