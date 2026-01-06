// prometheus-dingtalk-hook 是一个轻量的 Alertmanager → 钉钉机器人转发服务。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "Path to YAML config file")
	flag.Parse()

	// 输出版本信息
	fmt.Printf("prometheus-dingtalk-hook %s (commit: %s, built at: %s)\n", version, commit, date)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	rt, err := runtime.LoadFromFile(logger, configPath)
	if err != nil {
		logger.Error("load config failed", "err", err)
		os.Exit(1)
	}

	store := runtime.NewStore(rt)

	reloadMgr, err := reload.New(logger, configPath, store, rt.Config.Reload.Enabled, rt.Config.Reload.Interval.Duration())
	if err != nil {
		logger.Error("init reload failed", "err", err)
		os.Exit(1)
	}

	adminHandler := admin.New(admin.Options{
		Logger:     logger,
		ConfigPath: configPath,
		Store:      store,
		Reload:     reloadMgr,
	})

	srv := server.New(server.Options{
		Logger:       logger,
		ListenAddr:   rt.Config.Server.Listen,
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

	logger.Info("starting server", "listen", rt.Config.Server.Listen, "path", rt.Config.Server.Path)
	if err := srv.ListenAndServe(); err != nil {
		if err == server.ErrServerClosed {
			logger.Info("server closed")
			return
		}
		logger.Error("server error", "err", err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
