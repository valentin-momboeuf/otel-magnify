package opamp

import (
	"encoding/hex"
	"log"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func (s *Server) handleRemoteConfigStatus(workloadID string, rcs *protobufs.RemoteConfigStatus) {
	statusStr := remoteConfigStatusString(rcs.Status)
	if statusStr == "" {
		return
	}

	configHash := hex.EncodeToString(rcs.LastRemoteConfigHash)
	snap := models.RemoteConfigStatus{
		Status:       statusStr,
		ConfigHash:   configHash,
		ErrorMessage: rcs.ErrorMessage,
		UpdatedAt:    time.Now().UTC(),
	}

	if err := s.store.UpdateWorkloadConfigStatus(workloadID, configHash, statusStr, rcs.ErrorMessage); err != nil {
		log.Printf("update workload_config status %s/%s: %v", shortID(workloadID), shortID(configHash), err)
	}

	if wl, err := s.store.GetWorkload(workloadID); err == nil {
		wl.RemoteConfigStatus = &snap
		if err := s.store.UpsertWorkload(wl); err != nil {
			log.Printf("upsert workload status %s: %v", shortID(workloadID), err)
		}
	}

	if s.notifier != nil {
		s.notifier.BroadcastConfigStatus(workloadID, snap)
	}

	if statusStr == "failed" {
		s.attemptAutoRollback(workloadID, configHash, rcs.ErrorMessage)
	}
}

func (s *Server) attemptAutoRollback(workloadID, failedHash, reason string) {
	prev, err := s.store.GetLastAppliedWorkloadConfig(workloadID)
	if err != nil {
		log.Printf("rollback lookup %s: %v", shortID(workloadID), err)
		return
	}
	if prev == nil {
		return
	}
	if prev.ConfigID == failedHash {
		log.Printf("rollback target equals failed hash %s/%s, aborting", shortID(workloadID), shortID(failedHash))
		return
	}
	// Auto-rollback is workload-wide: no specific instance target.
	if err := s.pushFn(workloadID, []byte(prev.Content), ""); err != nil {
		log.Printf("rollback push %s->%s: %v", shortID(workloadID), shortID(prev.ConfigID), err)
		return
	}
	if err := s.store.RecordWorkloadConfig(models.WorkloadConfig{
		WorkloadID: workloadID, ConfigID: prev.ConfigID, Status: "pending", PushedBy: "auto-rollback",
	}); err != nil {
		log.Printf("rollback record %s: %v", shortID(workloadID), err)
	}
	if s.notifier != nil {
		s.notifier.BroadcastAutoRollback(workloadID, failedHash, prev.ConfigID, reason)
	}
}

func remoteConfigStatusString(s protobufs.RemoteConfigStatuses) string {
	switch s {
	case protobufs.RemoteConfigStatuses_RemoteConfigStatuses_UNSET:
		return ""
	case protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLYING:
		return "applying"
	case protobufs.RemoteConfigStatuses_RemoteConfigStatuses_APPLIED:
		return "applied"
	case protobufs.RemoteConfigStatuses_RemoteConfigStatuses_FAILED:
		return "failed"
	default:
		return ""
	}
}

// shortID returns the first 8 chars of an ID, or the full string if shorter.
// Used only for log prefixes — prevents out-of-range on short test IDs.
func shortID(id string) string {
	if len(id) < 8 {
		return id
	}
	return id[:8]
}
