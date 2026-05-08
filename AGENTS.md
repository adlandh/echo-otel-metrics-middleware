# AGENTS.md

## Repository Shape

- This is a Go 1.26 library module: `github.com/adlandh/echo-otel-metrics-middleware`.
- The public API is the root package `echotelmetrics`; there are no subpackages or command entrypoints.
- Target Echo is v5 only: import `github.com/labstack/echo/v5`, and handlers use `func(c *echo.Context) error`.
- OpenSpec main specs live under `openspec/specs/`; `openspec/changes/` is ignored and may contain local-only archived or in-progress change artifacts.

## Verification Commands

- Fast test pass: `go test ./...`
- CI-equivalent test command: `go test -race -coverprofile=coverage.txt -covermode=atomic ./...`
- Vet: `go vet ./...`
- Lint: `golangci-lint run`
- CI downloads `.golangci.yml` from `adlandh/golangci-lint-config`; a local `.golangci.yml` can exist for development but is gitignored.

## Middleware Constraints

- The middleware creates OpenTelemetry metric instruments only; do not configure SDK readers, exporters, scrape endpoints, or Prometheus collectors in library code.
- Default metric attributes must stay bounded: method, Echo route pattern, status code, normalized scheme, and error state. Do not add raw path, query string, host header, client IP, user ID, or request-specific IDs by default.
- Echo v5 central error handling writes error bodies after middleware returns; response-size metrics are recorded via `echo.Response.After` so error-handler-generated bodies are included.
- `X-Forwarded-Proto` is normalized to `http` or `https`; do not record arbitrary forwarded scheme values.

## GitHub Workflow Gotcha

- Pushing commits that add or edit `.github/workflows/*.yml` requires GitHub auth with `workflow` scope. If push is rejected, run `gh auth refresh -h github.com -s workflow` before retrying.
