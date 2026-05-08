// Package echotelmetrics provides Echo middleware that records HTTP server
// metrics with OpenTelemetry.
//
// The package creates metric instruments only. Applications remain responsible
// for configuring the OpenTelemetry SDK, readers, and exporters. Default
// attributes use Echo route patterns instead of raw request paths to keep metric
// cardinality bounded.
package echotelmetrics
