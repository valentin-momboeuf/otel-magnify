package ext

import "otel-magnify/pkg/models"

type AlertNotifier interface {
	Send(alert models.Alert)
}
