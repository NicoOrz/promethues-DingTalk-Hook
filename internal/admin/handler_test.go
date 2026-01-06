package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"prometheus-dingtalk-hook/internal/config"
	"prometheus-dingtalk-hook/internal/reload"
	"prometheus-dingtalk-hook/internal/runtime"
	"prometheus-dingtalk-hook/internal/template"
)

func TestHandler_handleExport_TemplateDirMissing(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("test: true\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile: %v", err)
	}

	h := &handler{configPath: configPath}
	rt := &runtime.Runtime{
		Config: &config.Config{
			Template: config.TemplateConfig{},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/export", nil)
	rr := httptest.NewRecorder()
	h.handleExport(rr, req, rt)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want %d body=%s", rr.Code, http.StatusInternalServerError, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type=%q want %q", got, "application/json")
	}
	if got := rr.Header().Get("Content-Disposition"); got != "" {
		t.Fatalf("Content-Disposition=%q want empty", got)
	}

	var resp apiResp
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v body=%q", err, rr.Body.String())
	}
	if resp.Code == 0 {
		t.Fatalf("resp.code=%d want non-zero", resp.Code)
	}
	if !strings.Contains(resp.Message, "template.dir") {
		t.Fatalf("resp.message=%q want contains %q", resp.Message, "template.dir")
	}
}

func TestApplyImport_RollbackDoesNotCorruptMissingConfig(t *testing.T) {
	baseDir := t.TempDir()
	configPath := filepath.Join(baseDir, "config.yaml")
	templatesDir := filepath.Join(baseDir, "templates")

	cfg := &config.Config{
		Template: config.TemplateConfig{
			Default: "default",
			Dir:     templatesDir,
		},
		DingTalk: config.DingTalkConfig{
			Timeout: config.Duration(2 * time.Second),
			Robots: []config.RobotConfig{
				{
					Name:    "default",
					Webhook: "http://example.com",
					MsgType: "markdown",
					Title:   "Alertmanager",
				},
			},
			Receivers: map[string][]string{
				"default": {"default"},
			},
		},
	}

	cfgForStore := *cfg
	cfgForStore.Template.Dir = ""
	rt, err := runtime.Build(nil, configPath, baseDir, &cfgForStore)
	if err != nil {
		t.Fatalf("runtime.Build: %v", err)
	}
	store := runtime.NewStore(rt)
	reloadMgr, err := reload.New(nil, configPath, store, false, 0)
	if err != nil {
		t.Fatalf("reload.New: %v", err)
	}

	err = applyImport(
		context.Background(),
		nil,
		reloadMgr,
		configPath,
		cfg,
		[]byte("this: ["),
		map[string][]byte{"default": []byte(template.EmbeddedDefaultText())},
	)
	if err == nil {
		t.Fatalf("applyImport: want error")
	}

	if _, err := os.Stat(configPath); err == nil {
		t.Fatalf("configPath should not exist after rollback")
	} else if !os.IsNotExist(err) {
		t.Fatalf("os.Stat(configPath): %v", err)
	}

	if _, err := os.Stat(templatesDir); err == nil {
		t.Fatalf("templatesDir should not exist after rollback")
	} else if !os.IsNotExist(err) {
		t.Fatalf("os.Stat(templatesDir): %v", err)
	}
}
