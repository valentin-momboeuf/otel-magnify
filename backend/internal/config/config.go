package config

import "os"

type Config struct {
	DBDriver        string // "sqlite" or "pgx"
	DBDSN           string // file path for sqlite, connection string for postgres
	ListenAddr      string // e.g. ":8080"
	OpAMPAddr       string // e.g. ":4320"
	JWTSecret       string
	CORSOrigins     string // comma-separated allowed origins
	MinAgentVersion string // minimum required agent version; empty = disabled
	WebhookURL      string // HTTP endpoint to notify on alert fire; empty = disabled
}

func Load() Config {
	return Config{
		DBDriver:        getenv("DB_DRIVER", "sqlite"),
		DBDSN:           getenv("DB_DSN", "otel-magnify.db"),
		ListenAddr:      getenv("LISTEN_ADDR", ":8080"),
		OpAMPAddr:       getenv("OPAMP_ADDR", ":4320"),
		JWTSecret:       getenv("JWT_SECRET", ""),
		CORSOrigins:     getenv("CORS_ORIGINS", "http://localhost:5173"),
		MinAgentVersion: getenv("MIN_AGENT_VERSION", ""),
		WebhookURL:      getenv("WEBHOOK_URL", ""),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
