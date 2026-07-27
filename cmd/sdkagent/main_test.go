package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/open-telemetry/opamp-go/client"
	"github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"
)

const (
	sdkAgentTokenSentinel    = "sdkagent-red-token-sentinel"
	sdkAgentEndpointSentinel = "ws://sdkagent-red-endpoint.invalid/v1/opamp"
	sdkAgentRawErrorSentinel = "sdkagent-red-raw-error"
)

func TestParseConfigRequiresTokenFileAndIgnoresLegacyEnvironment(t *testing.T) {
	t.Setenv("OPAMP_SHARED_SECRET", sdkAgentTokenSentinel)

	_, err := parseConfig(nil)
	if err == nil {
		t.Fatal("parseConfig accepted a missing token file")
	}
	assertNoSDKAgentSensitiveData(t, err.Error())
}

func TestParseConfigUsesSecureTransportDefaults(t *testing.T) {
	got, err := parseConfig([]string{"--token-file", "/run/secrets/opamp-token"})
	if err != nil {
		t.Fatal("parseConfig rejected the minimum secure configuration")
	}
	if got.endpoint != "wss://localhost:4320/v1/opamp" {
		t.Fatal("parseConfig did not default to the secure WebSocket endpoint")
	}
	if got.tokenFile != "/run/secrets/opamp-token" {
		t.Fatal("parseConfig did not retain the token file path")
	}
	if got.allowInsecureTransport {
		t.Fatal("parseConfig enabled insecure transport by default")
	}
}

func TestParseConfigValidatesTransportOverrides(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name: "refuses ws without opt in",
			args: []string{
				"--token-file", "/run/secrets/opamp-token",
				"--endpoint", "ws://localhost:4320/v1/opamp",
			},
			wantErr: true,
		},
		{
			name: "accepts ws with explicit opt in",
			args: []string{
				"--token-file", "/run/secrets/opamp-token",
				"--endpoint", "ws://localhost:4320/v1/opamp",
				"--allow-insecure-transport",
			},
		},
		{
			name: "refuses insecure opt in with wss",
			args: []string{
				"--token-file", "/run/secrets/opamp-token",
				"--allow-insecure-transport",
			},
			wantErr: true,
		},
		{
			name: "refuses private ca with ws",
			args: []string{
				"--token-file", "/run/secrets/opamp-token",
				"--endpoint", "ws://localhost:4320/v1/opamp",
				"--allow-insecure-transport",
				"--tls-ca-file", "/run/secrets/opamp-ca.pem",
			},
			wantErr: true,
		},
		{
			name: "accepts private ca with wss",
			args: []string{
				"--token-file", "/run/secrets/opamp-token",
				"--tls-ca-file", "/run/secrets/opamp-ca.pem",
			},
		},
		{
			name: "refuses invalid scheme",
			args: []string{
				"--token-file", "/run/secrets/opamp-token",
				"--endpoint", "https://localhost:4320/v1/opamp",
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseConfig(test.args)
			if test.wantErr && err == nil {
				t.Fatal("parseConfig accepted an invalid transport configuration")
			}
			if !test.wantErr && err != nil {
				t.Fatal("parseConfig rejected a valid transport configuration")
			}
			if err != nil {
				assertNoSDKAgentSensitiveData(t, err.Error())
			}
		})
	}
}

func TestParseConfigRejectsInlineAndPositionalTokensWithoutEchoingThem(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "inline token flag",
			args: []string{
				"--token-file", "/run/secrets/opamp-token",
				"--token", sdkAgentTokenSentinel,
			},
		},
		{
			name: "positional token",
			args: []string{
				"--token-file", "/run/secrets/opamp-token",
				sdkAgentTokenSentinel,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseConfig(test.args)
			if err == nil {
				t.Fatal("parseConfig accepted an inline credential")
			}
			assertNoSDKAgentSensitiveData(t, err.Error())
			if test.name == "positional token" && err.Error() != "unexpected positional arguments" {
				t.Fatal("parseConfig returned a non-generic positional argument error")
			}
		})
	}
}

func TestParseConfigRequiresPositiveProbeTimeout(t *testing.T) {
	for _, value := range []string{"0s", "-1s"} {
		_, err := parseConfig([]string{
			"--token-file", "/run/secrets/opamp-token",
			"--probe-only",
			"--probe-timeout", value,
		})
		if err == nil {
			t.Fatal("parseConfig accepted a non-positive probe timeout")
		}
	}

	got, err := parseConfig([]string{
		"--token-file", "/run/secrets/opamp-token",
		"--probe-only",
		"--probe-timeout", "250ms",
	})
	if err != nil {
		t.Fatal("parseConfig rejected a positive probe timeout")
	}
	if !got.probeOnly || got.probeTimeout != 250*time.Millisecond {
		t.Fatal("parseConfig did not retain the probe configuration")
	}
}

func TestParseConfigBoundsProbeTimeout(t *testing.T) {
	got, err := parseConfig([]string{
		"--token-file", "/run/secrets/opamp-token",
		"--probe-only",
		"--probe-timeout", "5m",
	})
	if err != nil {
		t.Fatal("parseConfig rejected the maximum probe timeout")
	}
	if got.probeTimeout != 5*time.Minute {
		t.Fatal("parseConfig did not retain the maximum probe timeout")
	}

	_, err = parseConfig([]string{
		"--token-file", "/run/secrets/opamp-token",
		"--probe-only",
		"--probe-timeout", "5m1ns",
	})
	if err == nil {
		t.Fatal("parseConfig accepted a probe timeout above the maximum")
	}
	assertNoSDKAgentSensitiveData(t, err.Error())
}

func TestReadTokenRejectsInvalidFiles(t *testing.T) {
	emptyPath := writeSDKAgentTestFile(t, "empty-token", nil)
	whitespacePath := writeSDKAgentTestFile(t, "whitespace-token", []byte(" \n\t "))

	tests := []struct {
		name string
		path string
	}{
		{name: "missing", path: filepath.Join(t.TempDir(), "missing-token")},
		{name: "directory", path: t.TempDir()},
		{name: "empty", path: emptyPath},
		{name: "whitespace", path: whitespacePath},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := readToken(test.path)
			if err == nil {
				t.Fatal("readToken accepted an invalid token file")
			}
			assertNoSDKAgentSensitiveData(t, err.Error())
		})
	}
}

func TestReadTokenTrimsSurroundingWhitespace(t *testing.T) {
	path := writeSDKAgentTestFile(
		t,
		"trimmed-token",
		[]byte(" \n\t"+sdkAgentTokenSentinel+"\r\n "),
	)

	got, err := readToken(path)
	if err != nil {
		t.Fatal("readToken rejected a valid token file")
	}
	if got != sdkAgentTokenSentinel {
		t.Fatal("readToken did not trim surrounding whitespace exactly once")
	}
}

func TestBuildAuthorizationHeaderUsesOneExactBearerValue(t *testing.T) {
	header := buildAuthorizationHeader(sdkAgentTokenSentinel)
	values := header.Values("Authorization")
	if len(values) != 1 || values[0] != "Bearer "+sdkAgentTokenSentinel {
		t.Fatal("authorization header was not the single exact Bearer value")
	}
	if len(header) != 1 {
		t.Fatal("authorization header contained unexpected fields")
	}
}

func TestBuildTLSConfigAppendsPrivateCAWithoutDisablingVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	certificate := server.Certificate()
	caPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certificate.Raw,
	})
	caPath := writeSDKAgentTestFile(t, "private-ca.pem", caPEM)

	systemPool, _ := x509.SystemCertPool()
	got, err := buildTLSConfig(caPath)
	if err != nil {
		t.Fatal("buildTLSConfig rejected a valid private CA")
	}
	if got == nil || got.RootCAs == nil {
		t.Fatal("buildTLSConfig returned no root pool")
	}
	if got.InsecureSkipVerify {
		t.Fatal("buildTLSConfig disabled certificate verification")
	}
	if got.MinVersion != tls.VersionTLS12 {
		t.Fatal("buildTLSConfig did not require TLS 1.2")
	}
	if !certPoolContainsSubject(got.RootCAs, certificate.RawSubject) {
		t.Fatal("buildTLSConfig did not append the private CA")
	}
	if systemPool != nil {
		for _, subject := range systemPool.Subjects() {
			if !certPoolContainsSubject(got.RootCAs, subject) {
				t.Fatal("buildTLSConfig discarded a system root")
			}
		}
	}
}

func TestBuildStartSettingsSetsSecurityAndRefusesRedirects(t *testing.T) {
	var uid types.InstanceUid
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	settings := buildStartSettings(
		sdkAgentEndpointSentinel,
		sdkAgentTokenSentinel,
		tlsConfig,
		uid,
		types.Callbacks{},
	)

	if settings.OpAMPServerURL != sdkAgentEndpointSentinel {
		t.Fatal("start settings did not retain the endpoint")
	}
	if settings.TLSConfig != tlsConfig {
		t.Fatal("start settings did not retain the TLS config")
	}
	values := settings.Header.Values("Authorization")
	if len(values) != 1 || values[0] != "Bearer "+sdkAgentTokenSentinel {
		t.Fatal("start settings did not contain the exact Bearer header")
	}
	if settings.Callbacks.CheckRedirect == nil {
		t.Fatal("start settings did not install redirect refusal")
	}
	request, err := http.NewRequest(http.MethodGet, "wss://redirect.invalid/v1/opamp", nil)
	if err != nil {
		t.Fatal("create redirect request")
	}
	if err := settings.Callbacks.CheckRedirect(request, nil, nil); err == nil {
		t.Fatal("start settings accepted an HTTP redirect")
	}
}

func TestBuildStartSettingsDoesNotForwardAuthorizationAcrossRedirect(t *testing.T) {
	targetHit := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		select {
		case targetHit <- struct{}{}:
		default:
		}
	}))
	t.Cleanup(target.Close)

	targetURL := "ws" + strings.TrimPrefix(target.URL, "http")
	sourceHit := make(chan struct{}, 1)
	sourceSawWrongHeader := atomic.Bool{}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		values := request.Header.Values("Authorization")
		if len(values) != 1 || values[0] != "Bearer "+sdkAgentTokenSentinel {
			sourceSawWrongHeader.Store(true)
		}
		select {
		case sourceHit <- struct{}{}:
		default:
		}
		w.Header().Set("Location", targetURL)
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(source.Close)

	var uid types.InstanceUid
	uid[0] = 1
	settings := buildStartSettings(
		"ws"+strings.TrimPrefix(source.URL, "http"),
		sdkAgentTokenSentinel,
		nil,
		uid,
		types.Callbacks{},
	)
	opampClient := client.NewWebSocket(nil)
	if err := opampClient.SetAgentDescription(&protobufs.AgentDescription{
		IdentifyingAttributes: []*protobufs.KeyValue{kv("service.name", "redirect-test")},
	}); err != nil {
		t.Fatal("initialize redirect test client")
	}
	if err := opampClient.Start(context.Background(), settings); err != nil {
		t.Fatal("start redirect test client")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = opampClient.Stop(ctx)
	})

	select {
	case <-sourceHit:
	case <-time.After(2 * time.Second):
		t.Fatal("redirect source was not reached")
	}
	select {
	case <-targetHit:
		t.Fatal("redirect target received the authorization request")
	case <-time.After(time.Second):
	}
	if sourceSawWrongHeader.Load() {
		t.Fatal("redirect source received an incorrect authorization header")
	}
}

func TestRunMainProbePrintsOnlyGenericOutcome(t *testing.T) {
	tokenPath := writeSDKAgentTestFile(
		t,
		"probe-token",
		[]byte(sdkAgentTokenSentinel),
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runMainWithDependencies(
		[]string{
			"--token-file", tokenPath,
			"--endpoint", sdkAgentEndpointSentinel,
			"--allow-insecure-transport",
			"--probe-only",
			"--probe-timeout", "200ms",
		},
		&stdout,
		&stderr,
		func() sdkAgentClient {
			t.Fatal("probe initialized a full OpAMP client")
			return nil
		},
		func(
			context.Context,
			string,
			http.Header,
			*tls.Config,
		) (probeCloseFunc, *http.Response, error) {
			response := &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(
					sdkAgentTokenSentinel + sdkAgentRawErrorSentinel,
				)),
			}
			response.Header.Set("WWW-Authenticate", `Bearer realm="opamp"`)
			return nil, response, errors.New(sdkAgentRawErrorSentinel)
		},
	)

	if exitCode != 10 {
		t.Fatal("runMain returned the wrong authentication exit")
	}
	if stdout.Len() != 0 {
		t.Fatal("probe wrote to stdout")
	}
	if stderr.String() != "OpAMP authentication rejected\n" {
		t.Fatal("probe did not write the fixed authentication message")
	}
	assertNoSDKAgentSensitiveData(t, stdout.String())
	assertNoSDKAgentSensitiveData(t, stderr.String())
}

func TestRunMainProbeSendsNoAgentMessage(t *testing.T) {
	tokenPath := writeSDKAgentTestFile(
		t,
		"probe-token",
		[]byte(sdkAgentTokenSentinel),
	)
	type probeObservation struct {
		authorized bool
		message    bool
	}
	observed := make(chan probeObservation, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(w, request, nil)
		if err != nil {
			observed <- probeObservation{}
			return
		}
		defer connection.Close()

		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		_, _, readErr := connection.ReadMessage()
		observed <- probeObservation{
			authorized: request.Header.Get("Authorization") == "Bearer "+sdkAgentTokenSentinel,
			message:    readErr == nil,
		}
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runMainWithDependencies(
		[]string{
			"--token-file", tokenPath,
			"--endpoint", "ws" + strings.TrimPrefix(server.URL, "http"),
			"--allow-insecure-transport",
			"--probe-only",
			"--probe-timeout", "2s",
		},
		&stdout,
		&stderr,
		func() sdkAgentClient {
			t.Fatal("probe initialized a full OpAMP client")
			return client.NewWebSocket(nil)
		},
		dialProbeWebSocket,
	)

	if exitCode != 0 {
		t.Fatalf("runMain probe exit = %d, stderr = %q", exitCode, stderr.String())
	}
	select {
	case got := <-observed:
		if !got.authorized {
			t.Fatal("probe handshake omitted the bearer credential")
		}
		if got.message {
			t.Fatal("probe sent an AgentToServer message after the authenticated handshake")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("probe server did not observe the connection closing")
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatal("successful probe produced output")
	}
}

func TestDialProbeWebSocketDoesNotFollowRedirect(t *testing.T) {
	targetHit := make(chan struct{}, 1)
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetHit <- struct{}{}
	}))
	t.Cleanup(target.Close)

	sourceSawAuthorization := atomic.Bool{}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		sourceSawAuthorization.Store(
			request.Header.Get("Authorization") == "Bearer "+sdkAgentTokenSentinel,
		)
		w.Header().Set("Location", "ws"+strings.TrimPrefix(target.URL, "http"))
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(source.Close)

	closeConnection, response, err := dialProbeWebSocket(
		context.Background(),
		"ws"+strings.TrimPrefix(source.URL, "http"),
		buildAuthorizationHeader(sdkAgentTokenSentinel),
		nil,
	)
	if closeConnection != nil {
		_ = closeConnection()
		t.Fatal("redirect response unexpectedly established a WebSocket")
	}
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusFound {
		t.Fatal("probe dialer did not surface the redirect as a failed handshake")
	}
	if !sourceSawAuthorization.Load() {
		t.Fatal("probe source did not receive the bearer credential")
	}
	select {
	case <-targetHit:
		t.Fatal("probe forwarded the bearer credential across a redirect")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRunProbeSucceedsAfterAuthenticatedHandshake(t *testing.T) {
	connection := &trackingSDKAgentCloser{}
	body := &trackingSDKAgentReadCloser{Reader: strings.NewReader("")}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	dialCalls := atomic.Int32{}

	exitCode, message := runProbe(
		context.Background(),
		sdkAgentEndpointSentinel,
		buildAuthorizationHeader(sdkAgentTokenSentinel),
		tlsConfig,
		200*time.Millisecond,
		func(
			_ context.Context,
			endpoint string,
			header http.Header,
			gotTLSConfig *tls.Config,
		) (probeCloseFunc, *http.Response, error) {
			dialCalls.Add(1)
			if endpoint != sdkAgentEndpointSentinel {
				t.Fatal("probe used a different endpoint")
			}
			values := header.Values("Authorization")
			if len(values) != 1 || values[0] != "Bearer "+sdkAgentTokenSentinel {
				t.Fatal("probe used a different credential header")
			}
			if gotTLSConfig != tlsConfig {
				t.Fatal("probe used a different TLS configuration")
			}
			return connection.Close, &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				Header:     make(http.Header),
				Body:       body,
			}, nil
		},
	)

	if exitCode != 0 || message != "" {
		t.Fatal("authenticated WebSocket handshake did not succeed")
	}
	if dialCalls.Load() != 1 {
		t.Fatal("probe did not perform exactly one handshake")
	}
	if !connection.closed.Load() || !body.closed.Load() {
		t.Fatal("probe did not close all handshake resources")
	}
}

func TestRunProbeRejectsNonUpgradeConnectionResult(t *testing.T) {
	connection := &trackingSDKAgentCloser{}
	exitCode, message := runProbe(
		context.Background(),
		sdkAgentEndpointSentinel,
		buildAuthorizationHeader(sdkAgentTokenSentinel),
		nil,
		200*time.Millisecond,
		func(context.Context, string, http.Header, *tls.Config) (probeCloseFunc, *http.Response, error) {
			return connection.Close, &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	)

	if exitCode != exitProbeTransient || message != probeServiceMessage {
		t.Fatal("probe accepted a connection without a 101 upgrade response")
	}
	if !connection.closed.Load() {
		t.Fatal("probe did not close a rejected connection result")
	}
}

func TestRunProbeTimesOutDuringHandshake(t *testing.T) {
	exitCode, message := runProbe(
		context.Background(),
		sdkAgentEndpointSentinel,
		buildAuthorizationHeader(sdkAgentTokenSentinel),
		nil,
		20*time.Millisecond,
		func(ctx context.Context, _ string, _ http.Header, _ *tls.Config) (probeCloseFunc, *http.Response, error) {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		},
	)

	if exitCode != 11 || message != "OpAMP probe timed out" {
		t.Fatal("probe timeout did not return the stable transient outcome")
	}
	assertNoSDKAgentSensitiveData(t, message)
}

func TestRunProbeClassifiesHandshakeFailures(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		challenge     string
		dialErr       error
		wantExit      int
		wantMessage   string
		wantBodyClose bool
	}{
		{
			name:          "authoritative 401",
			statusCode:    http.StatusUnauthorized,
			challenge:     `Bearer realm="opamp"`,
			dialErr:       errors.New(sdkAgentRawErrorSentinel),
			wantExit:      10,
			wantMessage:   "OpAMP authentication rejected",
			wantBodyClose: true,
		},
		{
			name:          "401 without challenge",
			statusCode:    http.StatusUnauthorized,
			dialErr:       errors.New(sdkAgentRawErrorSentinel),
			wantExit:      11,
			wantMessage:   "OpAMP service unavailable",
			wantBodyClose: true,
		},
		{
			name:          "service unavailable",
			statusCode:    http.StatusServiceUnavailable,
			dialErr:       errors.New(sdkAgentRawErrorSentinel),
			wantExit:      11,
			wantMessage:   "OpAMP service unavailable",
			wantBodyClose: true,
		},
		{
			name:          "other http failure",
			statusCode:    http.StatusTeapot,
			dialErr:       errors.New(sdkAgentRawErrorSentinel),
			wantExit:      11,
			wantMessage:   "OpAMP service unavailable",
			wantBodyClose: true,
		},
		{
			name:        "transport failure",
			dialErr:     errors.New(sdkAgentRawErrorSentinel),
			wantExit:    11,
			wantMessage: "OpAMP transport unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := &trackingSDKAgentReadCloser{
				Reader: strings.NewReader(sdkAgentTokenSentinel),
			}
			tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

			exitCode, message := runProbe(
				context.Background(),
				sdkAgentEndpointSentinel,
				buildAuthorizationHeader(sdkAgentTokenSentinel),
				tlsConfig,
				200*time.Millisecond,
				func(
					_ context.Context,
					endpoint string,
					header http.Header,
					gotTLSConfig *tls.Config,
				) (probeCloseFunc, *http.Response, error) {
					if endpoint != sdkAgentEndpointSentinel {
						t.Fatal("probe handshake used a different endpoint")
					}
					values := header.Values("Authorization")
					if len(values) != 1 || values[0] != "Bearer "+sdkAgentTokenSentinel {
						t.Fatal("probe handshake used a different credential header")
					}
					if gotTLSConfig != tlsConfig {
						t.Fatal("probe handshake used a different TLS config")
					}
					if test.statusCode == 0 {
						return nil, nil, test.dialErr
					}
					response := &http.Response{
						StatusCode: test.statusCode,
						Header:     make(http.Header),
						Body:       body,
					}
					if test.challenge != "" {
						response.Header.Set("WWW-Authenticate", test.challenge)
					}
					return nil, response, test.dialErr
				},
			)

			if exitCode != test.wantExit || message != test.wantMessage {
				t.Fatal("probe returned an unstable handshake outcome")
			}
			if body.closed.Load() != test.wantBodyClose {
				t.Fatal("probe did not close the handshake response body")
			}
			assertNoSDKAgentSensitiveData(t, message)
		})
	}
}

func TestAgentCapabilitiesRemainReadOnlyByDefault(t *testing.T) {
	got := agentCapabilities(false)
	want := protobufs.AgentCapabilities_AgentCapabilities_ReportsStatus
	if got != want {
		t.Fatalf("agentCapabilities(false) = %v, want %v", got, want)
	}
}

func TestAgentCapabilitiesEnableRemoteConfigReportingExplicitly(t *testing.T) {
	got := agentCapabilities(true)
	for _, capability := range []protobufs.AgentCapabilities{
		protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig,
		protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig,
		protobufs.AgentCapabilities_AgentCapabilities_ReportsEffectiveConfig,
	} {
		if got&capability == 0 {
			t.Fatalf("agentCapabilities(true) = %v, missing %v", got, capability)
		}
	}
}

func TestRemoteConfigStateApplyRejectsMissingHash(t *testing.T) {
	state := &remoteConfigState{}
	status, err := state.apply(&protobufs.AgentRemoteConfig{
		Config: &protobufs.AgentConfigMap{},
	})
	if err == nil {
		t.Fatal("apply returned nil error for a remote config without a hash")
	}
	if status != nil {
		t.Fatalf("apply status = %#v, want nil", status)
	}
}

func TestRemoteConfigStateApplyRejectsMissingConfig(t *testing.T) {
	state := &remoteConfigState{}
	status, err := state.apply(&protobufs.AgentRemoteConfig{
		ConfigHash: []byte("remote-config-hash"),
	})
	if err == nil {
		t.Fatal("apply returned nil error for a remote config without content")
	}
	if status != nil {
		t.Fatalf("apply status = %#v, want nil", status)
	}
}

func TestRemoteConfigStateApplyStoresIndependentEffectiveConfig(t *testing.T) {
	state := &remoteConfigState{}
	remote := &protobufs.AgentRemoteConfig{
		ConfigHash: []byte("remote-config-hash"),
		Config: &protobufs.AgentConfigMap{ConfigMap: map[string]*protobufs.AgentConfigFile{
			"": {
				Body:        []byte("receivers:\n  otlp: {}\n"),
				ContentType: "text/yaml",
			},
		}},
	}

	status, err := state.apply(remote)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if status.Status != protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED {
		t.Fatalf("status = %v, want APPLIED", status.Status)
	}
	if !bytes.Equal(status.LastRemoteConfigHash, remote.ConfigHash) {
		t.Fatalf("status hash = %q, want %q", status.LastRemoteConfigHash, remote.ConfigHash)
	}

	remote.Config.ConfigMap[""].Body[0] = 'X'
	first, err := state.effectiveConfig(context.Background())
	if err != nil {
		t.Fatalf("effectiveConfig: %v", err)
	}
	wantBody := []byte("receivers:\n  otlp: {}\n")
	if got := first.ConfigMap.ConfigMap[""].Body; !bytes.Equal(got, wantBody) {
		t.Fatalf("effective body = %q, want %q", got, wantBody)
	}

	first.ConfigMap.ConfigMap[""].Body[0] = 'Y'
	second, err := state.effectiveConfig(context.Background())
	if err != nil {
		t.Fatalf("second effectiveConfig: %v", err)
	}
	if got := second.ConfigMap.ConfigMap[""].Body; !bytes.Equal(got, wantBody) {
		t.Fatalf("effectiveConfig returned shared mutable state: %q", got)
	}
}

type trackingSDKAgentCloser struct {
	closed atomic.Bool
}

func (c *trackingSDKAgentCloser) Close() error {
	c.closed.Store(true)
	return nil
}

type trackingSDKAgentReadCloser struct {
	io.Reader
	closed atomic.Bool
}

func (c *trackingSDKAgentReadCloser) Close() error {
	c.closed.Store(true)
	return nil
}

func assertNoSDKAgentSensitiveData(t *testing.T, output string) {
	t.Helper()
	for _, forbidden := range []string{
		sdkAgentTokenSentinel,
		sdkAgentEndpointSentinel,
		sdkAgentRawErrorSentinel,
		"Authorization",
		"Bearer " + sdkAgentTokenSentinel,
	} {
		if strings.Contains(output, forbidden) {
			t.Fatal("output exposed sensitive connection data")
		}
	}
}

func writeSDKAgentTestFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal("write private test file")
	}
	return path
}

func certPoolContainsSubject(pool *x509.CertPool, subject []byte) bool {
	for _, candidate := range pool.Subjects() {
		if bytes.Equal(candidate, subject) {
			return true
		}
	}
	return false
}
