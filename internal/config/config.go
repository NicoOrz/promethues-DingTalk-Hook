// Package config provides YAML loading, defaulting, and validation.
package config

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Auth     AuthConfig     `yaml:"auth"`
	Admin    AdminConfig    `yaml:"admin"`
	Reload   ReloadConfig   `yaml:"reload"`
	Template TemplateConfig `yaml:"template"`
	DingTalk DingTalkConfig `yaml:"dingtalk"`
}

type ServerConfig struct {
	Listen       string   `yaml:"listen"`
	Path         string   `yaml:"path"`
	ReadTimeout  Duration `yaml:"read_timeout"`
	WriteTimeout Duration `yaml:"write_timeout"`
	IdleTimeout  Duration `yaml:"idle_timeout"`
	MaxBodyBytes int64    `yaml:"max_body_bytes"`
}

type AuthConfig struct {
	Token string `yaml:"token"`
}

type AdminConfig struct {
	Enabled    bool            `yaml:"enabled"`
	PathPrefix string          `yaml:"path_prefix"`
	BasicAuth  BasicAuthConfig `yaml:"basic_auth"`
}

type BasicAuthConfig struct {
	Username       string `yaml:"username"`
	Password       string `yaml:"password"`
	PasswordSHA256 string `yaml:"password_sha256"`
	Salt           string `yaml:"salt"`
}

type ReloadConfig struct {
	Enabled  bool     `yaml:"enabled"`
	Interval Duration `yaml:"interval"`
}

type TemplateConfig struct {
	Dir string `yaml:"dir"`
}

type DingTalkConfig struct {
	Timeout  Duration        `yaml:"timeout"`
	Robots   []RobotConfig   `yaml:"robots"`
	Channels []ChannelConfig `yaml:"channels"`
	Routes   []RouteConfig   `yaml:"routes"`
}

type RobotConfig struct {
	Name    string `yaml:"name"`
	Webhook string `yaml:"webhook"`
	Secret  string `yaml:"secret"`
	MsgType string `yaml:"msg_type"`
	Title   string `yaml:"title"`
}

type WhenConfig struct {
	Receiver []string            `yaml:"receiver"`
	Status   []string            `yaml:"status"`
	Labels   map[string][]string `yaml:"labels"`
}

type MentionConfig struct {
	AtAll     bool     `yaml:"at_all"`
	AtMobiles []string `yaml:"at_mobiles"`
	AtUserIds []string `yaml:"at_user_ids"`
}

type MentionRuleConfig struct {
	Name    string        `yaml:"name"`
	When    WhenConfig    `yaml:"when"`
	Mention MentionConfig `yaml:"mention"`
}

type ChannelConfig struct {
	Name         string              `yaml:"name"`
	Robots       []string            `yaml:"robots"`
	Template     string              `yaml:"template"`
	Mention      MentionConfig       `yaml:"mention"`
	MentionRules []MentionRuleConfig `yaml:"mention_rules"`
}

type RouteConfig struct {
	Name     string     `yaml:"name"`
	When     WhenConfig `yaml:"when"`
	Channels []string   `yaml:"channels"`
}

func Load(path string) (*Config, error) {
	cfgPath := strings.TrimSpace(path)
	if cfgPath == "" {
		return nil, errors.New("config path is empty")
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(data, filepath.Dir(cfgPath))
}

func Parse(data []byte, baseDir string) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	applyDefaults(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	if strings.TrimSpace(cfg.Template.Dir) != "" && !filepath.IsAbs(cfg.Template.Dir) {
		cfg.Template.Dir = filepath.Join(baseDir, cfg.Template.Dir)
	}

	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = "0.0.0.0:8080"
	}
	if cfg.Server.Path == "" {
		cfg.Server.Path = "/alert"
	}
	if cfg.Server.ReadTimeout == 0 {
		cfg.Server.ReadTimeout = Duration(5 * time.Second)
	}
	if cfg.Server.WriteTimeout == 0 {
		cfg.Server.WriteTimeout = Duration(10 * time.Second)
	}
	if cfg.Server.IdleTimeout == 0 {
		cfg.Server.IdleTimeout = Duration(60 * time.Second)
	}
	if cfg.Server.MaxBodyBytes == 0 {
		cfg.Server.MaxBodyBytes = 4 << 20
	}

	if cfg.Admin.PathPrefix == "" {
		cfg.Admin.PathPrefix = "/admin"
	}

	if cfg.Reload.Interval == 0 {
		cfg.Reload.Interval = Duration(2 * time.Second)
	}

	if cfg.DingTalk.Timeout == 0 {
		cfg.DingTalk.Timeout = Duration(5 * time.Second)
	}

	for i := range cfg.DingTalk.Robots {
		if cfg.DingTalk.Robots[i].MsgType == "" {
			cfg.DingTalk.Robots[i].MsgType = "markdown"
		}
	}
}

func validate(cfg *Config) error {
	if !strings.HasPrefix(cfg.Server.Path, "/") {
		cfg.Server.Path = "/" + cfg.Server.Path
	}

	if cfg.Admin.PathPrefix != "" && !strings.HasPrefix(cfg.Admin.PathPrefix, "/") {
		cfg.Admin.PathPrefix = "/" + cfg.Admin.PathPrefix
	}

	if cfg.Admin.Enabled {
		if strings.TrimSpace(cfg.Admin.BasicAuth.Username) == "" {
			return errors.New("admin.basic_auth.username must not be empty")
		}
		if strings.TrimSpace(cfg.Admin.BasicAuth.Password) == "" && strings.TrimSpace(cfg.Admin.BasicAuth.PasswordSHA256) == "" {
			return errors.New("admin.basic_auth.password or admin.basic_auth.password_sha256 is required")
		}
		if strings.TrimSpace(cfg.Admin.BasicAuth.Password) != "" && strings.TrimSpace(cfg.Admin.BasicAuth.PasswordSHA256) != "" {
			return errors.New("admin.basic_auth.password and admin.basic_auth.password_sha256 are mutually exclusive")
		}
		if strings.TrimSpace(cfg.Admin.BasicAuth.PasswordSHA256) != "" {
			sha := strings.TrimSpace(cfg.Admin.BasicAuth.PasswordSHA256)
			if len(sha) != sha256.Size*2 {
				return fmt.Errorf("admin.basic_auth.password_sha256 must be %d hex chars", sha256.Size*2)
			}
			if _, err := hex.DecodeString(sha); err != nil {
				return fmt.Errorf("admin.basic_auth.password_sha256 must be hex: %w", err)
			}
			if strings.TrimSpace(cfg.Admin.BasicAuth.Salt) == "" {
				return errors.New("admin.basic_auth.salt is required when password_sha256 is set")
			}
			if _, err := base64.StdEncoding.DecodeString(strings.TrimSpace(cfg.Admin.BasicAuth.Salt)); err != nil {
				return fmt.Errorf("admin.basic_auth.salt must be base64: %w", err)
			}
		}
	}

	if len(cfg.DingTalk.Robots) == 0 {
		return errors.New("dingtalk.robots must not be empty")
	}

	robotNames := make(map[string]RobotConfig, len(cfg.DingTalk.Robots))
	for _, robot := range cfg.DingTalk.Robots {
		name := strings.TrimSpace(robot.Name)
		if name == "" {
			return errors.New("dingtalk.robots[].name must not be empty")
		}
		if _, exists := robotNames[name]; exists {
			return fmt.Errorf("dingtalk.robots has duplicate name %q", name)
		}
		webhook := strings.TrimSpace(robot.Webhook)
		if webhook == "" {
			return fmt.Errorf("dingtalk.robots[%s].webhook must not be empty", name)
		}
		msgType := strings.TrimSpace(robot.MsgType)
		if msgType != "markdown" && msgType != "text" {
			return fmt.Errorf("dingtalk.robots[%s].msg_type must be markdown or text", name)
		}
		robotNames[name] = robot
	}

	if len(cfg.DingTalk.Channels) == 0 {
		return errors.New("dingtalk.channels must not be empty (must include name \"default\")")
	}

	channelNames := make(map[string]ChannelConfig, len(cfg.DingTalk.Channels))
	for _, ch := range cfg.DingTalk.Channels {
		name := strings.TrimSpace(ch.Name)
		if name == "" {
			return errors.New("dingtalk.channels[].name must not be empty")
		}
		if _, exists := channelNames[name]; exists {
			return fmt.Errorf("dingtalk.channels has duplicate name %q", name)
		}
		if len(ch.Robots) == 0 {
			return fmt.Errorf("dingtalk.channels[%s].robots must not be empty", name)
		}
		for _, r := range ch.Robots {
			if _, ok := robotNames[r]; !ok {
				return fmt.Errorf("dingtalk.channels[%s] references unknown robot %q", name, r)
			}
		}
		channelNames[name] = ch
	}
	if _, ok := channelNames["default"]; !ok {
		return errors.New("dingtalk.channels.default is required")
	}

	for _, route := range cfg.DingTalk.Routes {
		routeName := strings.TrimSpace(route.Name)
		if routeName == "" {
			return errors.New("dingtalk.routes[].name must not be empty")
		}
		if len(route.Channels) == 0 {
			return fmt.Errorf("dingtalk.routes[%s].channels must not be empty", routeName)
		}
		for _, ch := range route.Channels {
			if _, ok := channelNames[ch]; !ok {
				return fmt.Errorf("dingtalk.routes[%s] references unknown channel %q", routeName, ch)
			}
		}
	}

	return nil
}

func (c DingTalkConfig) RobotsByName() map[string]RobotConfig {
	out := make(map[string]RobotConfig, len(c.Robots))
	for _, r := range c.Robots {
		out[r.Name] = r
	}
	return out
}

var templateNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func ValidTemplateName(name string) bool {
	return templateNameRE.MatchString(name)
}
