package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"otel-magnify/internal/alerts"
	"otel-magnify/internal/api"
	"otel-magnify/internal/auth"
	"otel-magnify/internal/config"
	"otel-magnify/internal/opamp"
	"otel-magnify/internal/store"
	"otel-magnify/pkg/models"
)

//go:embed dist
var frontendDist embed.FS

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

	// Seed admin user if env vars are set (idempotent)
	seedAdmin(db)

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

	// Embedded frontend assets
	var staticFS fs.FS
	sub, err := fs.Sub(frontendDist, "dist")
	if err == nil {
		staticFS = sub
	}

	// REST API
	a := auth.New(cfg.JWTSecret)
	router := api.NewRouter(db, a, hub, opampSrv, cfg.CORSOrigins, staticFS)

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

// seedAdmin creates an admin user on startup if SEED_ADMIN_EMAIL and
// SEED_ADMIN_PASSWORD are set. Skips silently if the email already exists.
func seedAdmin(db *store.DB) {
	email := os.Getenv("SEED_ADMIN_EMAIL")
	password := os.Getenv("SEED_ADMIN_PASSWORD")
	if email == "" || password == "" {
		return
	}

	// Idempotent: skip if already exists
	if _, err := db.GetUserByEmail(email); err == nil {
		log.Printf("Seed admin: user %s already exists, skipping", email)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		log.Printf("Seed admin: failed to hash password: %v", err)
		return
	}

	user := models.User{
		ID:           "admin-seed-001",
		Email:        email,
		PasswordHash: string(hash),
		Role:         "admin",
	}
	if err := db.CreateUser(user); err != nil {
		log.Printf("Seed admin: failed to create user: %v", err)
		return
	}
	log.Printf("Seed admin: created user %s", email)
}
