package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad_DefaultsAndTemplatePath(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "custom.tmpl"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(
		"server:\n"+
			"  listen: \"127.0.0.1:8080\"\n"+
			"template:\n"+
			"  dir: \"templates\"\n"+
			"dingtalk:\n"+
			"  robots:\n"+
			"    - name: \"default\"\n"+
			"      webhook: \"http://example.invalid\"\n"+
			"      msg_type: \"markdown\"\n"+
			"  channels:\n"+
			"    - name: \"default\"\n"+
			"      robots: [\"default\"]\n",
	), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Path != "/alert" {
		t.Fatalf("Server.Path=%q", cfg.Server.Path)
	}
	if cfg.DingTalk.Timeout.Duration() != 5*time.Second {
		t.Fatalf("DingTalk.Timeout=%s", cfg.DingTalk.Timeout.Duration())
	}

	wantDir := filepath.Join(dir, "templates")
	if cfg.Template.Dir != wantDir {
		t.Fatalf("Template.Dir=%q want %q", cfg.Template.Dir, wantDir)
	}
}

func TestLoad_RejectMissingDefaultChannel(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
dingtalk:
  robots:
    - name: "r1"
      webhook: "http://example.invalid"
      msg_type: "markdown"
  channels:
    - name: "ops"
      robots: ["r1"]
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(cfgPath); err == nil {
		t.Fatalf("expected error")
	}
}

func TestLoad_RejectInvalidRobotWebhook(t *testing.T) {
	testCases := []struct {
		name          string
		webhook       string
		expectedError string
	}{
		{
			name:          "invalid scheme",
			webhook:       "ftp://example.invalid",
			expectedError: "scheme must be http or https",
		},
		{
			name:          "missing host",
			webhook:       "https:///robot/send?access_token=xxx",
			expectedError: "host must not be empty",
		},
		{
			name:          "unparseable url",
			webhook:       "://invalid",
			expectedError: "must be a valid url",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			cfgText := "\n" +
				"dingtalk:\n" +
				"  robots:\n" +
				"    - name: \"r1\"\n" +
				"      webhook: \"" + tc.webhook + "\"\n" +
				"      msg_type: \"markdown\"\n" +
				"  channels:\n" +
				"    - name: \"default\"\n" +
				"      robots: [\"r1\"]\n"

			if err := os.WriteFile(cfgPath, []byte(cfgText), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if _, err := Load(cfgPath); err == nil {
				t.Fatalf("expected error")
			} else if !strings.Contains(err.Error(), tc.expectedError) {
				t.Fatalf("unexpected error: %v, want error containing %q", err, tc.expectedError)
			}
		})
	}
}
