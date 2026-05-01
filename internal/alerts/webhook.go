package alerts

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// WebhookNotifier POSTs alert payloads to a configured HTTP endpoint.
type WebhookNotifier struct {
	url    string
	client *http.Client
}

// NewWebhookNotifier returns a notifier that posts to url, or nil when url is empty.
func NewWebhookNotifier(url string) *WebhookNotifier {
	if url == "" {
		return nil
	}
	return &WebhookNotifier{
		url:    url,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Send marshals the alert as JSON and POSTs it to the configured webhook URL.
func (w *WebhookNotifier) Send(alert models.Alert) {
	payload, err := json.Marshal(map[string]any{
		"alert":    alert,
		"event":    "alert_fired",
		"fired_at": alert.FiredAt.Format(time.RFC3339),
	})
	if err != nil {
		log.Printf("webhook: marshal error: %v", err)
		return
	}

	resp, err := w.client.Post(w.url, "application/json", bytes.NewReader(payload))
	if err != nil {
		log.Printf("webhook: send error: %v", err)
		return
	}
	//nolint:errcheck // deferred cleanup of HTTP response body; close error is not actionable
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Printf("webhook: server returned %d", resp.StatusCode)
	}
}
