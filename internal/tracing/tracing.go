package tracing

import (
	"context"
	"errors"

	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

// JaegerTraceProvider returns a new instance of sdktrace.TracerProvider configured with Jaeger exporter, along with its shutdown function.
// The provided URL is used as the collector endpoint for sending traces.
func JaegerTraceProvider(url string) (*sdktrace.TracerProvider, func(context.Context), error) {
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(url)))
	if err != nil {
		return nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("managed-tokens"),
			semconv.DeploymentEnvironmentKey.String("production"),
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
func LogErrorWithTrace(span trace.Span, logger *log.Entry, msg string, keyValues ...KeyValueForLog) {
	for _, keyvalue := range keyValues {
		span.SetAttributes(attribute.KeyValue{Key: attribute.Key(keyvalue.Key), Value: attribute.StringValue(keyvalue.Value)})
		logger = logger.WithField(keyvalue.Key, keyvalue.Value)
	}

	err := errors.New(msg)
	span.SetStatus(codes.Error, msg)
	span.RecordError(err)
	logger.Error(msg)
}

// LogSuccessWithTrace logs a success message with the passed in logger and sets the passed-in span's status to OK
func LogSuccessWithTrace(span trace.Span, logger *log.Entry, msg string, keyValues ...KeyValueForLog) {
	for _, keyvalue := range keyValues {
		span.SetAttributes(attribute.KeyValue{Key: attribute.Key(keyvalue.Key), Value: attribute.StringValue(keyvalue.Value)})
		logger = logger.WithField(keyvalue.Key, keyvalue.Value)
	}

	span.SetStatus(codes.Ok, msg)
	logger.Info(msg)
}
