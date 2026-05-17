package echotelmetrics

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMiddleware_RecordsDefaultMetrics(t *testing.T) {
	e, reader := setupTest(t)
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
	e, reader := setupTest(t)
	e.GET("/teapot", func(_ *echo.Context) error {
		return echo.NewHTTPError(http.StatusTeapot, "short and stout")
	})
	serveGet(e, "/teapot")

	metrics := collectMetrics(t, reader)
	requestCount := sumDataPoint[int64](t, metrics, defaultRequestCountName)
	assertAttribute(t, requestCount.Attributes, "http.response.status_code", int64(http.StatusTeapot))
	assertAttribute(t, requestCount.Attributes, "error", true)

	responseSize := histogramDataPoint[int64](t, metrics, defaultResponseSizeName)
	if responseSize.Sum == 0 {
		t.Fatal("response size for HTTP error was 0, want error handler body size")
	}
}

func TestMiddleware_MultiWriteResponseRecordsOneFinalResponseSize(t *testing.T) {
	e, reader := setupTest(t)
	e.GET("/multi-write", func(c *echo.Context) error {
		if _, err := c.Response().Write([]byte("hello")); err != nil {
			return err
		}
		_, err := c.Response().Write([]byte("world"))
		return err
	})

	serveGet(e, "/multi-write")

	assertResponseSizeDatapoint(t, reader, len("helloworld"))
}

func TestMiddleware_ErrorHandlerResponseSizeIsIncluded(t *testing.T) {
	e, reader := setupTest(t)
	e.HTTPErrorHandler = func(c *echo.Context, _ error) {
		_ = c.String(http.StatusInternalServerError, "handled error")
	}
	e.GET("/custom-error", func(*echo.Context) error {
		return errors.New("boom")
	})

	serveGet(e, "/custom-error")

	assertResponseSizeDatapoint(t, reader, len("handled error"))
}

func TestMiddleware_NormalizesScheme(t *testing.T) {
	e, reader := setupTest(t)
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
	e, reader := setupTest(t)
	serveGet(e, "/missing/42?token=secret")

	requestCount := sumDataPoint[int64](t, collectMetrics(t, reader), defaultRequestCountName)
	assertAttribute(t, requestCount.Attributes, "http.route", unknownRoute)
}

func TestMiddleware_SkipsRequests(t *testing.T) {
	e, reader := setupTest(t, WithSkipper(func(*echo.Context) bool { return true }))
	e.GET("/health", func(c *echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	serveGet(e, "/health")

	metrics := collectMetrics(t, reader)
	if _, ok := metrics[defaultRequestCountName]; ok {
		t.Fatal("request count metric was recorded for skipped request")
	}
}

func TestMiddleware_DisablesInstrument(t *testing.T) {
	e, reader := setupTest(t, WithRequestCount(InstrumentConfig{Disabled: true}))
	e.GET("/users", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	serveGet(e, "/users")

	metrics := collectMetrics(t, reader)
	if _, ok := metrics[defaultRequestCountName]; ok {
		t.Fatal("disabled request count metric was recorded")
	}
	if _, ok := metrics[defaultRequestDurationName]; !ok {
		t.Fatal("enabled request duration metric was not recorded")
	}
}

func TestMiddleware_DisabledInstrumentStaysDisabledAcrossLaterOptions(t *testing.T) {
	e, reader := setupTest(t,
		WithRequestCount(InstrumentConfig{Disabled: true}),
		WithRequestCount(InstrumentConfig{Name: "custom.server.requests"}),
	)
	e.GET("/users", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	serveGet(e, "/users")

	metrics := collectMetrics(t, reader)
	if _, ok := metrics[defaultRequestCountName]; ok {
		t.Fatal("default request count metric was recorded after being disabled")
	}
	if _, ok := metrics["custom.server.requests"]; ok {
		t.Fatal("renamed request count metric was recorded after being disabled")
	}
}

func TestMiddleware_CustomMetricNameAndAttributes(t *testing.T) {
	e, reader := setupTest(t,
		WithRequestCount(InstrumentConfig{Name: "custom.server.requests"}),
		WithAttributes(func(*echo.Context, error) []attribute.KeyValue {
			return []attribute.KeyValue{attribute.String("service.tier", "edge")}
		}),
	)
	e.GET("/custom", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	serveGet(e, "/custom")

	requestCount := sumDataPoint[int64](t, collectMetrics(t, reader), "custom.server.requests")
	assertAttribute(t, requestCount.Attributes, "service.tier", "edge")
}

func TestMiddleware_AppliesAllOptionsAndCustomNames(t *testing.T) {
	e, reader := setupTest(t,
		WithMeterName("custom.meter"),
		WithMeterVersion("9.9.9"),
		WithRequestCount(InstrumentConfig{Name: "rc.custom", Description: "rc desc", Unit: "{r}"}),
		WithRequestDuration(InstrumentConfig{Name: "rd.custom", Description: "rd desc", Unit: "ms"}),
		WithRequestSize(InstrumentConfig{Name: "rs.custom", Description: "rs desc", Unit: "By"}),
		WithResponseSize(InstrumentConfig{Name: "rsp.custom", Description: "rsp desc", Unit: "By"}),
		WithActiveRequests(InstrumentConfig{Name: "ar.custom", Description: "ar desc", Unit: "{r}"}),
	)
	e.GET("/all", func(c *echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	serveGet(e, "/all")

	metrics := collectMetrics(t, reader)
	for _, name := range []string{"rc.custom", "rd.custom", "rs.custom", "rsp.custom", "ar.custom"} {
		if _, ok := metrics[name]; !ok {
			t.Fatalf("expected custom metric %q to be recorded", name)
		}
	}
}

func TestMiddleware_DisablesAllInstruments(t *testing.T) {
	e, reader := setupTest(t,
		WithRequestCount(InstrumentConfig{Disabled: true}),
		WithRequestDuration(InstrumentConfig{Disabled: true}),
		WithRequestSize(InstrumentConfig{Disabled: true}),
		WithResponseSize(InstrumentConfig{Disabled: true}),
		WithActiveRequests(InstrumentConfig{Disabled: true}),
	)
	e.GET("/none", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	serveGet(e, "/none")

	metrics := collectMetrics(t, reader)
	if len(metrics) != 0 {
		t.Fatalf("expected no metrics recorded, got %d", len(metrics))
	}
}

func TestMiddleware_NilOptionIsIgnored(t *testing.T) {
	_, provider := newMeterProvider()
	mw, err := New(nil, WithMeterProvider(provider))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if mw == nil {
		t.Fatal("middleware is nil")
	}
}

func TestMiddleware_HandlerErrorMarksError(t *testing.T) {
	e, reader := setupTest(t)
	e.GET("/boom", func(_ *echo.Context) error {
		return errors.New("boom")
	})
	serveGet(e, "/boom")

	requestCount := sumDataPoint[int64](t, collectMetrics(t, reader), defaultRequestCountName)
	assertAttribute(t, requestCount.Attributes, "error", true)
	assertAttribute(t, requestCount.Attributes, "http.response.status_code", int64(http.StatusInternalServerError))
}

func TestResponseStatus(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*echo.Response)
		err   error
		want  int
	}{
		{"committed_response_uses_status", func(r *echo.Response) { r.WriteHeader(http.StatusAccepted) }, errors.New("late"), http.StatusAccepted},
		{"zero_status_defaults_ok", func(*echo.Response) {}, nil, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := echo.NewResponse(httptest.NewRecorder(), nil)
			tt.setup(response)
			if got := responseStatus(response, tt.err); got != tt.want {
				t.Fatalf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRequestSizeNegativeContentLength(t *testing.T) {
	e := echo.New()
	request := httptest.NewRequest(http.MethodGet, "/x", nil)
	request.ContentLength = -1
	c := e.NewContext(request, httptest.NewRecorder())
	if got := requestSize(c); got != 0 {
		t.Fatalf("requestSize = %d, want 0", got)
	}
}

func TestResponseSizeNegative(t *testing.T) {
	response := echo.NewResponse(httptest.NewRecorder(), nil)
	response.Size = -10
	if got := responseSize(response); got != 0 {
		t.Fatalf("responseSize = %d, want 0", got)
	}
}

func TestSchemeDetection(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*http.Request)
		want  string
	}{
		{"tls", func(r *http.Request) { r.TLS = &tls.ConnectionState{} }, "https"},
		{"forwarded_proto_https", func(r *http.Request) { r.Header.Set(echo.HeaderXForwardedProto, "HTTPS") }, "https"},
		{"forwarded_proto_list", func(r *http.Request) { r.Header.Set(echo.HeaderXForwardedProto, "https, http") }, "https"},
		{"url_scheme", func(r *http.Request) { r.URL = &url.URL{Scheme: "https", Path: "/"} }, "https"},
		{"default_http", func(r *http.Request) { r.URL = &url.URL{Path: "/"} }, "http"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			tt.setup(r)
			if got := scheme(r); got != tt.want {
				t.Fatalf("scheme = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewWithConfigInstrumentErrors(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Config)
		want      string
	}{
		{
			name: "request_count",
			configure: func(config *Config) {
				config.RequestCount = InstrumentConfig{Name: invalidInstrumentName()}
			},
			want: "creating request count instrument",
		},
		{
			name: "request_duration",
			configure: func(config *Config) {
				config.RequestDuration = InstrumentConfig{Name: invalidInstrumentName()}
			},
			want: "creating request duration instrument",
		},
		{
			name: "request_size",
			configure: func(config *Config) {
				config.RequestSize = InstrumentConfig{Name: invalidInstrumentName()}
			},
			want: "creating request size instrument",
		},
		{
			name: "response_size",
			configure: func(config *Config) {
				config.ResponseSize = InstrumentConfig{Name: invalidInstrumentName()}
			},
			want: "creating response size instrument",
		},
		{
			name: "active_requests",
			configure: func(config *Config) {
				config.ActiveRequests = InstrumentConfig{Name: invalidInstrumentName()}
			},
			want: "creating active requests instrument",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, provider := newMeterProvider()
			config := Config{MeterProvider: provider}
			tt.configure(&config)

			_, err := NewWithConfig(config)
			if err == nil {
				t.Fatalf("NewWithConfig error = nil, want %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewWithConfig error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestNewReturnsInstrumentError(t *testing.T) {
	_, provider := newMeterProvider()
	_, err := New(
		WithMeterProvider(provider),
		WithActiveRequests(InstrumentConfig{Name: invalidInstrumentName()}),
	)
	if err == nil {
		t.Fatal("New error = nil, want instrument creation error")
	}
}

func TestMiddlewarePanicsOnInvalidConfig(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Middleware did not panic")
		}
	}()

	_, provider := newMeterProvider()
	Middleware(
		WithMeterProvider(provider),
		WithActiveRequests(InstrumentConfig{Name: invalidInstrumentName()}),
	)
}

func setupTest(t *testing.T, opts ...Option) (*echo.Echo, *sdkmetric.ManualReader) {
	t.Helper()
	reader, provider := newMeterProvider()
	e := echo.New()
	allOpts := append([]Option{WithMeterProvider(provider)}, opts...)
	e.Use(Middleware(allOpts...))

	return e, reader
}

func serveGet(e *echo.Echo, path string) {
	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
}

func newMeterProvider() (*sdkmetric.ManualReader, *sdkmetric.MeterProvider) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	return reader, provider
}

func invalidInstrumentName() string {
	return strings.Repeat("a", 1024)
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

func assertResponseSizeDatapoint(t *testing.T, reader *sdkmetric.ManualReader, want int) {
	t.Helper()

	responseSize := histogramDataPoint[int64](t, collectMetrics(t, reader), defaultResponseSizeName)
	if responseSize.Count != 1 {
		t.Fatalf("response size count = %d, want 1", responseSize.Count)
	}
	if responseSize.Sum != int64(want) {
		t.Fatalf("response size sum = %d, want %d", responseSize.Sum, want)
	}
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
