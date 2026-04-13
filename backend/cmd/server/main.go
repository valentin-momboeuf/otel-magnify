package main

import (
	"fmt"
	"os"

	"otel-magnify/internal/config"
)

func main() {
	cfg := config.Load()
	if cfg.JWTSecret == "" {
		fmt.Fprintln(os.Stderr, "JWT_SECRET is required")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "otel-magnify starting on %s\n", cfg.ListenAddr)
}
