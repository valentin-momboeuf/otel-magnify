package opamp

import (
	"testing"
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

func TestInstanceCountStartsZero(t *testing.T) {
	srv := New(nil, nil, Options{})
	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.ConnectedInstanceCount() != 0 {
		t.Errorf("expected 0 connected instances, got %d", srv.ConnectedInstanceCount())
	}
}
