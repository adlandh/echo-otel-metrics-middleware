# echo-otel-metrics-middleware

Echo middleware for recording HTTP server metrics with OpenTelemetry.

The middleware records request count, request duration, request body size, response body size, and active request metrics. It does not expose a Prometheus endpoint or configure exporters. Configure the OpenTelemetry SDK in your application, then install the middleware on Echo.

```go
e := echo.New()
e.Use(echotelmetrics.Middleware())
```

Default attributes use bounded values such as HTTP method, Echo route pattern, status code, normalized scheme, and error state. Custom attributes must also be bounded. Avoid raw paths, query strings, host headers, user IDs, client IPs, and request-specific IDs.
