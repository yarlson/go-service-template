package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func TestRuntimeExposesHTTPAndProcessMetrics(t *testing.T) {
	runtime, err := Setup(t.Context(), "test-service", "test-version", "")
	require.NoError(t, err)
	t.Cleanup(func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, runtime.Shutdown(shutdownContext))
	})

	handler := otelhttp.NewHandler(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		"http.server",
		otelhttp.WithMeterProvider(runtime.meterProvider),
	)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	runtime.RecordDatabaseCheck(t.Context(), 25*time.Millisecond, nil)

	metricsRequest := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil)
	metricsResponse := httptest.NewRecorder()
	runtime.MetricsHandler.ServeHTTP(metricsResponse, metricsRequest)
	result := metricsResponse.Result()
	body, err := io.ReadAll(result.Body)
	require.NoError(t, err)
	require.NoError(t, result.Body.Close())

	assert.Equal(t, http.StatusOK, metricsResponse.Code)
	assert.Contains(t, string(body), "http_server_request_duration_seconds")
	assert.Contains(t, string(body), "go_goroutines")
	assert.Contains(t, string(body), "process_cpu_seconds_total")
	assert.Regexp(t, `service_database_available\{[^}]*\} 1\n`, string(body))
	assert.Contains(t, string(body), "service_database_check_duration_seconds")
	assert.Contains(t, string(body), `le="0.025"`)
}
