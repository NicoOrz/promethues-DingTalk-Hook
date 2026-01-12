package template

import (
	"strings"
	"testing"

	"prometheus-dingtalk-hook/internal/alertmanager"
	"prometheus-dingtalk-hook/internal/config"
)

func TestRender_DefaultTemplate(t *testing.T) {
	r, err := NewRenderer(config.TemplateConfig{})
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}

	out, err := r.Render("", alertmanager.WebhookMessage{
		Receiver: "default",
		Status:   "firing",
		Alerts: []alertmanager.Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname": "HighCPU",
				},
				Annotations: map[string]string{
					"summary": "cpu too high",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "### 🔥 Alert Firing (1)") {
		t.Fatalf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "- **Severity**:") {
		t.Fatalf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "- **Summary**: cpu too high") {
		t.Fatalf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "- **Description**: -") {
		t.Fatalf("unexpected output: %q", out)
	}
}
