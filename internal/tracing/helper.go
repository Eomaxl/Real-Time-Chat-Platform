package tracing

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TraceDBOperation traces a database operation
func TraceDBOperation(ctx context.Context, operation, table string, fn func(context.Context) error) error {
	ctx, span := StartSpan(ctx, "database", operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", operation),
			attribute.String("db.table", table),
		),
	)
	defer span.End()

	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)

	span.SetAttributes(attribute.Int64("db.duration_ms", duration.Milliseconds()))

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// TraceRedisOperation traces a Redis operation
func TraceRedisOperation(ctx context.Context, command string, fn func(context.Context) error) error {
	ctx, span := StartSpan(ctx, "redis", command,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "redis"),
			attribute.String("db.operation", command),
		),
	)
	defer span.End()

	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)

	span.SetAttributes(attribute.Int64("redis.duration_ms", duration.Milliseconds()))

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// TraceHTTPCall traces an HTTP call to another service
func TraceHTTPCall(ctx context.Context, method, url, service string, fn func(context.Context) error) error {
	ctx, span := StartSpan(ctx, "http-client", method+" "+url,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("http.method", method),
			attribute.String("http.url", url),
			attribute.String("peer.service", service),
		),
	)
	defer span.End()

	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)

	span.SetAttributes(attribute.Int64("http.duration_ms", duration.Milliseconds()))

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// TraceBusinessOperation traces a business logic operation
func TraceBusinessOperation(ctx context.Context, operationName string, fn func(context.Context) error) error {
	ctx, span := StartSpan(ctx, "business-logic", operationName,
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)

	span.SetAttributes(attribute.Int64("operation.duration_ms", duration.Milliseconds()))

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// TraceMessagePublish traces a message publish operation
func TraceMessagePublish(ctx context.Context, topic string, fn func(context.Context) error) error {
	ctx, span := StartSpan(ctx, "messaging", "publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "redis-pubsub"),
			attribute.String("messaging.destination", topic),
			attribute.String("messaging.operation", "publish"),
		),
	)
	defer span.End()

	err := fn(ctx)

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

// TraceWebSocketOperation traces a WebSocket operation
func TraceWebSocketOperation(ctx context.Context, operation string, fn func(context.Context) error) error {
	ctx, span := StartSpan(ctx, "websocket", operation,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("websocket.operation", operation),
		),
	)
	defer span.End()

	err := fn(ctx)

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return err
	}

	span.SetStatus(codes.Ok, "")
	return nil
}
