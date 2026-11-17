// Package server wires up the HTTP router, middleware and handlers.
package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"walletcore/internal/config"
)

// Server holds shared dependencies used by HTTP handlers.
type Server struct {
	cfg  config.Config
	pool *pgxpool.Pool
}

// New constructs a Server with its dependencies.
func New(cfg config.Config, pool *pgxpool.Pool) *Server {
	return &Server{cfg: cfg, pool: pool}
}

// Router builds the chi router with middleware and routes.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	// Standard middleware stack.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(15 * time.Second))

	// Operational endpoints.
	r.Get("/healthz", s.handleHealthz) // liveness: is the process up?
	r.Get("/readyz", s.handleReadyz)   // readiness: can we reach the DB?

	// Versioned API. Feature handlers (auth, wallets, transfers) land here next.
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/ping", s.handlePing)
	})

	return r
}
