package config

import (
	"os"
	"reflect"
	"testing"
	"time"
)

func TestLoadWorkloadDefaults(t *testing.T) {
	for _, k := range []string{
		"WORKLOAD_RETENTION_DAYS",
		"WORKLOAD_DISCONNECT_GRACE_SECONDS",
		"WORKLOAD_JANITOR_INTERVAL_SECONDS",
		"WORKLOAD_EVENT_RETENTION_DAYS",
	} {
		os.Unsetenv(k)
	}
	c := Load()
	if c.WorkloadRetention != 30*24*time.Hour {
		t.Errorf("WorkloadRetention default wrong: %v", c.WorkloadRetention)
	}
	if c.WorkloadDisconnectGrace != 120*time.Second {
		t.Errorf("WorkloadDisconnectGrace default wrong: %v", c.WorkloadDisconnectGrace)
	}
	if c.WorkloadJanitorInterval != 300*time.Second {
		t.Errorf("WorkloadJanitorInterval default wrong: %v", c.WorkloadJanitorInterval)
	}
	if c.WorkloadEventRetention != 30*24*time.Hour {
		t.Errorf("WorkloadEventRetention default wrong: %v", c.WorkloadEventRetention)
	}
}

func TestLoadWorkloadOverrides(t *testing.T) {
	t.Setenv("WORKLOAD_RETENTION_DAYS", "7")
	t.Setenv("WORKLOAD_DISCONNECT_GRACE_SECONDS", "30")
	t.Setenv("WORKLOAD_JANITOR_INTERVAL_SECONDS", "60")
	t.Setenv("WORKLOAD_EVENT_RETENTION_DAYS", "14")
	c := Load()
	if c.WorkloadRetention != 7*24*time.Hour {
		t.Errorf("got %v", c.WorkloadRetention)
	}
	if c.WorkloadDisconnectGrace != 30*time.Second {
		t.Errorf("got %v", c.WorkloadDisconnectGrace)
	}
	if c.WorkloadJanitorInterval != 60*time.Second {
		t.Errorf("got %v", c.WorkloadJanitorInterval)
	}
	if c.WorkloadEventRetention != 14*24*time.Hour {
		t.Errorf("got %v", c.WorkloadEventRetention)
	}
}

func TestLoadInvalidValuesFallBackToDefault(t *testing.T) {
	t.Setenv("WORKLOAD_RETENTION_DAYS", "not-a-number")
	c := Load()
	if c.WorkloadRetention != 30*24*time.Hour {
		t.Errorf("got %v", c.WorkloadRetention)
	}
}

func TestConfigHasNoLegacyOpAMPSharedSecret(t *testing.T) {
	if _, found := reflect.TypeOf(Config{}).FieldByName("OpAMPSharedSecret"); found {
		t.Fatal("Config still exposes the legacy OpAMPSharedSecret field")
	}
}

func TestLoadOpAMPTransportDefaults(t *testing.T) {
	for _, key := range []string{
		"OPAMP_INSECURE",
		"OPAMP_TLS_CERT_FILE",
		"OPAMP_TLS_KEY_FILE",
	} {
		t.Setenv(key, "")
	}

	c := Load()

	if c.OpAMPInsecure != "false" {
		t.Fatalf("OpAMPInsecure = %q, want %q", c.OpAMPInsecure, "false")
	}
	if c.OpAMPTLSCertFile != "" {
		t.Fatalf("OpAMPTLSCertFile = %q, want empty", c.OpAMPTLSCertFile)
	}
	if c.OpAMPTLSKeyFile != "" {
		t.Fatalf("OpAMPTLSKeyFile = %q, want empty", c.OpAMPTLSKeyFile)
	}
}

func TestLoadOpAMPTransportOverrides(t *testing.T) {
	t.Setenv("OPAMP_INSECURE", "true")
	t.Setenv("OPAMP_TLS_CERT_FILE", "/run/secrets/opamp.crt")
	t.Setenv("OPAMP_TLS_KEY_FILE", "/run/secrets/opamp.key")

	c := Load()

	if c.OpAMPInsecure != "true" {
		t.Fatalf("OpAMPInsecure = %q, want %q", c.OpAMPInsecure, "true")
	}
	if c.OpAMPTLSCertFile != "/run/secrets/opamp.crt" {
		t.Fatalf("OpAMPTLSCertFile = %q, want configured path", c.OpAMPTLSCertFile)
	}
	if c.OpAMPTLSKeyFile != "/run/secrets/opamp.key" {
		t.Fatalf("OpAMPTLSKeyFile = %q, want configured path", c.OpAMPTLSKeyFile)
	}
}

func TestLoadDatabasePoolDefaults(t *testing.T) {
	for _, key := range []string{
		"DB_MAX_OPEN_CONNS",
		"DB_MAX_IDLE_CONNS",
		"DB_CONN_MAX_IDLE_TIME_SECONDS",
		"DB_CONN_MAX_LIFETIME_SECONDS",
	} {
		t.Setenv(key, "")
	}

	c := Load()
	if c.DBMaxOpenConns != 40 {
		t.Fatalf("DBMaxOpenConns = %d, want 40", c.DBMaxOpenConns)
	}
	if c.DBMaxIdleConns != 10 {
		t.Fatalf("DBMaxIdleConns = %d, want 10", c.DBMaxIdleConns)
	}
	if c.DBConnMaxIdleTime != 5*time.Minute {
		t.Fatalf("DBConnMaxIdleTime = %v, want %v", c.DBConnMaxIdleTime, 5*time.Minute)
	}
	if c.DBConnMaxLifetime != 30*time.Minute {
		t.Fatalf("DBConnMaxLifetime = %v, want %v", c.DBConnMaxLifetime, 30*time.Minute)
	}
}

func TestLoadDatabasePoolIdleTimeOverride(t *testing.T) {
	t.Setenv("DB_CONN_MAX_IDLE_TIME_SECONDS", "45")

	c := Load()
	if c.DBConnMaxIdleTime != 45*time.Second {
		t.Fatalf("DBConnMaxIdleTime = %v, want %v", c.DBConnMaxIdleTime, 45*time.Second)
	}
}

func TestLoadDatabasePoolLifetimeInvalidValuesFallBackToDefault(t *testing.T) {
	for _, value := range []string{"not-a-number", "0", "-1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("DB_CONN_MAX_LIFETIME_SECONDS", value)

			c := Load()
			if c.DBConnMaxLifetime != 30*time.Minute {
				t.Fatalf("DBConnMaxLifetime = %v, want %v", c.DBConnMaxLifetime, 30*time.Minute)
			}
		})
	}
}
