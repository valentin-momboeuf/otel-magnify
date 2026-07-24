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
	fakeClient := &fakeSDKAgentProbeClient{
		onStart: func(settings types.StartSettings) {
			settings.Callbacks.OnConnectFailed(
				context.Background(),
				errors.New(sdkAgentRawErrorSentinel),
			)
		},
	}
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
			return fakeClient
		},
		func(
			context.Context,
			string,
			http.Header,
			*tls.Config,
		) (io.Closer, *http.Response, error) {
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
	assertSDKAgentProbeStopped(t, fakeClient)
}

func TestRunProbeSucceedsOnlyAfterRealOnConnect(t *testing.T) {
	client := &fakeSDKAgentProbeClient{
		onStart: func(settings types.StartSettings) {
			settings.Callbacks.OnConnect(context.Background())
		},
	}
	settings := sdkAgentProbeStartSettings()
	dialCalls := atomic.Int32{}

	exitCode, message := runProbe(
		context.Background(),
		client,
		settings,
		200*time.Millisecond,
		func(context.Context, string, http.Header, *tls.Config) (io.Closer, *http.Response, error) {
			dialCalls.Add(1)
			return nil, nil, errors.New(sdkAgentRawErrorSentinel)
		},
	)

	if exitCode != 0 {
		t.Fatal("probe did not succeed after the real OnConnect callback")
	}
	if message != "" {
		t.Fatal("successful probe returned output")
	}
	if dialCalls.Load() != 0 {
		t.Fatal("successful probe performed a diagnostic handshake")
	}
	assertSDKAgentProbeStopped(t, client)
}

func TestRunProbeTimesOutWithoutOnConnect(t *testing.T) {
	client := &fakeSDKAgentProbeClient{}
	exitCode, message := runProbe(
		context.Background(),
		client,
		sdkAgentProbeStartSettings(),
		20*time.Millisecond,
		func(context.Context, string, http.Header, *tls.Config) (io.Closer, *http.Response, error) {
			t.Fatal("timeout probe performed an unexpected diagnostic handshake")
			return nil, nil, nil
		},
	)

	if exitCode != 11 || message != "OpAMP probe timed out" {
		t.Fatal("probe timeout did not return the stable transient outcome")
	}
	assertNoSDKAgentSensitiveData(t, message)
	assertSDKAgentProbeStopped(t, client)
}

func TestRunProbeClassifiesDiagnosticFailures(t *testing.T) {
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
			client := &fakeSDKAgentProbeClient{
				onStart: func(settings types.StartSettings) {
					settings.Callbacks.OnConnectFailed(
						context.Background(),
						errors.New(sdkAgentRawErrorSentinel),
					)
				},
			}
			body := &trackingSDKAgentReadCloser{
				Reader: strings.NewReader(sdkAgentTokenSentinel),
			}

			exitCode, message := runProbe(
				context.Background(),
				client,
				sdkAgentProbeStartSettings(),
				200*time.Millisecond,
				func(
					_ context.Context,
					endpoint string,
					header http.Header,
					tlsConfig *tls.Config,
				) (io.Closer, *http.Response, error) {
					if endpoint != sdkAgentEndpointSentinel {
						t.Fatal("diagnostic handshake used a different endpoint")
					}
					values := header.Values("Authorization")
					if len(values) != 1 || values[0] != "Bearer "+sdkAgentTokenSentinel {
						t.Fatal("diagnostic handshake used a different credential header")
					}
					if tlsConfig == nil || tlsConfig.MinVersion != tls.VersionTLS12 {
						t.Fatal("diagnostic handshake used a different TLS config")
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
				t.Fatal("probe returned an unstable diagnostic outcome")
			}
			if body.closed.Load() != test.wantBodyClose {
				t.Fatal("probe did not close the diagnostic response body")
			}
			assertNoSDKAgentSensitiveData(t, message)
			assertSDKAgentProbeStopped(t, client)
		})
	}
}

func TestRunProbeDeduplicatesFailedCallbacks(t *testing.T) {
	client := &fakeSDKAgentProbeClient{
		onStart: func(settings types.StartSettings) {
			settings.Callbacks.OnConnectFailed(
				context.Background(),
				errors.New(sdkAgentRawErrorSentinel),
			)
			settings.Callbacks.OnConnectFailed(
				context.Background(),
				errors.New(sdkAgentRawErrorSentinel),
			)
		},
	}
	dialCalls := atomic.Int32{}

	exitCode, _ := runProbe(
		context.Background(),
		client,
		sdkAgentProbeStartSettings(),
		200*time.Millisecond,
		func(context.Context, string, http.Header, *tls.Config) (io.Closer, *http.Response, error) {
			dialCalls.Add(1)
			return nil, &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}, errors.New(sdkAgentRawErrorSentinel)
		},
	)

	if exitCode != 11 {
		t.Fatal("duplicate failure probe returned the wrong exit")
	}
	if dialCalls.Load() != 1 {
		t.Fatal("duplicate failure callbacks triggered multiple diagnostics")
	}
	assertSDKAgentProbeStopped(t, client)
}

func TestRunProbeDiagnosticSuccessDoesNotCountAsProbeSuccess(t *testing.T) {
	client := &fakeSDKAgentProbeClient{
		onStart: func(settings types.StartSettings) {
			settings.Callbacks.OnConnectFailed(
				context.Background(),
				errors.New(sdkAgentRawErrorSentinel),
			)
		},
	}
	connection := &trackingSDKAgentCloser{}

	exitCode, message := runProbe(
		context.Background(),
		client,
		sdkAgentProbeStartSettings(),
		20*time.Millisecond,
		func(context.Context, string, http.Header, *tls.Config) (io.Closer, *http.Response, error) {
			return connection, &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	)

	if exitCode != 11 || message != "OpAMP probe timed out" {
		t.Fatal("diagnostic handshake was incorrectly treated as probe success")
	}
	if !connection.closed.Load() {
		t.Fatal("probe did not close the diagnostic connection")
	}
	assertSDKAgentProbeStopped(t, client)
}

func TestRunProbePrefersRacingRealConnection(t *testing.T) {
	var observed types.StartSettings
	client := &fakeSDKAgentProbeClient{
		onStart: func(settings types.StartSettings) {
			observed = settings
			settings.Callbacks.OnConnectFailed(
				context.Background(),
				errors.New(sdkAgentRawErrorSentinel),
			)
		},
	}

	exitCode, message := runProbe(
		context.Background(),
		client,
		sdkAgentProbeStartSettings(),
		200*time.Millisecond,
		func(context.Context, string, http.Header, *tls.Config) (io.Closer, *http.Response, error) {
			observed.Callbacks.OnConnect(context.Background())
			return nil, &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}, errors.New(sdkAgentRawErrorSentinel)
		},
	)

	if exitCode != 0 || message != "" {
		t.Fatal("probe did not prefer the racing real connection")
	}
	assertSDKAgentProbeStopped(t, client)
}

func TestRunProbePrefersRealConnectionDeliveredWhileStopping(t *testing.T) {
	var observed types.StartSettings
	client := &fakeSDKAgentProbeClient{
		onStart: func(settings types.StartSettings) {
			observed = settings
			settings.Callbacks.OnConnectFailed(
				context.Background(),
				errors.New(sdkAgentRawErrorSentinel),
			)
		},
	}
	client.onStop = func() {
		observed.Callbacks.OnConnect(context.Background())
	}

	exitCode, message := runProbe(
		context.Background(),
		client,
		sdkAgentProbeStartSettings(),
		200*time.Millisecond,
		func(context.Context, string, http.Header, *tls.Config) (io.Closer, *http.Response, error) {
			response := &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}
			response.Header.Set("WWW-Authenticate", opampBearerChallenge)
			return nil, response, errors.New(sdkAgentRawErrorSentinel)
		},
	)

	if exitCode != 0 || message != "" {
		t.Fatal("probe ignored a real connection delivered while the client stopped")
	}
	assertSDKAgentProbeStopped(t, client)
}

func TestRunProbeTreatsStopFailureAsTransient(t *testing.T) {
	client := &fakeSDKAgentProbeClient{
		onStart: func(settings types.StartSettings) {
			settings.Callbacks.OnConnect(context.Background())
		},
		stopErr: errors.New(sdkAgentRawErrorSentinel),
	}

	exitCode, message := runProbe(
		context.Background(),
		client,
		sdkAgentProbeStartSettings(),
		200*time.Millisecond,
		func(context.Context, string, http.Header, *tls.Config) (io.Closer, *http.Response, error) {
			t.Fatal("successful probe performed a diagnostic handshake")
			return nil, nil, nil
		},
	)

	if exitCode != exitProbeTransient || message != probeTransportMessage {
		t.Fatal("probe did not classify a client stop failure as transient")
	}
	assertNoSDKAgentSensitiveData(t, message)
	assertSDKAgentProbeStopped(t, client)
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

type fakeSDKAgentProbeClient struct {
	onStart   func(types.StartSettings)
	onStop    func()
	stopErr   error
	stopCalls atomic.Int32
}

func (c *fakeSDKAgentProbeClient) Start(_ context.Context, settings types.StartSettings) error {
	if c.onStart != nil {
		c.onStart(settings)
	}
	return nil
}

func (c *fakeSDKAgentProbeClient) Stop(context.Context) error {
	c.stopCalls.Add(1)
	if c.onStop != nil {
		c.onStop()
	}
	return c.stopErr
}

func (c *fakeSDKAgentProbeClient) SetAgentDescription(*protobufs.AgentDescription) error {
	return nil
}

func (c *fakeSDKAgentProbeClient) SetCapabilities(*protobufs.AgentCapabilities) error {
	return nil
}

func (c *fakeSDKAgentProbeClient) SetRemoteConfigStatus(*protobufs.RemoteConfigStatus) error {
	return nil
}

func (c *fakeSDKAgentProbeClient) UpdateEffectiveConfig(context.Context) error {
	return nil
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

func sdkAgentProbeStartSettings() types.StartSettings {
	var uid types.InstanceUid
	return buildStartSettings(
		sdkAgentEndpointSentinel,
		sdkAgentTokenSentinel,
		&tls.Config{MinVersion: tls.VersionTLS12},
		uid,
		types.Callbacks{},
	)
}

func assertSDKAgentProbeStopped(t *testing.T, client *fakeSDKAgentProbeClient) {
	t.Helper()
	if client.stopCalls.Load() != 1 {
		t.Fatal("probe did not stop its client exactly once")
	}
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
