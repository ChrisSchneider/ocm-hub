package observability

import (
	"context"
	"errors"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Setup installs global OpenTelemetry providers configured by the standard
// OTEL_* environment variables. Traces and metrics are gated independently
// (see tracesEnabled, metricsEnabled). Without exporter variables, spans are
// still created (so logs carry trace ids) but nothing is exported.
func Setup(ctx context.Context) (func(context.Context) error, error) {
	res, err := sdkresource.New(ctx,
		sdkresource.WithAttributes(attribute.String("service.name", "ocm-server")),
		sdkresource.WithFromEnv(), // OTEL_SERVICE_NAME / OTEL_RESOURCE_ATTRIBUTES override
		sdkresource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, err
	}
	var shutdowns []func(context.Context) error
	tpOpts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
	if tracesEnabled() {
		exp, err := autoexport.NewSpanExporter(ctx)
		if err != nil {
			return nil, err
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(exp))
	}
	if metricsEnabled() {
		reader, err := autoexport.NewMetricReader(ctx)
		if err != nil {
			return nil, err
		}
		mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithResource(res))
		otel.SetMeterProvider(mp)
		shutdowns = append(shutdowns, mp.Shutdown)
	}
	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	shutdowns = append(shutdowns, tp.Shutdown)
	return func(ctx context.Context) error {
		var errs []error
		for _, s := range shutdowns {
			errs = append(errs, s(ctx))
		}
		return errors.Join(errs...)
	}, nil
}

// tracesEnabled reports whether spans are exported: OTEL_TRACES_EXPORTER set
// (and not "none") or an OTLP endpoint for traces configured.
func tracesEnabled() bool {
	return exportEnabled("OTEL_TRACES_EXPORTER", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
}

// metricsEnabled reports whether metrics are exported: OTEL_METRICS_EXPORTER
// set (and not "none") or an OTLP endpoint for metrics configured.
func metricsEnabled() bool {
	return exportEnabled("OTEL_METRICS_EXPORTER", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT")
}

// exportEnabled gates one signal. OTEL_SDK_DISABLED=true disables export of
// every signal (spans still get ids locally); an explicit exporter "none"
// wins over endpoint variables.
func exportEnabled(exporterVar, endpointVar string) bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("OTEL_SDK_DISABLED")), "true") {
		return false
	}
	switch exp := strings.TrimSpace(os.Getenv(exporterVar)); {
	case strings.EqualFold(exp, "none"):
		return false
	case exp != "":
		return true
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv(endpointVar) != ""
}
