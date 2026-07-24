// opamp-load establishes a reproducible number of OpAMP collector connections.
package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/open-telemetry/opamp-go/client"
	"github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"
)

const (
	connectTimeout = 30 * time.Second
	stopTimeout    = 5 * time.Second
)

type config struct {
	endpoint   string
	tokenFile  string
	tlsConfig  *tls.Config
	collectors int
	ramp       time.Duration
	hold       time.Duration
	readyFile  string
}

type summary struct {
	Attempted    uint64 `json:"attempted"`
	Connected    uint64 `json:"connected"`
	Failed       uint64 `json:"failed"`
	Cancelled    uint64 `json:"cancelled"`
	Disconnected uint64 `json:"disconnected"`
	StopFailed   uint64 `json:"stop_failed"`
	Interrupted  bool   `json:"interrupted"`
}

type counters struct {
	attempted    atomic.Uint64
	connected    atomic.Uint64
	failed       atomic.Uint64
	cancelled    atomic.Uint64
	disconnected atomic.Uint64
	stopFailed   atomic.Uint64
}

type collectorFunc func(context.Context, string, string, *tls.Config, <-chan struct{}, *sync.WaitGroup, *sync.WaitGroup, *counters)

func main() {
	os.Exit(runMain())
}

func runMain() int {
	config, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	token, err := readTokenFile(config.tokenFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	result, runErr := run(ctx, config, token)
	stop()
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, "write summary failed")
		return 1
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "load test failed")
		return 1
	}
	if result.Interrupted {
		return 130
	}
	if result.Failed != 0 || result.Cancelled != 0 || result.StopFailed != 0 || result.Connected != result.Attempted || result.Disconnected != result.Connected {
		return 1
	}
	return 0
}

func parseConfig(args []string) (config, error) {
	flags := flag.NewFlagSet("opamp-load", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	config := config{}
	var allowInsecureTransport bool
	var tlsCAFile string
	flags.StringVar(&config.endpoint, "endpoint", "wss://localhost:4320/v1/opamp", "OpAMP WebSocket endpoint")
	flags.StringVar(&config.tokenFile, "token-file", "", "path to the OpAMP bearer token")
	flags.BoolVar(&allowInsecureTransport, "allow-insecure-transport", false, "allow plaintext WebSocket transport")
	flags.StringVar(&tlsCAFile, "tls-ca-file", "", "path to an additional private CA")
	flags.IntVar(&config.collectors, "collectors", 5000, "number of collectors to connect")
	flags.DurationVar(&config.ramp, "ramp", 5*time.Minute, "time used to start all collectors")
	flags.DurationVar(&config.hold, "hold", 10*time.Minute, "time to hold connections after they are established")
	flags.StringVar(&config.readyFile, "ready-file", "", "write a JSON summary after all collectors connect")

	if err := flags.Parse(args); err != nil {
		return config, errors.New("invalid command-line arguments")
	}
	if flags.NArg() != 0 {
		return config, errors.New("unexpected positional arguments")
	}
	if config.tokenFile == "" {
		return config, errors.New("token file is required")
	}
	endpointURL, err := url.Parse(config.endpoint)
	if err != nil ||
		endpointURL.Host == "" ||
		endpointURL.RawQuery != "" ||
		endpointURL.Fragment != "" ||
		strings.TrimSpace(config.endpoint) != config.endpoint {
		return config, errors.New("endpoint is invalid")
	}
	if endpointURL.User != nil {
		return config, errors.New("endpoint user info is not allowed")
	}
	switch endpointURL.Scheme {
	case "ws":
		if !allowInsecureTransport {
			return config, errors.New("plaintext transport requires explicit opt-in")
		}
		if tlsCAFile != "" {
			return config, errors.New("TLS CA cannot be used with plaintext transport")
		}
	case "wss":
		if allowInsecureTransport {
			return config, errors.New("insecure transport opt-in requires plaintext transport")
		}
	default:
		return config, errors.New("endpoint scheme must be ws or wss")
	}
	if config.collectors <= 0 {
		return config, errors.New("collectors must be greater than zero")
	}
	if config.ramp < 0 {
		return config, errors.New("ramp must not be negative")
	}
	if config.hold < 0 {
		return config, errors.New("hold must not be negative")
	}
	config.tlsConfig, err = buildTLSConfig(tlsCAFile)
	if err != nil {
		return config, err
	}

	return config, nil
}

func readTokenFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", errors.New("token file is unavailable")
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("token file is unavailable")
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return "", errors.New("token file is unavailable")
	}
	token := strings.TrimSpace(string(content))
	if token == "" {
		return "", errors.New("token file is empty")
	}
	return token, nil
}

func buildAuthorizationHeader(token string) http.Header {
	header := make(http.Header, 1)
	header.Set("Authorization", "Bearer "+token)
	return header
}

func buildTLSConfig(caPath string) (*tls.Config, error) {
	if caPath == "" {
		return nil, nil
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, errors.New("TLS CA file is unavailable")
	}
	rootCAs, systemPoolErr := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	} else if systemPoolErr != nil {
		return nil, errors.New("system certificate pool is unavailable")
	}
	if !rootCAs.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("TLS CA file is invalid")
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootCAs,
	}, nil
}

func rejectRedirect(_ *http.Request, _ []*http.Request) error {
	return errors.New("redirect refused")
}

func buildStartSettings(
	endpoint string,
	token string,
	tlsConfig *tls.Config,
	instanceUID types.InstanceUid,
	callbacks types.Callbacks,
) types.StartSettings {
	callbacks.CheckRedirect = func(request *http.Request, viaRequests []*http.Request, _ []*http.Response) error {
		return rejectRedirect(request, viaRequests)
	}
	return types.StartSettings{
		OpAMPServerURL: endpoint,
		Header:         buildAuthorizationHeader(token),
		TLSConfig:      tlsConfig,
		InstanceUid:    instanceUID,
		Callbacks:      callbacks,
	}
}

func run(ctx context.Context, config config, token string) (summary, error) {
	return runWithReporter(ctx, config, token, runCollector, func(ready summary) error {
		return writeSummary(config.readyFile, ready)
	})
}

func runWithCollector(ctx context.Context, config config, token string, collector collectorFunc) summary {
	result, _ := runWithReporter(ctx, config, token, collector, nil)
	return result
}

func runWithReporter(
	ctx context.Context,
	config config,
	token string,
	collector collectorFunc,
	reportReady func(summary) error,
) (summary, error) {
	var counters counters
	var ready sync.WaitGroup
	var workers sync.WaitGroup
	stop := make(chan struct{})
	startedAt := time.Now()

	for index := 0; index < config.collectors; index++ {
		if ctx.Err() != nil {
			break
		}
		counters.attempted.Add(1)
		ready.Add(1)
		workers.Add(1)
		go collector(ctx, config.endpoint, token, config.tlsConfig, stop, &ready, &workers, &counters)

		if config.ramp == 0 {
			continue
		}
		nextStart := startedAt.Add(time.Duration(index+1) * config.ramp / time.Duration(config.collectors))
		if delay := time.Until(nextStart); delay > 0 {
			if !wait(ctx, delay) {
				break
			}
		}
	}

	ready.Wait()
	if ctx.Err() == nil && counters.failed.Load() == 0 && counters.cancelled.Load() == 0 {
		readySummary := snapshot(&counters, false)
		if reportReady != nil {
			if err := reportReady(readySummary); err != nil {
				close(stop)
				workers.Wait()
				return snapshot(&counters, ctx.Err() != nil), fmt.Errorf("write ready summary: %w", err)
			}
		}
		wait(ctx, config.hold)
	}
	close(stop)
	workers.Wait()

	return snapshot(&counters, ctx.Err() != nil), nil
}

func snapshot(counters *counters, interrupted bool) summary {
	return summary{
		Attempted:    counters.attempted.Load(),
		Connected:    counters.connected.Load(),
		Failed:       counters.failed.Load(),
		Cancelled:    counters.cancelled.Load(),
		Disconnected: counters.disconnected.Load(),
		StopFailed:   counters.stopFailed.Load(),
		Interrupted:  interrupted,
	}
}

func wait(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		return ctx.Err() == nil
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func stopCollector(opampClient client.OpAMPClient) error {
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()

	return opampClient.Stop(ctx)
}

func writeSummary(path string, result summary) error {
	if path == "" {
		return nil
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".opamp-load-ready-*.json")
	if err != nil {
		return err
	}
	temporaryPath := file.Name()
	defer func() {
		// #nosec G703 -- CreateTemp owns this temporary path.
		_ = os.Remove(temporaryPath)
	}()

	if err := json.NewEncoder(file).Encode(result); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// #nosec G703 -- the operator supplies the ready-file destination.
	return os.Rename(temporaryPath, path)
}

func runCollector(
	ctx context.Context,
	endpoint string,
	token string,
	tlsConfig *tls.Config,
	stop <-chan struct{},
	ready *sync.WaitGroup,
	workers *sync.WaitGroup,
	counters *counters,
) {
	defer workers.Done()
	var readyOnce sync.Once
	markReady := func() {
		readyOnce.Do(ready.Done)
	}
	defer markReady()

	opampClient := client.NewWebSocket(nil)
	if err := opampClient.SetAgentDescription(&protobufs.AgentDescription{
		IdentifyingAttributes: []*protobufs.KeyValue{
			keyValue("service.name", "otelcol-load-test"),
			keyValue("service.version", "load-test"),
		},
	}); err != nil {
		counters.failed.Add(1)
		return
	}

	var instanceUID types.InstanceUid
	if _, err := rand.Read(instanceUID[:]); err != nil {
		counters.failed.Add(1)
		return
	}
	if ctx.Err() != nil {
		counters.cancelled.Add(1)
		return
	}

	connectResult := make(chan bool, 1)
	var connectOnce sync.Once
	recordConnect := func(connected bool) {
		connectOnce.Do(func() {
			connectResult <- connected
		})
	}

	settings := buildStartSettings(
		endpoint,
		token,
		tlsConfig,
		instanceUID,
		types.Callbacks{
			OnConnect: func(context.Context) {
				recordConnect(true)
			},
			OnConnectFailed: func(context.Context, error) {
				recordConnect(false)
			},
		},
	)
	if err := opampClient.Start(ctx, settings); err != nil {
		if ctx.Err() != nil {
			counters.cancelled.Add(1)
		} else {
			counters.failed.Add(1)
		}
		return
	}

	connected := false
	select {
	case connected = <-connectResult:
	case <-ctx.Done():
		counters.cancelled.Add(1)
		_ = stopCollector(opampClient)
		return
	case <-time.After(connectTimeout):
	}
	if !connected {
		counters.failed.Add(1)
		_ = stopCollector(opampClient)
		return
	}

	counters.connected.Add(1)
	markReady()
	select {
	case <-stop:
	case <-ctx.Done():
	}
	if err := stopCollector(opampClient); err != nil {
		counters.stopFailed.Add(1)
		return
	}
	counters.disconnected.Add(1)
}

func keyValue(key string, value string) *protobufs.KeyValue {
	return &protobufs.KeyValue{
		Key: key,
		Value: &protobufs.AnyValue{
			Value: &protobufs.AnyValue_StringValue{StringValue: value},
		},
	}
}
