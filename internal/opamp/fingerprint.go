package opamp

import (
	"crypto/sha256"
	"encoding/hex"
)

// FingerprintResult is the computed identity of a workload from a set of
// resource attributes. Source identifies which strategy matched; Keys records
// the attributes that contributed (for debugging and UI display).
type FingerprintResult struct {
	ID     string // 24 hex chars (sha256 prefix)
	Source string // "k8s" | "host" | "uid"
	Keys   map[string]string
}

var k8sWorkloadAttrs = []struct {
	Attr string
	Kind string
}{
	{"k8s.deployment.name", "deployment"},
	{"k8s.daemonset.name", "daemonset"},
	{"k8s.statefulset.name", "statefulset"},
	{"k8s.job.name", "job"},
	{"k8s.cronjob.name", "cronjob"},
}

// Fingerprint computes a stable workload identity from OpAMP resource
// attributes. Strategy (first match wins):
//  1. K8s: namespace + workload-kind + name (cluster defaulted to "unknown").
//  2. Host: service.name + host.name.
//  3. UID: fallback to the OpAMP InstanceUid (cardinality 1 by construction).
func Fingerprint(attrs map[string]string, instanceUID string) FingerprintResult {
	namespace := attrs["k8s.namespace.name"]
	if namespace != "" {
		for _, w := range k8sWorkloadAttrs {
			if name := attrs[w.Attr]; name != "" {
				cluster := attrs["k8s.cluster.name"]
				if cluster == "" {
					cluster = "unknown"
				}
				raw := "k8s|" + cluster + "|" + namespace + "|" + w.Kind + "|" + name
				return FingerprintResult{
					ID:     hash24(raw),
					Source: "k8s",
					Keys: map[string]string{
						"cluster":   cluster,
						"namespace": namespace,
						"kind":      w.Kind,
						"name":      name,
					},
				}
			}
		}
	}

	serviceName := attrs["service.name"]
	if host := attrs["host.name"]; host != "" {
		raw := "host|" + serviceName + "|" + host
		return FingerprintResult{
			ID:     hash24(raw),
			Source: "host",
			Keys:   map[string]string{"service.name": serviceName, "host.name": host},
		}
	}

	raw := "uid|" + instanceUID
	return FingerprintResult{
		ID:     hash24(raw),
		Source: "uid",
		Keys:   map[string]string{"instance_uid": instanceUID},
	}
}

func hash24(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:24]
}
