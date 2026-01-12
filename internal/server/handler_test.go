package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"prometheus-dingtalk-hook/internal/config"
	"prometheus-dingtalk-hook/internal/runtime"
)

func TestHandler_TokenAuth(t *testing.T) {
	dt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	t.Cleanup(dt.Close)

		cfg := &config.Config{
			Auth: config.AuthConfig{Token: "t"},
			Template: config.TemplateConfig{},
			DingTalk: config.DingTalkConfig{
				Timeout: config.Duration(2 * time.Second),
				Robots: []config.RobotConfig{
					{
					Name:    "default",
					Webhook: dt.URL,
					MsgType: "markdown",
					Title:   "Alertmanager",
				},
			},
			Channels: []config.ChannelConfig{
				{
					Name:   "default",
					Robots: []string{"default"},
				},
			},
		},
	}
	rt, err := runtime.Build(nil, "", "", cfg)
	if err != nil {
		t.Fatalf("runtime.Build: %v", err)
	}
	store := runtime.NewStore(rt)

	h := NewHandler(HandlerOptions{
		AlertPath:    "/alert",
		State:        store,
		MaxBodyBytes: 1 << 20,
	})

	body := map[string]any{"receiver": "default", "status": "firing", "alerts": []any{}}
	b, _ := json.Marshal(body)

	{
		req := httptest.NewRequest(http.MethodPost, "/alert", bytes.NewReader(b))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("missing token status=%d want %d", rr.Code, http.StatusUnauthorized)
		}
	}

	{
		req := httptest.NewRequest(http.MethodPost, "/alert", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer t")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("bearer token status=%d want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
		}
	}

	{
		req := httptest.NewRequest(http.MethodPost, "/alert", bytes.NewReader(b))
		req.Header.Set("X-Token", "t")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("x-token status=%d want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
		}
	}
}
