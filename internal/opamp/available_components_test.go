// Package opamp implements the OpAMP server, agent registry, fingerprinting, and remote config push.
package opamp

import (
	"reflect"
	"testing"

	"github.com/open-telemetry/opamp-go/protobufs"
)

func TestFlattenAvailableComponents_NilOrEmpty(t *testing.T) {
	if got := flattenAvailableComponents(nil); got != nil {
		t.Fatalf("nil input: got %+v, want nil", got)
	}
	if got := flattenAvailableComponents(&protobufs.AvailableComponents{}); got != nil {
		t.Fatalf("empty input: got %+v, want nil", got)
	}
}

func TestFlattenAvailableComponents_FlattensAndSorts(t *testing.T) {
	in := &protobufs.AvailableComponents{
		Hash: []byte{0xde, 0xad, 0xbe, 0xef},
		Components: map[string]*protobufs.ComponentDetails{
			"receivers": {
				SubComponentMap: map[string]*protobufs.ComponentDetails{
					"otlp":   {},
					"jaeger": {},
				},
			},
			"processors": {
				SubComponentMap: map[string]*protobufs.ComponentDetails{
					"batch": {},
				},
			},
			"extensions": nil, // should be skipped
		},
	}

	got := flattenAvailableComponents(in)
	if got == nil {
		t.Fatal("got nil, want populated")
	}
	if got.Hash != "deadbeef" {
		t.Errorf("Hash = %q, want deadbeef", got.Hash)
	}
	wantReceivers := []string{"jaeger", "otlp"}
	if !reflect.DeepEqual(got.Components["receivers"], wantReceivers) {
		t.Errorf("receivers = %v, want %v (sorted)", got.Components["receivers"], wantReceivers)
	}
	if !reflect.DeepEqual(got.Components["processors"], []string{"batch"}) {
		t.Errorf("processors = %v, want [batch]", got.Components["processors"])
	}
	if _, ok := got.Components["extensions"]; ok {
		t.Errorf("nil category should be skipped, got %v", got.Components["extensions"])
	}
}
