package ext

import (
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

type Store interface {
	CreateUser(u models.User) error
	GetUserByEmail(email string) (models.User, error)
	UpdateUser(u models.User) error

	// Groups (RBAC fondation, Spec A).
	ListSystemGroups() ([]models.Group, error)
	GetGroupByName(name string) (models.Group, error)
	AttachUserToGroupByName(userID, groupName string) error
	GetUserGroups(userID string) ([]models.Group, error)

	// User preferences (theme + language).
	GetUserPreferences(userID string) (models.UserPreferences, error)
	UpsertUserPreferences(p models.UserPreferences) error

	UpsertWorkload(w models.Workload) error
	GetWorkload(id string) (models.Workload, error)
	ListWorkloads(includeArchived bool) ([]models.Workload, error)
	MarkWorkloadDisconnected(id string, retentionUntil time.Time) error
	ClearWorkloadRetention(id string) error
	ArchiveExpiredWorkloads(now time.Time) (int64, error)
	DeleteWorkload(id string) error

	InsertWorkloadEvent(e models.WorkloadEvent) (int64, error)
	ListWorkloadEvents(workloadID string, limit int, since time.Time) ([]models.WorkloadEvent, error)
	PurgeOldWorkloadEvents(cutoff time.Time) (int64, error)

	CreateConfig(c models.Config) error
	GetConfig(id string) (models.Config, error)
	ListConfigs() ([]models.Config, error)

	RecordWorkloadConfig(wc models.WorkloadConfig) error
	UpdateWorkloadConfigStatus(workloadID, configID, status, errorMessage string) error
	GetLatestPendingWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)
	GetWorkloadConfigHistory(workloadID string) ([]models.WorkloadConfig, error)
	GetLastAppliedWorkloadConfig(workloadID string) (*models.WorkloadConfig, error)
	GetPushActivity(days int) ([]models.PushActivityPoint, error)

	CreateAlert(a models.Alert) error
	ResolveAlert(id string) error
	ListAlerts(includeResolved bool) ([]models.Alert, error)
	GetUnresolvedAlertByWorkloadAndRule(workloadID, rule string) (*models.Alert, error)

	Close() error
	Migrate() error
}
