package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWorkloadJSONRoundTrip(t *testing.T) {
	w := Workload{
		ID:                "abc123",
		FingerprintSource: "k8s",
		FingerprintKeys:   FingerprintKeys{"cluster": "prod", "namespace": "obs", "kind": "deployment", "name": "otel"},
		DisplayName:       "otel-collector",
		Type:              "collector",
		Version:           "0.100.0",
		Status:            "connected",
		LastSeenAt:        time.Unix(0, 0).UTC(),
		Labels:            Labels{"k8s.pod.name": "otel-abc"},
	}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back Workload
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.FingerprintSource != "k8s" || back.FingerprintKeys["namespace"] != "obs" {
		t.Fatalf("lost fields: %+v", back)
	}
}

func TestWorkloadEventJSONRoundTrip(t *testing.T) {
	e := WorkloadEvent{
		ID:          42,
		WorkloadID:  "abc123",
		InstanceUID: "uid-1",
		PodName:     "pod-a",
		EventType:   "connected",
		Version:     "0.100.0",
		OccurredAt:  time.Unix(0, 0).UTC(),
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back WorkloadEvent
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.EventType != "connected" || back.PodName != "pod-a" {
		t.Fatalf("lost fields: %+v", back)
	}
}
