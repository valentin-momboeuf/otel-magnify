// sdkagent is a minimal OpAMP client that simulates an SDK-instrumented service.
// Used for local development and demo purposes only.
package main

import (
	"context"
	"crypto/rand"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/open-telemetry/opamp-go/client"
	"github.com/open-telemetry/opamp-go/client/types"
	"github.com/open-telemetry/opamp-go/protobufs"
)

func main() {
	name := flag.String("name", "my-sdk-service", "Service name reported to OpAMP server")
	version := flag.String("version", "1.0.0", "Service version")
	env := flag.String("env", "dev", "Deployment environment label")
	endpoint := flag.String("endpoint", "ws://localhost:4320/v1/opamp", "OpAMP server WebSocket endpoint")
	flag.Parse()

	logger := log.New(os.Stdout, "["+*name+"] ", log.LstdFlags)

	opampClient := client.NewWebSocket(nil)

	if err := opampClient.SetAgentDescription(&protobufs.AgentDescription{
		IdentifyingAttributes: []*protobufs.KeyValue{
			kv("service.name", *name),
			kv("service.version", *version),
		},
		NonIdentifyingAttributes: []*protobufs.KeyValue{
			kv("deployment.environment", *env),
		},
	}); err != nil {
		logger.Fatalf("SetAgentDescription: %v", err)
	}

	capabilities := protobufs.AgentCapabilities_AgentCapabilities_ReportsStatus
	if err := opampClient.SetCapabilities(&capabilities); err != nil {
		logger.Fatalf("SetCapabilities: %v", err)
	}

	var uid types.InstanceUid
	if _, err := rand.Read(uid[:]); err != nil {
		logger.Fatalf("generate instance uid: %v", err)
	}

	settings := types.StartSettings{
		OpAMPServerURL: *endpoint,
		InstanceUid:    uid,
		Callbacks: types.Callbacks{
			OnConnect: func(_ context.Context) {
				logger.Printf("connected to %s", *endpoint)
			},
			OnConnectFailed: func(_ context.Context, err error) {
				logger.Printf("connection failed: %v", err)
			},
			OnError: func(_ context.Context, err *protobufs.ServerErrorResponse) {
				logger.Printf("server error: %v", err.GetErrorMessage())
			},
		},
	}

	if err := opampClient.Start(context.Background(), settings); err != nil {
		logger.Fatalf("Start: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh

	logger.Println("shutting down...")
	if err := opampClient.Stop(context.Background()); err != nil {
		logger.Printf("Stop: %v", err)
	}
}

func kv(key, val string) *protobufs.KeyValue {
	return &protobufs.KeyValue{
		Key: key,
		Value: &protobufs.AnyValue{
			Value: &protobufs.AnyValue_StringValue{StringValue: val},
		},
	}
}
