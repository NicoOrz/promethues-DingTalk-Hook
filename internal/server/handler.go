// 包 server 提供 HTTP 服务入口（告警接收、鉴权、管理接口）。
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"prometheus-dingtalk-hook/internal/alertmanager"
	"prometheus-dingtalk-hook/internal/dingtalk"
	"prometheus-dingtalk-hook/internal/reload"
	"prometheus-dingtalk-hook/internal/router"
	"prometheus-dingtalk-hook/internal/runtime"
)

type HandlerOptions struct {
	Logger       log.Logger
	AlertPath    string
	AdminPrefix  string
	AdminHandler http.Handler
	State        *runtime.Store
	Reload       *reload.Manager
	MaxBodyBytes int64
}

func defaultMarkdownTitle(msg alertmanager.WebhookMessage) string {
	if msg.CommonAnnotations != nil {
		if v := strings.TrimSpace(msg.CommonAnnotations["summary"]); v != "" {
			return v
		}
	}
	if len(msg.Alerts) > 0 && msg.Alerts[0].Annotations != nil {
		if v := strings.TrimSpace(msg.Alerts[0].Annotations["summary"]); v != "" {
			return v
		}
	}
	if msg.CommonLabels != nil {
		if v := strings.TrimSpace(msg.CommonLabels["alertname"]); v != "" {
			return v
		}
	}
	if len(msg.Alerts) > 0 && msg.Alerts[0].Labels != nil {
		if v := strings.TrimSpace(msg.Alerts[0].Labels["alertname"]); v != "" {
			return v
		}
	}
	return "Alertmanager"
}

func NewHandler(opts HandlerOptions) http.Handler {
	if opts.Logger == nil {
		opts.Logger = log.NewNopLogger()
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": "ready"})
	})

	if opts.Reload != nil {
		mux.HandleFunc("/-/reload", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"code": 405, "message": "method not allowed"})
				return
			}
			if err := opts.Reload.Reload(r.Context(), true); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"code": 500, "message": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": "ok"})
		})
	}

	if opts.AdminHandler != nil {
		prefix := opts.AdminPrefix
		if prefix == "" {
			prefix = "/admin"
		}
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		mux.Handle(prefix+"/", http.StripPrefix(prefix, opts.AdminHandler))
		mux.Handle(prefix, http.RedirectHandler(prefix+"/", http.StatusMovedPermanently))
	}

	path := opts.AlertPath
	if path == "" {
		path = "/alert"
	}
	mux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleAlert(w, r, opts)
	}))

	return mux
}

func handleAlert(w http.ResponseWriter, r *http.Request, opts HandlerOptions) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"code": 405, "message": "method not allowed"})
		return
	}

	if ct := strings.TrimSpace(r.Header.Get("Content-Type")); ct != "" && !strings.Contains(ct, "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"code": 415, "message": "content-type must be application/json"})
		return
	}

	rt := opts.State.Load()
	if rt == nil {
		level.Error(opts.Logger).Log("msg", "runtime state is nil")
		writeJSON(w, http.StatusInternalServerError, map[string]any{"code": 500, "message": "runtime not ready"})
		return
	}

	if err := checkToken(r, rt.Config.Auth.Token); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"code": 401, "message": "unauthorized"})
		return
	}

	body := http.MaxBytesReader(w, r.Body, opts.MaxBodyBytes)
	defer body.Close()

	data, err := io.ReadAll(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": 400, "message": "read body failed"})
		return
	}

	var msg alertmanager.WebhookMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		level.Warn(opts.Logger).Log("msg", "invalid payload", "err", err)
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": 400, "message": "invalid json"})
		return
	}

	channelNames := router.FirstMatch(rt.Routes, msg)
	if len(channelNames) == 0 {
		channelNames = []string{"default"}
	}

	var sendErrs []error
	for _, channelName := range channelNames {
		channel, ok := rt.Channels[channelName]
		if !ok {
			sendErrs = append(sendErrs, errors.New("unknown channel "+channelName))
			continue
		}

		content, err := rt.Renderer.Render(channel.Template, msg)
		if err != nil {
			level.Error(opts.Logger).Log("msg", "render failed", "channel", channel.Name, "err", err)
			sendErrs = append(sendErrs, err)
			continue
		}

		mention := channel.EffectiveMention(msg)
		var at *dingtalk.At
		if mention.AtAll || len(mention.AtMobiles) > 0 || len(mention.AtUserIds) > 0 {
			at = &dingtalk.At{
				AtMobiles: mention.AtMobiles,
				AtUserIds: mention.AtUserIds,
				IsAtAll:   mention.AtAll,
			}
		}

		for _, robot := range channel.Robots {
			msgType := strings.TrimSpace(robot.MsgType)
			dtMsg := dingtalk.Message{
				MsgType: msgType,
				Title:   strings.TrimSpace(robot.Title),
				At:      at,
			}
			switch msgType {
			case "markdown":
				if dtMsg.Title == "" {
					dtMsg.Title = defaultMarkdownTitle(msg)
				}
				dtMsg.Markdown = content
			case "text":
				dtMsg.Text = content
			default:
				sendErrs = append(sendErrs, errors.New("unsupported msg_type "+msgType))
				continue
			}

			if err := rt.DingTalk.Send(r.Context(), robot.Webhook, robot.Secret, dtMsg); err != nil {
				level.Error(opts.Logger).Log("msg", "send failed", "robot", robot.Name, "receiver", msg.Receiver, "channel", channel.Name, "err", err)
				sendErrs = append(sendErrs, err)
			}
		}
	}

	if len(sendErrs) > 0 {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"code": 500, "message": "send failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "message": "ok"})
}

func checkToken(r *http.Request, expected string) error {
	if strings.TrimSpace(expected) == "" {
		return nil
	}

	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		token := strings.TrimSpace(auth[len("bearer "):])
		if token == expected {
			return nil
		}
		return errors.New("token mismatch")
	}

	if token := strings.TrimSpace(r.Header.Get("X-Token")); token != "" {
		if token == expected {
			return nil
		}
		return errors.New("token mismatch")
	}

	return errors.New("missing token")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
