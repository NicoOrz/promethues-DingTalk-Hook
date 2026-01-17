// 包 server 提供 HTTP Server 封装，便于在 main 中初始化与优雅关闭。
package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-kit/log"

	"prometheus-dingtalk-hook/internal/reload"
	"prometheus-dingtalk-hook/internal/runtime"
)

var ErrServerClosed = http.ErrServerClosed

type Options struct {
	Logger       log.Logger
	ListenAddr   string
	AlertPath    string
	AdminPrefix  string
	AdminHandler http.Handler
	State        *runtime.Store
	Reload       *reload.Manager
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
	MaxBodyBytes int64
}

type Server struct {
	logger log.Logger
	srv    *http.Server
}

func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = log.NewNopLogger()
	}

	handler := NewHandler(HandlerOptions{
		Logger:       opts.Logger,
		AlertPath:    opts.AlertPath,
		AdminPrefix:  opts.AdminPrefix,
		AdminHandler: opts.AdminHandler,
		State:        opts.State,
		Reload:       opts.Reload,
		MaxBodyBytes: opts.MaxBodyBytes,
	})

	return &Server{
		logger: opts.Logger,
		srv: &http.Server{
			Addr:         opts.ListenAddr,
			Handler:      handler,
			ReadTimeout:  opts.ReadTimeout,
			WriteTimeout: opts.WriteTimeout,
			IdleTimeout:  opts.IdleTimeout,
		},
	}
}

func (s *Server) ListenAndServe() error {
	if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return http.ErrServerClosed
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
