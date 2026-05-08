package echotelmetrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel/attribute"
)

func benchHandler(c *echo.Context) error {
	return c.String(http.StatusOK, "ok")
}

func newBenchEcho(b *testing.B, options ...Option) *echo.Echo {
	b.Helper()

	e := echo.New()
	if len(options) > 0 {
		e.Use(Middleware(options...))
	}
	e.GET("/users/:id", benchHandler)

	return e
}

func runBench(b *testing.B, e *echo.Echo, body string) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		recorder := httptest.NewRecorder()
		var reader *strings.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}

		var req *http.Request
		if reader != nil {
			req = httptest.NewRequest(http.MethodGet, "/users/42", reader)
		} else {
			req = httptest.NewRequest(http.MethodGet, "/users/42", nil)
		}

		e.ServeHTTP(recorder, req)
	}
}

func BenchmarkBaseline(b *testing.B) {
	e := newBenchEcho(b)
	runBench(b, e, "")
}

func BenchmarkMiddlewareDefault(b *testing.B) {
	_, provider := newMeterProvider()
	e := newBenchEcho(b, WithMeterProvider(provider))
	runBench(b, e, "")
}

func BenchmarkMiddlewareWithBody(b *testing.B) {
	_, provider := newMeterProvider()
	e := newBenchEcho(b, WithMeterProvider(provider))
	runBench(b, e, "payload-body-content")
}

func BenchmarkMiddlewareSkipped(b *testing.B) {
	_, provider := newMeterProvider()
	e := newBenchEcho(b,
		WithMeterProvider(provider),
		WithSkipper(func(*echo.Context) bool { return true }),
	)
	runBench(b, e, "")
}

func BenchmarkMiddlewareAllDisabled(b *testing.B) {
	_, provider := newMeterProvider()
	e := newBenchEcho(b,
		WithMeterProvider(provider),
		WithRequestCount(InstrumentConfig{Disabled: true}),
		WithRequestDuration(InstrumentConfig{Disabled: true}),
		WithRequestSize(InstrumentConfig{Disabled: true}),
		WithResponseSize(InstrumentConfig{Disabled: true}),
		WithActiveRequests(InstrumentConfig{Disabled: true}),
	)
	runBench(b, e, "")
}

func BenchmarkMiddlewareWithAttributes(b *testing.B) {
	_, provider := newMeterProvider()
	e := newBenchEcho(b,
		WithMeterProvider(provider),
		WithAttributes(func(*echo.Context, error) []attribute.KeyValue {
			return []attribute.KeyValue{
				attribute.String("service.tier", "edge"),
				attribute.String("service.region", "eu-west-1"),
			}
		}),
	)
	runBench(b, e, "")
}

func BenchmarkMiddlewareParallel(b *testing.B) {
	_, provider := newMeterProvider()
	e := newBenchEcho(b, WithMeterProvider(provider))

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
			e.ServeHTTP(recorder, req)
		}
	})
}
