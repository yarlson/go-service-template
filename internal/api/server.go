package api

import (
	"context"
	"fmt"
	"github.com/go-chi/chi/v5"
	"go-service-template/internal/infrastructure"
	"net/http"
)

// Server represents the HTTP server that will handle requests.
type Server struct {
	config *infrastructure.Config
	router *chi.Mux
}

// NewServer constructs a new Server, using the provided configuration.
func NewServer(config *infrastructure.Config) *Server {
	router := NewRouter(config.App.JwtPublicKey)
	return &Server{
		config: config,
		router: router,
	}
}

// Start initiates the HTTP server listening on the specified address.
func (s *Server) Start() error {
	address := fmt.Sprintf("%s:%d", s.config.App.BindAddress, s.config.App.Port)
	return http.ListenAndServe(address, s.router)
}

// Shutdown gracefully shuts down the server without interrupting any active connections.
func (s *Server) Shutdown(ctx context.Context) error {
	server := &http.Server{
		Addr:    s.Address(),
		Handler: s.router,
	}
	return server.Shutdown(ctx)
}

// Address constructs the address string based on the configuration.
func (s *Server) Address() string {
	return fmt.Sprintf("%s:%d", s.config.App.BindAddress, s.config.App.Port)
}
