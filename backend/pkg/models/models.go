package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// Labels is a map[string]string stored as JSON TEXT in the DB.
type Labels map[string]string

func (l Labels) Value() (string, error) {
	b, err := json.Marshal(l)
	return string(b), err
}

func (l *Labels) Scan(src any) error {
	switch v := src.(type) {
	case string:
		return json.Unmarshal([]byte(v), l)
	case []byte:
		return json.Unmarshal(v, l)
	case nil:
		*l = make(Labels)
		return nil
	default:
		return json.Unmarshal([]byte("{}"), l)
	}
}

type Agent struct {
	ID                  string               `json:"id"`
	DisplayName         string               `json:"display_name"`
	Type                string               `json:"type"`   // "collector" | "sdk"
	Version             string               `json:"version"`
	Status              string               `json:"status"` // "connected" | "disconnected" | "degraded"
	LastSeenAt          time.Time            `json:"last_seen_at"`
	Labels              Labels               `json:"labels"`
	ActiveConfigID      *string              `json:"active_config_id,omitempty"`
	RemoteConfigStatus  *RemoteConfigStatus  `json:"remote_config_status,omitempty"`
	AvailableComponents *AvailableComponents `json:"available_components,omitempty"`
	AcceptsRemoteConfig bool                 `json:"accepts_remote_config"`
}

// AvailableComponents maps OTel Collector categories (receivers, processors,
// exporters, extensions, connectors) to the set of component types the agent
// reports as installed. Populated from OpAMP AgentToServer.available_components.
type AvailableComponents struct {
	// Components keyed by category, each value a sorted list of component type names.
	Components map[string][]string `json:"components"`
	// Hash reported by the agent (hex-encoded); used to detect changes.
	Hash string `json:"hash,omitempty"`
}

func (a AvailableComponents) Value() (string, error) {
	b, err := json.Marshal(a)
	return string(b), err
}

func (a *AvailableComponents) Scan(src any) error {
	switch v := src.(type) {
	case string:
		if v == "" {
			return nil
		}
		return json.Unmarshal([]byte(v), a)
	case []byte:
		if len(v) == 0 {
			return nil
		}
		return json.Unmarshal(v, a)
	case nil:
		return nil
	default:
		return fmt.Errorf("unsupported type for AvailableComponents: %T", src)
	}
}

type RemoteConfigStatus struct {
	Status       string    `json:"status"` // "applying" | "applied" | "failed"
	ConfigHash   string    `json:"config_hash"`
	ErrorMessage string    `json:"error_message,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (r RemoteConfigStatus) Value() (string, error) {
	b, err := json.Marshal(r)
	return string(b), err
}

func (r *RemoteConfigStatus) Scan(src any) error {
	switch v := src.(type) {
	case string:
		if v == "" {
			return nil
		}
		return json.Unmarshal([]byte(v), r)
	case []byte:
		if len(v) == 0 {
			return nil
		}
		return json.Unmarshal(v, r)
	case nil:
		return nil
	default:
		return fmt.Errorf("unsupported type for RemoteConfigStatus: %T", src)
	}
}

type Config struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

type AgentConfig struct {
	AgentID      string    `json:"agent_id"`
	ConfigID     string    `json:"config_id"`
	AppliedAt    time.Time `json:"applied_at"`
	Status       string    `json:"status"` // "pending" | "applied" | "failed"
	ErrorMessage string    `json:"error_message,omitempty"`
	PushedBy     string    `json:"pushed_by,omitempty"`
	Content      string    `json:"content,omitempty"` // filled by JOIN in history queries
}

type Alert struct {
	ID         string     `json:"id"`
	AgentID    string     `json:"agent_id"`
	Rule       string     `json:"rule"`     // "agent_down" | "config_drift" | "version_outdated"
	Severity   string     `json:"severity"` // "warning" | "critical"
	Message    string     `json:"message"`
	FiredAt    time.Time  `json:"fired_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

type User struct {
	ID           string  `json:"id"`
	Email        string  `json:"email"`
	PasswordHash string  `json:"-"`
	Role         string  `json:"role"` // "admin" | "viewer"
	TenantID     *string `json:"tenant_id,omitempty"`
}

// FingerprintKeys is a small JSON map persisted alongside a Workload to
// record which resource attributes contributed to its identity.
type FingerprintKeys map[string]string

func (f FingerprintKeys) Value() (string, error) {
	b, err := json.Marshal(f)
	return string(b), err
}

func (f *FingerprintKeys) Scan(src any) error {
	switch v := src.(type) {
	case string:
		if v == "" {
			*f = FingerprintKeys{}
			return nil
		}
		return json.Unmarshal([]byte(v), f)
	case []byte:
		if len(v) == 0 {
			*f = FingerprintKeys{}
			return nil
		}
		return json.Unmarshal(v, f)
	case nil:
		*f = FingerprintKeys{}
		return nil
	default:
		return fmt.Errorf("unsupported type for FingerprintKeys: %T", src)
	}
}

// Workload is a logical unit of management: a K8s Deployment / DaemonSet /
// StatefulSet, a single host process, or a cardinality-1 fallback keyed on
// the OpAMP InstanceUid.
type Workload struct {
	ID                  string               `json:"id"`
	FingerprintSource   string               `json:"fingerprint_source"` // "k8s" | "host" | "uid"
	FingerprintKeys     FingerprintKeys      `json:"fingerprint_keys"`
	DisplayName         string               `json:"display_name"`
	Type                string               `json:"type"`   // "collector" | "sdk"
	Version             string               `json:"version"`
	Status              string               `json:"status"` // "connected" | "disconnected" | "degraded"
	LastSeenAt          time.Time            `json:"last_seen_at"`
	Labels              Labels               `json:"labels"`
	ActiveConfigID      *string              `json:"active_config_id,omitempty"`
	ActiveConfigHash    string               `json:"active_config_hash,omitempty"`
	RemoteConfigStatus  *RemoteConfigStatus  `json:"remote_config_status,omitempty"`
	AvailableComponents *AvailableComponents `json:"available_components,omitempty"`
	AcceptsRemoteConfig bool                 `json:"accepts_remote_config"`
	RetentionUntil      *time.Time           `json:"retention_until,omitempty"`
	ArchivedAt          *time.Time           `json:"archived_at,omitempty"`
}

// WorkloadEvent is an append-only record of a pod transition on a workload.
type WorkloadEvent struct {
	ID          int64     `json:"id"`
	WorkloadID  string    `json:"workload_id"`
	InstanceUID string    `json:"instance_uid"`
	PodName     string    `json:"pod_name,omitempty"`
	EventType   string    `json:"event_type"` // "connected" | "disconnected" | "version_changed"
	Version     string    `json:"version,omitempty"`
	PrevVersion string    `json:"prev_version,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
}
