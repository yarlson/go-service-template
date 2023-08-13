package api

import (
	"context"
	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
	"go-service-template/internal/infrastructure"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const userKey contextKey = "user"

// LoggingMiddleware logs information about each request.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		logger := infrastructure.GetLog() // Get the logger instance
		next.ServeHTTP(w, r)
		logger.WithFields(logrus.Fields{
			"method":   r.Method,
			"uri":      r.RequestURI,
			"remote":   r.RemoteAddr,
			"duration": time.Since(startTime).String(),
		}).Info("Handled request")
	})
}

// JwtMiddleware handles JWT authentication
func JwtMiddleware(publicKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Authorization header is missing", http.StatusUnauthorized)
				return
			}

			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "Invalid authorization header format", http.StatusUnauthorized)
				return
			}

			tokenString := parts[1]
			formattedPublicKey := strings.Replace(publicKey, "||", "\n", -1) // Replace || with newlines
			parsedKey, err := jwt.ParseRSAPublicKeyFromPEM([]byte(formattedPublicKey))
			if err != nil {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				return parsedKey, nil
			})

			if err != nil || !token.Valid {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, token.Claims)))
		})
	}
}
