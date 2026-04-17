package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/bcrypt"

	"otel-magnify/internal/alerts"
	"otel-magnify/internal/auth"
	"otel-magnify/internal/config"
	"otel-magnify/internal/store"
	"otel-magnify/pkg/ext"
	"otel-magnify/pkg/models"
	"otel-magnify/pkg/server"
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

	seedAdmin(db)

	// Auth
	a := auth.New(cfg.JWTSecret)

	// Server options
	var opts []server.Option

	if wh := alerts.NewWebhookNotifier(cfg.WebhookURL); wh != nil {
		opts = append(opts, server.WithNotifier(wh))
	}

	if sub, err := fs.Sub(frontendDist, "dist"); err == nil {
		opts = append(opts, server.WithStaticFS(sub))
	}

	srv := server.New(server.Config{
		ListenAddr:      cfg.ListenAddr,
		OpAMPAddr:       cfg.OpAMPAddr,
		CORSOrigins:     cfg.CORSOrigins,
		MinAgentVersion: cfg.MinAgentVersion,
	}, db, a, opts...)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		cancel()
	}()

	if err := srv.Run(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// seedAdmin creates an admin user on startup if SEED_ADMIN_EMAIL and
// SEED_ADMIN_PASSWORD are set. Skips silently if the email already exists.
func seedAdmin(db ext.Store) {
	email := os.Getenv("SEED_ADMIN_EMAIL")
	password := os.Getenv("SEED_ADMIN_PASSWORD")
	if email == "" || password == "" {
		return
	}

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
