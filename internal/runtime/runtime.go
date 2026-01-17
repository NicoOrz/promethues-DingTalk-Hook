// Package runtime compiles config into runtime state for request handling.
package runtime

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-kit/log"

	"prometheus-dingtalk-hook/internal/alertmanager"
	"prometheus-dingtalk-hook/internal/config"
	"prometheus-dingtalk-hook/internal/dingtalk"
	"prometheus-dingtalk-hook/internal/router"
	"prometheus-dingtalk-hook/internal/template"
)

type Channel struct {
	Name         string
	Robots       []config.RobotConfig
	Template     string
	Mention      config.MentionConfig
	MentionRules []router.MentionRule
}

func (c Channel) EffectiveMention(msg alertmanager.WebhookMessage) config.MentionConfig {
	out := c.Mention
	for _, rule := range c.MentionRules {
		if rule.When.Match(msg) {
			out = router.MergeMention(out, rule.Mention)
		}
	}
	return normalizeMention(out)
}

type Runtime struct {
	ConfigPath string
	BaseDir    string

	Config   *config.Config
	Renderer *template.Renderer
	DingTalk *dingtalk.Client

	Robots   map[string]config.RobotConfig
	Channels map[string]Channel
	Routes   []router.Route

	LoadedAt time.Time
}

func LoadFromFile(logger log.Logger, configPath string) (*Runtime, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	baseDir := filepath.Dir(configPath)
	rt, err := Build(logger, configPath, baseDir, cfg)
	if err != nil {
		return nil, err
	}
	return rt, nil
}

func Build(logger log.Logger, configPath, baseDir string, cfg *config.Config) (*Runtime, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	renderer, err := template.NewRenderer(cfg.Template)
	if err != nil {
		return nil, err
	}

	dt := dingtalk.NewClient(cfg.DingTalk.Timeout.Duration())
	robots := cfg.DingTalk.RobotsByName()

	channels, err := compileChannels(cfg, robots, cfg.DingTalk.Channels)
	if err != nil {
		return nil, err
	}
	for name, ch := range channels {
		tplName := strings.TrimSpace(ch.Template)
		if tplName == "" {
			tplName = renderer.DefaultName()
		}
		if !renderer.HasTemplate(tplName) {
			return nil, fmt.Errorf("channel %q references unknown template %q", name, tplName)
		}
	}

	routes := router.CompileRoutes(cfg.DingTalk.Routes)

	if _, ok := channels["default"]; !ok {
		return nil, fmt.Errorf("default channel is required")
	}

	return &Runtime{
		ConfigPath: configPath,
		BaseDir:    baseDir,
		Config:     cfg,
		Renderer:   renderer,
		DingTalk:   dt,
		Robots:     robots,
		Channels:   channels,
		Routes:     routes,
		LoadedAt:   time.Now(),
	}, nil
}

func compileChannels(cfg *config.Config, robots map[string]config.RobotConfig, channelsCfg []config.ChannelConfig) (map[string]Channel, error) {
	out := make(map[string]Channel, len(channelsCfg))
	for _, ch := range channelsCfg {
		name := strings.TrimSpace(ch.Name)
		if name == "" {
			return nil, fmt.Errorf("channel name is empty")
		}

		tplName := strings.TrimSpace(ch.Template)
		if tplName == "" {
			tplName = "default"
		}
		if tplName != "" && !config.ValidTemplateName(tplName) {
			return nil, fmt.Errorf("channel %q has invalid template name %q", name, tplName)
		}

		robotCfgs := make([]config.RobotConfig, 0, len(ch.Robots))
		for _, r := range ch.Robots {
			robot, ok := robots[r]
			if !ok {
				return nil, fmt.Errorf("channel %q references unknown robot %q", name, r)
			}
			robotCfgs = append(robotCfgs, robot)
		}

		mention := normalizeMention(ch.Mention)
		rules := router.CompileMentionRules(ch.MentionRules)
		for i := range rules {
			rules[i].Mention = normalizeMention(rules[i].Mention)
		}

		out[name] = Channel{
			Name:         name,
			Robots:       robotCfgs,
			Template:     tplName,
			Mention:      mention,
			MentionRules: rules,
		}
	}
	return out, nil
}

func normalizeMention(m config.MentionConfig) config.MentionConfig {
	if m.AtAll {
		m.AtMobiles = nil
		m.AtUserIds = nil
		return m
	}

	userIds := make([]string, 0, len(m.AtUserIds))
	seenUserIds := make(map[string]struct{}, len(m.AtUserIds))
	for _, v := range m.AtUserIds {
		v = strings.TrimSpace(v)
		v = strings.TrimPrefix(v, "@")
		if v == "" {
			continue
		}
		if _, ok := seenUserIds[v]; ok {
			continue
		}
		seenUserIds[v] = struct{}{}
		userIds = append(userIds, v)
	}
	m.AtUserIds = userIds

	mobiles := make([]string, 0, len(m.AtMobiles))
	seen := make(map[string]struct{}, len(m.AtMobiles))
	for _, v := range m.AtMobiles {
		v = strings.TrimSpace(v)
		v = strings.TrimPrefix(v, "@")
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		mobiles = append(mobiles, v)
	}
	m.AtMobiles = mobiles
	return m
}
