package api

import (
	"context"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go-service-template/internal/infrastructure"
	"net/http"
)

// MetricsServer represents the server to serve Prometheus metrics.
type MetricsServer struct {
	config *infrastructure.MetricsConfig
	server *http.Server
}

// NewMetricsServer constructs a new MetricsServer with the given address.
func NewMetricsServer(config *infrastructure.MetricsConfig) *MetricsServer {
	r := chi.NewRouter()
	r.Handle("/metrics", promhttp.Handler())
	return &MetricsServer{
		config: config,
		server: &http.Server{
			Addr:    fmt.Sprintf("%s:%d", config.BindAddress, config.Port),
			Handler: r,
		},
	}
}

// Start initiates the metrics server listening on the specified address.
func (m *MetricsServer) Start() error {
	return m.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server without interrupting any active connections.
func (m *MetricsServer) Shutdown(ctx context.Context) error {
	return m.server.Shutdown(ctx)
}

// Address constructs the address string based on the configuration.
func (m *MetricsServer) Address() string {
	return fmt.Sprintf("%s:%d", m.config.BindAddress, m.config.Port)
}
