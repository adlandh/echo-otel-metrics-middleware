package echotelmetrics

import (
	"github.com/labstack/echo/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Mirrored in README.md — update the instrument table there when changing these values.
const (
	defaultMeterName    = "github.com/adlandh/echo-otel-metrics-middleware"
	defaultMeterVersion = "0.1.0"

	defaultRequestCountName    = "http.server.request.count"
	defaultRequestDurationName = "http.server.request.duration"
	defaultRequestSizeName     = "http.server.request.body.size"
	defaultResponseSizeName    = "http.server.response.body.size"
	defaultActiveRequestsName  = "http.server.active_requests"
)

// Skipper decides whether a request should bypass metric recording.
type Skipper func(*echo.Context) bool

// AttributeExtractor returns additional bounded-cardinality attributes for a request.
// Do not return raw paths, query strings, user IDs, IP addresses, or request-specific IDs.
type AttributeExtractor func(*echo.Context, error) []attribute.KeyValue

// InstrumentConfig configures one metric instrument.
type InstrumentConfig struct {
	Name        string
	Description string
	Unit        string
	Disabled    bool
}

// Config configures the OpenTelemetry metrics middleware.
type Config struct {
	MeterProvider metric.MeterProvider
	MeterName     string
	MeterVersion  string
	Skipper       Skipper
	Attributes    AttributeExtractor

	RequestCount    InstrumentConfig
	RequestDuration InstrumentConfig
	RequestSize     InstrumentConfig
	ResponseSize    InstrumentConfig
	ActiveRequests  InstrumentConfig

	// Recorders are MetricRecorders registered at initialization. Use
	// WithRecorder to populate this slice through the functional-options API,
	// or assign it directly when building a Config value. Recorders may also
	// be added after construction through (*Recorder).AddRecorder.
	Recorders []MetricRecorder
}

// Option mutates middleware configuration.
type Option func(*Config)

// DefaultConfig returns the middleware defaults.
func DefaultConfig() Config {
	return Config{
		MeterProvider: otel.GetMeterProvider(),
		MeterName:     defaultMeterName,
		MeterVersion:  defaultMeterVersion,
		Skipper:       nil,
		Attributes:    nil,
		RequestCount: InstrumentConfig{
			Name:        defaultRequestCountName,
			Description: "Total number of HTTP server requests.",
			Unit:        "{request}",
		},
		RequestDuration: InstrumentConfig{
			Name:        defaultRequestDurationName,
			Description: "Duration of HTTP server requests.",
			Unit:        "s",
		},
		RequestSize: InstrumentConfig{
			Name:        defaultRequestSizeName,
			Description: "Size of HTTP request bodies.",
			Unit:        "By",
		},
		ResponseSize: InstrumentConfig{
			Name:        defaultResponseSizeName,
			Description: "Size of HTTP response bodies.",
			Unit:        "By",
		},
		ActiveRequests: InstrumentConfig{
			Name:        defaultActiveRequestsName,
			Description: "Number of active HTTP server requests.",
			Unit:        "{request}",
		},
	}
}

// WithMeterProvider sets the OpenTelemetry meter provider used to create instruments.
func WithMeterProvider(provider metric.MeterProvider) Option {
	return func(config *Config) {
		config.MeterProvider = provider
	}
}

// WithMeterName sets the instrumentation scope name.
func WithMeterName(name string) Option {
	return func(config *Config) {
		config.MeterName = name
	}
}

// WithMeterVersion sets the instrumentation scope version.
func WithMeterVersion(version string) Option {
	return func(config *Config) {
		config.MeterVersion = version
	}
}

// WithSkipper sets the function used to skip request instrumentation.
func WithSkipper(skipper Skipper) Option {
	return func(config *Config) {
		config.Skipper = skipper
	}
}

// WithAttributes sets a function that adds custom bounded-cardinality attributes.
func WithAttributes(extractor AttributeExtractor) Option {
	return func(config *Config) {
		config.Attributes = extractor
	}
}

// WithRequestCount configures the completed request counter.
func WithRequestCount(instrument InstrumentConfig) Option {
	return func(config *Config) {
		config.RequestCount = mergeInstrumentConfig(config.RequestCount, instrument)
	}
}

// WithRequestDuration configures the request duration histogram.
func WithRequestDuration(instrument InstrumentConfig) Option {
	return func(config *Config) {
		config.RequestDuration = mergeInstrumentConfig(config.RequestDuration, instrument)
	}
}

// WithRequestSize configures the request body size histogram.
func WithRequestSize(instrument InstrumentConfig) Option {
	return func(config *Config) {
		config.RequestSize = mergeInstrumentConfig(config.RequestSize, instrument)
	}
}

// WithResponseSize configures the response body size histogram.
func WithResponseSize(instrument InstrumentConfig) Option {
	return func(config *Config) {
		config.ResponseSize = mergeInstrumentConfig(config.ResponseSize, instrument)
	}
}

// WithActiveRequests configures the active request up-down counter.
func WithActiveRequests(instrument InstrumentConfig) Option {
	return func(config *Config) {
		config.ActiveRequests = mergeInstrumentConfig(config.ActiveRequests, instrument)
	}
}

func mergeInstrumentConfig(current InstrumentConfig, next InstrumentConfig) InstrumentConfig {
	if next.Name != "" {
		current.Name = next.Name
	}

	if next.Description != "" {
		current.Description = next.Description
	}

	if next.Unit != "" {
		current.Unit = next.Unit
	}

	current.Disabled = next.Disabled

	return current
}
