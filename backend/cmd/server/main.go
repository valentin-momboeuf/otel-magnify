package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"otel-magnify/internal/alerts"
	"otel-magnify/internal/api"
	"otel-magnify/internal/auth"
	"otel-magnify/internal/config"
	"otel-magnify/internal/opamp"
	"otel-magnify/internal/store"
)

func main() {
	cfg := config.Load()
	if cfg.JWTSecret == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET environment variable is required")
		os.Exit(1)
	}

	// Database
	db, err := store.Open(cfg.DBDriver, cfg.DBDSN)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Database migrations applied")

	// WebSocket hub
	hub := api.NewHub()
	go hub.Run()

	// OpAMP server
	opampSrv := opamp.New(db, hub)
	opampHandler, connCtx, err := opampSrv.Attach()
	if err != nil {
		log.Fatalf("Failed to attach OpAMP server: %v", err)
	}

	// Start OpAMP HTTP server on separate port
	opampMux := http.NewServeMux()
	opampMux.HandleFunc("/v1/opamp", opampHandler)
	opampHTTP := &http.Server{
		Addr:        cfg.OpAMPAddr,
		Handler:     opampMux,
		ConnContext: connCtx,
	}
	go func() {
		log.Printf("OpAMP server listening on %s", cfg.OpAMPAddr)
		if err := opampHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("OpAMP server: %v", err)
		}
	}()

	// Alert engine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	alertEngine := alerts.New(db, hub, 5*time.Minute)
	go alertEngine.Start(ctx, 30*time.Second)
	log.Println("Alert engine started (30s interval)")

	// REST API
	a := auth.New(cfg.JWTSecret)
	router := api.NewRouter(db, a, hub, opampSrv)

	apiHTTP := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: router,
	}

	go func() {
		log.Printf("API server listening on %s", cfg.ListenAddr)
		if err := apiHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API server: %v", err)
		}
	}()

	// Graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Shutting down...")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	apiHTTP.Shutdown(shutdownCtx)
	opampHTTP.Shutdown(shutdownCtx)
	opampSrv.Stop(shutdownCtx)
	hub.Stop()
	log.Println("Shutdown complete")
}
