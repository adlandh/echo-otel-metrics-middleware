## Purpose

Define the expected behavior, configuration surface, and documentation requirements for the Echo v5 OpenTelemetry metrics middleware.

## Requirements

### Requirement: Middleware records default HTTP metrics
The middleware SHALL record OpenTelemetry HTTP server metrics for each non-skipped Echo v5 request using default instruments for request count, request duration, request body size, response body size, and active requests.

#### Scenario: Successful request records metrics
- **WHEN** a configured Echo v5 route handles a request successfully
- **THEN** the middleware records one completed request, one duration measurement, request and response size measurements when known, and active request state for the request lifetime

#### Scenario: Handler error records metrics
- **WHEN** a downstream Echo handler returns an error
- **THEN** the middleware still records completed request metrics using the final response status available to Echo

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
The middleware SHALL allow applications to configure meter provider, meter name, meter version, metric names, enabled instruments, skipped requests, and custom attribute extraction.

#### Scenario: Request is skipped
- **WHEN** a configured skipper matches the incoming Echo v5 context
- **THEN** the middleware calls the next handler without recording any metrics for that request

#### Scenario: Instrument is disabled
- **WHEN** configuration disables a specific default instrument
- **THEN** the middleware does not create or record that instrument while keeping other enabled instruments functional

#### Scenario: Custom attributes are added
- **WHEN** configuration provides an attribute extraction function
- **THEN** the middleware adds the returned attributes to recorded measurements along with the default attributes

### Requirement: Middleware exposes an Echo-compatible handler
The middleware SHALL expose an API that can be installed with Echo v5's middleware registration flow and wraps `echo.HandlerFunc` from `github.com/labstack/echo/v5` without changing handler behavior.

#### Scenario: Handler response is preserved
- **WHEN** an application registers the middleware around an Echo v5 handler
- **THEN** requests and responses behave the same as they would without the middleware, aside from metrics side effects

#### Scenario: Middleware initializes with defaults
- **WHEN** an application creates middleware without custom configuration
- **THEN** the middleware initializes successfully with default metric names, instruments, and attribute behavior

### Requirement: Middleware behavior is tested and documented
The project SHALL include tests, godoc examples, and a `README.md` that together demonstrate default metrics, custom configuration, skipped requests, route pattern attributes, error handling, and end-to-end OpenTelemetry SDK wiring with a concrete exporter.

#### Scenario: Tests validate recorded data
- **WHEN** tests run with an in-memory OpenTelemetry metric reader
- **THEN** they verify the expected instruments and attributes are recorded for representative Echo v5 requests

#### Scenario: Example shows SDK wiring
- **WHEN** users read the godoc examples in `example_test.go`
- **THEN** they can see how to configure an OpenTelemetry meter provider separately from installing the Echo middleware

#### Scenario: README documents recorded instruments and attributes
- **WHEN** users read `README.md`
- **THEN** they find a table listing every default instrument with its name, kind, unit, and description, and a table listing every default attribute with its key and bounded value source

#### Scenario: README shows end-to-end exporter wiring
- **WHEN** users read `README.md`
- **THEN** they find at least one complete, copy-pasteable example that wires the middleware to an OpenTelemetry SDK meter provider with a concrete exporter so that recorded metrics are observable

#### Scenario: README documents skipper and custom-attributes recipes
- **WHEN** users read `README.md`
- **THEN** they find a recipe showing how to install a `Skipper` to bypass instrumentation for selected routes, and a recipe showing how to attach bounded custom attributes through `WithAttributes`

#### Scenario: README warns against unbounded attributes
- **WHEN** users read `README.md`
- **THEN** they find an explicit warning, with concrete bounded-versus-unbounded examples, against using raw paths, query strings, host headers, client IP addresses, user IDs, or request-specific IDs as metric attributes

#### Scenario: Godoc examples back the README recipes
- **WHEN** users browse the package on `pkg.go.dev`
- **THEN** they find executable godoc examples for the default installation, custom meter provider wiring, skipper configuration, and custom attribute extraction
