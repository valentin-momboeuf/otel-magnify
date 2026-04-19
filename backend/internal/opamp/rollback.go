package opamp

import (
	"encoding/hex"
	"log"
	"time"

	"github.com/open-telemetry/opamp-go/protobufs"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func (s *Server) handleRemoteConfigStatus(agentUID string, rcs *protobufs.RemoteConfigStatus) {
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

	if err := s.store.UpdateAgentConfigStatus(agentUID, configHash, statusStr, rcs.ErrorMessage); err != nil {
		log.Printf("update agent_config status %s/%s: %v", shortID(agentUID), shortID(configHash), err)
	}

	if agent, err := s.store.GetAgent(agentUID); err == nil {
		agent.RemoteConfigStatus = &snap
		if err := s.store.UpsertAgent(agent); err != nil {
			log.Printf("upsert agent status %s: %v", shortID(agentUID), err)
		}
	}

	if s.notifier != nil {
		s.notifier.BroadcastConfigStatus(agentUID, snap)
	}

	if statusStr == "failed" {
		s.attemptAutoRollback(agentUID, configHash, rcs.ErrorMessage)
	}
}

func (s *Server) attemptAutoRollback(agentUID, failedHash, reason string) {
	prev, err := s.store.GetLastAppliedAgentConfig(agentUID)
	if err != nil {
		log.Printf("rollback lookup %s: %v", shortID(agentUID), err)
		return
	}
	if prev == nil {
		return
	}
	if prev.ConfigID == failedHash {
		log.Printf("rollback target equals failed hash %s/%s, aborting", shortID(agentUID), shortID(failedHash))
		return
	}
	if err := s.pushFn(agentUID, []byte(prev.Content)); err != nil {
		log.Printf("rollback push %s→%s: %v", shortID(agentUID), shortID(prev.ConfigID), err)
		return
	}
	if err := s.store.RecordAgentConfig(models.AgentConfig{
		AgentID: agentUID, ConfigID: prev.ConfigID, Status: "pending", PushedBy: "auto-rollback",
	}); err != nil {
		log.Printf("rollback record %s: %v", shortID(agentUID), err)
	}
	if s.notifier != nil {
		s.notifier.BroadcastAutoRollback(agentUID, failedHash, prev.ConfigID, reason)
	}
}

func remoteConfigStatusString(s protobufs.RemoteConfigStatuses) string {
	switch s {
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
