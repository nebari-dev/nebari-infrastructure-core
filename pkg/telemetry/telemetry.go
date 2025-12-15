package telemetry

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	serviceName    = "nebari-infrastructure-core"
	serviceVersion = "1.0.0"
)

// Setup initializes OpenTelemetry based on environment configuration.
// OTEL_EXPORTER: "none" (default), "console", "otlp", or "both"
// OTEL_ENDPOINT: OTLP endpoint (default: "localhost:4317")
func Setup(ctx context.Context) (trace.Tracer, func(context.Context) error, error) {
	exporterType := os.Getenv("OTEL_EXPORTER")
	if exporterType == "" {
		exporterType = "none"
	}

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String(serviceVersion),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create resource: %w", err)
	}

	var exporters []sdktrace.SpanExporter

	// Setup exporters based on configuration
	switch exporterType {
	case "none":
		// No exporters - traces are still collected but not exported
		// This is the default for production use
	case "console":
		consoleExporter, err := stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create console exporter: %w", err)
		}
		exporters = append(exporters, consoleExporter)
	case "otlp":
		endpoint := os.Getenv("OTEL_ENDPOINT")
		if endpoint == "" {
			endpoint = "localhost:4317"
		}

		otlpExporter, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(), // TODO: make configurable for production
		)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
		}
		exporters = append(exporters, otlpExporter)
	case "both":
		// Console exporter
		consoleExporter, err := stdouttrace.New(
			stdouttrace.WithPrettyPrint(),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create console exporter: %w", err)
		}
		exporters = append(exporters, consoleExporter)

		// OTLP exporter
		endpoint := os.Getenv("OTEL_ENDPOINT")
		if endpoint == "" {
			endpoint = "localhost:4317"
		}

		otlpExporter, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(), // TODO: make configurable for production
		)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
		}
		exporters = append(exporters, otlpExporter)
	}

	// Create trace provider with all configured exporters
	var batchOptions []sdktrace.BatchSpanProcessorOption
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
	)

	for _, exporter := range exporters {
		tp.RegisterSpanProcessor(sdktrace.NewBatchSpanProcessor(exporter, batchOptions...))
	}

	otel.SetTracerProvider(tp)

	tracer := tp.Tracer(serviceName)

	// Return shutdown function
	shutdown := func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}

	return tracer, shutdown, nil
}
