package ext

import "github.com/magnify-labs/otel-magnify/pkg/models"

// AlertNotifier dispatches an alert to an external sink (webhook, email, Slack, …).
type AlertNotifier interface {
	Send(alert models.Alert)
}
