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
	if !strings.Contains(out, "### 🔥 告警触发（1）") {
		t.Fatalf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "- **严重度**:") {
		t.Fatalf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "- **摘要**: cpu too high") {
		t.Fatalf("unexpected output: %q", out)
	}
	if !strings.Contains(out, "- **描述**: -") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestNewRenderer_DirEmptyFallbackToEmbeddedDefault(t *testing.T) {
	dir := t.TempDir()
	r, err := NewRenderer(config.TemplateConfig{Dir: dir})
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	if r.DefaultName() != "default" {
		t.Fatalf("DefaultName=%q", r.DefaultName())
	}
	if !r.HasTemplate("default") {
		t.Fatalf("missing embedded default template")
	}
}
