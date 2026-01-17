package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"prometheus-dingtalk-hook/internal/reload"
	"prometheus-dingtalk-hook/internal/runtime"
)

func TestHandler_ReloadEndpoint(t *testing.T) {
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
	mgr, err := reload.New(nil, cfgPath, store, false, 2*time.Second)
	if err != nil {
		t.Fatalf("reload.New: %v", err)
	}

	h := NewHandler(HandlerOptions{
		State:        store,
		Reload:       mgr,
		MaxBodyBytes: 1 << 20,
	})

	{
		req := httptest.NewRequest(http.MethodPost, "/-/reload", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
	}

	if err := os.WriteFile(cfgPath, []byte(`dingtalk: [invalid`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	{
		req := httptest.NewRequest(http.MethodPost, "/-/reload", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
	}
}
