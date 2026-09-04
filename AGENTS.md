# AGENTS.md

## Repository Shape

- This is a Go 1.26 library module: `github.com/adlandh/echo-otel-metrics-middleware`.
- The public API is the root package `echotelmetrics` (not the module's hyphenated basename); there are no subpackages or command entrypoints.
- Target Echo is v5 only: import `github.com/labstack/echo/v5`, and handlers use `func(c *echo.Context) error`.
- `openspec/specs/` contains the tracked behavioral spec; `openspec/changes/` is ignored and may contain local-only change artifacts.

## Verification Commands

- Tests: `go test ./...`
- One test: `go test . -run '^TestName$'`
- One benchmark: `go test . -run '^$' -bench '^BenchmarkName$' -benchmem`
- CI-equivalent test command: `go test -race -coverprofile=coverage.txt -covermode=atomic ./...`
- Vet: `go vet ./...`
- Lint: `golangci-lint run`
- CI lint replaces the ignored local `.golangci.yml` with `https://raw.githubusercontent.com/adlandh/golangci-lint-config/refs/heads/main/.golangci.yml`; use that file when reproducing CI lint results.

## Middleware Constraints

- The middleware creates OpenTelemetry metric instruments only; do not configure SDK readers, exporters, scrape endpoints, or Prometheus collectors in library code.
- Default metric attributes must stay bounded: method, Echo route pattern, status code, normalized scheme, and error state. Do not add raw path, query string, host header, client IP, user ID, or request-specific IDs by default.
- The `error` attribute is true for either a non-nil handler error or a final status of 500 or higher.
- For an uncommitted handler error, Echo writes the error body after middleware returns; response size must remain an exactly-once `echo.Response.After` measurement so that body is included.
- `X-Forwarded-Proto` is normalized to `http` or `https`; do not record arbitrary forwarded scheme values.
- With active-request metrics enabled, a custom attribute extractor runs before the handler and again after it for completed metrics; do not assume one call per request. Extractor panics are recovered and logged.
- Custom recorders run synchronously after the handler, in registration order, and receive the completed metrics' shared attribute slice; they must not mutate it. `AddRecorder` is safe during request serving, and recorder panics are isolated and logged.
- `Middleware` panics on instrument initialization failure; `New`, `NewWithConfig`, `NewRecorder`, and `NewRecorderWithConfig` return errors. Preserve these public contracts.
- Defaults in `config.go` and `requestAttributes` in `middleware.go` are mirrored by the instrument and attribute tables in `README.md`; update both sides together.

## GitHub Workflow Gotcha

- Pushing commits that add or edit `.github/workflows/*.yml` requires GitHub auth with `workflow` scope. If push is rejected, run `gh auth refresh -h github.com -s workflow` before retrying.
