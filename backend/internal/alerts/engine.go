package alerts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"time"

	"otel-magnify/internal/api"
	"otel-magnify/internal/store"
	"otel-magnify/pkg/models"
)

type Engine struct {
	db          *store.DB
	hub         *api.Hub
	downTimeout time.Duration
}

func New(db *store.DB, hub *api.Hub, downTimeout time.Duration) *Engine {
	return &Engine{
		db:          db,
		hub:         hub,
		downTimeout: downTimeout,
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
	}

	if !isDown && existing != nil {
		if err := e.db.ResolveAlert(existing.ID); err != nil {
			log.Printf("alert engine: resolve alert: %v", err)
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
