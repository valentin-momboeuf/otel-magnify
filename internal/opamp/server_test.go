package opamp

import (
	"context"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"
)

func TestNewOpAMPServer(t *testing.T) {
	srv := New(nil, nil, Options{})
	if srv == nil {
		t.Fatal("New returned nil")
	}
}

func TestIsCollectorName(t *testing.T) {
	collectors := []string{
		"otelcol",
		"otelcol-contrib",
		"otelcol-custom",
		"OtelCol-Contrib",
		"io.opentelemetry.collector",
		"my-opentelemetry-collector",
	}
	for _, name := range collectors {
		if !isCollectorName(name) {
			t.Errorf("isCollectorName(%q) = false, want true", name)
		}
	}

	sdks := []string{
		"my-service",
		"payment-api",
		"",
		"flask-app",
	}
	for _, name := range sdks {
		if isCollectorName(name) {
			t.Errorf("isCollectorName(%q) = true, want false", name)
		}
	}
}

func TestClassifyAgent_CollectorByOtelcolVersion(t *testing.T) {
	attrs := map[string]string{
		"otelcol.version": "0.150.1",
		"service.name":    "my-custom-collector",
	}
	if got := classifyAgent(attrs); got != "collector" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "collector")
	}
}

func TestClassifyAgent_CollectorByOsDescription(t *testing.T) {
	attrs := map[string]string{
		"os.description": "otelcol/0.150.1 (linux/amd64)",
	}
	if got := classifyAgent(attrs); got != "collector" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "collector")
	}
}

func TestClassifyAgent_SDKByLanguage(t *testing.T) {
	attrs := map[string]string{
		"telemetry.sdk.language": "go",
		"service.name":           "otelcol-trap",
	}
	if got := classifyAgent(attrs); got != "sdk" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "sdk")
	}
}

func TestClassifyAgent_FallbackByServiceName_Collector(t *testing.T) {
	attrs := map[string]string{
		"service.name": "otelcol-foo",
	}
	if got := classifyAgent(attrs); got != "collector" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "collector")
	}
}

func TestClassifyAgent_FallbackByServiceName_SDK(t *testing.T) {
	attrs := map[string]string{
		"service.name": "my-app",
	}
	if got := classifyAgent(attrs); got != "sdk" {
		t.Errorf("classifyAgent(%v) = %q, want %q", attrs, got, "sdk")
	}
}

func TestInstanceCountStartsZero(t *testing.T) {
	srv := New(nil, nil, Options{})
	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.ConnectedInstanceCount() != 0 {
		t.Errorf("expected 0 connected instances, got %d", srv.ConnectedInstanceCount())
	}
}

// TestOnMessage_UnknownInstance_RequestsFullState guards the resync path:
// when an agent sends a heartbeat for a UID we have no record of (typical
// after a server restart with ephemeral DB), we must set ReportFullState so
// the agent re-sends its AgentDescription and the workload can be bootstrapped.
func TestOnMessage_UnknownInstance_RequestsFullState(t *testing.T) {
	srv := New(nil, nil, Options{})
	uid := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}

	reply := srv.onMessage(context.Background(), nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		SequenceNum: 5,
	})

	if reply == nil {
		t.Fatal("onMessage returned nil reply")
	}
	wantFlag := uint64(protobufs.ServerToAgentFlags_ServerToAgentFlags_ReportFullState)
	if reply.Flags&wantFlag == 0 {
		t.Errorf("expected ReportFullState flag set, got Flags=0x%x", reply.Flags)
	}
}

// TestOnMessage_KnownInstance_DoesNotRequestFullState is the regression guard:
// once the registry knows the instance, subsequent heartbeats must not carry
// the ReportFullState flag (we already have the state we need).
func TestOnMessage_KnownInstance_DoesNotRequestFullState(t *testing.T) {
	srv := New(nil, nil, Options{})
	ctx := context.Background()
	uid := []byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80,
		0x90, 0xa0, 0xb0, 0xc0, 0xd0, 0xe0, 0xf0, 0x11}

	// Seed the registry with an AgentDescription-bearing message.
	_ = srv.onMessage(ctx, nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		SequenceNum: 1,
		AgentDescription: &protobufs.AgentDescription{
			IdentifyingAttributes: []*protobufs.KeyValue{
				{
					Key: "service.name",
					Value: &protobufs.AnyValue{
						Value: &protobufs.AnyValue_StringValue{StringValue: "otelcol"},
					},
				},
			},
		},
	})

	reply := srv.onMessage(ctx, nil, &protobufs.AgentToServer{
		InstanceUid: uid,
		SequenceNum: 2,
	})

	if reply == nil {
		t.Fatal("onMessage returned nil reply")
	}
	flag := uint64(protobufs.ServerToAgentFlags_ServerToAgentFlags_ReportFullState)
	if reply.Flags&flag != 0 {
		t.Errorf("known-instance heartbeat must not request full state, got Flags=0x%x", reply.Flags)
	}
}
