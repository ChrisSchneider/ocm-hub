package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"sync"
	"time"

	slogctx "github.com/veqryn/slog-context"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
)

type accessKey struct{}

type accessAttrs struct {
	mu    sync.Mutex
	attrs []slog.Attr
}

// Annotate adds attributes to the current request's access-log line.
func Annotate(ctx context.Context, attrs ...slog.Attr) {
	if a, ok := ctx.Value(accessKey{}).(*accessAttrs); ok {
		a.mu.Lock()
		a.attrs = append(a.attrs, attrs...)
		a.mu.Unlock()
	}
}

type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Middleware adds OpenTelemetry instrumentation, an X-Request-Id (the trace
// id), the request-scoped logger in the context (also used by the OCM
// bindings via slog-context) and one access-log line per request.
func Middleware(h http.Handler, logger *slog.Logger) http.Handler {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := requestIDFor(ctx)
		ctx = WithRequestID(ctx, id)
		ctx = slogctx.NewCtx(ctx, logger)
		access := &accessAttrs{}
		ctx = context.WithValue(ctx, accessKey{}, access)
		w.Header().Set("X-Request-Id", id)
		rec := &recorder{ResponseWriter: w}
		start := time.Now()
		defer func() {
			p := recover()
			status := rec.status
			if status == 0 {
				status = http.StatusOK
				if p != nil {
					status = http.StatusInternalServerError
				}
			}
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.EscapedPath()),
				slog.Int("status", status),
				slog.Int64("bytes", rec.bytes),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
				slog.Bool("aborted", p != nil),
			}
			access.mu.Lock()
			attrs = append(attrs, access.attrs...)
			access.mu.Unlock()
			level := slog.LevelInfo
			if status >= 500 || p != nil {
				level = slog.LevelError
			}
			logger.LogAttrs(ctx, level, "request", attrs...)
			if p != nil {
				panic(p)
			}
		}()
		h.ServeHTTP(rec, r.WithContext(ctx))
	})
	return otelhttp.NewHandler(inner, "ocm-server",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method }))
}

func requestIDFor(ctx context.Context) string {
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		return sc.TraceID().String()
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
