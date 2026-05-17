package echotelmetrics

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// recorderCall captures one MetricRecorder invocation for assertions.
type recorderCall struct {
	status   int
	err      error
	duration time.Duration
	attrs    []attribute.KeyValue
}

// captureRecorder returns a MetricRecorder that appends each invocation to the
// supplied slice (guarded by a mutex for race safety).
func captureRecorder(mu *sync.Mutex, calls *[]recorderCall) MetricRecorder {
	return func(_ *echo.Context, status int, err error, duration time.Duration, attrs []attribute.KeyValue) {
		mu.Lock()
		defer mu.Unlock()

		copied := make([]attribute.KeyValue, len(attrs))
		copy(copied, attrs)
		*calls = append(*calls, recorderCall{
			status:   status,
			err:      err,
			duration: duration,
			attrs:    copied,
		})
	}
}

func TestRecorder_SingleRecorderInvokedAtRequestCompletion(t *testing.T) {
	var (
		mu    sync.Mutex
		calls []recorderCall
	)

	reader, provider := newMeterProvider()
	r, err := NewRecorder(WithMeterProvider(provider), WithRecorder(captureRecorder(&mu, &calls)))
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	e := echo.New()
	e.Use(r.Handler())
	e.GET("/users/:id", func(c *echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	serveGet(e, "/users/42")

	mu.Lock()
	defer mu.Unlock()

	if got := len(calls); got != 1 {
		t.Fatalf("recorder calls = %d, want 1", got)
	}

	call := calls[0]
	if call.status != http.StatusOK {
		t.Fatalf("status = %d, want %d", call.status, http.StatusOK)
	}
	if call.err != nil {
		t.Fatalf("err = %v, want nil", call.err)
	}
	if call.duration <= 0 {
		t.Fatalf("duration = %v, want > 0", call.duration)
	}

	assertAttributeSlice(t, call.attrs, "http.request.method", http.MethodGet)
	assertAttributeSlice(t, call.attrs, "http.route", "/users/:id")

	// Default instruments still recorded as before.
	requestCount := sumDataPoint[int64](t, collectMetrics(t, reader), defaultRequestCountName)
	if requestCount.Value != 1 {
		t.Fatalf("default request count = %d, want 1", requestCount.Value)
	}
}

func TestRecorder_MultipleRecordersInRegistrationOrder(t *testing.T) {
	var (
		mu    sync.Mutex
		order []string
	)

	makeAppender := func(name string) MetricRecorder {
		return func(*echo.Context, int, error, time.Duration, []attribute.KeyValue) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, name)
		}
	}

	_, provider := newMeterProvider()
	r, err := NewRecorder(
		WithMeterProvider(provider),
		WithRecorder(makeAppender("init-1"), makeAppender("init-2")),
	)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	r.AddRecorder(makeAppender("post-3"))
	r.AddRecorder(makeAppender("post-4"))

	e := echo.New()
	e.Use(r.Handler())
	e.GET("/order", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	serveGet(e, "/order")

	mu.Lock()
	defer mu.Unlock()

	want := []string{"init-1", "init-2", "post-3", "post-4"}
	if len(order) != len(want) {
		t.Fatalf("invocation count = %d, want %d (got %v)", len(order), len(want), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q (got %v)", i, order[i], want[i], order)
		}
	}
}

func TestRecorder_SharesDefaultAndCustomAttributes(t *testing.T) {
	var (
		mu       sync.Mutex
		captured []attribute.KeyValue
	)

	_, provider := newMeterProvider()
	r, err := NewRecorder(
		WithMeterProvider(provider),
		WithAttributes(func(*echo.Context, error) []attribute.KeyValue {
			return []attribute.KeyValue{attribute.String("tenant.tier", "pro")}
		}),
		WithRecorder(func(_ *echo.Context, _ int, _ error, _ time.Duration, attrs []attribute.KeyValue) {
			mu.Lock()
			defer mu.Unlock()
			captured = make([]attribute.KeyValue, len(attrs))
			copy(captured, attrs)
		}),
	)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	e := echo.New()
	e.Use(r.Handler())
	e.GET("/attrs", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	serveGet(e, "/attrs")

	mu.Lock()
	defer mu.Unlock()

	assertAttributeSlice(t, captured, "http.request.method", http.MethodGet)
	assertAttributeSlice(t, captured, "http.route", "/attrs")
	assertAttributeSlice(t, captured, "tenant.tier", "pro")
}

func TestRecorder_CompletedAttributesReadHandlerContext(t *testing.T) {
	var (
		mu       sync.Mutex
		captured []attribute.KeyValue
	)

	reader, provider := newMeterProvider()
	r, err := NewRecorder(
		WithMeterProvider(provider),
		WithActiveRequests(InstrumentConfig{Disabled: true}),
		WithAttributes(func(c *echo.Context, _ error) []attribute.KeyValue {
			value, _ := c.Get("tenant.id").(string)
			return []attribute.KeyValue{attribute.String("tenant.id", value)}
		}),
		WithRecorder(func(_ *echo.Context, _ int, _ error, _ time.Duration, attrs []attribute.KeyValue) {
			mu.Lock()
			defer mu.Unlock()
			captured = make([]attribute.KeyValue, len(attrs))
			copy(captured, attrs)
		}),
	)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	e := echo.New()
	e.Use(r.Handler())
	e.GET("/handler-context", func(c *echo.Context) error {
		c.Set("tenant.id", "tenant-from-handler")
		return c.NoContent(http.StatusOK)
	})
	serveGet(e, "/handler-context")

	requestCount := sumDataPoint[int64](t, collectMetrics(t, reader), defaultRequestCountName)
	assertAttribute(t, requestCount.Attributes, "tenant.id", "tenant-from-handler")

	mu.Lock()
	defer mu.Unlock()
	assertAttributeSlice(t, captured, "tenant.id", "tenant-from-handler")
}

func TestRecorder_SkippedRequestBypassesRecorders(t *testing.T) {
	var calls atomic.Int64

	_, provider := newMeterProvider()
	r, err := NewRecorder(
		WithMeterProvider(provider),
		WithSkipper(func(*echo.Context) bool { return true }),
		WithRecorder(func(*echo.Context, int, error, time.Duration, []attribute.KeyValue) {
			calls.Add(1)
		}),
	)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	e := echo.New()
	e.Use(r.Handler())
	e.GET("/skip", func(c *echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	serveGet(e, "/skip")

	if got := calls.Load(); got != 0 {
		t.Fatalf("recorder invocations on skipped request = %d, want 0", got)
	}
}

func TestRecorder_PanicDoesNotAbortRequest(t *testing.T) {
	reader, provider := newMeterProvider()
	r, err := NewRecorder(
		WithMeterProvider(provider),
		WithRecorder(func(*echo.Context, int, error, time.Duration, []attribute.KeyValue) {
			panic("boom")
		}),
	)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	e := echo.New()
	e.Use(r.Handler())
	e.GET("/panic", func(c *echo.Context) error {
		return c.String(http.StatusOK, "still ok")
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "still ok" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "still ok")
	}

	requestCount := sumDataPoint[int64](t, collectMetrics(t, reader), defaultRequestCountName)
	if requestCount.Value != 1 {
		t.Fatalf("default request count after panic = %d, want 1", requestCount.Value)
	}
}

func TestRecorder_PanicDoesNotSkipSubsequentRecorders(t *testing.T) {
	var afterCalled atomic.Int64

	_, provider := newMeterProvider()
	r, err := NewRecorder(
		WithMeterProvider(provider),
		WithRecorder(
			func(*echo.Context, int, error, time.Duration, []attribute.KeyValue) {
				panic("boom")
			},
			func(*echo.Context, int, error, time.Duration, []attribute.KeyValue) {
				afterCalled.Add(1)
			},
		),
	)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	e := echo.New()
	e.Use(r.Handler())
	e.GET("/panic-then-counter", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	serveGet(e, "/panic-then-counter")

	if got := afterCalled.Load(); got != 1 {
		t.Fatalf("subsequent recorder invocations = %d, want 1", got)
	}
}

func TestRecorder_RecoveredPanicIsLogged(t *testing.T) {
	buf, logger := newBufferedErrorLogger()

	_, provider := newMeterProvider()
	r, err := NewRecorder(
		WithMeterProvider(provider),
		WithRecorder(func(*echo.Context, int, error, time.Duration, []attribute.KeyValue) {
			panic("boom-message")
		}),
	)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	e := echo.New()
	e.Logger = logger
	e.Use(r.Handler())
	e.GET("/log-panic", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	serveGet(e, "/log-panic")

	assertLogContains(t, buf, "boom-message")
	assertLogContains(t, buf, "recorder panic")
}

func TestRecorder_AttributeExtractorPanicDoesNotAbortRequest(t *testing.T) {
	buf, logger := newBufferedErrorLogger()

	reader, provider := newMeterProvider()
	r, err := NewRecorder(
		WithMeterProvider(provider),
		WithAttributes(func(*echo.Context, error) []attribute.KeyValue {
			panic("extractor-boom")
		}),
	)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	e := echo.New()
	e.Logger = logger
	e.Use(r.Handler())
	e.GET("/extractor-panic", func(c *echo.Context) error {
		return c.NoContent(http.StatusAccepted)
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/extractor-panic", nil))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}

	requestCount := sumDataPoint[int64](t, collectMetrics(t, reader), defaultRequestCountName)
	if requestCount.Value != 1 {
		t.Fatalf("default request count after panic = %d, want 1", requestCount.Value)
	}
	assertLogContains(t, buf, "extractor-boom")
	assertLogContains(t, buf, "attribute extractor panic")
}

func TestRecorder_CompletedAttributesAreExtractedOnce(t *testing.T) {
	var calls atomic.Int64

	_, provider := newMeterProvider()
	r, err := NewRecorder(
		WithMeterProvider(provider),
		WithActiveRequests(InstrumentConfig{Disabled: true}),
		WithResponseSize(InstrumentConfig{Disabled: true}),
		WithAttributes(func(*echo.Context, error) []attribute.KeyValue {
			calls.Add(1)
			return []attribute.KeyValue{attribute.String("service.tier", "edge")}
		}),
	)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	e := echo.New()
	e.Use(r.Handler())
	e.GET("/attrs-once", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	serveGet(e, "/attrs-once")

	if got := calls.Load(); got != 1 {
		t.Fatalf("attribute extractor calls = %d, want 1", got)
	}
}

func newBufferedErrorLogger() (*bytes.Buffer, *slog.Logger) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelError}))

	return buf, logger
}

func assertLogContains(t *testing.T, buf *bytes.Buffer, want string) {
	t.Helper()

	if !bytes.Contains(buf.Bytes(), []byte(want)) {
		t.Fatalf("expected %q in log output, got: %s", want, buf.String())
	}
}

func TestRecorder_AddRecorderAfterConstruction(t *testing.T) {
	var calls atomic.Int64

	_, provider := newMeterProvider()
	r, err := NewRecorder(WithMeterProvider(provider))
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	e := echo.New()
	e.Use(r.Handler())
	e.GET("/post", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	r.AddRecorder(func(*echo.Context, int, error, time.Duration, []attribute.KeyValue) {
		calls.Add(1)
	})
	serveGet(e, "/post")

	if got := calls.Load(); got != 1 {
		t.Fatalf("recorder invocations after AddRecorder = %d, want 1", got)
	}
}

func TestRecorder_AddRecorderIgnoresNil(t *testing.T) {
	_, provider := newMeterProvider()
	r, err := NewRecorder(WithMeterProvider(provider))
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	r.AddRecorder(nil)

	if got := len(r.snapshotRecorders()); got != 0 {
		t.Fatalf("snapshot length after nil AddRecorder = %d, want 0", got)
	}
}

func TestRecorder_ConcurrentAddRecorderAndServing(t *testing.T) {
	_, provider := newMeterProvider()
	r, err := NewRecorder(WithMeterProvider(provider))
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	e := echo.New()
	e.Use(r.Handler())
	e.GET("/concurrent", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	noop := func(*echo.Context, int, error, time.Duration, []attribute.KeyValue) {}

	const goroutines = 20

	var wg sync.WaitGroup

	wg.Add(goroutines * 2)

	for range goroutines {
		go func() {
			defer wg.Done()
			r.AddRecorder(noop)
		}()
		go func() {
			defer wg.Done()
			serveGet(e, "/concurrent")
		}()
	}

	wg.Wait()
}

func TestRecorder_MeterReturnsNonNil(t *testing.T) {
	_, provider := newMeterProvider()
	r, err := NewRecorder(WithMeterProvider(provider))
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	if r.Meter() == nil {
		t.Fatal("Meter() returned nil")
	}
}

func TestRecorder_MeterBuildsCustomCounter(t *testing.T) {
	reader, provider := newMeterProvider()
	r, err := NewRecorder(WithMeterProvider(provider))
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	counter, err := r.Meter().Int64Counter("custom.user.metric")
	if err != nil {
		t.Fatalf("Int64Counter: %v", err)
	}
	counter.Add(context.Background(), 7)

	metrics := collectMetrics(t, reader)
	if _, ok := metrics["custom.user.metric"]; !ok {
		t.Fatal("custom counter was not recorded under the configured meter")
	}
}

func TestRecorder_MeterDefaultProviderIsNonNil(t *testing.T) {
	r, err := NewRecorder()
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	if r.Meter() == nil {
		t.Fatal("Meter() returned nil for default provider")
	}
}

func TestExistingConstructorsKeepBehavior(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, provider metric.MeterProvider) echo.MiddlewareFunc
	}{
		{
			name: "Middleware",
			setup: func(_ *testing.T, provider metric.MeterProvider) echo.MiddlewareFunc {
				return Middleware(WithMeterProvider(provider))
			},
		},
		{
			name: "New",
			setup: func(t *testing.T, provider metric.MeterProvider) echo.MiddlewareFunc {
				mw, err := New(WithMeterProvider(provider))
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				return mw
			},
		},
		{
			name: "NewWithConfig",
			setup: func(t *testing.T, provider metric.MeterProvider) echo.MiddlewareFunc {
				mw, err := NewWithConfig(Config{MeterProvider: provider})
				if err != nil {
					t.Fatalf("NewWithConfig: %v", err)
				}
				return mw
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reader, provider := newMeterProvider()
			e := echo.New()
			e.Use(tc.setup(t, provider))
			e.GET("/baseline", func(c *echo.Context) error {
				return c.String(http.StatusOK, "ok")
			})
			serveGet(e, "/baseline")

			requestCount := sumDataPoint[int64](t, collectMetrics(t, reader), defaultRequestCountName)
			if requestCount.Value != 1 {
				t.Fatalf("%s: default request count = %d, want 1", tc.name, requestCount.Value)
			}
		})
	}
}

func TestRecorder_HandlerErrorPropagatesToRecorder(t *testing.T) {
	var (
		mu       sync.Mutex
		captured error
	)
	want := errors.New("downstream failure")

	_, provider := newMeterProvider()
	r, err := NewRecorder(
		WithMeterProvider(provider),
		WithRecorder(func(_ *echo.Context, _ int, e error, _ time.Duration, _ []attribute.KeyValue) {
			mu.Lock()
			defer mu.Unlock()
			captured = e
		}),
	)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	e := echo.New()
	e.Use(r.Handler())
	e.GET("/fail", func(*echo.Context) error {
		return want
	})
	serveGet(e, "/fail")

	mu.Lock()
	defer mu.Unlock()
	if !errors.Is(captured, want) {
		t.Fatalf("recorder err = %v, want %v", captured, want)
	}
}

func TestRecorder_NewRecorderWithConfigAcceptsRecorders(t *testing.T) {
	var calls atomic.Int64

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	r, err := NewRecorderWithConfig(Config{
		MeterProvider: provider,
		Recorders: []MetricRecorder{
			func(*echo.Context, int, error, time.Duration, []attribute.KeyValue) {
				calls.Add(1)
			},
		},
	})
	if err != nil {
		t.Fatalf("NewRecorderWithConfig: %v", err)
	}

	e := echo.New()
	e.Use(r.Handler())
	e.GET("/via-config", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	serveGet(e, "/via-config")

	if got := calls.Load(); got != 1 {
		t.Fatalf("recorder invocations = %d, want 1", got)
	}
}

// assertAttributeSlice asserts that a key/value pair is present in a
// []attribute.KeyValue slice (as opposed to an attribute.Set).
func assertAttributeSlice(t *testing.T, attrs []attribute.KeyValue, key string, want any) {
	t.Helper()

	for _, kv := range attrs {
		if string(kv.Key) != key {
			continue
		}

		switch typed := want.(type) {
		case string:
			if got := kv.Value.AsString(); got != typed {
				t.Fatalf("attribute %q = %q, want %q", key, got, typed)
			}
		case int64:
			if got := kv.Value.AsInt64(); got != typed {
				t.Fatalf("attribute %q = %d, want %d", key, got, typed)
			}
		case bool:
			if got := kv.Value.AsBool(); got != typed {
				t.Fatalf("attribute %q = %t, want %t", key, got, typed)
			}
		default:
			t.Fatalf("unsupported assertion type %T", want)
		}

		return
	}

	t.Fatalf("attribute %q not present in slice (keys: %v)", key, keysOf(attrs))
}

func keysOf(attrs []attribute.KeyValue) []string {
	keys := make([]string, len(attrs))
	for i, kv := range attrs {
		keys[i] = string(kv.Key)
	}
	return keys
}
