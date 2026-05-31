## Purpose

Define the expected behavior, configuration surface, and documentation requirements for the Echo v5 OpenTelemetry metrics middleware.

## Requirements

### Requirement: Middleware records default HTTP metrics
The middleware SHALL record OpenTelemetry HTTP server metrics for each non-skipped Echo v5 request using default instruments for request count, request duration, request body size, response body size, and active requests. Completed request instruments, including response body size, SHALL record at most one measurement per request.

#### Scenario: Successful request records metrics
- **WHEN** a configured Echo v5 route handles a request successfully
- **THEN** the middleware records one completed request, one duration measurement, one request size measurement when known, one response size measurement when known, and active request state for the request lifetime

#### Scenario: Multi-write response records one response size measurement
- **WHEN** a handler writes a response body in multiple chunks during one request
- **THEN** the middleware records one response body size measurement whose value is the final number of response body bytes sent for that request

#### Scenario: Handler error records metrics
- **WHEN** a downstream Echo handler returns an error
- **THEN** the middleware still records completed request metrics using the final response status available to Echo

#### Scenario: Error handler response size is included
- **WHEN** Echo's central error handler writes an error response body after the downstream handler returns an error
- **THEN** the middleware records one response body size measurement that includes the error-handler-written bytes

### Requirement: Middleware uses OpenTelemetry metrics APIs
The middleware SHALL emit metrics through OpenTelemetry `metric` instruments and SHALL NOT require Prometheus client collectors or an embedded scrape endpoint.

#### Scenario: Custom meter provider is used
- **WHEN** configuration provides a custom OpenTelemetry meter provider
- **THEN** the middleware creates instruments from that provider instead of relying on a concrete exporter or Prometheus registry

#### Scenario: No exporter is configured by middleware
- **WHEN** an application installs the middleware without configuring an OpenTelemetry SDK exporter
- **THEN** the middleware does not start a metrics endpoint or exporter on its own

### Requirement: Middleware provides bounded default attributes
The middleware SHALL attach bounded default attributes that identify HTTP method, route pattern, status code, normalized scheme, and error state without using raw URL paths, query strings, host headers, user IDs, client IPs, or other unbounded values by default.

#### Scenario: Route pattern is recorded instead of raw path
- **WHEN** a request matches an Echo v5 route with path parameters
- **THEN** the route attribute uses the Echo v5 route pattern rather than the concrete request path

#### Scenario: Unmatched route avoids raw path
- **WHEN** a request does not resolve to an Echo route pattern
- **THEN** the middleware uses a bounded fallback route value and does not record the raw request path

### Requirement: Middleware is configurable
The middleware SHALL allow applications to configure meter provider, meter name, meter version, metric names, enabled instruments, skipped requests, and custom attribute extraction. Custom attribute extraction MUST be isolated from request handling failures: if an extractor panics, the middleware SHALL recover, log the recovered value through the configured Echo logger, omit custom attributes for that extraction, and continue request handling and default metric recording.

#### Scenario: Request is skipped
- **WHEN** a configured skipper matches the incoming Echo v5 context
- **THEN** the middleware calls the next handler without recording any metrics for that request

#### Scenario: Instrument is disabled
- **WHEN** configuration disables a specific default instrument
- **THEN** the middleware does not create or record that instrument while keeping other enabled instruments functional

#### Scenario: Disabled instrument remains disabled across partial options
- **WHEN** configuration disables a specific default instrument and a later option changes that instrument's name, description, or unit without explicitly enabling it
- **THEN** the middleware keeps that instrument disabled and does not create or record it

#### Scenario: Custom attributes are added
- **WHEN** configuration provides an attribute extraction function
- **THEN** the middleware adds the returned attributes to recorded measurements along with the default attributes

#### Scenario: Custom attribute extractor panic is isolated
- **WHEN** a configured attribute extraction function panics during a request
- **THEN** the middleware recovers the panic, logs the recovered value through the Echo logger, records default metrics without custom attributes from that failed extraction, and preserves the handler response

### Requirement: Middleware exposes an Echo-compatible handler
The middleware SHALL expose an API that can be installed with Echo v5's middleware registration flow and wraps `echo.HandlerFunc` from `github.com/labstack/echo/v5` without changing handler behavior.

#### Scenario: Handler response is preserved
- **WHEN** an application registers the middleware around an Echo v5 handler
- **THEN** requests and responses behave the same as they would without the middleware, aside from metrics side effects

#### Scenario: Middleware initializes with defaults
- **WHEN** an application creates middleware without custom configuration
- **THEN** the middleware initializes successfully with default metric names, instruments, and attribute behavior

### Requirement: Middleware behavior is tested and documented
The project SHALL include tests, godoc examples, and a `README.md` that together demonstrate default metrics, custom configuration, skipped requests, route pattern attributes, error handling, bounded custom attribute extraction, extension panic isolation, end-to-end OpenTelemetry SDK wiring with a concrete exporter, and use of the error-returning constructors with explicit error handling.

#### Scenario: Tests validate recorded data
- **WHEN** tests run with an in-memory OpenTelemetry metric reader
- **THEN** they verify the expected instruments and attributes are recorded for representative Echo v5 requests

#### Scenario: Tests validate defensive extension behavior
- **WHEN** tests run for custom attributes and instrument configuration
- **THEN** they verify extractor panic isolation, completed custom attributes are extracted once per request, and disabled instruments remain disabled across later partial options

#### Scenario: Example shows SDK wiring
- **WHEN** users read the godoc examples in `example_test.go`
- **THEN** they can see how to configure an OpenTelemetry meter provider separately from installing the Echo middleware

#### Scenario: README documents recorded instruments and attributes
- **WHEN** users read `README.md`
- **THEN** they find a table listing every default instrument with its name, kind, unit, and description, and a table listing every default attribute with its key and bounded value source

#### Scenario: README shows end-to-end exporter wiring
- **WHEN** users read `README.md`
- **THEN** they find at least one complete, copy-pasteable example that wires the middleware to an OpenTelemetry SDK meter provider with a concrete exporter so that recorded metrics are observable

#### Scenario: README documents skipper and bounded custom-attributes recipes
- **WHEN** users read `README.md`
- **THEN** they find a recipe showing how to install a `Skipper` to bypass instrumentation for selected routes, and a recipe showing how to attach custom attributes by mapping request-derived values through a bounded allowlist before recording them

#### Scenario: README warns against unbounded attributes
- **WHEN** users read `README.md`
- **THEN** they find an explicit warning, with concrete bounded-versus-unbounded examples, against using raw paths, query strings, host headers, client IP addresses, user IDs, or request-specific IDs as metric attributes

#### Scenario: Godoc examples back the README recipes
- **WHEN** users browse the package on `pkg.go.dev`
- **THEN** they find executable godoc examples for the default installation, custom meter provider wiring, skipper configuration, and bounded custom attribute extraction

#### Scenario: README demonstrates the error-returning constructor
- **WHEN** users read `README.md` near the constructor-variants reference
- **THEN** they find a copy-pasteable snippet that calls `New(...)` and handles the returned error explicitly with `if err != nil`, and prose noting when `NewWithConfig`/`NewRecorderWithConfig` with a programmatically built `Config` is preferable to the variadic-options form

#### Scenario: Godoc example backs the error-returning constructor recipe
- **WHEN** users browse the package on `pkg.go.dev`
- **THEN** they find an executable `ExampleNew` godoc example that constructs the middleware with `New(...)`, handles the returned error explicitly, and installs the resulting middleware on an Echo instance

### Requirement: Middleware records custom per-request metrics
The middleware SHALL allow applications to register zero or more custom metric recorders at initialization through the existing functional-options API. Each registered recorder MUST be invoked exactly once per non-skipped request, after the downstream handler has returned and the final response status is known, with the Echo context, the final status code, the handler error (or nil), the measured request duration, and the same completed-request bounded attribute slice the default completed-request instruments receive for that request.

#### Scenario: Single recorder is invoked at request completion
- **WHEN** an application installs the middleware with a single custom recorder registered through the initialization option
- **THEN** the middleware invokes that recorder exactly once for the request, after the handler returns, with the final status, handler error, request duration, and the same attribute slice used by the default request count and duration instruments

#### Scenario: Multiple recorders are invoked in registration order
- **WHEN** an application registers more than one custom recorder at initialization
- **THEN** the middleware invokes each recorder exactly once per request in the order they were registered

#### Scenario: Recorders share the default and custom attribute set
- **WHEN** the middleware is configured with a custom attribute extractor and one or more custom recorders
- **THEN** each recorder receives an attribute slice containing both the default attributes (HTTP method, route pattern, status code, normalized scheme, error state) and the additional attributes returned by the completed-request extractor call

#### Scenario: Completed custom attributes are extracted after handler completion
- **WHEN** a handler stores request-scoped context data before returning successfully and custom attributes are configured to read that data
- **THEN** the completed default instruments and custom recorders use custom attributes extracted after the handler has returned

#### Scenario: Completed custom attributes are extracted once
- **WHEN** a request completes successfully and custom attributes are configured
- **THEN** the middleware calls the extractor once for the completed-request metric path and reuses that custom attribute set for completed default instruments and custom recorders

#### Scenario: Skipped request bypasses custom recorders
- **WHEN** the configured skipper returns true for a request
- **THEN** the middleware does not invoke any custom recorder for that request, in addition to not recording default instruments

### Requirement: Middleware allows registering custom recorders after construction
The middleware SHALL provide a public, concurrency-safe API to register additional custom metric recorders after the middleware has been constructed and while it is serving requests. Recorders registered after construction MUST be invoked for all subsequent non-skipped requests under the same contract as recorders registered at initialization.

#### Scenario: Recorder added after construction is invoked on subsequent requests
- **WHEN** an application registers a custom recorder after middleware construction
- **THEN** every subsequent non-skipped request invokes that recorder under the same contract as recorders registered at initialization

#### Scenario: Concurrent registration and serving is safe
- **WHEN** an application registers a custom recorder while requests are being served concurrently
- **THEN** registration completes without data races and in-flight requests either invoke the new recorder or do not, but never observe a partial or corrupted recorder list

### Requirement: Middleware isolates custom recorder failures
The middleware SHALL recover from any panic raised by a custom recorder so that the panic neither aborts the request, prevents subsequent recorders from running, nor blocks the default instruments from recording. Recovered panics MUST be logged through the configured Echo logger so that operators can observe recorder failures.

#### Scenario: Panic in one recorder does not abort the request
- **WHEN** a custom recorder panics during a request
- **THEN** the request continues to a normal response, default instruments still record, and the response observed by the client is unchanged from the no-panic case

#### Scenario: Panic in one recorder does not skip subsequent recorders
- **WHEN** a custom recorder panics during a request and additional recorders are registered after it
- **THEN** the middleware invokes every subsequent recorder for that same request

#### Scenario: Recovered panics are logged
- **WHEN** a custom recorder panics during a request
- **THEN** the middleware logs the recovered value through the Echo logger at error level so that the failure is observable

### Requirement: Middleware exposes the configured meter
The middleware SHALL expose the configured OpenTelemetry `metric.Meter` so that applications can construct their own instruments under the same instrumentation scope (meter provider, meter name, meter version) without re-deriving them. The exposed meter MUST be the same meter used internally to create the default instruments.

#### Scenario: Application creates a custom instrument using the exposed meter
- **WHEN** an application retrieves the meter through the public accessor and creates a custom counter
- **THEN** the resulting counter is bound to the same meter provider, meter name, and meter version that the middleware uses for its default instruments

#### Scenario: Exposed meter is non-nil for default and custom configurations
- **WHEN** an application retrieves the meter from a middleware constructed with default configuration or with a custom meter provider
- **THEN** the returned meter is non-nil in both cases

### Requirement: Custom metrics extension preserves backward compatibility
The middleware SHALL keep the existing `Middleware()`, `New()`, and `NewWithConfig()` constructor signatures and behaviors unchanged. The custom-metrics extension MUST be additive only and MUST be reachable through a separate constructor that returns a richer value exposing post-construction registration and meter access.

#### Scenario: Existing constructors keep their signatures and behavior
- **WHEN** an application uses `Middleware()`, `New()`, or `NewWithConfig()` exactly as before this change
- **THEN** the constructors compile, return the same types, and behave identically to before, with no requirement to opt into the custom-metrics API

#### Scenario: Custom-metrics-aware constructor coexists with existing ones
- **WHEN** an application uses the custom-metrics-aware constructor to obtain post-construction registration and meter access
- **THEN** the resulting middleware behaves identically to one built with `New()` for default and recorder-free requests, and additionally supports recorder registration and meter retrieval

### Requirement: Custom-metrics behavior is documented and exemplified
The project SHALL include godoc examples and `README.md` content that demonstrate registering a custom recorder at initialization, registering a custom recorder after construction, retrieving the exposed meter to build a user-owned instrument, and the panic-isolation guarantee.

#### Scenario: Godoc example for initialization-time registration exists
- **WHEN** users browse the package on `pkg.go.dev`
- **THEN** they find an executable godoc example that registers a custom recorder at initialization and uses it to record a value through a user-defined instrument

#### Scenario: Godoc example for post-construction registration exists
- **WHEN** users browse the package on `pkg.go.dev`
- **THEN** they find an executable godoc example that constructs the middleware first and registers a custom recorder afterward

#### Scenario: README documents custom-metrics recipe and panic guarantee
- **WHEN** users read `README.md`
- **THEN** they find a recipe section that shows both registration paths, an explanation of the shared attribute set, a note that recorder panics are recovered and logged, and the meter accessor for building user-owned instruments
