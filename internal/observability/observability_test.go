package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	slogctx "github.com/veqryn/slog-context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("not JSON: %q", l)
		}
		out = append(out, m)
	}
	return out
}

func TestLoggerAddsTraceIDs(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf, slog.LevelInfo)
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("t").Start(t.Context(), "op")
	ctx = WithRequestID(ctx, "req-1")
	logger.With("k", "v").InfoContext(ctx, "hello")
	span.End()
	l := lines(t, &buf)[0]
	if l["trace_id"] != span.SpanContext().TraceID().String() || l["span_id"] == nil || l["request_id"] != "req-1" || l["k"] != "v" {
		t.Fatalf("line = %v", l)
	}
}

func TestMiddleware(t *testing.T) {
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	otel.SetTextMapPropagator(propagation.TraceContext{})
	var buf bytes.Buffer
	logger := NewLogger(&buf, slog.LevelInfo)
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slogctx.FromCtx(r.Context()).InfoContext(r.Context(), "from bindings")
		Annotate(r.Context(), slog.String("component", "acme.org/x"))
		w.WriteHeader(http.StatusTeapot)
		w.Write([]byte("hi"))
	}), logger)

	req := httptest.NewRequest("GET", "/v1/acme.org/x", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("X-Request-Id = %q", got)
	}
	ls := lines(t, &buf)
	if len(ls) != 2 {
		t.Fatalf("want 2 log lines, got %d: %s", len(ls), buf.String())
	}
	if ls[0]["msg"] != "from bindings" || ls[0]["request_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("bindings line = %v", ls[0])
	}
	access := ls[1]
	if access["msg"] != "request" || access["status"] != float64(418) || access["bytes"] != float64(2) ||
		access["component"] != "acme.org/x" || access["path"] != "/v1/acme.org/x" {
		t.Fatalf("access line = %v", access)
	}
}

func TestMiddlewareLogsAbortedRequests(t *testing.T) {
	var buf bytes.Buffer
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		panic(http.ErrAbortHandler)
	}), NewLogger(&buf, slog.LevelInfo))
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
	}
	srv.Close() // waits for the handler to finish
	if !strings.Contains(buf.String(), `"aborted":true`) {
		t.Fatalf("log = %s", buf.String())
	}
}

func TestSetupWithoutExporterIsLocalOnly(t *testing.T) {
	clearOTelEnv(t)
	shutdown, err := Setup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, span := otel.Tracer("t").Start(t.Context(), "x")
	defer span.End()
	if !span.SpanContext().IsValid() {
		t.Fatal("spans must have ids even without an exporter")
	}
	_ = ctx
	if err := shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}

var otelEnv = []string{"OTEL_SDK_DISABLED", "OTEL_TRACES_EXPORTER", "OTEL_METRICS_EXPORTER", "OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"}

func clearOTelEnv(t *testing.T) {
	t.Helper()
	for _, k := range otelEnv {
		t.Setenv(k, "")
	}
}

func TestExportGates(t *testing.T) {
	tests := []struct {
		name            string
		env             map[string]string
		traces, metrics bool
	}{
		{"nothing set", nil, false, false},
		{"traces exporter only", map[string]string{"OTEL_TRACES_EXPORTER": "otlp"}, true, false},
		{"metrics exporter only", map[string]string{"OTEL_METRICS_EXPORTER": "prometheus"}, false, true},
		{"traces exporter none", map[string]string{"OTEL_TRACES_EXPORTER": "none"}, false, false},
		{"metrics exporter none", map[string]string{"OTEL_METRICS_EXPORTER": "none"}, false, false},
		{"shared endpoint enables both", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318"}, true, true},
		{"traces endpoint only", map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://collector:4318/v1/traces"}, true, false},
		{"metrics endpoint only", map[string]string{"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "http://collector:4318/v1/metrics"}, false, true},
		{"endpoint with traces none", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_TRACES_EXPORTER": "none"}, false, true},
		{"sdk disabled wins", map[string]string{"OTEL_SDK_DISABLED": "true", "OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318",
			"OTEL_TRACES_EXPORTER": "otlp", "OTEL_METRICS_EXPORTER": "otlp"}, false, false},
		{"sdk disabled false", map[string]string{"OTEL_SDK_DISABLED": "false", "OTEL_TRACES_EXPORTER": "otlp"}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearOTelEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if got := tracesEnabled(); got != tt.traces {
				t.Errorf("tracesEnabled = %v, want %v", got, tt.traces)
			}
			if got := metricsEnabled(); got != tt.metrics {
				t.Errorf("metricsEnabled = %v, want %v", got, tt.metrics)
			}
		})
	}
}

func TestSetupSDKDisabledKeepsLocalSpans(t *testing.T) {
	clearOTelEnv(t)
	t.Setenv("OTEL_SDK_DISABLED", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	shutdown, err := Setup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, span := otel.Tracer("t").Start(t.Context(), "x")
	span.End()
	if !span.SpanContext().IsValid() {
		t.Fatal("spans must have ids with the SDK export disabled")
	}
	if err := shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}
