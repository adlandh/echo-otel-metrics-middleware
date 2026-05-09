package echotelmetrics

import (
	"context"
	"sync"
	"time"

	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricRecorder is invoked once per non-skipped request after the downstream
// handler has returned and the final response status is known. The middleware
// passes the Echo context, the final HTTP status code, the handler error (or
// nil), the measured request duration, and the bounded attribute slice that
// was used to record the default instruments for the same request.
//
// The provided attrs slice is shared with the default instruments. Recorders
// MUST NOT mutate it. To attach additional attributes for an instrument call,
// build a new slice on top of attrs (for example with append on a fresh
// slice) and pass that to metric.WithAttributes.
//
// Recorders run synchronously in the request goroutine. A panic raised inside
// a recorder is recovered by the middleware, logged through the Echo logger,
// and does not abort the request, skip subsequent recorders, or prevent
// default instruments from recording.
type MetricRecorder func(c *echo.Context, status int, err error, duration time.Duration, attrs []attribute.KeyValue)

// Recorder owns the middleware lifecycle and exposes post-construction
// extension points: registration of additional MetricRecorders and direct
// access to the configured OpenTelemetry meter.
//
// Use NewRecorder or NewRecorderWithConfig when you need post-construction
// registration or to build your own instruments under the same instrumentation
// scope. For the simpler install path, use Middleware, New, or NewWithConfig.
//
// A Recorder is safe for concurrent use. Its Handler may be installed on
// multiple Echo instances; all of them share the same default instruments and
// registered recorders.
type Recorder struct {
	meter       metric.Meter
	recorders   []MetricRecorder
	instruments instruments
	config      Config
	mu          sync.RWMutex
}

// NewRecorder creates a Recorder from functional options. It returns an error
// if OpenTelemetry instruments cannot be initialized.
func NewRecorder(options ...Option) (*Recorder, error) {
	config := DefaultConfig()

	for _, option := range options {
		if option != nil {
			option(&config)
		}
	}

	return NewRecorderWithConfig(config)
}

// NewRecorderWithConfig creates a Recorder from an explicit Config.
func NewRecorderWithConfig(config Config) (*Recorder, error) {
	config = applyConfigDefaults(config)

	meter := config.MeterProvider.Meter(
		config.MeterName,
		metric.WithInstrumentationVersion(config.MeterVersion),
	)

	createdInstruments, err := newInstrumentsForMeter(meter, config)
	if err != nil {
		return nil, err
	}

	r := &Recorder{
		config:      config,
		instruments: createdInstruments,
		meter:       meter,
	}

	if len(config.Recorders) > 0 {
		r.recorders = append(r.recorders, config.Recorders...)
	}

	return r, nil
}

// Handler returns the Echo middleware function that records the default HTTP
// server metrics and fans out to every registered MetricRecorder. The same
// MiddlewareFunc may be registered on multiple Echo instances and reused
// concurrently.
func (r *Recorder) Handler() echo.MiddlewareFunc {
	return r.wrap
}

// AddRecorder registers an additional MetricRecorder after construction. It is
// safe to call concurrently with request serving. The new recorder takes
// effect on subsequent non-skipped requests; in-flight requests may or may not
// observe it but never see a partial state.
//
// A nil recorder is silently ignored.
func (r *Recorder) AddRecorder(recorder MetricRecorder) {
	if recorder == nil {
		return
	}

	r.mu.Lock()
	combined := make([]MetricRecorder, 0, len(r.recorders)+1)
	combined = append(combined, r.recorders...)
	combined = append(combined, recorder)
	r.recorders = combined
	r.mu.Unlock()
}

// Meter returns the OpenTelemetry meter the middleware uses to construct its
// default instruments. The meter is bound to the configured MeterProvider,
// MeterName, and MeterVersion, so user-built instruments share the
// instrumentation scope with the defaults.
func (r *Recorder) Meter() metric.Meter {
	return r.meter
}

// WithRecorder registers one or more MetricRecorders at initialization. The
// recorders are appended to the configuration in the order they appear, and
// across multiple WithRecorder calls.
//
// Nil recorders are silently ignored.
func WithRecorder(recorders ...MetricRecorder) Option {
	return func(config *Config) {
		for _, recorder := range recorders {
			if recorder != nil {
				config.Recorders = append(config.Recorders, recorder)
			}
		}
	}
}

func (r *Recorder) snapshotRecorders() []MetricRecorder {
	r.mu.RLock()
	snapshot := r.recorders
	r.mu.RUnlock()

	return snapshot
}

func (r *Recorder) invokeRecorders(
	c *echo.Context,
	status int,
	err error,
	duration time.Duration,
	attrs []attribute.KeyValue,
) {
	snapshot := r.snapshotRecorders()
	for index, recorder := range snapshot {
		invokeRecorder(c, recorder, index, status, err, duration, attrs)
	}
}

func invokeRecorder(
	c *echo.Context,
	recorder MetricRecorder,
	index int,
	status int,
	err error,
	duration time.Duration,
	attrs []attribute.KeyValue,
) {
	defer func() {
		if recovered := recover(); recovered != nil {
			c.Logger().Error(
				"echo-otel-metrics: recorder panic",
				"recorder_index", index,
				"panic", recovered,
				"route", route(c),
				"method", c.Request().Method,
			)
		}
	}()

	recorder(c, status, err, duration, attrs)
}

func (r *Recorder) wrap(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		if r.config.Skipper != nil && r.config.Skipper(c) {
			return next(c)
		}

		ctx := c.Request().Context()
		start := time.Now()
		response := echo.NewResponse(c.Response(), c.Logger())
		c.SetResponse(response)

		activeCustomAttrs := r.customAttributes(c, nil)

		var (
			handlerErr          error
			completedAttributes []attribute.KeyValue
		)

		r.registerResponseSize(
			ctx,
			c,
			response,
			&handlerErr,
			&completedAttributes,
			activeCustomAttrs,
		)

		if r.instruments.activeRequests != nil {
			activeAttrs := activeAttributesWithCustom(c, activeCustomAttrs)

			r.instruments.activeRequests.Add(ctx, 1, metric.WithAttributes(activeAttrs...))
			defer r.instruments.activeRequests.Add(ctx, -1, metric.WithAttributes(activeAttrs...))
		}

		handlerErr = next(c)
		status := responseStatus(response, handlerErr)
		duration := time.Since(start)

		completedCustomAttrs := r.completedCustomAttributes(c, handlerErr, activeCustomAttrs)

		attributes := requestAttributesWithCustom(c, status, handlerErr, completedCustomAttrs)
		completedAttributes = attributes

		options := metric.WithAttributes(attributes...)

		if r.instruments.requestCount != nil {
			r.instruments.requestCount.Add(ctx, 1, options)
		}

		if r.instruments.requestDuration != nil {
			r.instruments.requestDuration.Record(ctx, duration.Seconds(), options)
		}

		if r.instruments.requestSize != nil {
			r.instruments.requestSize.Record(ctx, requestSize(c), options)
		}

		r.invokeRecorders(c, status, handlerErr, duration, attributes)

		return handlerErr
	}
}

func (r *Recorder) registerResponseSize(
	ctx context.Context,
	c *echo.Context,
	response *echo.Response,
	handlerErr *error,
	completedAttributes *[]attribute.KeyValue,
	activeCustomAttrs []attribute.KeyValue,
) {
	if r.instruments.responseSize == nil {
		return
	}

	response.After(func() {
		status := responseStatus(response, *handlerErr)
		attributes := *completedAttributes

		if attributes == nil {
			attributes = requestAttributesWithCustom(c, status, *handlerErr, activeCustomAttrs)
		}

		options := metric.WithAttributes(attributes...)

		r.instruments.responseSize.Record(ctx, responseSize(response), options)
	})
}

func (r *Recorder) completedCustomAttributes(
	c *echo.Context,
	handlerErr error,
	fallback []attribute.KeyValue,
) []attribute.KeyValue {
	if handlerErr == nil {
		return fallback
	}

	return r.customAttributes(c, handlerErr)
}

func (r *Recorder) customAttributes(c *echo.Context, err error) (attributes []attribute.KeyValue) {
	if r.config.Attributes == nil {
		return nil
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			c.Logger().Error(
				"echo-otel-metrics: attribute extractor panic",
				"panic", recovered,
				"route", route(c),
				"method", c.Request().Method,
			)

			attributes = nil
		}
	}()

	return r.config.Attributes(c, err)
}

func requestAttributesWithCustom(
	c *echo.Context,
	status int,
	err error,
	custom []attribute.KeyValue,
) []attribute.KeyValue {
	attributes := requestAttributes(c, status, err)
	attributes = append(attributes, custom...)

	return attributes
}

func activeAttributesWithCustom(c *echo.Context, custom []attribute.KeyValue) []attribute.KeyValue {
	attributes := activeAttributes(c)
	attributes = append(attributes, custom...)

	return attributes
}
