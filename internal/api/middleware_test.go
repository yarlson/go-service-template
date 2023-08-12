package api

import (
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"go-service-template/internal/infrastructure"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingMiddleware(t *testing.T) {
	// Creating a test logger and replacing the infrastructure log with it
	testLogger, hook := test.NewNullLogger()
	infrastructure.SetLog(testLogger) // You'll need to create this method to set the logger in the infrastructure package

	// Creating a sample next handler
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("next handler"))
	})

	// Creating a request to pass to the handler
	req, err := http.NewRequest("GET", "/test", nil)
	req.RequestURI = "/test" // Explicitly setting the RequestURI field
	assert.NoError(t, err)

	// Creating a ResponseRecorder to record the response
	rr := httptest.NewRecorder()

	// Creating the middleware handler
	handler := LoggingMiddleware(nextHandler)

	// Serve the request
	handler.ServeHTTP(rr, req)

	// Checking if next handler was called
	assert.Equal(t, "next handler", rr.Body.String())

	// Checking if the log entry was made
	assert.Equal(t, 1, len(hook.Entries))
	entry := hook.LastEntry()
	assert.Equal(t, logrus.InfoLevel, entry.Level)
	assert.Equal(t, "Handled request", entry.Message)
	assert.Equal(t, "GET", entry.Data["method"])
	assert.Equal(t, "/test", entry.Data["uri"]) // Checking for the expected URI
	assert.Equal(t, req.RemoteAddr, entry.Data["remote"])

	// Since we don't control the exact time duration, we'll just check if it exists
	_, durationExists := entry.Data["duration"]
	assert.True(t, durationExists, "Duration should be logged")
}
