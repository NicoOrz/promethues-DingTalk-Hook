package reload

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"prometheus-dingtalk-hook/internal/runtime"
)

func TestReload_RollbackOnError(t *testing.T) {
	dir := t.TempDir()
	tplDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(tplDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tplDir, "default.tmpl"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
template:
  dir: "templates"
  default: "default"
dingtalk:
  robots:
    - name: "r1"
      webhook: "http://example.invalid"
      msg_type: "text"
  channels:
    - name: "default"
      robots: ["r1"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rt, err := runtime.LoadFromFile(nil, cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	store := runtime.NewStore(rt)

	mgr, err := New(nil, cfgPath, store, false, 2*time.Second)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	old := store.Load()
	if err := os.WriteFile(cfgPath, []byte(`dingtalk: [invalid`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := mgr.Reload(context.Background(), true); err == nil {
		t.Fatalf("expected error")
	}
	if store.Load() != old {
		t.Fatalf("runtime should not change on reload error")
	}
	if mgr.Status().LastError == "" {
		t.Fatalf("expected LastError")
	}
}

func TestReload_SuccessUpdatesStore(t *testing.T) {
	dir := t.TempDir()
	tplDir := filepath.Join(dir, "templates")
	if err := os.MkdirAll(tplDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tplDir, "default.tmpl"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
auth:
  token: "a"
template:
  dir: "templates"
  default: "default"
dingtalk:
  robots:
    - name: "r1"
      webhook: "http://example.invalid"
      msg_type: "text"
  channels:
    - name: "default"
      robots: ["r1"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rt, err := runtime.LoadFromFile(nil, cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	store := runtime.NewStore(rt)
	mgr, err := New(nil, cfgPath, store, false, 2*time.Second)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := os.WriteFile(cfgPath, []byte(`
auth:
  token: "b"
template:
  dir: "templates"
  default: "default"
dingtalk:
  robots:
    - name: "r1"
      webhook: "http://example.invalid"
      msg_type: "text"
  channels:
    - name: "default"
      robots: ["r1"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := mgr.Reload(context.Background(), true); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if store.Load().Config.Auth.Token != "b" {
		t.Fatalf("token=%q want %q", store.Load().Config.Auth.Token, "b")
	}
}
