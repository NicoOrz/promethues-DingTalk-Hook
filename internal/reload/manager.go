// Package reload provides safe hot reload for config and templates.
package reload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"prometheus-dingtalk-hook/internal/runtime"
)

type Manager struct {
	logger     log.Logger
	configPath string
	store      *runtime.Store

	interval time.Duration
	enabled  bool

	mu              sync.Mutex
	lastFingerprint string
	lastSuccess     time.Time
	lastError       error
}

type Status struct {
	Enabled     bool      `json:"enabled"`
	LastSuccess time.Time `json:"last_success"`
	LastError   string    `json:"last_error"`
}

func New(logger log.Logger, configPath string, store *runtime.Store, enabled bool, interval time.Duration) (*Manager, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	if store == nil {
		return nil, errors.New("store is nil")
	}
	if strings.TrimSpace(configPath) == "" {
		return nil, errors.New("configPath is empty")
	}
	if interval <= 0 {
		interval = 2 * time.Second
	}

	m := &Manager{
		logger:     logger,
		configPath: configPath,
		store:      store,
		enabled:    enabled,
		interval:   interval,
	}

	fp, err := m.fingerprintFromCurrent()
	if err == nil {
		m.lastFingerprint = fp
	}

	return m, nil
}

func (m *Manager) Start(ctx context.Context) {
	if !m.enabled {
		return
	}
	ticker := time.NewTicker(m.interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = m.ReloadIfChanged(ctx)
			}
		}
	}()
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := Status{
		Enabled:     m.enabled,
		LastSuccess: m.lastSuccess,
	}
	if m.lastError != nil {
		st.LastError = m.lastError.Error()
	}
	return st
}

func (m *Manager) ReloadIfChanged(ctx context.Context) error {
	fp, err := m.fingerprintFromCurrent()
	if err != nil {
		return err
	}

	m.mu.Lock()
	unchanged := (fp == m.lastFingerprint)
	m.mu.Unlock()
	if unchanged {
		return nil
	}
	return m.Reload(ctx, false)
}

func (m *Manager) Reload(ctx context.Context, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	currentFP, err := m.fingerprintFromCurrent()
	if err != nil {
		m.lastError = err
		return err
	}
	if !force && currentFP == m.lastFingerprint {
		return nil
	}

	next, err := runtime.LoadFromFile(m.logger, m.configPath)
	if err != nil {
		m.lastError = err
		level.Error(m.logger).Log("msg", "reload failed", "err", err)
		return err
	}

	nextFP, err := fingerprint(m.configPath, next)
	if err != nil {
		m.lastError = err
		level.Error(m.logger).Log("msg", "reload failed (fingerprint)", "err", err)
		return err
	}

	m.store.Store(next)
	m.lastFingerprint = nextFP
	m.lastSuccess = time.Now()
	m.lastError = nil
	level.Info(m.logger).Log("msg", "reload ok")
	return nil
}

func (m *Manager) fingerprintFromCurrent() (string, error) {
	return fingerprint(m.configPath, m.store.Load())
}

func fingerprint(configPath string, rt *runtime.Runtime) (string, error) {
	h := sha256.New()
	if err := hashFileStat(h, configPath); err != nil {
		return "", err
	}

	var tplDir string
	if rt != nil && rt.Config != nil {
		tplDir = strings.TrimSpace(rt.Config.Template.Dir)
	}

	if tplDir != "" {
		if err := hashTemplateDir(h, tplDir); err != nil {
			return "", err
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashFileStat(h hash.Hash, path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	_, _ = h.Write([]byte("file:"))
	_, _ = h.Write([]byte(path))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%d", st.Size(), st.ModTime().UnixNano())))
	_, _ = h.Write([]byte{0})
	return nil
}

func hashTemplateDir(h hash.Hash, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = h.Write([]byte("dir:"))
			_, _ = h.Write([]byte(dir))
			_, _ = h.Write([]byte{0})
			_, _ = h.Write([]byte("missing"))
			_, _ = h.Write([]byte{0})
			return nil
		}
		return fmt.Errorf("read template dir %s: %w", dir, err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".tmpl" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	_, _ = h.Write([]byte("dir:"))
	_, _ = h.Write([]byte(dir))
	_, _ = h.Write([]byte{0})

	for _, name := range names {
		if err := hashFileStat(h, filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}
