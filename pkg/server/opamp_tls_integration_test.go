package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/magnify-labs/otel-magnify/internal/auth"
	"github.com/magnify-labs/otel-magnify/internal/opamp"
	"github.com/magnify-labs/otel-magnify/internal/store"
	"github.com/magnify-labs/otel-magnify/internal/testdb"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

func TestServerKeepsAPIAvailableWhenOpAMPTransportIsUnavailable(t *testing.T) {
	database := openTransportTestDB(t)
	tests := []struct {
		name string
		cfg  func(string, string) Config
	}{
		{
			name: "secure default without certificate",
			cfg: func(apiAddr, opampAddr string) Config {
				return Config{ListenAddr: apiAddr, OpAMPAddr: opampAddr}
			},
		},
		{
			name: "invalid TLS pair",
			cfg: func(apiAddr, opampAddr string) Config {
				dir := t.TempDir()
				certPath := filepath.Join(dir, "invalid.crt")
				keyPath := filepath.Join(dir, "invalid.key")
				if err := os.WriteFile(certPath, []byte("not a certificate"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(keyPath, []byte("not a key"), 0o600); err != nil {
					t.Fatal(err)
				}
				return Config{
					ListenAddr:       apiAddr,
					OpAMPAddr:        opampAddr,
					OpAMPInsecure:    "false",
					OpAMPTLSCertFile: certPath,
					OpAMPTLSKeyFile:  keyPath,
				}
			},
		},
		{
			name: "invalid insecure selector",
			cfg: func(apiAddr, opampAddr string) Config {
				return Config{
					ListenAddr:    apiAddr,
					OpAMPAddr:     opampAddr,
					OpAMPInsecure: "False",
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiAddr := reserveTCPAddr(t)
			opampAddr := reserveTCPAddr(t)
			srv := New(tt.cfg(apiAddr, opampAddr), database, auth.New("test-secret-key-at-least-32-bytes!"))
			stop := runTransportTestServer(t, srv, apiAddr)

			response, err := http.Get("http://" + apiAddr + "/healthz")
			if err != nil {
				stop()
				t.Fatalf("GET /healthz: %v", err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				stop()
				t.Fatalf("GET /healthz status = %d, want 200", response.StatusCode)
			}

			conn, err := net.DialTimeout("tcp", opampAddr, 100*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				stop()
				t.Fatal("OpAMP listener is available without a valid explicit transport")
			}
			stop()
		})
	}
}

func TestServerWSSRequiresDynamicDatabaseTokenAndTLS12(t *testing.T) {
	now := time.Now().UTC()
	certPath, keyPath, caPEM := writeTestOpAMPCertificate(t, now.Add(-time.Hour), now.Add(time.Hour))
	apiAddr := reserveTCPAddr(t)
	opampAddr := reserveTCPAddr(t)
	database := openTransportTestDB(t)
	authProvider := auth.New("test-secret-key-at-least-32-bytes!")
	srv := New(Config{
		ListenAddr:       apiAddr,
		OpAMPAddr:        opampAddr,
		OpAMPInsecure:    "false",
		OpAMPTLSCertFile: certPath,
		OpAMPTLSKeyFile:  keyPath,
	}, database, authProvider)
	stop := runTransportTestServer(t, srv, apiAddr)
	defer stop()

	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caPEM) {
		t.Fatal("append test CA")
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootCAs,
		ServerName: "localhost",
	}
	wssURL := "wss://" + opampAddr + "/v1/opamp"

	_, response, err := (&websocket.Dialer{TLSClientConfig: tlsConfig}).Dial(wssURL, nil)
	if err == nil {
		t.Fatal("anonymous WSS handshake succeeded with zero managed tokens")
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous WSS response = %#v, error = %v; want 401", response, err)
	}
	_ = response.Body.Close()

	token := createManagedTokenThroughAPI(t, apiAddr, authProvider)
	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	connection, response, err := (&websocket.Dialer{TLSClientConfig: tlsConfig}).Dial(wssURL, headers)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		t.Fatalf("WSS handshake with DB-created token: %v", err)
	}
	_ = connection.Close()

	plaintextConnection, plaintextResponse, plaintextErr := websocket.DefaultDialer.Dial("ws://"+opampAddr+"/v1/opamp", headers)
	if plaintextConnection != nil {
		_ = plaintextConnection.Close()
	}
	if plaintextResponse != nil {
		_ = plaintextResponse.Body.Close()
	}
	if plaintextErr == nil {
		t.Fatal("plaintext WS handshake succeeded on the TLS OpAMP listener")
	}

	legacyTLS := &tls.Config{
		MaxVersion: tls.VersionTLS11,
		RootCAs:    rootCAs,
		ServerName: "localhost",
	}
	legacyConn, err := tls.Dial("tcp", opampAddr, legacyTLS)
	if err == nil {
		_ = legacyConn.Close()
		t.Fatal("OpAMP TLS listener accepted a client limited to TLS 1.1")
	}
}

func TestServerExplicitInsecureWSStillRequiresDatabaseToken(t *testing.T) {
	apiAddr := reserveTCPAddr(t)
	opampAddr := reserveTCPAddr(t)
	database := openTransportTestDB(t)
	authProvider := auth.New("test-secret-key-at-least-32-bytes!")
	srv := New(Config{
		ListenAddr:    apiAddr,
		OpAMPAddr:     opampAddr,
		OpAMPInsecure: "true",
	}, database, authProvider)
	stop := runTransportTestServer(t, srv, apiAddr)
	defer stop()

	wsURL := "ws://" + opampAddr + "/v1/opamp"
	_, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("anonymous plaintext WS handshake succeeded")
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous WS response = %#v, error = %v; want 401", response, err)
	}
	_ = response.Body.Close()

	token := createManagedTokenThroughAPI(t, apiAddr, authProvider)
	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	connection, response, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		t.Fatalf("explicit insecure WS handshake with DB-created token: %v", err)
	}
	_ = connection.Close()
}

func TestServerClosesAPIListenerWhenValidOpAMPBindFails(t *testing.T) {
	now := time.Now().UTC()
	certPath, keyPath, _ := writeTestOpAMPCertificate(t, now.Add(-time.Hour), now.Add(time.Hour))
	apiAddr := reserveTCPAddr(t)
	blockedOpAMP, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blockedOpAMP.Close()

	database := openTransportTestDB(t)
	srv := New(Config{
		ListenAddr:       apiAddr,
		OpAMPAddr:        blockedOpAMP.Addr().String(),
		OpAMPInsecure:    "false",
		OpAMPTLSCertFile: certPath,
		OpAMPTLSKeyFile:  keyPath,
	}, database, auth.New("test-secret-key-at-least-32-bytes!"))

	err = srv.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want OpAMP bind failure")
	}

	reboundAPI, err := net.Listen("tcp", apiAddr)
	if err != nil {
		t.Fatalf("API listener leaked after OpAMP bind failure: %v", err)
	}
	_ = reboundAPI.Close()
}

func TestServerConfigAndOpAMPOptionsExposeNoStaticCredential(t *testing.T) {
	if _, found := reflect.TypeOf(Config{}).FieldByName("OpAMPSharedSecret"); found {
		t.Fatal("server.Config still exposes OpAMPSharedSecret")
	}
	if _, found := reflect.TypeOf(opamp.Options{}).FieldByName("SharedSecret"); found {
		t.Fatal("opamp.Options still exposes SharedSecret")
	}
}

func runTransportTestServer(t *testing.T, srv *Server, apiAddr string) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Run(ctx)
	}()
	waitForTransportTestHealth(t, apiAddr, errCh)
	return func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Fatalf("Server.Run() error = %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Server.Run() did not return after cancellation")
		}
	}
}

func waitForTransportTestHealth(t *testing.T, apiAddr string, errCh <-chan error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			t.Fatalf("Server.Run() returned before API became healthy: %v", err)
		default:
		}
		response, err := http.Get("http://" + apiAddr + "/healthz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("API /healthz did not become available on %s", apiAddr)
}

func createManagedTokenThroughAPI(t *testing.T, apiAddr string, authProvider ext.AuthProvider) string {
	t.Helper()
	adminToken, err := authProvider.GenerateToken("user-001", "admin@test.com", []string{"administrator"})
	if err != nil {
		t.Fatalf("generate admin token: %v", err)
	}
	request, err := http.NewRequest(
		http.MethodPost,
		"http://"+apiAddr+"/api/v1/opamp/tokens",
		bytes.NewBufferString(`{"name":"transport-integration"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+adminToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("create managed token: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create managed token status = %d, body = %s", response.StatusCode, body)
	}
	var created struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode managed token: %v", err)
	}
	if created.Value == "" {
		t.Fatal("create managed token returned an empty value")
	}
	return created.Value
}

func openTransportTestDB(t *testing.T) *store.DB {
	t.Helper()
	database, err := store.Open(testdb.New(t).DSN, store.PoolConfig{
		MaxOpenConns:    4,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func reserveTCPAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func writeTestOpAMPCertificate(t *testing.T, notBefore, notAfter time.Time) (string, string, []byte) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "otel-magnify test CA"},
		NotBefore:             notBefore.Add(-time.Hour),
		NotAfter:              notAfter.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caTemplate, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, "opamp.crt")
	keyPath := filepath.Join(dir, "opamp.key")
	certPEM := append(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		caPEM...,
	)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath, caPEM
}
