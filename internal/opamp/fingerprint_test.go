package opamp

import "testing"

func TestFingerprintK8sDeployment(t *testing.T) {
	attrs := map[string]string{
		"k8s.cluster.name":    "prod",
		"k8s.namespace.name":  "obs",
		"k8s.deployment.name": "otel",
		"service.name":        "otelcol",
		"host.name":           "node-1",
	}
	fp := Fingerprint(attrs, "uid-xyz")
	if fp.Source != "k8s" {
		t.Fatalf("source = %q, want k8s", fp.Source)
	}
	if fp.Keys["namespace"] != "obs" || fp.Keys["kind"] != "deployment" {
		t.Fatalf("keys = %+v", fp.Keys)
	}
	if len(fp.ID) != 24 {
		t.Fatalf("id len = %d, want 24", len(fp.ID))
	}
	// Stability: same attrs give same id regardless of instance uid
	fp2 := Fingerprint(attrs, "uid-other")
	if fp.ID != fp2.ID {
		t.Fatalf("unstable id: %s vs %s", fp.ID, fp2.ID)
	}
}

func TestFingerprintK8sDaemonsetSeparatesFromDeployment(t *testing.T) {
	base := map[string]string{"k8s.cluster.name": "prod", "k8s.namespace.name": "obs"}
	dep := copyWith(base, "k8s.deployment.name", "otel")
	ds := copyWith(base, "k8s.daemonset.name", "otel")
	if Fingerprint(dep, "").ID == Fingerprint(ds, "").ID {
		t.Fatalf("Deployment and DaemonSet must not collide")
	}
}

func TestFingerprintK8sNamespaceIsolation(t *testing.T) {
	prod := map[string]string{"k8s.namespace.name": "prod", "k8s.deployment.name": "otel"}
	dev := map[string]string{"k8s.namespace.name": "dev", "k8s.deployment.name": "otel"}
	if Fingerprint(prod, "").ID == Fingerprint(dev, "").ID {
		t.Fatalf("namespaces must not collide")
	}
}

func TestFingerprintK8sDefaultCluster(t *testing.T) {
	attrs := map[string]string{"k8s.namespace.name": "obs", "k8s.deployment.name": "otel"}
	fp := Fingerprint(attrs, "")
	if fp.Keys["cluster"] != "unknown" {
		t.Fatalf("default cluster = %q, want unknown", fp.Keys["cluster"])
	}
}

func TestFingerprintHostFallback(t *testing.T) {
	attrs := map[string]string{"service.name": "payment-api", "host.name": "vm-42"}
	fp := Fingerprint(attrs, "uid-xyz")
	if fp.Source != "host" {
		t.Fatalf("source = %q, want host", fp.Source)
	}
	if fp.Keys["service.name"] != "payment-api" || fp.Keys["host.name"] != "vm-42" {
		t.Fatalf("keys = %+v", fp.Keys)
	}
}

func TestFingerprintUIDFallback(t *testing.T) {
	attrs := map[string]string{"service.name": "payment-api"}
	fp := Fingerprint(attrs, "uid-xyz")
	if fp.Source != "uid" {
		t.Fatalf("source = %q, want uid", fp.Source)
	}
	if fp.Keys["instance_uid"] != "uid-xyz" {
		t.Fatalf("keys = %+v", fp.Keys)
	}
}

func TestFingerprintStability(t *testing.T) {
	// Calling Fingerprint twice with identical inputs must yield identical IDs (no
	// map-iteration-order flakiness).
	attrs := map[string]string{"k8s.cluster.name": "prod", "k8s.namespace.name": "obs", "k8s.deployment.name": "otel"}
	for i := 0; i < 100; i++ {
		id1 := Fingerprint(attrs, "").ID
		id2 := Fingerprint(attrs, "").ID
		if id1 != id2 {
			t.Fatal("non-deterministic fingerprint")
		}
	}
}

func copyWith(m map[string]string, k, v string) map[string]string {
	out := make(map[string]string, len(m)+1)
	for k2, v2 := range m {
		out[k2] = v2
	}
	out[k] = v
	return out
}
