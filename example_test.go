package echotelmetrics_test

import (
	"context"
	"net/http"
	"time"

	echotelmetrics "github.com/adlandh/echo-otel-metrics-middleware"
	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel"
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
