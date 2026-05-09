package echotelmetrics

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const unknownRoute = "unknown"

type instruments struct {
	requestCount    metric.Int64Counter
	requestDuration metric.Float64Histogram
	requestSize     metric.Int64Histogram
	responseSize    metric.Int64Histogram
	activeRequests  metric.Int64UpDownCounter
}

// New creates Echo middleware and returns an error if OpenTelemetry instruments cannot be initialized.
//
// The returned middleware records the default HTTP server metrics. To register
// custom MetricRecorders after construction or to access the configured meter,
// use NewRecorder instead.
func New(options ...Option) (echo.MiddlewareFunc, error) {
	recorder, err := NewRecorder(options...)
	if err != nil {
		return nil, err
	}

	return recorder.Handler(), nil
}

// Middleware creates Echo middleware with default configuration and panics on invalid configuration.
//
// The returned middleware records the default HTTP server metrics. To register
// custom MetricRecorders after construction or to access the configured meter,
// use NewRecorder instead.
func Middleware(options ...Option) echo.MiddlewareFunc {
	handler, err := New(options...)
	if err != nil {
		panic(err)
	}

	return handler
}

// NewWithConfig creates Echo middleware from an explicit Config.
//
// The returned middleware records the default HTTP server metrics. To register
// custom MetricRecorders after construction or to access the configured meter,
// use NewRecorderWithConfig instead.
func NewWithConfig(config Config) (echo.MiddlewareFunc, error) {
	recorder, err := NewRecorderWithConfig(config)
	if err != nil {
		return nil, err
	}

	return recorder.Handler(), nil
}

func newInstrumentsForMeter(meter metric.Meter, config Config) (instruments, error) {
	created := instruments{}

	var err error

	created.requestCount, err = newRequestCount(meter, config.RequestCount)
	if err != nil {
		return instruments{}, err
	}

	created.requestDuration, err = newRequestDuration(meter, config.RequestDuration)
	if err != nil {
		return instruments{}, err
	}

	created.requestSize, err = newRequestSize(meter, config.RequestSize)
	if err != nil {
		return instruments{}, err
	}

	created.responseSize, err = newResponseSize(meter, config.ResponseSize)
	if err != nil {
		return instruments{}, err
	}

	created.activeRequests, err = newActiveRequests(meter, config.ActiveRequests)
	if err != nil {
		return instruments{}, err
	}

	return created, nil
}

func newRequestCount(meter metric.Meter, config InstrumentConfig) (metric.Int64Counter, error) {
	if config.Disabled {
		return nil, nil
	}

	requestCount, err := meter.Int64Counter(
		config.Name,
		metric.WithDescription(config.Description),
		metric.WithUnit(config.Unit),
	)
	if err != nil {
		return nil, fmt.Errorf("creating request count instrument: %w", err)
	}

	return requestCount, nil
}

func newRequestDuration(meter metric.Meter, config InstrumentConfig) (metric.Float64Histogram, error) {
	if config.Disabled {
		return nil, nil
	}

	requestDuration, err := meter.Float64Histogram(
		config.Name,
		metric.WithDescription(config.Description),
		metric.WithUnit(config.Unit),
	)
	if err != nil {
		return nil, fmt.Errorf("creating request duration instrument: %w", err)
	}

	return requestDuration, nil
}

func newRequestSize(meter metric.Meter, config InstrumentConfig) (metric.Int64Histogram, error) {
	if config.Disabled {
		return nil, nil
	}

	requestSize, err := meter.Int64Histogram(
		config.Name,
		metric.WithDescription(config.Description),
		metric.WithUnit(config.Unit),
	)
	if err != nil {
		return nil, fmt.Errorf("creating request size instrument: %w", err)
	}

	return requestSize, nil
}

func newResponseSize(meter metric.Meter, config InstrumentConfig) (metric.Int64Histogram, error) {
	if config.Disabled {
		return nil, nil
	}

	responseSize, err := meter.Int64Histogram(
		config.Name,
		metric.WithDescription(config.Description),
		metric.WithUnit(config.Unit),
	)
	if err != nil {
		return nil, fmt.Errorf("creating response size instrument: %w", err)
	}

	return responseSize, nil
}

func newActiveRequests(meter metric.Meter, config InstrumentConfig) (metric.Int64UpDownCounter, error) {
	if config.Disabled {
		return nil, nil
	}

	activeRequests, err := meter.Int64UpDownCounter(
		config.Name,
		metric.WithDescription(config.Description),
		metric.WithUnit(config.Unit),
	)
	if err != nil {
		return nil, fmt.Errorf("creating active requests instrument: %w", err)
	}

	return activeRequests, nil
}

func applyConfigDefaults(config Config) Config {
	defaults := DefaultConfig()

	if config.MeterProvider == nil {
		config.MeterProvider = defaults.MeterProvider
	}

	if config.MeterName == "" {
		config.MeterName = defaults.MeterName
	}

	if config.MeterVersion == "" {
		config.MeterVersion = defaults.MeterVersion
	}

	config.RequestCount = mergeInstrumentConfig(defaults.RequestCount, config.RequestCount)
	config.RequestDuration = mergeInstrumentConfig(defaults.RequestDuration, config.RequestDuration)
	config.RequestSize = mergeInstrumentConfig(defaults.RequestSize, config.RequestSize)
	config.ResponseSize = mergeInstrumentConfig(defaults.ResponseSize, config.ResponseSize)
	config.ActiveRequests = mergeInstrumentConfig(defaults.ActiveRequests, config.ActiveRequests)

	return config
}

// Mirrored in README.md — update the default-attributes table there when changing these values.
func requestAttributes(c *echo.Context, status int, err error) []attribute.KeyValue {
	r := c.Request()

	return []attribute.KeyValue{
		attribute.String("http.request.method", r.Method),
		attribute.String("http.route", route(c)),
		attribute.Int("http.response.status_code", status),
		attribute.String("url.scheme", scheme(r)),
		attribute.Bool("error", err != nil || status >= http.StatusInternalServerError),
	}
}

func activeAttributes(c *echo.Context) []attribute.KeyValue {
	r := c.Request()

	return []attribute.KeyValue{
		attribute.String("http.request.method", r.Method),
		attribute.String("http.route", route(c)),
		attribute.String("url.scheme", scheme(r)),
	}
}

func responseStatus(response *echo.Response, err error) int {
	status := response.Status
	if err != nil && !response.Committed {
		if httpError, ok := errors.AsType[*echo.HTTPError](err); ok {
			return httpError.Code
		}

		return http.StatusInternalServerError
	}

	if status > 0 {
		return status
	}

	return http.StatusOK
}

func requestSize(c *echo.Context) int64 {
	contentLength := c.Request().ContentLength
	if contentLength < 0 {
		return 0
	}

	return contentLength
}

func responseSize(response *echo.Response) int64 {
	size := response.Size
	if size < 0 {
		return 0
	}

	return size
}

func route(c *echo.Context) string {
	path := c.Path()
	if path == "" {
		return unknownRoute
	}

	return path
}

func scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}

	if value := r.Header.Get(echo.HeaderXForwardedProto); value != "" {
		return normalizeScheme(value)
	}

	if value := r.URL.Scheme; value != "" {
		return normalizeScheme(value)
	}

	return "http"
}

func normalizeScheme(value string) string {
	if scheme, _, ok := strings.Cut(value, ","); ok {
		value = scheme
	}

	if strings.EqualFold(strings.TrimSpace(value), "https") {
		return "https"
	}

	return "http"
}
