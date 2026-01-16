// Package admin provides Basic Auth protected admin APIs and a simple Web UI.
package admin

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"prometheus-dingtalk-hook/internal/alertmanager"
	"prometheus-dingtalk-hook/internal/config"
	"prometheus-dingtalk-hook/internal/dingtalk"
	"prometheus-dingtalk-hook/internal/reload"
	"prometheus-dingtalk-hook/internal/runtime"
	"prometheus-dingtalk-hook/internal/template"

	"gopkg.in/yaml.v3"
)

//go:embed ui/index.html
var indexHTML []byte

type Options struct {
	Logger     *slog.Logger
	ConfigPath string
	Store      *runtime.Store
	Reload     *reload.Manager
}

func New(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &handler{
		logger:     opts.Logger,
		configPath: opts.ConfigPath,
		store:      opts.Store,
		reload:     opts.Reload,
	}
}

type handler struct {
	logger     *slog.Logger
	configPath string
	store      *runtime.Store
	reload     *reload.Manager
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt := h.store.Load()
	if rt == nil || rt.Config == nil {
		writeJSON(w, http.StatusServiceUnavailable, apiResp{Code: 1, Message: "runtime not ready"})
		return
	}

	if !rt.Config.Admin.Enabled {
		http.NotFound(w, r)
		return
	}

	if !checkBasicAuth(r, rt.Config.Admin.BasicAuth) {
		w.Header().Set("WWW-Authenticate", `Basic realm="admin"`)
		writeJSON(w, http.StatusUnauthorized, apiResp{Code: 1, Message: "unauthorized"})
		return
	}

	switch {
	case r.URL.Path == "" || r.URL.Path == "/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(indexHTML)
		return

	case r.URL.Path == "/api/v1/status":
		h.handleStatus(w, r, rt)
		return

	case r.URL.Path == "/api/v1/reload":
		h.handleReload(w, r)
		return

	case r.URL.Path == "/api/v1/config":
		h.handleConfig(w, r)
		return

	case r.URL.Path == "/api/v1/config/json":
		h.handleConfigJSON(w, r)
		return

	case r.URL.Path == "/api/v1/templates":
		h.handleTemplates(w, r, rt)
		return

	case strings.HasPrefix(r.URL.Path, "/api/v1/templates/"):
		raw := strings.TrimPrefix(r.URL.Path, "/api/v1/templates/")
		name, err := url.PathUnescape(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: "invalid template path"})
			return
		}
		h.handleTemplate(w, r, rt, name)
		return

	case r.URL.Path == "/api/v1/render":
		h.handleRender(w, r, rt)
		return

	case r.URL.Path == "/api/v1/send":
		h.handleSend(w, r, rt)
		return

	case r.URL.Path == "/api/v1/export":
		h.handleExport(w, r, rt)
		return

	case r.URL.Path == "/api/v1/import":
		h.handleImport(w, r, rt)
		return
	}

	http.NotFound(w, r)
}

type apiResp struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

type configSensitiveInfo struct {
	AuthTokenSet           bool                          `json:"auth_token_set"`
	AdminPasswordSet       bool                          `json:"admin_password_set"`
	AdminPasswordSHA256Set bool                          `json:"admin_password_sha256_set"`
	AdminSaltSet           bool                          `json:"admin_salt_set"`
	Robots                 map[string]robotSensitiveInfo `json:"robots"`
}

type robotSensitiveInfo struct {
	WebhookSet bool `json:"webhook_set"`
	SecretSet  bool `json:"secret_set"`
}

type configClearSensitive struct {
	AuthToken           bool                           `json:"auth_token"`
	AdminPassword       bool                           `json:"admin_password"`
	AdminPasswordSHA256 bool                           `json:"admin_password_sha256"`
	AdminSalt           bool                           `json:"admin_salt"`
	Robots              map[string]robotClearSensitive `json:"robots"`
}

type robotClearSensitive struct {
	Webhook bool `json:"webhook"`
	Secret  bool `json:"secret"`
}

type adminConfigJSON struct {
	Server   config.ServerConfig
	Auth     adminAuthConfigJSON
	Admin    adminAdminConfigJSON
	Reload   config.ReloadConfig
	Template adminTemplateConfigJSON
	DingTalk adminDingTalkConfigJSON
}

type adminAuthConfigJSON struct {
	Token config.Secret
}

type adminAdminConfigJSON struct {
	Enabled    bool
	PathPrefix string
	BasicAuth  adminBasicAuthConfigJSON
}

type adminBasicAuthConfigJSON struct {
	Username       string
	Password       config.Secret
	PasswordSHA256 config.Secret
	Salt           config.Secret
}

type adminTemplateConfigJSON struct {
	Dir string
}

type adminDingTalkConfigJSON struct {
	Timeout  config.Duration
	Robots   []adminRobotConfigJSON
	Channels []config.ChannelConfig
	Routes   []config.RouteConfig
}

type adminRobotConfigJSON struct {
	Name    string
	Webhook config.SecretURL
	Secret  config.Secret
	MsgType string
	Title   string
}

func toAdminConfigJSON(cfg *config.Config, baseDir string) adminConfigJSON {
	out := adminConfigJSON{
		Server: cfg.Server,
		Auth: adminAuthConfigJSON{
			Token: config.Secret(cfg.Auth.Token),
		},
		Admin: adminAdminConfigJSON{
			Enabled:    cfg.Admin.Enabled,
			PathPrefix: cfg.Admin.PathPrefix,
			BasicAuth: adminBasicAuthConfigJSON{
				Username:       cfg.Admin.BasicAuth.Username,
				Password:       config.Secret(cfg.Admin.BasicAuth.Password),
				PasswordSHA256: config.Secret(cfg.Admin.BasicAuth.PasswordSHA256),
				Salt:           config.Secret(cfg.Admin.BasicAuth.Salt),
			},
		},
		Reload: cfg.Reload,
		Template: adminTemplateConfigJSON{
			Dir: pathToRelIfUnderBase(baseDir, cfg.Template.Dir),
		},
		DingTalk: adminDingTalkConfigJSON{
			Timeout:  cfg.DingTalk.Timeout,
			Robots:   make([]adminRobotConfigJSON, len(cfg.DingTalk.Robots)),
			Channels: append([]config.ChannelConfig(nil), cfg.DingTalk.Channels...),
			Routes:   append([]config.RouteConfig(nil), cfg.DingTalk.Routes...),
		},
	}

	for i, r := range cfg.DingTalk.Robots {
		out.DingTalk.Robots[i] = adminRobotConfigJSON{
			Name:    r.Name,
			Webhook: config.SecretURL(r.Webhook),
			Secret:  config.Secret(r.Secret),
			MsgType: r.MsgType,
			Title:   r.Title,
		}
	}

	return out
}

func scrubSecretPlaceholders(cfg *config.Config) {
	if cfg == nil {
		return
	}

	if cfg.Auth.Token == config.RedactedSecret {
		cfg.Auth.Token = ""
	}

	if cfg.Admin.BasicAuth.Password == config.RedactedSecret {
		cfg.Admin.BasicAuth.Password = ""
	}
	if cfg.Admin.BasicAuth.PasswordSHA256 == config.RedactedSecret {
		cfg.Admin.BasicAuth.PasswordSHA256 = ""
	}
	if cfg.Admin.BasicAuth.Salt == config.RedactedSecret {
		cfg.Admin.BasicAuth.Salt = ""
	}

	for i := range cfg.DingTalk.Robots {
		if cfg.DingTalk.Robots[i].Webhook == config.RedactedSecret {
			cfg.DingTalk.Robots[i].Webhook = ""
		}
		if cfg.DingTalk.Robots[i].Secret == config.RedactedSecret {
			cfg.DingTalk.Robots[i].Secret = ""
		}
	}
}

func (h *handler) handleStatus(w http.ResponseWriter, r *http.Request, rt *runtime.Runtime) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, apiResp{Code: 1, Message: "method not allowed"})
		return
	}
	var reloadStatus any
	if h.reload != nil {
		reloadStatus = h.reload.Status()
	}
	writeJSON(w, http.StatusOK, apiResp{Code: 0, Data: map[string]any{
		"mode":      "channels",
		"loaded_at": rt.LoadedAt,
		"reload":    reloadStatus,
		"templates": rt.Renderer.TemplateNames(),
		"channels":  sortedKeys(rt.Channels),
	}})
}

func (h *handler) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, apiResp{Code: 1, Message: "method not allowed"})
		return
	}
	if h.reload == nil {
		writeJSON(w, http.StatusNotImplemented, apiResp{Code: 1, Message: "reload is not configured"})
		return
	}
	if err := h.reload.Reload(r.Context(), true); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, apiResp{Code: 0, Message: "ok"})
}

func (h *handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := os.ReadFile(h.configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	case http.MethodPut:
		if h.reload == nil {
			writeJSON(w, http.StatusNotImplemented, apiResp{Code: 1, Message: "reload is not configured"})
			return
		}
		newData, err := readLimited(r.Body, 2<<20)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
			return
		}
		oldData, _ := os.ReadFile(h.configPath)

		baseDir := filepath.Dir(h.configPath)
		parsed, err := config.Parse(newData, baseDir)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
			return
		}
		if _, err := runtime.Build(h.logger, h.configPath, baseDir, parsed); err != nil {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
			return
		}

		if err := writeFileAtomic(h.configPath, newData, 0o600); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
			return
		}

		if err := h.reload.Reload(r.Context(), true); err != nil {
			_ = writeFileAtomic(h.configPath, oldData, 0o600)
			_ = h.reload.Reload(r.Context(), true)
			writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, apiResp{Code: 0, Message: "ok"})
		return
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPut)
		writeJSON(w, http.StatusMethodNotAllowed, apiResp{Code: 1, Message: "method not allowed"})
		return
	}
}

func (h *handler) handleConfigJSON(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := os.ReadFile(h.configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
			return
		}

		baseDir := filepath.Dir(h.configPath)
		parsed, err := config.Parse(data, baseDir)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
			return
		}

		sensitive := configSensitiveInfo{
			AuthTokenSet:           strings.TrimSpace(parsed.Auth.Token) != "",
			AdminPasswordSet:       strings.TrimSpace(parsed.Admin.BasicAuth.Password) != "",
			AdminPasswordSHA256Set: strings.TrimSpace(parsed.Admin.BasicAuth.PasswordSHA256) != "",
			AdminSaltSet:           strings.TrimSpace(parsed.Admin.BasicAuth.Salt) != "",
			Robots:                 make(map[string]robotSensitiveInfo, len(parsed.DingTalk.Robots)),
		}
		for _, robot := range parsed.DingTalk.Robots {
			name := strings.TrimSpace(robot.Name)
			if name == "" {
				continue
			}
			sensitive.Robots[name] = robotSensitiveInfo{
				WebhookSet: strings.TrimSpace(robot.Webhook) != "",
				SecretSet:  strings.TrimSpace(robot.Secret) != "",
			}
		}

		cfg := toAdminConfigJSON(parsed, baseDir)

		writeJSON(w, http.StatusOK, apiResp{Code: 0, Data: map[string]any{
			"config":    cfg,
			"sensitive": sensitive,
		}})
		return

	case http.MethodPut:
		if h.reload == nil {
			writeJSON(w, http.StatusNotImplemented, apiResp{Code: 1, Message: "reload is not configured"})
			return
		}

		var req struct {
			Config         config.Config        `json:"config"`
			ClearSensitive configClearSensitive `json:"clear_sensitive"`
		}
		if err := decodeJSONLimited(r.Body, &req, 2<<20); err != nil {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
			return
		}

		baseDir := filepath.Dir(h.configPath)
		oldCfgBytes, err := os.ReadFile(h.configPath)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
			return
		}
		oldCfg, err := config.Parse(oldCfgBytes, baseDir)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
			return
		}

		merged := req.Config
		scrubSecretPlaceholders(&merged)
		mergeSensitiveConfig(&merged, oldCfg, req.ClearSensitive)

		yamlBytes, err := yaml.Marshal(&merged)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
			return
		}

		parsed, err := config.Parse(yamlBytes, baseDir)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
			return
		}
		if _, err := runtime.Build(h.logger, h.configPath, baseDir, parsed); err != nil {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
			return
		}

		if err := writeFileAtomic(h.configPath, yamlBytes, 0o600); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
			return
		}

		if err := h.reload.Reload(r.Context(), true); err != nil {
			_ = writeFileAtomic(h.configPath, oldCfgBytes, 0o600)
			_ = h.reload.Reload(r.Context(), true)
			writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, apiResp{Code: 0, Message: "ok"})
		return

	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPut)
		writeJSON(w, http.StatusMethodNotAllowed, apiResp{Code: 1, Message: "method not allowed"})
		return
	}
}

func pathToRelIfUnderBase(baseDir, p string) string {
	baseDir = strings.TrimSpace(baseDir)
	p = strings.TrimSpace(p)
	if baseDir == "" || p == "" {
		return p
	}
	if !filepath.IsAbs(p) {
		return p
	}
	rel, err := filepath.Rel(baseDir, p)
	if err != nil {
		return p
	}
	if rel == "." {
		return "."
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return p
	}
	return rel
}

func mergeSensitiveConfig(dst *config.Config, old *config.Config, clear configClearSensitive) {
	if dst == nil || old == nil {
		return
	}

	if clear.AuthToken {
		dst.Auth.Token = ""
	} else if strings.TrimSpace(dst.Auth.Token) == "" {
		dst.Auth.Token = old.Auth.Token
	}

	userSetAdminPassword := strings.TrimSpace(dst.Admin.BasicAuth.Password) != ""
	userSetAdminSHA := strings.TrimSpace(dst.Admin.BasicAuth.PasswordSHA256) != ""
	if clear.AdminPassword {
		dst.Admin.BasicAuth.Password = ""
	} else if strings.TrimSpace(dst.Admin.BasicAuth.Password) == "" && !userSetAdminSHA && !clear.AdminPasswordSHA256 {
		dst.Admin.BasicAuth.Password = old.Admin.BasicAuth.Password
	}

	if clear.AdminPasswordSHA256 {
		dst.Admin.BasicAuth.PasswordSHA256 = ""
	} else if strings.TrimSpace(dst.Admin.BasicAuth.PasswordSHA256) == "" && !userSetAdminPassword && !clear.AdminPassword {
		dst.Admin.BasicAuth.PasswordSHA256 = old.Admin.BasicAuth.PasswordSHA256
	}

	if clear.AdminSalt {
		dst.Admin.BasicAuth.Salt = ""
	} else if strings.TrimSpace(dst.Admin.BasicAuth.Salt) == "" && !userSetAdminPassword && !clear.AdminPassword {
		dst.Admin.BasicAuth.Salt = old.Admin.BasicAuth.Salt
	}

	oldRobots := make(map[string]config.RobotConfig, len(old.DingTalk.Robots))
	for _, r := range old.DingTalk.Robots {
		oldRobots[strings.TrimSpace(r.Name)] = r
	}

	for i := range dst.DingTalk.Robots {
		name := strings.TrimSpace(dst.DingTalk.Robots[i].Name)
		prev, ok := oldRobots[name]
		if !ok {
			continue
		}

		clearRobot := robotClearSensitive{}
		if clear.Robots != nil {
			clearRobot = clear.Robots[name]
		}

		if clearRobot.Webhook {
			dst.DingTalk.Robots[i].Webhook = ""
		} else if strings.TrimSpace(dst.DingTalk.Robots[i].Webhook) == "" {
			dst.DingTalk.Robots[i].Webhook = prev.Webhook
		}

		if clearRobot.Secret {
			dst.DingTalk.Robots[i].Secret = ""
		} else if strings.TrimSpace(dst.DingTalk.Robots[i].Secret) == "" {
			dst.DingTalk.Robots[i].Secret = prev.Secret
		}
	}
}

func (h *handler) handleTemplates(w http.ResponseWriter, r *http.Request, rt *runtime.Runtime) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, apiResp{Code: 1, Message: "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, apiResp{Code: 0, Data: map[string]any{
		"templates": rt.Renderer.TemplateNames(),
	}})
}

func (h *handler) handleTemplate(w http.ResponseWriter, r *http.Request, rt *runtime.Runtime, name string) {
	if !config.ValidTemplateName(name) {
		writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: "invalid template name"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		text, err := h.readTemplate(rt, name)
		if err != nil {
			writeJSON(w, http.StatusNotFound, apiResp{Code: 1, Message: err.Error()})
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(text))
		return

	case http.MethodPut:
		if h.reload == nil {
			writeJSON(w, http.StatusNotImplemented, apiResp{Code: 1, Message: "reload is not configured"})
			return
		}
		dir := strings.TrimSpace(rt.Config.Template.Dir)
		if dir == "" {
			writeJSON(w, http.StatusConflict, apiResp{Code: 1, Message: "template.dir is not configured"})
			return
		}
		if err := ensureUnderBase(filepath.Dir(h.configPath), dir); err != nil {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
			return
		}

		data, err := readLimited(r.Body, 2<<20)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
			return
		}

		if err := template.ValidateText(string(data)); err != nil {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
			return
		}

		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
			return
		}

		path := filepath.Join(dir, name+".tmpl")
		old, oldErr := os.ReadFile(path)
		oldExists := oldErr == nil

		if err := writeFileAtomic(path, data, 0o644); err != nil {
			writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
			return
		}

		if err := h.reload.Reload(r.Context(), true); err != nil {
			if oldExists {
				_ = writeFileAtomic(path, old, 0o644)
			} else {
				_ = os.Remove(path)
			}
			_ = h.reload.Reload(r.Context(), true)
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, apiResp{Code: 0, Message: "ok"})
		return

	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPut)
		writeJSON(w, http.StatusMethodNotAllowed, apiResp{Code: 1, Message: "method not allowed"})
		return
	}
}

func (h *handler) readTemplate(rt *runtime.Runtime, name string) (string, error) {
	dir := strings.TrimSpace(rt.Config.Template.Dir)
	if dir != "" {
		path := filepath.Join(dir, name+".tmpl")
		if b, err := os.ReadFile(path); err == nil {
			return string(b), nil
		}
	}

	if name == "default" {
		return template.EmbeddedDefaultText(), nil
	}

	return "", errors.New("template not found")
}

func (h *handler) handleRender(w http.ResponseWriter, r *http.Request, rt *runtime.Runtime) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, apiResp{Code: 1, Message: "method not allowed"})
		return
	}

	var req struct {
		Channel      string                      `json:"channel"`
		Template     string                      `json:"template"`
		TemplateText string                      `json:"template_text"`
		Payload      alertmanager.WebhookMessage `json:"payload"`
	}
	if err := decodeJSONLimited(r.Body, &req, 2<<20); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
		return
	}

	var content string
	var err error
	if strings.TrimSpace(req.TemplateText) != "" {
		content, err = template.RenderText(req.TemplateText, req.Payload)
	} else if strings.TrimSpace(req.Channel) != "" {
		ch, ok := rt.Channels[strings.TrimSpace(req.Channel)]
		if !ok {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: "unknown channel"})
			return
		}
		content, err = rt.Renderer.Render(ch.Template, req.Payload)
	} else {
		content, err = rt.Renderer.Render(strings.TrimSpace(req.Template), req.Payload)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, apiResp{Code: 0, Data: map[string]any{"content": content}})
}

func (h *handler) handleSend(w http.ResponseWriter, r *http.Request, rt *runtime.Runtime) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, apiResp{Code: 1, Message: "method not allowed"})
		return
	}

	var req struct {
		Channel string                      `json:"channel"`
		Payload alertmanager.WebhookMessage `json:"payload"`
		RawText string                      `json:"raw_text"`
	}
	if err := decodeJSONLimited(r.Body, &req, 2<<20); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
		return
	}

	chName := strings.TrimSpace(req.Channel)
	if chName == "" {
		chName = "default"
	}
	ch, ok := rt.Channels[chName]
	if !ok {
		writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: "unknown channel"})
		return
	}

	var content string
	if strings.TrimSpace(req.RawText) != "" {
		content = req.RawText
	} else {
		var err error
		content, err = rt.Renderer.Render(ch.Template, req.Payload)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
			return
		}
	}

	mention := ch.EffectiveMention(req.Payload)
	var at *dingtalk.At
	if mention.AtAll || len(mention.AtMobiles) > 0 || len(mention.AtUserIds) > 0 {
		at = &dingtalk.At{AtMobiles: mention.AtMobiles, AtUserIds: mention.AtUserIds, IsAtAll: mention.AtAll}
	}

	var sendErrs []error
	for _, robot := range ch.Robots {
		msgType := strings.TrimSpace(robot.MsgType)
		dtMsg := dingtalk.Message{
			MsgType: msgType,
			Title:   robot.Title,
			At:      at,
		}
		switch msgType {
		case "markdown":
			dtMsg.Markdown = content
		case "text":
			dtMsg.Text = content
		default:
			sendErrs = append(sendErrs, fmt.Errorf("unsupported msg_type %q", msgType))
			continue
		}
		if err := rt.DingTalk.Send(r.Context(), robot.Webhook, robot.Secret, dtMsg); err != nil {
			sendErrs = append(sendErrs, err)
		}
	}
	if len(sendErrs) > 0 {
		writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: sendErrs[0].Error()})
		return
	}

	writeJSON(w, http.StatusOK, apiResp{Code: 0, Message: "ok"})
}

func (h *handler) handleExport(w http.ResponseWriter, r *http.Request, rt *runtime.Runtime) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, apiResp{Code: 1, Message: "method not allowed"})
		return
	}

	cfgBytes, err := os.ReadFile(h.configPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
		return
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := zipWriteFile(zw, "config.yaml", cfgBytes); err != nil {
		_ = zw.Close()
		writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
		return
	}
	if err := h.zipTemplates(zw, rt); err != nil {
		_ = zw.Close()
		writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
		return
	}
	if err := zw.Close(); err != nil {
		writeJSON(w, http.StatusInternalServerError, apiResp{Code: 1, Message: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="prometheus-dingtalk-hook-export.zip"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (h *handler) zipTemplates(zw *zip.Writer, rt *runtime.Runtime) error {
	dir := strings.TrimSpace(rt.Config.Template.Dir)
	if dir == "" {
		return errors.New("template.dir is not configured")
	}
	if err := ensureUnderBase(filepath.Dir(h.configPath), dir); err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".tmpl" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		if err := zipWriteFile(zw, path.Join("templates", e.Name()), b); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "default.tmpl")); err != nil && errors.Is(err, os.ErrNotExist) {
		if err := zipWriteFile(zw, "templates/default.tmpl", []byte(template.EmbeddedDefaultText())); err != nil {
			return err
		}
	}
	return nil
}

func (h *handler) handleImport(w http.ResponseWriter, r *http.Request, rt *runtime.Runtime) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, apiResp{Code: 1, Message: "method not allowed"})
		return
	}
	if h.reload == nil {
		writeJSON(w, http.StatusNotImplemented, apiResp{Code: 1, Message: "reload is not configured"})
		return
	}

	body, err := readLimited(r.Body, 10<<20)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
		return
	}

	cfgBytes, templates, err := parseZip(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
		return
	}

	baseDir := filepath.Dir(h.configPath)
	parsed, err := config.Parse(cfgBytes, baseDir)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
		return
	}
	if strings.TrimSpace(parsed.Template.Dir) == "" {
		writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: "template.dir is required for import"})
		return
	}
	if err := ensureUnderBase(baseDir, parsed.Template.Dir); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
		return
	}

	if err := applyImport(r.Context(), h.logger, h.reload, h.configPath, parsed, cfgBytes, templates); err != nil {
		writeJSON(w, http.StatusBadRequest, apiResp{Code: 1, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, apiResp{Code: 0, Message: "ok"})
}

func checkBasicAuth(r *http.Request, cfg config.BasicAuthConfig) bool {
	username, password, ok := r.BasicAuth()
	if !ok {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(username), []byte(cfg.Username)) != 1 {
		return false
	}

	if strings.TrimSpace(cfg.PasswordSHA256) != "" {
		salt, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cfg.Salt))
		if err != nil {
			return false
		}
		want, err := hex.DecodeString(strings.TrimSpace(cfg.PasswordSHA256))
		if err != nil {
			return false
		}
		sum := sha256.Sum256(append(salt, []byte(password)...))
		return subtle.ConstantTimeCompare(sum[:], want) == 1
	}

	return subtle.ConstantTimeCompare([]byte(password), []byte(cfg.Password)) == 1
}

func decodeJSONLimited(r io.Reader, v any, limit int64) error {
	data, err := readLimited(r, limit)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return err
	}
	return nil
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) >= limit {
		return nil, errors.New("body too large")
	}
	return data, nil
}

func writeJSON(w http.ResponseWriter, status int, v apiResp) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func ensureUnderBase(baseDir, target string) error {
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(base, pathAbs)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return fmt.Errorf("path %q must be under %q", target, baseDir)
	}
	return nil
}

func zipWriteFile(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func parseZip(data []byte) ([]byte, map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, err
	}
	var cfg []byte
	templates := make(map[string][]byte)
	for _, f := range zr.File {
		clean := path.Clean(f.Name)
		if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, nil, err
		}
		b, err := readLimited(rc, 2<<20)
		_ = rc.Close()
		if err != nil {
			return nil, nil, err
		}

		if clean == "config.yaml" {
			cfg = b
			continue
		}
		if strings.HasPrefix(clean, "templates/") && filepath.Ext(clean) == ".tmpl" {
			base := strings.TrimSuffix(path.Base(clean), ".tmpl")
			if !config.ValidTemplateName(base) {
				continue
			}
			templates[base] = b
		}
	}
	if len(cfg) == 0 {
		return nil, nil, errors.New("missing config.yaml in zip")
	}
	return cfg, templates, nil
}

func applyImport(ctx context.Context, logger *slog.Logger, reloadMgr *reload.Manager, configPath string, cfg *config.Config, cfgBytes []byte, templates map[string][]byte) error {
	if logger == nil {
		logger = slog.Default()
	}
	if reloadMgr == nil {
		return errors.New("reload is not configured")
	}
	if len(templates) == 0 {
		return errors.New("missing templates in zip")
	}

	baseDir := filepath.Dir(configPath)
	newTemplatesDir := strings.TrimSpace(cfg.Template.Dir)

	oldCfgBytes, err := os.ReadFile(configPath)
	oldCfgExists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	restoreConfig := func() {
		if oldCfgExists {
			_ = writeFileAtomic(configPath, oldCfgBytes, 0o600)
			return
		}
		_ = os.Remove(configPath)
	}

	if err := os.MkdirAll(filepath.Dir(newTemplatesDir), 0o755); err != nil {
		return err
	}

	stagingDir, err := os.MkdirTemp(filepath.Dir(newTemplatesDir), ".import-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stagingDir)

	for name, b := range templates {
		if err := template.ValidateText(string(b)); err != nil {
			return fmt.Errorf("invalid template %q: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(stagingDir, name+".tmpl"), b, 0o644); err != nil {
			return err
		}
	}

	// Validate by compiling everything in stagingDir first to avoid polluting the live dir.
	cfgCopy := *cfg
	cfgCopy.Template.Dir = stagingDir
	if _, err := runtime.Build(logger, configPath, baseDir, &cfgCopy); err != nil {
		return err
	}

	var backupDir string
	if st, err := os.Stat(newTemplatesDir); err == nil && st.IsDir() {
		backupDir = newTemplatesDir + ".bak-" + time.Now().Format("20060102150405")
		_ = os.RemoveAll(backupDir)
		if err := os.Rename(newTemplatesDir, backupDir); err != nil {
			return err
		}
	}

	restoreTemplates := func() {
		_ = os.RemoveAll(newTemplatesDir)
		if backupDir != "" {
			_ = os.Rename(backupDir, newTemplatesDir)
		}
	}

	if err := os.Rename(stagingDir, newTemplatesDir); err != nil {
		if backupDir != "" {
			_ = os.Rename(backupDir, newTemplatesDir)
		}
		return err
	}

	if err := writeFileAtomic(configPath, cfgBytes, 0o600); err != nil {
		restoreConfig()
		restoreTemplates()
		return err
	}

	if err := reloadMgr.Reload(ctx, true); err != nil {
		restoreConfig()
		restoreTemplates()
		_ = reloadMgr.Reload(ctx, true)
		return err
	}

	if backupDir != "" {
		_ = os.RemoveAll(backupDir)
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
