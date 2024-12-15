package tracing

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

// NewOTLPTraceProvider returns a new instance of sdktrace.TracerProvider configured with an OTLP trace HTTP exporter,
// along with its shutdown function. The provided URL is used as the collector endpoint for sending traces.
func NewOTLPHTTPTraceProvider(ctx context.Context, endpointURL, deploymentEnvironmentKey string) (*sdktrace.TracerProvider, func(context.Context), error) {
	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpointURL))
	if err != nil {
		return nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("managed-tokens"),
			semconv.DeploymentEnvironmentKey.String(deploymentEnvironmentKey),
		)),
	)
	return tp, func(ctx context.Context) { tp.Shutdown(ctx) }, nil
}

// KeyValueForLog is a struct that holds a key and value for logging purposes
type KeyValueForLog struct {
	Key   string
	Value string
}

// LogErrorWithTrace logs an error message modifies the passed in trace span.
// It sets the status of the span to an error, records the error, and logs the error message using the provided logger.
func LogErrorWithTrace(span trace.Span, err error, keyValues ...KeyValueForLog) {
	span = assembleSpan(span, keyValues...)
	span.SetStatus(codes.Error, err.Error())
	span.RecordError(err)
}

// LogSuccessWithTrace logs a success message with the passed in logger and sets the passed-in span's status to OK
func LogSuccessWithTrace(span trace.Span, msg string, keyValues ...KeyValueForLog) {
	span = assembleSpan(span, keyValues...)
	span.SetStatus(codes.Ok, msg)
	span.AddEvent(msg)
}

func assembleSpan(span trace.Span, keyValues ...KeyValueForLog) trace.Span {
	for _, keyValue := range keyValues {
		span.SetAttributes(attribute.KeyValue{Key: attribute.Key(keyValue.Key), Value: attribute.StringValue(keyValue.Value)})
	}
	return span
}
