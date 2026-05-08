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
