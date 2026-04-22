package ext

import "github.com/magnify-labs/otel-magnify/pkg/models"

type AlertNotifier interface {
	Send(alert models.Alert)
}
