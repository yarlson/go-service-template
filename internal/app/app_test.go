package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestInstrumentHTTPEmitsServerSpan(t *testing.T) {
	t.Parallel()

	spanRecorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))

	handler := instrumentHTTP(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		otelhttp.WithTracerProvider(provider),
	)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/livez", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	require.Len(t, spanRecorder.Ended(), 1)
	span := spanRecorder.Ended()[0]
	assert.Equal(t, http.MethodGet, span.Name())
	assert.Equal(t, trace.SpanKindServer, span.SpanKind())
	assert.True(t, span.SpanContext().IsValid())

	attributes := attribute.NewSet(span.Attributes()...)
	method, found := attributes.Value("http.request.method")
	require.True(t, found)
	assert.Equal(t, http.MethodGet, method.AsString())
	status, found := attributes.Value("http.response.status_code")
	require.True(t, found)
	assert.Equal(t, int64(http.StatusNoContent), status.AsInt64())
}
