// Package httpapi exposes the engine over HTTP.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/wang-hantao/parking-free/internal/engine"
)

// Config holds HTTP-server tunables.
type Config struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Server is the HTTP service.
type Server struct {
	cfg    Config
	logger *slog.Logger
	engine *engine.Evaluator
	srv    *http.Server
}

// New constructs a Server.
func New(cfg Config, logger *slog.Logger, ev *engine.Evaluator) *Server {
	s := &Server{cfg: cfg, logger: logger, engine: ev}
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(slogRequest(logger))

	r.Get("/healthz", s.handleHealthz)
	r.Get("/allowed", s.handleAllowed)

	s.srv = &http.Server{
		Addr:         cfg.Addr,
		Handler:      r,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}
	return s
}

// Run starts the server and blocks until ctx is cancelled, then
// performs a graceful shutdown.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("http server listening", "addr", s.cfg.Addr)
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s.logger.Info("http server shutting down")
		return s.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// slogRequest is a tiny middleware that logs basic request data.
func slogRequest(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}
