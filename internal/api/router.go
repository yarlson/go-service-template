package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/yarlson/chiprom"
	"net/http"
)

// NewRouter creates a new chi router and defines the routes.
func NewRouter(jwtPublicKey string) *chi.Mux {
	r := chi.NewRouter()
	r.Use(LoggingMiddleware)
	r.Use(JwtMiddleware(jwtPublicKey))

	// Create a new chi-prometheus middleware with a given namespace
	promMiddleware := chiprom.NewMiddleware("namespace")

	// Use the chi-prometheus middleware
	r.Use(promMiddleware)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Hello world"))
	})

	return r
}
