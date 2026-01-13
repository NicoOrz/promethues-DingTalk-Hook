// Package template provides template rendering for Alertmanager webhooks.
package template

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"prometheus-dingtalk-hook/internal/alertmanager"
	"prometheus-dingtalk-hook/internal/config"
)

//go:embed templates/default.tmpl
var embeddedDefaultTemplate string

func EmbeddedDefaultText() string {
	return embeddedDefaultTemplate
}

type Renderer struct {
	defaultName string
	templates   map[string]*template.Template
}

type RenderData struct {
	Payload       alertmanager.WebhookMessage
	FiringCount   int
	ResolvedCount int
}

func NewRenderer(cfg config.TemplateConfig) (*Renderer, error) {
	defaultName := "default"

	templates := make(map[string]*template.Template, 8)

	if err := loadTemplateText(templates, "default", embeddedDefaultTemplate); err != nil {
		return nil, err
	}

	if strings.TrimSpace(cfg.Dir) != "" {
		entries, err := os.ReadDir(cfg.Dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				entries = nil
			} else {
				return nil, fmt.Errorf("read template dir: %w", err)
			}
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if filepath.Ext(name) != ".tmpl" {
				continue
			}
			base := strings.TrimSuffix(name, ".tmpl")
			if !config.ValidTemplateName(base) {
				continue
			}
			path := filepath.Join(cfg.Dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read template: %w", err)
			}
			if err := loadTemplateText(templates, base, string(data)); err != nil {
				return nil, err
			}
		}
	}

	if _, ok := templates[defaultName]; !ok {
		return nil, fmt.Errorf("default template %q not found", defaultName)
	}

	return &Renderer{
		defaultName: defaultName,
		templates:   templates,
	}, nil
}

func (r *Renderer) DefaultName() string {
	return r.defaultName
}

func (r *Renderer) TemplateNames() []string {
	out := make([]string, 0, len(r.templates))
	for name := range r.templates {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (r *Renderer) HasTemplate(name string) bool {
	_, ok := r.templates[name]
	return ok
}

func (r *Renderer) Render(templateName string, payload alertmanager.WebhookMessage) (string, error) {
	name := strings.TrimSpace(templateName)
	if name == "" {
		name = r.defaultName
	}
	tmpl, ok := r.templates[name]
	if !ok {
		return "", fmt.Errorf("template %q not found", name)
	}

	var firing, resolved int
	for _, a := range payload.Alerts {
		switch strings.ToLower(a.Status) {
		case "firing":
			firing++
		case "resolved":
			resolved++
		}
	}

	buf := new(bytes.Buffer)
	if err := tmpl.Execute(buf, RenderData{
		Payload:       payload,
		FiringCount:   firing,
		ResolvedCount: resolved,
	}); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return strings.TrimSpace(buf.String()), nil
}

func RenderText(tplText string, payload alertmanager.WebhookMessage) (string, error) {
	tmpl := template.New("preview").Funcs(template.FuncMap{
		"default": defaultString,
		"kv":      formatKV,
	})
	parsed, err := tmpl.Parse(tplText)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	r := &Renderer{
		defaultName: "preview",
		templates: map[string]*template.Template{
			"preview": parsed,
		},
	}
	return r.Render("preview", payload)
}

func ValidateText(tplText string) error {
	tmpl := template.New("validate").Funcs(template.FuncMap{
		"default": defaultString,
		"kv":      formatKV,
	})
	_, err := tmpl.Parse(tplText)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}
	return nil
}

func loadTemplateText(dst map[string]*template.Template, name, tplText string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("template name is empty")
	}
	tmpl := template.New(name).Funcs(template.FuncMap{
		"default": defaultString,
		"kv":      formatKV,
	})
	parsed, err := tmpl.Parse(tplText)
	if err != nil {
		return fmt.Errorf("parse template %q: %w", name, err)
	}
	dst[name] = parsed
	return nil
}

func defaultString(fallback string, v any) string {
	switch s := v.(type) {
	case string:
		if strings.TrimSpace(s) == "" {
			return fallback
		}
		return s
	default:
		if v == nil {
			return fallback
		}
		return fmt.Sprint(v)
	}
}

func formatKV(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, m[k]))
	}
	return strings.Join(parts, " ")
}
