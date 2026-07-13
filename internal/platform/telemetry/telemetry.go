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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	"github.com/your-org/go-service-template/internal/platform/messaging"
)

type Shutdown func(context.Context) error

type Runtime struct {
	MetricsHandler http.Handler
	meterProvider  *sdkmetric.MeterProvider
	databaseUp     metric.Int64Gauge
	databaseCheck  metric.Float64Histogram
	messaging      messagingMetrics
	shutdown       []Shutdown
}

type messagingMetrics struct {
	publishDuration   metric.Float64Histogram
	processDuration   metric.Float64Histogram
	queueAge          metric.Float64Histogram
	attempts          metric.Int64Histogram
	processed         metric.Int64Counter
	permissionChanges metric.Int64Counter
	failures          metric.Int64Counter
	inFlight          metric.Int64UpDownCounter
	backlog           metric.Int64Gauge
	awsAvailable      metric.Int64Gauge
	awsCheck          metric.Float64Histogram
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

func (r Runtime) RecordMessagePublish(ctx context.Context, duration time.Duration, publishError error) {
	r.messaging.publishDuration.Record(ctx, duration.Seconds())
	if publishError != nil {
		r.messaging.failures.Add(ctx, 1, metric.WithAttributes(attribute.String("operation", "publish")))
	}
}

func (r Runtime) RecordMessageReceiveFailure(ctx context.Context, class string) {
	r.messaging.failures.Add(ctx, 1, metric.WithAttributes(
		attribute.String("operation", "receive"),
		attribute.String("class", class),
	))
}

func (r Runtime) RecordMessageProcess(ctx context.Context, process messaging.MessageProcess) {
	attributes := metric.WithAttributes(attribute.String("outcome", process.Outcome))
	r.messaging.processDuration.Record(ctx, process.Duration.Seconds(), attributes)
	r.messaging.queueAge.Record(ctx, process.QueueAge.Seconds())
	r.messaging.attempts.Record(ctx, int64(process.Attempt))
	r.messaging.processed.Add(ctx, 1, attributes)
	if process.Outcome != "success" {
		r.messaging.failures.Add(ctx, 1, metric.WithAttributes(attribute.String("operation", "process")))
	}
}

func (r Runtime) AddMessagesInFlight(ctx context.Context, delta int64) {
	r.messaging.inFlight.Add(ctx, delta)
}

func (r Runtime) RecordPermissionOutcome(ctx context.Context, outcome string) {
	r.messaging.permissionChanges.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

func (r Runtime) RecordAWSCheck(ctx context.Context, dependency string, duration time.Duration, checkError error) {
	available := int64(1)
	if checkError != nil {
		available = 0
	}
	attributes := metric.WithAttributes(attribute.String("dependency", dependency))
	r.messaging.awsAvailable.Record(ctx, available, attributes)
	r.messaging.awsCheck.Record(ctx, duration.Seconds(), attributes)
}

func (r Runtime) RecordSQSBacklog(ctx context.Context, visible, inFlight int64) {
	r.messaging.backlog.Record(ctx, visible, metric.WithAttributes(attribute.String("state", "visible")))
	r.messaging.backlog.Record(ctx, inFlight, metric.WithAttributes(attribute.String("state", "in_flight")))
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
	messagingInstruments, err := newMessagingMetrics(meter)
	if err != nil {
		return Runtime{}, errors.Join(err, meterProvider.Shutdown(ctx))
	}
	otel.SetMeterProvider(meterProvider)
	runtime := Runtime{
		MetricsHandler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		meterProvider:  meterProvider,
		databaseUp:     databaseUp,
		databaseCheck:  databaseCheck,
		messaging:      messagingInstruments,
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

func newMessagingMetrics(meter metric.Meter) (messagingMetrics, error) {
	metrics := messagingMetrics{}
	var err error
	metrics.publishDuration, err = meter.Float64Histogram("service.messaging.publish.duration", metric.WithUnit("s"))
	if err != nil {
		return messagingMetrics{}, fmt.Errorf("create message publish duration metric: %w", err)
	}
	metrics.processDuration, err = meter.Float64Histogram("service.messaging.process.duration", metric.WithUnit("s"))
	if err != nil {
		return messagingMetrics{}, fmt.Errorf("create message process duration metric: %w", err)
	}
	metrics.queueAge, err = meter.Float64Histogram("service.messaging.queue.age", metric.WithUnit("s"))
	if err != nil {
		return messagingMetrics{}, fmt.Errorf("create message queue age metric: %w", err)
	}
	metrics.attempts, err = meter.Int64Histogram("service.messaging.attempts")
	if err != nil {
		return messagingMetrics{}, fmt.Errorf("create message attempts metric: %w", err)
	}
	metrics.processed, err = meter.Int64Counter("service.messaging.processed")
	if err != nil {
		return messagingMetrics{}, fmt.Errorf("create processed messages metric: %w", err)
	}
	metrics.permissionChanges, err = meter.Int64Counter("service.permissions.changes")
	if err != nil {
		return messagingMetrics{}, fmt.Errorf("create permission changes metric: %w", err)
	}
	metrics.failures, err = meter.Int64Counter("service.messaging.failures")
	if err != nil {
		return messagingMetrics{}, fmt.Errorf("create message failures metric: %w", err)
	}
	metrics.inFlight, err = meter.Int64UpDownCounter("service.messaging.in_flight")
	if err != nil {
		return messagingMetrics{}, fmt.Errorf("create in-flight messages metric: %w", err)
	}
	metrics.backlog, err = meter.Int64Gauge("service.messaging.backlog")
	if err != nil {
		return messagingMetrics{}, fmt.Errorf("create message backlog metric: %w", err)
	}
	metrics.awsAvailable, err = meter.Int64Gauge("service.aws.available")
	if err != nil {
		return messagingMetrics{}, fmt.Errorf("create AWS availability metric: %w", err)
	}
	metrics.awsCheck, err = meter.Float64Histogram("service.aws.check.duration", metric.WithUnit("s"))
	if err != nil {
		return messagingMetrics{}, fmt.Errorf("create AWS check duration metric: %w", err)
	}
	return metrics, nil
}
