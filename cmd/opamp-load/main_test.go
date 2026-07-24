package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/open-telemetry/opamp-go/client/types"
)

const testCredential = "opamp-load-private-test-credential"

func TestParseConfig(t *testing.T) {
	tokenFile := writePrivateTestFile(t, "token", testCredential)

	tests := []struct {
		name      string
		args      []string
		check     func(*testing.T, config)
		wantError bool
	}{
		{
			name: "secure defaults",
			args: []string{"--token-file", tokenFile},
			check: func(t *testing.T, got config) {
				t.Helper()
				if got.endpoint != "wss://localhost:4320/v1/opamp" {
					t.Fatal("default endpoint is not secure")
				}
				if got.tokenFile != tokenFile {
					t.Fatal("token file path was not retained")
				}
				if got.collectors != 5000 || got.ramp != 5*time.Minute || got.hold != 10*time.Minute {
					t.Fatal("load defaults changed unexpectedly")
				}
			},
		},
		{
			name: "accepts explicit local insecure transport",
			args: []string{
				"--endpoint", "ws://otel-magnify:4320/v1/opamp",
				"--token-file", tokenFile,
				"--allow-insecure-transport",
				"--collectors", "5000",
				"--ramp", "1m",
				"--hold", "5m",
				"--ready-file", "/artifacts/ready.json",
			},
			check: func(t *testing.T, got config) {
				t.Helper()
				if got.endpoint != "ws://otel-magnify:4320/v1/opamp" ||
					got.tokenFile != tokenFile ||
					got.collectors != 5000 ||
					got.ramp != time.Minute ||
					got.hold != 5*time.Minute ||
					got.readyFile != "/artifacts/ready.json" {
					t.Fatal("explicit local transport configuration was not retained")
				}
			},
		},
		{name: "requires token file", args: nil, wantError: true},
		{
			name: "rejects plaintext without explicit opt in",
			args: []string{
				"--endpoint", "ws://otel-magnify:4320/v1/opamp",
				"--token-file", tokenFile,
			},
			wantError: true,
		},
		{
			name: "rejects insecure opt in for secure endpoint",
			args: []string{
				"--endpoint", "wss://otel-magnify:4320/v1/opamp",
				"--token-file", tokenFile,
				"--allow-insecure-transport",
			},
			wantError: true,
		},
		{
			name: "rejects private CA with plaintext",
			args: []string{
				"--endpoint", "ws://otel-magnify:4320/v1/opamp",
				"--token-file", tokenFile,
				"--allow-insecure-transport",
				"--tls-ca-file", "/private/ca.pem",
			},
			wantError: true,
		},
		{
			name: "rejects invalid scheme without echoing endpoint",
			args: []string{
				"--endpoint", "https://private.invalid/v1/opamp",
				"--token-file", tokenFile,
			},
			wantError: true,
		},
		{
			name: "rejects endpoint user info",
			args: []string{
				"--endpoint", "wss://inline-user:inline-password@private.invalid/v1/opamp",
				"--token-file", tokenFile,
			},
			wantError: true,
		},
		{
			name: "rejects endpoint query",
			args: []string{
				"--endpoint", "wss://private.invalid/v1/opamp?credential=" + testCredential,
				"--token-file", tokenFile,
			},
			wantError: true,
		},
		{
			name: "rejects endpoint fragment",
			args: []string{
				"--endpoint", "wss://private.invalid/v1/opamp#" + testCredential,
				"--token-file", tokenFile,
			},
			wantError: true,
		},
		{
			name: "rejects endpoint surrounding whitespace",
			args: []string{
				"--endpoint", "wss://private.invalid/v1/opamp ",
				"--token-file", tokenFile,
			},
			wantError: true,
		},
		{name: "rejects zero collectors", args: []string{"--token-file", tokenFile, "--collectors", "0"}, wantError: true},
		{name: "rejects negative collectors", args: []string{"--token-file", tokenFile, "--collectors", "-1"}, wantError: true},
		{name: "rejects empty endpoint", args: []string{"--token-file", tokenFile, "--endpoint", ""}, wantError: true},
		{name: "rejects negative ramp", args: []string{"--token-file", tokenFile, "--ramp", "-1s"}, wantError: true},
		{name: "rejects negative hold", args: []string{"--token-file", tokenFile, "--hold", "-1s"}, wantError: true},
		{name: "rejects malformed duration", args: []string{"--token-file", tokenFile, "--hold", "later"}, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseConfig(test.args)
			if test.wantError {
				if err == nil {
					t.Fatal("parseConfig did not return the expected generic error")
				}
				assertNoCredentialLeak(t, err.Error())
				if strings.Contains(err.Error(), "private.invalid") {
					t.Fatal("parseConfig error exposed an endpoint")
				}
				return
			}
			if err != nil {
				t.Fatal("parseConfig rejected valid input")
			}
			if test.check != nil {
				test.check(t, got)
			}
		})
	}
}

func TestParseConfigIgnoresLegacyCredentialEnvironment(t *testing.T) {
	t.Setenv("OPAMP_SHARED_SECRET", testCredential)

	_, err := parseConfig(nil)
	if err == nil {
		t.Fatal("legacy credential environment replaced the required token file")
	}
	assertNoCredentialLeak(t, err.Error())
}

func TestParseConfigRejectsPositionalArgumentsWithoutEchoingThem(t *testing.T) {
	tokenFile := writePrivateTestFile(t, "token", testCredential)

	_, err := parseConfig([]string{"--token-file", tokenFile, testCredential})
	if err == nil || err.Error() != "unexpected positional arguments" {
		t.Fatal("positional argument was not rejected with the generic error")
	}
	assertNoCredentialLeak(t, err.Error())
}

func TestReadTokenFile(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		_, err := readTokenFile(filepath.Join(t.TempDir(), "missing"))
		if err == nil {
			t.Fatal("missing token file was accepted")
		}
		assertNoCredentialLeak(t, err.Error())
	})

	t.Run("directory", func(t *testing.T) {
		_, err := readTokenFile(t.TempDir())
		if err == nil {
			t.Fatal("token directory was accepted as a regular file")
		}
		assertNoCredentialLeak(t, err.Error())
	})

	for _, content := range []string{"", " \n\t "} {
		content := content
		t.Run("empty or whitespace", func(t *testing.T) {
			path := writePrivateTestFile(t, "token", content)
			_, err := readTokenFile(path)
			if err == nil {
				t.Fatal("empty token file was accepted")
			}
			assertNoCredentialLeak(t, err.Error())
		})
	}

	t.Run("trims once in memory", func(t *testing.T) {
		path := writePrivateTestFile(t, "token", " \n"+testCredential+"\t ")
		got, err := readTokenFile(path)
		if err != nil {
			t.Fatal("valid token file was rejected")
		}
		if got != testCredential {
			t.Fatal("token file was not trimmed")
		}
	})
}

func TestBuildAuthorizationHeaderUsesOneExactBearerValue(t *testing.T) {
	header := buildAuthorizationHeader(testCredential)
	values := header.Values("Authorization")
	if len(header) != 1 || len(values) != 1 || values[0] != "Bearer "+testCredential {
		t.Fatal("Authorization header is not the single exact Bearer value")
	}
}

func TestBuildTLSConfigAppendsPrivateCAWithoutDisablingVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()

	certificate := server.Certificate()
	caFile := writePrivateTestFile(t, "private-ca.pem", string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certificate.Raw,
	})))

	systemRoots, err := x509.SystemCertPool()
	if err != nil {
		t.Fatal("load system root pool")
	}
	tlsConfig, err := buildTLSConfig(caFile)
	if err != nil {
		t.Fatal("build TLS config with private CA")
	}
	if tlsConfig == nil || tlsConfig.RootCAs == nil {
		t.Fatal("TLS config does not contain a root pool")
	}
	if tlsConfig.InsecureSkipVerify {
		t.Fatal("TLS verification was disabled")
	}
	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("TLS minimum version is not TLS 1.2")
	}
	for _, subject := range systemRoots.Subjects() {
		if !containsCertificateSubject(tlsConfig.RootCAs.Subjects(), subject) {
			t.Fatal("private CA pool discarded a system root")
		}
	}
	if !containsCertificateSubject(tlsConfig.RootCAs.Subjects(), certificate.RawSubject) {
		t.Fatal("private CA was not appended")
	}
}

func TestBuildStartSettingsCarriesTLSHeaderAndRedirectRefusal(t *testing.T) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	var instanceUID types.InstanceUid
	connected := false
	settings := buildStartSettings(
		"wss://private.invalid/v1/opamp",
		testCredential,
		tlsConfig,
		instanceUID,
		types.Callbacks{
			OnConnect: func(context.Context) {
				connected = true
			},
		},
	)

	if settings.TLSConfig != tlsConfig || settings.InstanceUid != instanceUID {
		t.Fatal("StartSettings lost TLS or instance identity")
	}
	values := settings.Header.Values("Authorization")
	if len(settings.Header) != 1 || len(values) != 1 || values[0] != "Bearer "+testCredential {
		t.Fatal("StartSettings does not contain the exact Bearer header")
	}
	if settings.Callbacks.CheckRedirect == nil {
		t.Fatal("StartSettings does not refuse redirects")
	}
	request, err := http.NewRequest(http.MethodGet, "https://redirect.invalid/v1/opamp", nil)
	if err != nil {
		t.Fatal("create redirect request")
	}
	if err := settings.Callbacks.CheckRedirect(request, nil, nil); err == nil {
		t.Fatal("StartSettings redirect callback accepted a redirect")
	}
	settings.Callbacks.OnConnect(context.Background())
	if !connected {
		t.Fatal("StartSettings discarded the supplied callbacks")
	}
}

func TestRejectRedirectReturnsGenericError(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "https://private.invalid/v1/opamp", nil)
	if err != nil {
		t.Fatal("create redirect request")
	}
	request.Header.Set("Authorization", "Bearer "+testCredential)

	err = rejectRedirect(request, []*http.Request{})
	if err == nil {
		t.Fatal("redirect was accepted")
	}
	assertNoCredentialLeak(t, err.Error())
	if strings.Contains(err.Error(), "private.invalid") || strings.Contains(err.Error(), "Authorization") {
		t.Fatal("redirect error exposed request details")
	}
}

func TestRunDoesNotReplayAuthorizationAcrossRedirect(t *testing.T) {
	var destinationRequests atomic.Int32
	var destinationAuthorization atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationRequests.Add(1)
		if r.Header.Get("Authorization") != "" {
			destinationAuthorization.Store(true)
		}
		http.Error(w, "not a websocket", http.StatusBadRequest)
	}))
	defer destination.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, _ := run(ctx, config{
		endpoint:   "ws" + strings.TrimPrefix(source.URL, "http"),
		collectors: 1,
	}, testCredential)

	if result.Failed != 1 {
		t.Fatal("redirect refusal was not reported as a failed connection")
	}
	if destinationRequests.Load() != 0 || destinationAuthorization.Load() {
		t.Fatal("redirect destination received a request or Authorization header")
	}
}

func TestRunStopsConnectedCollectorsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan struct{})
	result := make(chan summary, 1)
	go func() {
		result <- runWithCollector(ctx, config{collectors: 1, hold: time.Hour}, "", func(
			_ context.Context,
			_ string,
			_ string,
			_ *tls.Config,
			stop <-chan struct{},
			readyGroup *sync.WaitGroup,
			workers *sync.WaitGroup,
			counters *counters,
		) {
			defer workers.Done()
			counters.connected.Add(1)
			readyGroup.Done()
			close(ready)
			<-stop
			counters.disconnected.Add(1)
		})
	}()

	<-ready
	cancel()

	select {
	case got := <-result:
		if got.Attempted != 1 || got.Connected != 1 || got.Failed != 0 || got.Cancelled != 0 || got.Disconnected != 1 || got.StopFailed != 0 || !got.Interrupted {
			t.Fatalf("summary = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("run did not stop connected collectors after cancellation")
	}
}

func TestMainWritesSummaryAndExits130OnSignal(t *testing.T) {
	binaryPath := filepath.Join(t.TempDir(), "opamp-load")
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	if output, err := build.CombinedOutput(); err != nil {
		_ = output
		t.Fatal("build opamp-load test binary")
	}
	tokenFile := writePrivateTestFile(t, "token", testCredential)

	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			defer listener.Close()

			accepted := make(chan net.Conn, 1)
			acceptErr := make(chan error, 1)
			go func() {
				connection, err := listener.Accept()
				if err != nil {
					acceptErr <- err
					return
				}
				accepted <- connection
			}()

			endpoint := fmt.Sprintf("ws://%s/v1/opamp", listener.Addr())
			command := exec.Command(binaryPath,
				"--endpoint", endpoint,
				"--token-file", tokenFile,
				"--allow-insecure-transport",
				"--collectors", "1",
				"--ramp", "0s",
				"--hold", "1h",
			)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			if err := command.Start(); err != nil {
				t.Fatalf("start opamp-load: %v", err)
			}
			processDone := make(chan error, 1)
			go func() {
				processDone <- command.Wait()
			}()

			var connection net.Conn
			select {
			case connection = <-accepted:
				defer connection.Close()
			case err := <-acceptErr:
				t.Fatalf("accept connection: %v", err)
			case <-processDone:
				t.Fatal("opamp-load exited before connecting to the local listener")
			case <-time.After(10 * time.Second):
				t.Fatal("opamp-load did not connect to the local test listener")
			}

			if err := command.Process.Signal(signal); err != nil {
				t.Fatalf("signal opamp-load: %v", err)
			}

			select {
			case err := <-processDone:
				var exitError *exec.ExitError
				if !errors.As(err, &exitError) || exitError.ExitCode() != 130 {
					t.Fatal("opamp-load did not exit with status 130")
				}
			case <-time.After(10 * time.Second):
				_ = command.Process.Kill()
				<-processDone
				t.Fatal("opamp-load did not stop after signal")
			}

			assertNoCredentialLeak(t, stdout.String())
			assertNoCredentialLeak(t, stderr.String())
			var result summary
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal("decode final summary")
			}
			if !result.Interrupted {
				t.Fatalf("summary = %#v, want interrupted run", result)
			}
		})
	}
}

func writePrivateTestFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal("write private test file")
	}
	return path
}

func assertNoCredentialLeak(t *testing.T, output string) {
	t.Helper()

	if strings.Contains(output, testCredential) {
		t.Fatal("credential material appeared in output")
	}
}

func containsCertificateSubject(subjects [][]byte, expected []byte) bool {
	for _, subject := range subjects {
		if bytes.Equal(subject, expected) {
			return true
		}
	}
	return false
}
