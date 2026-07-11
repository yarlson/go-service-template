package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

type Shutdown func(context.Context) error

type Runtime struct {
	MetricsHandler http.Handler
	meterProvider  *sdkmetric.MeterProvider
	databaseUp     metric.Int64Gauge
	databaseCheck  metric.Float64Histogram
	shutdown       []Shutdown
}

func (r Runtime) Shutdown(ctx context.Context) error {
	errorsByProvider := make([]error, 0, len(r.shutdown))
	for index := len(r.shutdown) - 1; index >= 0; index-- {
		errorsByProvider = append(errorsByProvider, r.shutdown[index](ctx))
	}
	return errors.Join(errorsByProvider...)
}

func (r Runtime) RecordDatabaseCheck(ctx context.Context, duration time.Duration, checkError error) {
	available := int64(1)
	if checkError != nil {
		available = 0
	}
	r.databaseUp.Record(ctx, available)
	r.databaseCheck.Record(ctx, duration.Seconds())
}

func Setup(ctx context.Context, serviceName, version, endpoint string) (Runtime, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	serviceResource, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return Runtime{}, fmt.Errorf("create telemetry resource: %w", err)
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	metricExporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registry))
	if err != nil {
		return Runtime{}, fmt.Errorf("create Prometheus metric exporter: %w", err)
	}
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(metricExporter),
		sdkmetric.WithResource(serviceResource),
	)
	meter := meterProvider.Meter("github.com/your-org/go-service-template/internal/platform/telemetry")
	databaseUp, err := meter.Int64Gauge(
		"service.database.available",
		metric.WithDescription("Whether the latest PostgreSQL readiness check succeeded"),
	)
	if err != nil {
		return Runtime{}, errors.Join(fmt.Errorf("create database availability metric: %w", err), meterProvider.Shutdown(ctx))
	}
	databaseCheck, err := meter.Float64Histogram(
		"service.database.check.duration",
		metric.WithDescription("PostgreSQL readiness check duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
	)
	if err != nil {
		return Runtime{}, errors.Join(fmt.Errorf("create database check metric: %w", err), meterProvider.Shutdown(ctx))
	}
	otel.SetMeterProvider(meterProvider)
	runtime := Runtime{
		MetricsHandler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		meterProvider:  meterProvider,
		databaseUp:     databaseUp,
		databaseCheck:  databaseCheck,
		shutdown:       []Shutdown{meterProvider.Shutdown},
	}

	if endpoint == "" {
		return runtime, nil
	}

	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return Runtime{}, errors.Join(
			fmt.Errorf("create OTLP trace exporter: %w", err),
			meterProvider.Shutdown(ctx),
		)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(serviceResource),
	)
	otel.SetTracerProvider(provider)
	runtime.shutdown = append(runtime.shutdown, provider.Shutdown)

	return runtime, nil
}
