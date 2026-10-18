package ocm

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("chrisschneider.dev/delivery-bundler/internal/ocm")

// startSpan starts a span for a Service method; call the returned function
// with a pointer to the method's error result.
func startSpan(ctx context.Context, name string, ref Ref) (context.Context, func(*error)) {
	ctx, span := tracer.Start(ctx, "ocm."+name, trace.WithAttributes(
		attribute.String("ocm.component", ref.Component),
		attribute.String("ocm.version", ref.Version),
		attribute.String("ocm.repository", ref.Repository),
	))
	return ctx, func(errp *error) {
		if errp != nil && *errp != nil {
			span.RecordError(*errp)
			span.SetStatus(codes.Error, (*errp).Error())
		}
		span.End()
	}
}
