# echo-otel-metrics-middleware

OpenTelemetry HTTP server metrics middleware for [Echo v5](https://github.com/labstack/echo).

[![Go Reference](https://pkg.go.dev/badge/github.com/adlandh/echo-otel-metrics-middleware.svg)](https://pkg.go.dev/github.com/adlandh/echo-otel-metrics-middleware)
[![CI](https://github.com/adlandh/echo-otel-metrics-middleware/actions/workflows/test.yml/badge.svg)](https://github.com/adlandh/echo-otel-metrics-middleware/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/adlandh/echo-otel-metrics-middleware/branch/main/graph/badge.svg)](https://codecov.io/gh/adlandh/echo-otel-metrics-middleware)
[![Go Report Card](https://goreportcard.com/badge/github.com/adlandh/echo-otel-metrics-middleware)](https://goreportcard.com/report/github.com/adlandh/echo-otel-metrics-middleware)

Five standard HTTP server instruments, OTel HTTP semconv attribute names, and bounded cardinality by default — without touching your exporter configuration.

---

## Table of Contents

- [Why](#why)
- [Install](#install)
- [Quick start](#quick-start)
- [Working example: stdout exporter](#working-example-stdout-exporter)
- [Production example: Prometheus + /metrics endpoint](#production-example-prometheus--metrics-endpoint)
- [Recorded instruments](#recorded-instruments)
- [Default attributes](#default-attributes)
- [Configuration recipes](#configuration-recipes)
  - [Skipper](#skipper)
  - [Custom attributes](#custom-attributes)
  - [Renaming or disabling instruments](#renaming-or-disabling-instruments)
  - [Custom meter provider](#custom-meter-provider)
  - [Custom metrics](#custom-metrics)
- [Error semantics](#error-semantics)
- [Constructor variants](#constructor-variants)
- [Cardinality cheat sheet](#cardinality-cheat-sheet)
- [Compatibility](#compatibility)
- [License](#license)

---

## Why

The middleware records five OpenTelemetry HTTP server instruments for every Echo v5 request. It uses Echo's route pattern (`/users/:id`) — not the raw request path (`/users/42`) — for the `http.route` attribute, so metric cardinality stays bounded without any configuration. You bring the OpenTelemetry SDK and exporter; the library stays exporter-agnostic and never starts a `/metrics` endpoint on its own.

## Install

```bash
go get github.com/adlandh/echo-otel-metrics-middleware
```

## Quick start

```go
import echotelmetrics "github.com/adlandh/echo-otel-metrics-middleware"

e := echo.New()
e.Use(echotelmetrics.Middleware())
```

By default the middleware uses the global OpenTelemetry meter provider (`otel.GetMeterProvider()`). If your application has not configured an SDK with a reader and exporter, no metrics will flow anywhere. See the examples below to wire a real exporter.

## Working example: stdout exporter

```go
package main

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

func main() {
	exporter, err := stdoutmetric.New()
	if err != nil {
		panic(err)
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(time.Second)),
		),
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
	e.Logger.Fatal(e.Start(":8080"))
}
```

## Production example: Prometheus + /metrics endpoint

First, add the Prometheus exporter to your application (not to this library):

```bash
go get go.opentelemetry.io/otel/exporters/prometheus
```

Then wire it up. Note the `WithSkipper` that prevents the Prometheus scrape requests from inflating the request counter:

```go
package main

import (
	"net/http"
	"strings"

	echotelmetrics "github.com/adlandh/echo-otel-metrics-middleware"
	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel"
	prometheusexporter "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	exporter, err := prometheusexporter.New()
	if err != nil {
		panic(err)
	}

	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
	)
	otel.SetMeterProvider(provider)

	// Skip the scrape endpoint itself so it does not inflate metrics.
	skipMetricsRoute := func(c *echo.Context) bool {
		return strings.HasPrefix(c.Request().URL.Path, "/metrics")
	}

	e := echo.New()
	e.Use(echotelmetrics.Middleware(
		echotelmetrics.WithMeterProvider(provider),
		echotelmetrics.WithSkipper(skipMetricsRoute),
	))

	e.GET("/users/:id", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))

	e.Logger.Fatal(e.Start(":8080"))
}
```

## Recorded instruments

<!-- Mirrored in README.md — update when changing defaults in config.go -->

| Instrument name | Kind | Unit | Description |
|---|---|---|---|
| `http.server.request.count` | Int64Counter | `{request}` | Total number of HTTP server requests. |
| `http.server.request.duration` | Float64Histogram | `s` | Duration of HTTP server requests. |
| `http.server.request.body.size` | Int64Histogram | `By` | Size of HTTP request bodies. |
| `http.server.response.body.size` | Int64Histogram | `By` | Size of HTTP response bodies. |
| `http.server.active_requests` | Int64UpDownCounter | `{request}` | Number of active HTTP server requests. |

All five instruments are enabled by default. Any individual instrument can be renamed or disabled via configuration (see [Renaming or disabling instruments](#renaming-or-disabling-instruments)).

## Default attributes

<!-- Mirrored in README.md — update when changing defaults in middleware.go requestAttributes -->

| Attribute key | Value source | Bounded |
|---|---|---|
| `http.request.method` | `r.Method` | ✓ (`GET`, `POST`, …) |
| `http.route` | Echo route pattern (`c.Path()`), `"unknown"` if unmatched | ✓ |
| `http.response.status_code` | Final response status code | ✓ |
| `url.scheme` | TLS state → `X-Forwarded-Proto` (normalized) → URL scheme, default `"http"` | ✓ (`"http"` or `"https"` only) |
| `error` | `true` when handler returns an error or status ≥ 500 | ✓ (bool) |

`active_requests` uses `http.request.method`, `http.route`, and `url.scheme` only (no status code or error state, since the request has not completed).

## Configuration recipes

### Skipper

Skip instrumentation for selected requests. The handler is still called; only metric recording is bypassed.

```go
skip := func(c *echo.Context) bool {
    path := c.Request().URL.Path
    return path == "/healthz" || strings.HasPrefix(path, "/metrics")
}

e.Use(echotelmetrics.Middleware(echotelmetrics.WithSkipper(skip)))
```

### Custom attributes

Attach additional attributes to every measurement. **All custom attributes must be bounded** — the value set must be small and finite.

```go
extractor := func(c *echo.Context, _ error) []attribute.KeyValue {
    tier := c.Request().Header.Get("X-Tenant-Tier")
    if tier == "" {
        tier = "free"
    }
    return []attribute.KeyValue{
        attribute.String("tenant.tier", tier),
    }
}

e.Use(echotelmetrics.Middleware(echotelmetrics.WithAttributes(extractor)))
```

See [Cardinality cheat sheet](#cardinality-cheat-sheet) for examples of what is and is not safe to use as a metric attribute.

### Renaming or disabling instruments

Each instrument can be renamed, re-described, re-unitized, or disabled independently:

```go
e.Use(echotelmetrics.Middleware(
    // Rename the request counter to match your org's naming convention.
    echotelmetrics.WithRequestCount(echotelmetrics.InstrumentConfig{
        Name: "myapp.http.requests",
    }),
    // Disable body-size histograms if you don't need them.
    echotelmetrics.WithRequestSize(echotelmetrics.InstrumentConfig{Disabled: true}),
    echotelmetrics.WithResponseSize(echotelmetrics.InstrumentConfig{Disabled: true}),
))
```

Only the fields you set are overridden; unset fields keep their defaults.

### Custom meter provider

```go
e.Use(echotelmetrics.Middleware(
    echotelmetrics.WithMeterProvider(myProvider),
    echotelmetrics.WithMeterName("com.example.myservice"),
    echotelmetrics.WithMeterVersion("1.2.3"),
))
```

### Custom metrics

Register your own per-request metric recorders alongside the five default instruments. A `MetricRecorder` is invoked once per non-skipped request, after the handler returns, with the same bounded attribute slice the default instruments receive — so a custom counter automatically gets `http.route`, `http.request.method`, etc.

Two registration paths are supported. Use `WithRecorder` at initialization, or `(*Recorder).AddRecorder` after construction:

```go
recorder, err := echotelmetrics.NewRecorder(
    echotelmetrics.WithMeterProvider(provider),
    echotelmetrics.WithRecorder(func(c *echo.Context, status int, err error, duration time.Duration, attrs []attribute.KeyValue) {
        // attrs is shared with the default instruments — do not mutate it.
        // Build a new slice on top of attrs if you need extra labels.
    }),
)
if err != nil {
    panic(err)
}

e := echo.New()
e.Use(recorder.Handler())

// Later — feature flag flip, config reload, startup hook, etc.
recorder.AddRecorder(myExtraRecorder)
```

The configured meter is also exposed for instruments that don't fit the per-request recorder pattern (background workers, batch jobs, queue depth):

```go
queueDepth, err := recorder.Meter().Int64UpDownCounter("app.queue.depth")
```

`recorder.Meter()` returns the same `metric.Meter` the middleware uses internally, bound to the configured meter provider, name, and version. User-built instruments share the instrumentation scope with the defaults.

**Panic isolation.** A panic raised inside a recorder is recovered, logged through the Echo logger at error level, and does not abort the request, skip subsequent recorders, or prevent default instruments from recording.

See the executable godoc examples (`ExampleNewRecorder`, `ExampleRecorder_AddRecorder`, `ExampleRecorder_Meter`) on [pkg.go.dev](https://pkg.go.dev/github.com/adlandh/echo-otel-metrics-middleware) for full snippets.

## Error semantics

Three behaviours that are easy to miss:

- **`error` attribute** is `true` when the Echo handler returns a non-nil error **or** when the response status code is ≥ 500, even if no Go error was returned. Both paths set `error=true`.
- **Response body size** is recorded inside `echo.Response.After`, so it includes any bytes written by Echo's central error handler after the middleware's handler call returns. The size reflects what was actually sent to the client.
- **`url.scheme`** is normalized: `X-Forwarded-Proto: https, http` (comma-separated chain) becomes `"https"`, and any value other than a case-insensitive match for `"https"` becomes `"http"`. Arbitrary forwarded scheme values are never recorded.

## Constructor variants

| Constructor | Behaviour |
|---|---|
| `Middleware(opts...)` | Creates the middleware and **panics** if OTel instrument creation fails. Convenient for `e.Use(...)` one-liners. |
| `New(opts...)` | Returns `(echo.MiddlewareFunc, error)`. Use in production code where you want to handle the error explicitly. |
| `NewWithConfig(cfg)` | Like `New` but accepts a fully-constructed `Config` instead of variadic options. Use when you build the config programmatically. |
| `NewRecorder(opts...)` | Returns `(*Recorder, error)`. Use this when you need post-construction registration of custom `MetricRecorder`s or direct access to the configured meter. |
| `NewRecorderWithConfig(cfg)` | Like `NewRecorder` but accepts a `Config` value directly. |

## Cardinality cheat sheet

Metric cardinality explodes when attributes can take an unbounded number of values. Every distinct combination of attribute values creates a new time series in your metrics backend.

| Bounded ✓ (safe) | Unbounded ✗ (dangerous) |
|---|---|
| `http.route` = `"/users/:id"` | `http.route` = `"/users/42"` (raw path) |
| `tenant.tier` = `"free"`, `"pro"`, `"enterprise"` | `tenant.id` = `"t-9f3a..."` (per-tenant UUID) |
| `region` = `"us-east-1"`, `"eu-west-1"`, … | `client_ip` = `"203.0.113.42"` |
| `tenant.id` (if you have < ~100 tenants) | `user.id` = `"u-4829..."` |
| `error` = `true`, `false` | `request_id` = `"req-8fe2..."` |
| `url.scheme` = `"http"`, `"https"` | `url.full` = `"/search?q=opentelemetry&page=3"` |

## Compatibility

| Component | Version |
|---|---|
| Go | 1.26+ |
| Echo | v5 (`github.com/labstack/echo/v5`) |
| OpenTelemetry Go | v1.43.0+ (`go.opentelemetry.io/otel`) |

## License

MIT — see [LICENSE](LICENSE).
