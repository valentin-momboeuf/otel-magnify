package ext

import "otel-magnify/pkg/models"

type Store interface {
	CreateUser(u models.User) error
	GetUserByEmail(email string) (models.User, error)
	UpsertAgent(a models.Agent) error
	GetAgent(id string) (models.Agent, error)
	ListAgents() ([]models.Agent, error)
	UpdateAgentStatus(id, status string) error
	CreateConfig(c models.Config) error
	GetConfig(id string) (models.Config, error)
	ListConfigs() ([]models.Config, error)
	RecordAgentConfig(ac models.AgentConfig) error
	UpdateAgentConfigStatus(agentID, configID, status, errorMessage string) error
	GetLatestPendingAgentConfig(agentID string) (*models.AgentConfig, error)
	GetAgentConfigHistory(agentID string) ([]models.AgentConfig, error)
	GetLastAppliedAgentConfig(agentID string) (*models.AgentConfig, error)
	CreateAlert(a models.Alert) error
	ResolveAlert(id string) error
	ListAlerts(includeResolved bool) ([]models.Alert, error)
	GetUnresolvedAlertByAgentAndRule(agentID, rule string) (*models.Alert, error)
	Close() error
	Migrate() error
}
