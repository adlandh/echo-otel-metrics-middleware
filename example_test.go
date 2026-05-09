package echotelmetrics_test

import (
	"context"
	"net/http"
	"strings"
	"time"

	echotelmetrics "github.com/adlandh/echo-otel-metrics-middleware"
	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func ExampleMiddleware() {
	e := echo.New()
	e.Use(echotelmetrics.Middleware())
	e.GET("/users/:id", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
}

func ExampleMiddleware_withMeterProvider() {
	exporter, err := stdoutmetric.New()
	if err != nil {
		panic(err)
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(time.Second))),
	)
	defer func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			panic(err)
		}
	}()
	otel.SetMeterProvider(provider)

	e := echo.New()
	e.Use(echotelmetrics.Middleware(echotelmetrics.WithMeterProvider(provider)))
	e.GET("/users/:id", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
}

// ExampleMiddleware_withSkipper shows how to bypass metric recording for
// internal endpoints such as the health probe and Prometheus scrape endpoint.
func ExampleMiddleware_withSkipper() {
	skip := func(c *echo.Context) bool {
		path := c.Request().URL.Path
		return path == "/healthz" || strings.HasPrefix(path, "/metrics")
	}

	e := echo.New()
	e.Use(echotelmetrics.Middleware(echotelmetrics.WithSkipper(skip)))
	e.GET("/users/:id", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
}

// ExampleMiddleware_withAttributes shows how to attach bounded custom
// attributes to every recorded measurement. Only use attributes whose value
// set is small and finite (e.g., subscription tier, region). Never use raw
// paths, user IDs, request IDs, or client IP addresses.
func ExampleMiddleware_withAttributes() {
	extractor := func(c *echo.Context, _ error) []attribute.KeyValue {
		tier := c.Request().Header.Get("X-Tenant-Tier")
		if tier == "" {
			tier = "free"
		}
		return []attribute.KeyValue{
			attribute.String("tenant.tier", tier),
		}
	}

	e := echo.New()
	e.Use(echotelmetrics.Middleware(echotelmetrics.WithAttributes(extractor)))
	e.GET("/users/:id", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
}

// ExampleNewRecorder shows how to register a custom MetricRecorder at
// initialization. The recorder is invoked once per non-skipped request, after
// the handler returns, with the same bounded attribute slice the default
// instruments receive.
func ExampleNewRecorder() {
	recorder, err := echotelmetrics.NewRecorder()
	if err != nil {
		panic(err)
	}

	slowRequests, err := recorder.Meter().Int64Counter("app.requests.slow")
	if err != nil {
		panic(err)
	}

	recorder.AddRecorder(func(_ *echo.Context, _ int, _ error, duration time.Duration, attrs []attribute.KeyValue) {
		if duration < 500*time.Millisecond {
			return
		}
		ctx := context.Background()
		// attrs is shared with default instruments; do not mutate it.
		// Pass it through as-is so app.requests.slow carries the same
		// labels as http.server.request.count.
		slowRequests.Add(ctx, 1, otelMetricAttributes(attrs))
	})

	e := echo.New()
	e.Use(recorder.Handler())
	e.GET("/users/:id", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
}

// ExampleRecorder_AddRecorder shows how to register a custom MetricRecorder
// after the middleware has been constructed. AddRecorder is safe to call
// concurrently with request serving.
func ExampleRecorder_AddRecorder() {
	recorder, err := echotelmetrics.NewRecorder()
	if err != nil {
		panic(err)
	}

	e := echo.New()
	e.Use(recorder.Handler())
	e.GET("/users/:id", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	// Later — after a feature flag flips, after a startup hook completes,
	// or in response to a config reload — register an additional recorder
	// without touching the Echo registration above.
	recorder.AddRecorder(func(_ *echo.Context, status int, _ error, _ time.Duration, _ []attribute.KeyValue) {
		_ = status // forward to a custom counter or histogram of your choosing
	})
}

// ExampleRecorder_Meter shows how to retrieve the configured OpenTelemetry
// meter and build a user-owned instrument that is bound to the same
// instrumentation scope (meter provider, name, and version) as the default
// HTTP server instruments.
func ExampleRecorder_Meter() {
	recorder, err := echotelmetrics.NewRecorder()
	if err != nil {
		panic(err)
	}

	queueDepth, err := recorder.Meter().Int64UpDownCounter(
		"app.queue.depth",
		// description and unit omitted for brevity
	)
	if err != nil {
		panic(err)
	}

	// Use queueDepth from anywhere — recorder closures, background workers,
	// or unrelated subsystems — knowing the metric ships through the same
	// MeterProvider as the middleware's defaults.
	queueDepth.Add(context.Background(), 1)

	e := echo.New()
	e.Use(recorder.Handler())
	e.GET("/jobs/:id", func(c *echo.Context) error {
		return c.NoContent(http.StatusAccepted)
	})
}

// otelMetricAttributes converts a []attribute.KeyValue slice into a
// metric.MeasurementOption so it can be passed straight through to instrument
// recording calls without copying.
func otelMetricAttributes(attrs []attribute.KeyValue) metric.MeasurementOption {
	return metric.WithAttributes(attrs...)
}
