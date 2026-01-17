// prometheus-dingtalk-hook 是一个轻量的 Alertmanager → 钉钉机器人转发服务。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/promlog"

	"prometheus-dingtalk-hook/internal/admin"
	"prometheus-dingtalk-hook/internal/reload"
	"prometheus-dingtalk-hook/internal/runtime"
	"prometheus-dingtalk-hook/internal/server"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	var (
		showVersion      bool
		configPath       string
		webListenAddress string

		logCfg promlog.Config
	)

	logCfg.Level = &promlog.AllowedLevel{}
	_ = logCfg.Level.Set("info")
	flag.Var(logCfg.Level, "log.level", "Only log messages with the given severity or above. One of: [debug, info, warn, error]")

	logCfg.Format = &promlog.AllowedFormat{}
	_ = logCfg.Format.Set("logfmt")
	flag.Var(logCfg.Format, "log.format", "Output format of log messages. One of: [logfmt, json]")

	flag.BoolVar(&showVersion, "version", false, "Print version information and exit.")
	flag.StringVar(&configPath, "config.file", "config.yml", "Path to YAML config file")
	flag.StringVar(&webListenAddress, "web.listen-address", "", "Address to listen on for web interface and API")
	flag.Parse()

	if showVersion {
		fmt.Printf("prometheus-dingtalk-hook %s (commit: %s, built at: %s)\n", version, commit, date)
		return
	}

	logger := promlog.New(&logCfg)
	logger = log.With(logger, "app", "prometheus-dingtalk-hook")

	rt, err := runtime.LoadFromFile(logger, configPath)
	if err != nil {
		level.Error(logger).Log("msg", "load config failed", "err", err)
		os.Exit(1)
	}

	store := runtime.NewStore(rt)

	reloadMgr, err := reload.New(logger, configPath, store, rt.Config.Reload.Enabled, rt.Config.Reload.Interval.Duration())
	if err != nil {
		level.Error(logger).Log("msg", "init reload failed", "err", err)
		os.Exit(1)
	}

	adminHandler := admin.New(admin.Options{
		Logger:     logger,
		ConfigPath: configPath,
		Store:      store,
		Reload:     reloadMgr,
	})

	listenAddr := rt.Config.Server.Listen
	if v := strings.TrimSpace(webListenAddress); v != "" {
		listenAddr = v
	}

	srv := server.New(server.Options{
		Logger:       logger,
		ListenAddr:   listenAddr,
		AlertPath:    rt.Config.Server.Path,
		AdminPrefix:  rt.Config.Admin.PathPrefix,
		AdminHandler: adminHandler,
		State:        store,
		Reload:       reloadMgr,
		ReadTimeout:  rt.Config.Server.ReadTimeout.Duration(),
		WriteTimeout: rt.Config.Server.WriteTimeout.Duration(),
		IdleTimeout:  rt.Config.Server.IdleTimeout.Duration(),
		MaxBodyBytes: rt.Config.Server.MaxBodyBytes,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	reloadMgr.Start(ctx)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	level.Info(logger).Log(
		"msg", "starting server",
		"version", version,
		"commit", commit,
		"date", date,
		"listen", listenAddr,
		"path", rt.Config.Server.Path,
	)

	if err := srv.ListenAndServe(); err != nil {
		if err == server.ErrServerClosed {
			level.Info(logger).Log("msg", "server closed")
			return
		}
		level.Error(logger).Log("msg", "server error", "err", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
