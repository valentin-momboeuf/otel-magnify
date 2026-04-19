package alerts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/ext"
	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// Broadcaster pushes real-time updates to connected frontends.
type Broadcaster interface {
	BroadcastAlertUpdate(alert models.Alert)
}

// AlertStore is the subset of ext.Store used by the alert engine.
type AlertStore interface {
	ListAgents() ([]models.Agent, error)
	GetUnresolvedAlertByAgentAndRule(agentID, rule string) (*models.Alert, error)
	CreateAlert(a models.Alert) error
	ResolveAlert(id string) error
	GetLatestPendingAgentConfig(agentID string) (*models.AgentConfig, error)
}

type Engine struct {
	db          AlertStore
	hub         Broadcaster
	downTimeout time.Duration
	minVersion  string
	notifiers   []ext.AlertNotifier
}

func New(db AlertStore, hub Broadcaster, downTimeout time.Duration, minVersion string, notifiers ...ext.AlertNotifier) *Engine {
	return &Engine{
		db:          db,
		hub:         hub,
		downTimeout: downTimeout,
		minVersion:  minVersion,
		notifiers:   notifiers,
	}
}

func (e *Engine) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			e.Evaluate()
		case <-ctx.Done():
			return
		}
	}
}

func (e *Engine) Evaluate() {
	agents, err := e.db.ListAgents()
	if err != nil {
		log.Printf("alert engine: list agents: %v", err)
		return
	}

	now := time.Now().UTC()
	for _, agent := range agents {
		e.evaluateAgentDown(agent, now)
		e.evaluateConfigDrift(agent, now)
		e.evaluateVersionOutdated(agent, now)
	}
}

func (e *Engine) evaluateAgentDown(agent models.Agent, now time.Time) {
	isDown := now.Sub(agent.LastSeenAt) > e.downTimeout

	existing, err := e.db.GetUnresolvedAlertByAgentAndRule(agent.ID, "agent_down")
	if err != nil {
		log.Printf("alert engine: check existing alert for %s: %v", agent.ID, err)
		return
	}

	if isDown && existing == nil {
		alert := models.Alert{
			ID:       generateID(),
			AgentID:  agent.ID,
			Rule:     "agent_down",
			Severity: "critical",
			Message:  "Agent " + agent.ID + " not seen for " + e.downTimeout.String(),
			FiredAt:  now,
		}
		if err := e.db.CreateAlert(alert); err != nil {
			log.Printf("alert engine: create alert: %v", err)
			return
		}
		if e.hub != nil {
			e.hub.BroadcastAlertUpdate(alert)
		}
		for _, n := range e.notifiers {
			n := n
			go n.Send(alert)
		}
	}

	if !isDown && existing != nil {
		if err := e.db.ResolveAlert(existing.ID); err != nil {
			log.Printf("alert engine: resolve alert: %v", err)
		}
	}
}

func (e *Engine) evaluateConfigDrift(agent models.Agent, now time.Time) {
	pending, err := e.db.GetLatestPendingAgentConfig(agent.ID)
	if err != nil {
		log.Printf("alert engine: check config drift for %s: %v", agent.ID, err)
		return
	}

	// Agent has not applied the config we pushed within the timeout window.
	isDrifted := pending != nil && now.Sub(pending.AppliedAt) > e.downTimeout

	existing, _ := e.db.GetUnresolvedAlertByAgentAndRule(agent.ID, "config_drift")

	if isDrifted && existing == nil {
		alert := models.Alert{
			ID:       generateID(),
			AgentID:  agent.ID,
			Rule:     "config_drift",
			Severity: "warning",
			Message:  "Agent " + agent.ID + " has not applied config " + pending.ConfigID[:12] + " after " + e.downTimeout.String(),
			FiredAt:  now,
		}
		if err := e.db.CreateAlert(alert); err != nil {
			log.Printf("alert engine: create config_drift alert: %v", err)
			return
		}
		if e.hub != nil {
			e.hub.BroadcastAlertUpdate(alert)
		}
		for _, n := range e.notifiers {
			n := n
			go n.Send(alert)
		}
	}

	if !isDrifted && existing != nil {
		if err := e.db.ResolveAlert(existing.ID); err != nil {
			log.Printf("alert engine: resolve config_drift alert: %v", err)
		}
	}
}

func (e *Engine) evaluateVersionOutdated(agent models.Agent, now time.Time) {
	// Skip if the minimum version constraint is not configured or the agent
	// has not yet reported its version.
	if e.minVersion == "" || agent.Version == "" {
		return
	}

	// Lexicographic comparison works for semver strings with the same number
	// of digits per segment (e.g. "0.9.0" < "0.10.0" would fail — acceptable
	// for now; use semver library if stricter comparison is required).
	isOutdated := agent.Version < e.minVersion

	existing, _ := e.db.GetUnresolvedAlertByAgentAndRule(agent.ID, "version_outdated")

	if isOutdated && existing == nil {
		alert := models.Alert{
			ID:       generateID(),
			AgentID:  agent.ID,
			Rule:     "version_outdated",
			Severity: "warning",
			Message:  "Agent " + agent.ID + " version " + agent.Version + " is below minimum " + e.minVersion,
			FiredAt:  now,
		}
		if err := e.db.CreateAlert(alert); err != nil {
			log.Printf("alert engine: create version_outdated alert: %v", err)
			return
		}
		if e.hub != nil {
			e.hub.BroadcastAlertUpdate(alert)
		}
		for _, n := range e.notifiers {
			n := n
			go n.Send(alert)
		}
	}

	if !isOutdated && existing != nil {
		if err := e.db.ResolveAlert(existing.ID); err != nil {
			log.Printf("alert engine: resolve version_outdated alert: %v", err)
		}
	}
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
