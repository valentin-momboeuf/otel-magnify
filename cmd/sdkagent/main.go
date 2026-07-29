// sdkagent is a minimal OpAMP client that simulates an SDK-instrumented service.
// Used for local development and demo purposes only.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/open-telemetry/opamp-go/client"
	"github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"
	"google.golang.org/protobuf/proto"
)

const (
	defaultProbeTimeout = 10 * time.Second
	maxProbeTimeout     = 5 * time.Minute
	clientStopTimeout   = 5 * time.Second

	exitInvalidInput      = 2
	exitProbeAuthRejected = 10
	exitProbeTransient    = 11
	// #nosec G101 -- this is a protocol challenge literal, not a credential.
	opampBearerChallenge     = `Bearer realm="opamp"`
	probeAuthMessage         = "OpAMP authentication rejected"
	probeServiceMessage      = "OpAMP service unavailable"
	probeTransportMessage    = "OpAMP transport unavailable"
	probeTimeoutMessage      = "OpAMP probe timed out"
	unexpectedArgumentsError = "unexpected positional arguments"
)

type config struct {
	name                   string
	version                string
	env                    string
	endpoint               string
	tokenFile              string
	tlsCAFile              string
	acceptRemoteConfig     bool
	allowInsecureTransport bool
	probeOnly              bool
	probeTimeout           time.Duration
}

type sdkAgentClient interface {
	Start(context.Context, types.StartSettings) error
	Stop(context.Context) error
	SetAgentDescription(*protobufs.AgentDescription) error
	SetCapabilities(*protobufs.AgentCapabilities) error
	SetRemoteConfigStatus(*protobufs.RemoteConfigStatus) error
	UpdateEffectiveConfig(context.Context) error
}

type clientFactory func() sdkAgentClient

type probeCloseFunc func() error

type probeDialFunc func(
	context.Context,
	string,
	http.Header,
	*tls.Config,
) (probeCloseFunc, *http.Response, error)

type remoteConfigState struct {
	mu        sync.RWMutex
	effective *protobufs.AgentConfigMap
}

func (s *remoteConfigState) apply(remote *protobufs.AgentRemoteConfig) (*protobufs.RemoteConfigStatus, error) {
	if remote == nil || len(remote.ConfigHash) == 0 {
		return nil, errors.New("remote config hash is required")
	}
	if remote.Config == nil {
		return nil, errors.New("remote config content is required")
	}

	effective := proto.Clone(remote.Config).(*protobufs.AgentConfigMap)
	s.mu.Lock()
	s.effective = effective
	s.mu.Unlock()

	return &protobufs.RemoteConfigStatus{
		LastRemoteConfigHash: bytes.Clone(remote.ConfigHash),
		Status:               protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED,
	}, nil
}

func (s *remoteConfigState) effectiveConfig(_ context.Context) (*protobufs.EffectiveConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.effective == nil {
		return nil, nil
	}
	return &protobufs.EffectiveConfig{
		ConfigMap: proto.Clone(s.effective).(*protobufs.AgentConfigMap),
	}, nil
}

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

func runMain(args []string, stdout io.Writer, stderr io.Writer) int {
	return runMainWithDependencies(
		args,
		stdout,
		stderr,
		func() sdkAgentClient {
			return client.NewWebSocket(nil)
		},
		dialProbeWebSocket,
	)
}

func runMainWithDependencies(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	newClient clientFactory,
	probeDial probeDialFunc,
) int {
	cfg, err := parseConfig(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitInvalidInput
	}

	token, err := readToken(cfg.tokenFile)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitInvalidInput
	}
	tlsConfig, err := buildTLSConfig(cfg.tlsCAFile)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitInvalidInput
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return run(ctx, cfg, token, tlsConfig, stdout, stderr, newClient, probeDial)
}

func parseConfig(args []string) (config, error) {
	flags := flag.NewFlagSet("sdkagent", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	cfg := config{}
	flags.StringVar(&cfg.name, "name", "my-sdk-service", "Service name reported to OpAMP server")
	flags.StringVar(&cfg.version, "version", "1.0.0", "Service version")
	flags.StringVar(&cfg.env, "env", "dev", "Deployment environment label")
	flags.StringVar(&cfg.endpoint, "endpoint", "wss://localhost:4320/v1/opamp", "OpAMP server WebSocket endpoint")
	flags.StringVar(&cfg.tokenFile, "token-file", "", "Path to the OpAMP bearer token")
	flags.StringVar(&cfg.tlsCAFile, "tls-ca-file", "", "Path to a private PEM certificate authority")
	flags.BoolVar(&cfg.acceptRemoteConfig, "accept-remote-config", false, "Accept remote config for local activation testing")
	flags.BoolVar(&cfg.allowInsecureTransport, "allow-insecure-transport", false, "Allow plaintext WebSocket transport")
	flags.BoolVar(&cfg.probeOnly, "probe-only", false, "Exit after probing the OpAMP connection")
	flags.DurationVar(&cfg.probeTimeout, "probe-timeout", defaultProbeTimeout, "Maximum time allowed for a connection probe")

	if err := flags.Parse(args); err != nil {
		return config{}, errors.New("invalid command line arguments")
	}
	if flags.NArg() != 0 {
		return config{}, errors.New(unexpectedArgumentsError)
	}
	if strings.TrimSpace(cfg.tokenFile) == "" {
		return config{}, errors.New("token file is required")
	}
	if cfg.probeTimeout <= 0 {
		return config{}, errors.New("probe timeout must be positive")
	}
	if cfg.probeTimeout > maxProbeTimeout {
		return config{}, errors.New("probe timeout exceeds maximum")
	}

	endpointURL, err := url.Parse(cfg.endpoint)
	if err != nil ||
		endpointURL.Host == "" ||
		endpointURL.User != nil ||
		endpointURL.RawQuery != "" ||
		endpointURL.Fragment != "" ||
		strings.TrimSpace(cfg.endpoint) != cfg.endpoint {
		return config{}, errors.New("invalid OpAMP endpoint")
	}

	switch endpointURL.Scheme {
	case "ws":
		if !cfg.allowInsecureTransport {
			return config{}, errors.New("insecure transport requires explicit opt-in")
		}
		if cfg.tlsCAFile != "" {
			return config{}, errors.New("TLS CA file requires secure transport")
		}
	case "wss":
		if cfg.allowInsecureTransport {
			return config{}, errors.New("insecure transport opt-in requires a ws endpoint")
		}
	default:
		return config{}, errors.New("invalid OpAMP endpoint scheme")
	}

	return cfg, nil
}

func readToken(path string) (string, error) {
	// #nosec G304 -- the operator supplies the token-file path.
	content, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("token file is unreadable")
	}
	token := strings.TrimSpace(string(content))
	if token == "" {
		return "", errors.New("token file is empty")
	}
	return token, nil
}

func buildAuthorizationHeader(token string) http.Header {
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+token)
	return header
}

func buildTLSConfig(caPath string) (*tls.Config, error) {
	if caPath == "" {
		return nil, nil
	}

	// #nosec G304 -- the operator supplies the private CA path.
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, errors.New("TLS CA file is unreadable")
	}

	rootCAs, systemPoolErr := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	} else if systemPoolErr != nil {
		return nil, errors.New("system certificate pool is unavailable")
	}
	if !rootCAs.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("TLS CA file contains no certificates")
	}

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootCAs,
	}, nil
}

func buildStartSettings(
	endpoint string,
	token string,
	tlsConfig *tls.Config,
	instanceUID types.InstanceUid,
	callbacks types.Callbacks,
) types.StartSettings {
	callbacks.CheckRedirect = func(*http.Request, []*http.Request, []*http.Response) error {
		return errors.New("OpAMP redirects are not allowed")
	}
	return types.StartSettings{
		OpAMPServerURL: endpoint,
		Header:         buildAuthorizationHeader(token),
		TLSConfig:      tlsConfig,
		InstanceUid:    instanceUID,
		Callbacks:      callbacks,
	}
}

func run(
	ctx context.Context,
	cfg config,
	token string,
	tlsConfig *tls.Config,
	stdout io.Writer,
	stderr io.Writer,
	newClient clientFactory,
	probeDial probeDialFunc,
) int {
	if cfg.probeOnly {
		exitCode, message := runProbe(
			ctx,
			cfg.endpoint,
			buildAuthorizationHeader(token),
			tlsConfig,
			cfg.probeTimeout,
			probeDial,
		)
		if message != "" {
			_, _ = fmt.Fprintln(stderr, message)
		}
		return exitCode
	}

	opampClient := newClient()
	if opampClient == nil {
		_, _ = fmt.Fprintln(stderr, "initialize OpAMP client")
		return 1
	}

	if err := opampClient.SetAgentDescription(&protobufs.AgentDescription{
		IdentifyingAttributes: []*protobufs.KeyValue{
			kv("service.name", cfg.name),
			kv("service.version", cfg.version),
		},
		NonIdentifyingAttributes: []*protobufs.KeyValue{
			kv("deployment.environment", cfg.env),
		},
	}); err != nil {
		_, _ = fmt.Fprintln(stderr, "initialize OpAMP client")
		return 1
	}

	capabilities := agentCapabilities(cfg.acceptRemoteConfig)
	if err := opampClient.SetCapabilities(&capabilities); err != nil {
		_, _ = fmt.Fprintln(stderr, "initialize OpAMP client")
		return 1
	}

	var uid types.InstanceUid
	if _, err := rand.Read(uid[:]); err != nil {
		_, _ = fmt.Fprintln(stderr, "initialize OpAMP client")
		return 1
	}

	logger := log.New(stdout, "", log.LstdFlags)
	remoteState := &remoteConfigState{}
	settings := buildStartSettings(
		cfg.endpoint,
		token,
		tlsConfig,
		uid,
		types.Callbacks{
			GetEffectiveConfig: remoteState.effectiveConfig,
			OnConnect: func(_ context.Context) {
				logger.Print("connected")
			},
			OnConnectFailed: func(context.Context, error) {
				logger.Print("connection unavailable")
			},
			OnError: func(context.Context, *protobufs.ServerErrorResponse) {
				logger.Print("server error")
			},
			OnMessage: func(ctx context.Context, msg *types.MessageData) {
				if !cfg.acceptRemoteConfig || msg == nil || msg.RemoteConfig == nil {
					return
				}
				status, err := remoteState.apply(msg.RemoteConfig)
				if err != nil {
					logger.Print("remote config rejected")
					return
				}
				if err := opampClient.SetRemoteConfigStatus(status); err != nil {
					logger.Print("remote config reporting failed")
					return
				}
				if err := opampClient.UpdateEffectiveConfig(ctx); err != nil {
					logger.Print("remote config reporting failed")
					return
				}
				logger.Print("remote config applied")
			},
		},
	)

	if err := opampClient.Start(ctx, settings); err != nil {
		_, _ = fmt.Fprintln(stderr, "start OpAMP client")
		return 1
	}

	<-ctx.Done()

	logger.Print("shutting down")
	if err := stopClient(opampClient); err != nil {
		_, _ = fmt.Fprintln(stderr, "stop OpAMP client")
		return 1
	}
	return 0
}

func runProbe(
	ctx context.Context,
	endpoint string,
	header http.Header,
	tlsConfig *tls.Config,
	timeout time.Duration,
	probeDial probeDialFunc,
) (int, string) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	closeConnection, response, err := probeDial(
		probeCtx,
		endpoint,
		header.Clone(),
		tlsConfig,
	)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if closeConnection != nil {
		_ = closeConnection()
	}
	if closeConnection != nil && err == nil && response != nil &&
		response.StatusCode == http.StatusSwitchingProtocols {
		return 0, ""
	}
	if probeCtx.Err() != nil {
		return probeContextOutcome(probeCtx)
	}
	if response == nil {
		return exitProbeTransient, probeTransportMessage
	}
	if isAuthoritativeUnauthorized(response) {
		return exitProbeAuthRejected, probeAuthMessage
	}
	return exitProbeTransient, probeServiceMessage
}

func probeContextOutcome(ctx context.Context) (int, string) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return exitProbeTransient, probeTimeoutMessage
	}
	return exitProbeTransient, probeTransportMessage
}

func isAuthoritativeUnauthorized(response *http.Response) bool {
	if response.StatusCode != http.StatusUnauthorized {
		return false
	}
	challenges := response.Header.Values("WWW-Authenticate")
	return len(challenges) == 1 && challenges[0] == opampBearerChallenge
}

func dialProbeWebSocket(
	ctx context.Context,
	endpoint string,
	header http.Header,
	tlsConfig *tls.Config,
) (probeCloseFunc, *http.Response, error) {
	dialer := *websocket.DefaultDialer
	dialer.TLSClientConfig = tlsConfig
	connection, response, err := dialer.DialContext(ctx, endpoint, header)
	if connection == nil {
		return nil, response, err
	}
	return connection.Close, response, err
}

func stopClient(opampClient sdkAgentClient) error {
	ctx, cancel := context.WithTimeout(context.Background(), clientStopTimeout)
	defer cancel()
	return opampClient.Stop(ctx)
}

func agentCapabilities(acceptRemoteConfig bool) protobufs.AgentCapabilities {
	capabilities := protobufs.AgentCapabilities_AgentCapabilities_ReportsStatus
	if acceptRemoteConfig {
		capabilities |= protobufs.AgentCapabilities_AgentCapabilities_AcceptsRemoteConfig |
			protobufs.AgentCapabilities_AgentCapabilities_ReportsRemoteConfig |
			protobufs.AgentCapabilities_AgentCapabilities_ReportsEffectiveConfig
	}
	return capabilities
}

func kv(key, val string) *protobufs.KeyValue {
	return &protobufs.KeyValue{
		Key: key,
		Value: &protobufs.AnyValue{
			Value: &protobufs.AnyValue_StringValue{StringValue: val},
		},
	}
}
