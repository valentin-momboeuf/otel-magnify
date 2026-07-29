package server

import (
	"crypto/tls"
	"strings"
	"testing"
	"time"
)

func TestResolveOpAMPTransportMatrix(t *testing.T) {
	now := time.Date(2026, 7, 23, 8, 0, 0, 0, time.UTC)
	validCert, validKey, _ := writeTestOpAMPCertificate(t, now.Add(-time.Hour), now.Add(time.Hour))
	expiredCert, expiredKey, _ := writeTestOpAMPCertificate(t, now.Add(-2*time.Hour), now)
	futureCert, futureKey, _ := writeTestOpAMPCertificate(t, now.Add(time.Minute), now.Add(time.Hour))

	tests := []struct {
		name     string
		cfg      Config
		wantMode opampTransportMode
		wantErr  bool
	}{
		{name: "missing uses secure default and disables without TLS", cfg: Config{}, wantMode: opampTransportDisabled, wantErr: true},
		{name: "exact false disables without TLS", cfg: Config{OpAMPInsecure: "false"}, wantMode: opampTransportDisabled, wantErr: true},
		{name: "exact true enables explicit plaintext", cfg: Config{OpAMPInsecure: "true"}, wantMode: opampTransportInsecure},
		{name: "valid TLS pair enables secure transport", cfg: Config{OpAMPInsecure: "false", OpAMPTLSCertFile: validCert, OpAMPTLSKeyFile: validKey}, wantMode: opampTransportTLS},
		{name: "partial certificate disables", cfg: Config{OpAMPInsecure: "false", OpAMPTLSCertFile: validCert}, wantMode: opampTransportDisabled, wantErr: true},
		{name: "partial key disables", cfg: Config{OpAMPInsecure: "false", OpAMPTLSKeyFile: validKey}, wantMode: opampTransportDisabled, wantErr: true},
		{name: "invalid pair disables", cfg: Config{OpAMPInsecure: "false", OpAMPTLSCertFile: validCert, OpAMPTLSKeyFile: validCert}, wantMode: opampTransportDisabled, wantErr: true},
		{name: "expired leaf disables at exclusive NotAfter", cfg: Config{OpAMPInsecure: "false", OpAMPTLSCertFile: expiredCert, OpAMPTLSKeyFile: expiredKey}, wantMode: opampTransportDisabled, wantErr: true},
		{name: "future leaf disables", cfg: Config{OpAMPInsecure: "false", OpAMPTLSCertFile: futureCert, OpAMPTLSKeyFile: futureKey}, wantMode: opampTransportDisabled, wantErr: true},
		{name: "insecure conflicts with TLS pair", cfg: Config{OpAMPInsecure: "true", OpAMPTLSCertFile: validCert, OpAMPTLSKeyFile: validKey}, wantMode: opampTransportDisabled, wantErr: true},
		{name: "insecure conflicts with partial TLS", cfg: Config{OpAMPInsecure: "true", OpAMPTLSCertFile: validCert}, wantMode: opampTransportDisabled, wantErr: true},
		{name: "uppercase true is invalid", cfg: Config{OpAMPInsecure: "TRUE"}, wantMode: opampTransportDisabled, wantErr: true},
		{name: "numeric true is invalid", cfg: Config{OpAMPInsecure: "1"}, wantMode: opampTransportDisabled, wantErr: true},
		{name: "whitespace is invalid", cfg: Config{OpAMPInsecure: " false "}, wantMode: opampTransportDisabled, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, err := resolveOpAMPTransport(tt.cfg, now)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveOpAMPTransport() error = %v, wantErr %t", err, tt.wantErr)
			}
			if transport.mode != tt.wantMode {
				t.Fatalf("transport mode = %v, want %v", transport.mode, tt.wantMode)
			}
			if transport.mode == opampTransportTLS {
				if transport.tlsConfig == nil {
					t.Fatal("TLS transport has nil tls.Config")
				}
				if transport.tlsConfig.MinVersion != tls.VersionTLS12 {
					t.Fatalf("MinVersion = %x, want TLS 1.2", transport.tlsConfig.MinVersion)
				}
			}
		})
	}
}

func TestResolveOpAMPTransportErrorsDoNotDisclosePaths(t *testing.T) {
	const certPath = "/run/secrets/customer-sensitive-certificate-name"
	const keyPath = "/run/secrets/customer-sensitive-key-name"

	_, err := resolveOpAMPTransport(Config{
		OpAMPInsecure:    "false",
		OpAMPTLSCertFile: certPath,
		OpAMPTLSKeyFile:  keyPath,
	}, time.Now())
	if err == nil {
		t.Fatal("resolveOpAMPTransport() error = nil, want unavailable transport")
	}
	for _, sensitive := range []string{certPath, keyPath, "customer-sensitive"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("transport error disclosed sensitive path material: %q", err)
		}
	}
}
