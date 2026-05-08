package echotelmetrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMiddleware_RecordsDefaultMetrics(t *testing.T) {
	reader, provider := newMeterProvider()
	e := echo.New()
	e.Use(Middleware(WithMeterProvider(provider)))
	e.POST("/users/:id", func(c *echo.Context) error {
		return c.String(http.StatusCreated, "created")
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/users/42", strings.NewReader("payload"))
	request.Host = "example.test"
	e.ServeHTTP(recorder, request)

	metrics := collectMetrics(t, reader)

	requestCount := sumDataPoint[int64](t, metrics, defaultRequestCountName)
	if requestCount.Value != 1 {
		t.Fatalf("request count = %d, want 1", requestCount.Value)
	}
	assertAttribute(t, requestCount.Attributes, "http.request.method", http.MethodPost)
	assertAttribute(t, requestCount.Attributes, "http.route", "/users/:id")
	assertAttribute(t, requestCount.Attributes, "http.response.status_code", int64(http.StatusCreated))
	assertMissingAttribute(t, requestCount.Attributes, "server.address")
	assertAttribute(t, requestCount.Attributes, "error", false)

	duration := histogramDataPoint[float64](t, metrics, defaultRequestDurationName)
	if duration.Count != 1 {
		t.Fatalf("duration count = %d, want 1", duration.Count)
	}

	requestSize := histogramDataPoint[int64](t, metrics, defaultRequestSizeName)
	if requestSize.Sum != int64(len("payload")) {
		t.Fatalf("request size sum = %d, want %d", requestSize.Sum, len("payload"))
	}

	responseSize := histogramDataPoint[int64](t, metrics, defaultResponseSizeName)
	if responseSize.Sum != int64(len("created")) {
		t.Fatalf("response size sum = %d, want %d", responseSize.Sum, len("created"))
	}

	activeRequests := sumDataPoint[int64](t, metrics, defaultActiveRequestsName)
	if activeRequests.Value != 0 {
		t.Fatalf("active requests = %d, want 0 after request completes", activeRequests.Value)
	}
}

func TestMiddleware_RecordsHTTPErrorStatus(t *testing.T) {
	reader, provider := newMeterProvider()
	e := echo.New()
	e.Use(Middleware(WithMeterProvider(provider)))
	e.GET("/teapot", func(c *echo.Context) error {
		return echo.NewHTTPError(http.StatusTeapot, "short and stout")
	})

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/teapot", nil))

	metrics := collectMetrics(t, reader)
	requestCount := sumDataPoint[int64](t, metrics, defaultRequestCountName)
	assertAttribute(t, requestCount.Attributes, "http.response.status_code", int64(http.StatusTeapot))
	assertAttribute(t, requestCount.Attributes, "error", true)

	responseSize := histogramDataPoint[int64](t, metrics, defaultResponseSizeName)
	if responseSize.Sum == 0 {
		t.Fatal("response size for HTTP error was 0, want error handler body size")
	}
}

func TestMiddleware_NormalizesScheme(t *testing.T) {
	reader, provider := newMeterProvider()
	e := echo.New()
	e.Use(Middleware(WithMeterProvider(provider)))
	e.GET("/scheme", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/scheme", nil)
	request.Header.Set(echo.HeaderXForwardedProto, "custom-value")
	e.ServeHTTP(httptest.NewRecorder(), request)

	requestCount := sumDataPoint[int64](t, collectMetrics(t, reader), defaultRequestCountName)
	assertAttribute(t, requestCount.Attributes, "url.scheme", "http")
}

func TestMiddleware_RecordsUnknownRouteWithoutRawPath(t *testing.T) {
	reader, provider := newMeterProvider()
	e := echo.New()
	e.Use(Middleware(WithMeterProvider(provider)))

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing/42?token=secret", nil))

	requestCount := sumDataPoint[int64](t, collectMetrics(t, reader), defaultRequestCountName)
	assertAttribute(t, requestCount.Attributes, "http.route", unknownRoute)
}

func TestMiddleware_SkipsRequests(t *testing.T) {
	reader, provider := newMeterProvider()
	e := echo.New()
	e.Use(Middleware(
		WithMeterProvider(provider),
		WithSkipper(func(*echo.Context) bool { return true }),
	))
	e.GET("/health", func(c *echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	metrics := collectMetrics(t, reader)
	if _, ok := metrics[defaultRequestCountName]; ok {
		t.Fatal("request count metric was recorded for skipped request")
	}
}

func TestMiddleware_DisablesInstrument(t *testing.T) {
	reader, provider := newMeterProvider()
	e := echo.New()
	e.Use(Middleware(
		WithMeterProvider(provider),
		WithRequestCount(InstrumentConfig{Disabled: true}),
	))
	e.GET("/users", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))

	metrics := collectMetrics(t, reader)
	if _, ok := metrics[defaultRequestCountName]; ok {
		t.Fatal("disabled request count metric was recorded")
	}
	if _, ok := metrics[defaultRequestDurationName]; !ok {
		t.Fatal("enabled request duration metric was not recorded")
	}
}

func TestMiddleware_CustomMetricNameAndAttributes(t *testing.T) {
	reader, provider := newMeterProvider()
	e := echo.New()
	e.Use(Middleware(
		WithMeterProvider(provider),
		WithRequestCount(InstrumentConfig{Name: "custom.server.requests"}),
		WithAttributes(func(*echo.Context, error) []attribute.KeyValue {
			return []attribute.KeyValue{attribute.String("service.tier", "edge")}
		}),
	))
	e.GET("/custom", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/custom", nil))

	requestCount := sumDataPoint[int64](t, collectMetrics(t, reader), "custom.server.requests")
	assertAttribute(t, requestCount.Attributes, "service.tier", "edge")
}

func newMeterProvider() (*sdkmetric.ManualReader, *sdkmetric.MeterProvider) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	return reader, provider
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()

	resourceMetrics := metricdata.ResourceMetrics{}
	if err := reader.Collect(context.Background(), &resourceMetrics); err != nil {
		t.Fatalf("collecting metrics: %v", err)
	}

	metrics := map[string]metricdata.Metrics{}
	for _, scopeMetrics := range resourceMetrics.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			metrics[metric.Name] = metric
		}
	}

	return metrics
}

func sumDataPoint[N int64 | float64](
	t *testing.T,
	metrics map[string]metricdata.Metrics,
	name string,
) metricdata.DataPoint[N] {
	t.Helper()

	metric, ok := metrics[name]
	if !ok {
		t.Fatalf("metric %q was not recorded", name)
	}

	sum, ok := metric.Data.(metricdata.Sum[N])
	if !ok {
		t.Fatalf("metric %q has data type %T, want metricdata.Sum", name, metric.Data)
	}
	if len(sum.DataPoints) != 1 {
		t.Fatalf("metric %q has %d data points, want 1", name, len(sum.DataPoints))
	}

	return sum.DataPoints[0]
}

func histogramDataPoint[N int64 | float64](
	t *testing.T,
	metrics map[string]metricdata.Metrics,
	name string,
) metricdata.HistogramDataPoint[N] {
	t.Helper()

	metric, ok := metrics[name]
	if !ok {
		t.Fatalf("metric %q was not recorded", name)
	}

	histogram, ok := metric.Data.(metricdata.Histogram[N])
	if !ok {
		t.Fatalf("metric %q has data type %T, want metricdata.Histogram", name, metric.Data)
	}
	if len(histogram.DataPoints) != 1 {
		t.Fatalf("metric %q has %d data points, want 1", name, len(histogram.DataPoints))
	}

	return histogram.DataPoints[0]
}

func assertAttribute(t *testing.T, attributes attribute.Set, key string, want any) {
	t.Helper()

	value, ok := attributes.Value(attribute.Key(key))
	if !ok {
		t.Fatalf("attribute %q was not recorded", key)
	}

	switch typed := want.(type) {
	case string:
		if got := value.AsString(); got != typed {
			t.Fatalf("attribute %q = %q, want %q", key, got, typed)
		}
	case int64:
		if got := value.AsInt64(); got != typed {
			t.Fatalf("attribute %q = %d, want %d", key, got, typed)
		}
	case bool:
		if got := value.AsBool(); got != typed {
			t.Fatalf("attribute %q = %t, want %t", key, got, typed)
		}
	default:
		t.Fatalf("unsupported assertion type %T", want)
	}
}

func assertMissingAttribute(t *testing.T, attributes attribute.Set, key string) {
	t.Helper()

	if _, ok := attributes.Value(attribute.Key(key)); ok {
		t.Fatalf("attribute %q was recorded", key)
	}
}
