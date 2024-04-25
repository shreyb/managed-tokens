package tracing

import (
	"context"

	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// JaegerTraceProvider returns a new instance of sdktrace.TracerProvider configured with Jaeger exporter, along with its shutdown function.
// The provided URL is used as the collector endpoint for sending traces.
func JaegerTraceProvider(url string) (*sdktrace.TracerProvider, func(context.Context), error) {
	//   exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint("http://localhost:14268/api/traces")))
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(url)))
	if err != nil {
		return nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("token-push"),
			semconv.DeploymentEnvironmentKey.String("production"),
		)),
	)
	return tp, func(ctx context.Context) { tp.Shutdown(ctx) }, nil
}
