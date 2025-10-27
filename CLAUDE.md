# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A Terraform provider for managing SendGrid resources via the official Twilio SendGrid API. Built using the Terraform Plugin Framework (protocol v6), supporting Terraform 1.6+.

## Development Commands

### Build & Install
```bash
# Build the provider
go build -v ./...

# Install to local Terraform plugins directory
go install -v ./...

# Or use make (default runs fmt, lint, install, generate)
make install
```

### Testing
```bash
# Unit tests (fast, no API calls)
go test -v -cover -timeout=120s -parallel=10 ./...

# Acceptance tests (requires SENDGRID_API_KEY)
TF_ACC=1 go test -v -cover -timeout 120m ./...

# Or use make
make test      # unit tests
make testacc   # acceptance tests
```

#### Running Individual Tests
```bash
# Run a specific test function
go test -v -run TestAccSSOTeammateResource_basic ./internal/provider/

# Run acceptance tests for a specific resource
TF_ACC=1 go test -v -run TestAccSSOTeammateResource ./internal/provider/
```

#### Test Environment Variables
Acceptance tests require these environment variables:
- `SENDGRID_API_KEY` (required)
- `TEST_SSO_EMAIL` (for SSO teammate tests)
- `TEST_SUBUSER_ID` (for subuser access tests)
- `TEST_SUBUSER_USERNAME` (for subuser tests)
- `TEST_TEAMMATE_NAME` (for teammate data source tests)
- `TEST_USERNAME` (for general teammate tests)

### Code Quality
```bash
# Format code
gofmt -s -w -e .
# Or: make fmt

# Run linters
golangci-lint run
# Or: make lint

# Generate documentation
cd tools && go generate ./...
# Or: make generate
```

## Architecture

### Provider Structure

**Entry Point**: `main.go` - Standard Terraform provider server setup using `providerserver.Serve()`

**Provider Core**: `internal/provider/provider.go`
- `SendGridProvider` implements `provider.Provider` interface
- Configuration: `base_url` (optional, defaults to https://api.sendgrid.com) and `api_key` (optional, falls back to `SENDGRID_API_KEY` env var)
- `Client` struct holds `BaseURL` and `APIKey` for API calls
- Provider automatically propagates client to all resources and data sources via `Configure()`

### Resources

**`resource_sso_teammate.go`** - Manages SSO Teammates
- **Schema**: `id`, `email`, `first_name`, `last_name`, `has_restricted_subuser_access`, `subuser_access` (set of objects with `id`, `permission_type`, `scopes`)
- **API Endpoints**:
  - Create: `POST /v3/sso/teammates`
  - Update: `PATCH /v3/sso/teammates/{username}`
  - Read: `GET /v3/teammates/{username}` (note: different endpoint)
  - Delete: `DELETE /v3/teammates/{username}`
  - Subuser access pagination: `GET /v3/teammates/{username}/subuser_access` with `limit=100` and `after_subuser_id` for pagination
- **Important**: After create/update operations, the resource performs a full read-back including paginated subuser_access to ensure state is fully populated
- **Subuser Access**: Stored as a `types.Set` to prevent order-only diffs; each entry has `id` (int64), `permission_type` ("restricted" or "admin"), and `scopes` (set of strings)

### Data Sources

**`data_source_teammate.go`** - Lookup teammate by username
- Supports `on_behalf_of` header for subuser impersonation
- Returns full teammate details including scopes, contact info

**`data_source_teammate_subuser_access.go`** - Fetch subuser access list for a teammate
- Handles pagination via `after_subuser_id` parameter
- Returns `has_restricted_subuser_access` flag and list of subuser access entries

**`data_source_subusers.go`** - List all subusers
- Returns array of subuser details

### Testing Utilities

**`internal/testacc/testacc.go`** - Shared test helpers
- `TestAccProtoV6ProviderFactories`: Provider factories for acceptance tests
- `PreCheck()`: Minimal acceptance test requirements (API key present)
- `TestAccPreCheck()`: Strict check requiring `TF_ACC=1` and API key
- `RequireEnv()`: Require specific environment variables for individual tests
- `ConfigFromEnv()`: Helper to build provider + data source HCL configs

### Key Implementation Patterns

**API Client Pattern**: Direct use of `sendgrid.GetRequest()` and `sendgrid.API()` from sendgrid-go library
- Manually construct request objects with method, endpoint, body
- Handle status codes and parse JSON responses
- No SDK wrapper layer - direct REST API interaction

**Pagination Handling**: Subuser access endpoints use cursor-based pagination
- Loop until `_metadata.next_params.after_subuser_id` returns 0
- Set `limit=100` and `after_subuser_id` query params for subsequent pages
- Accumulate results across all pages before updating state

**State Management**: Resources perform full read-back after create/update
- Ensures computed attributes (like `status`) are populated
- Handles API normalization (e.g., empty strings vs null)
- Properly converts API responses to Terraform types (especially Set types)

**Set Schema for Subuser Access**: Using `types.Set` instead of `types.List` prevents Terraform from detecting order-only diffs, since SendGrid API may return subuser_access in unpredictable order

## SendGrid API Specifics

- Base URL: `https://api.sendgrid.com` (configurable)
- Authentication: Bearer token via `Authorization: Bearer {api_key}` header (handled by sendgrid-go)
- SSO Teammate endpoints use `/v3/sso/teammates` for create/update but `/v3/teammates` for read/delete
- Pagination uses `limit` and `after_subuser_id` query parameters
- Some endpoints support `on-behalf-of` header for parent account impersonation

## Common Development Tasks

### Adding a New Resource
1. Create `resource_<name>.go` in `internal/provider/`
2. Implement `resource.Resource` and `resource.ResourceWithConfigure` interfaces
3. Define schema with `schema.Schema` and model struct with `tfsdk` tags
4. Implement CRUD methods calling SendGrid API endpoints
5. Add constructor to `Resources()` in `provider.go`
6. Create `resource_<name>_test.go` with acceptance tests
7. Add example in `examples/resources/<name>/resource.tf`
8. Run `make generate` to update documentation

### Adding a New Data Source
1. Create `data_source_<name>.go` in `internal/provider/`
2. Implement `datasource.DataSource` interface
3. Define schema and model struct
4. Implement `Read()` method
5. Add constructor to `DataSources()` in `provider.go`
6. Create `data_source_<name>_test.go` with tests
7. Add example in `examples/data-sources/<name>/data-source.tf`
8. Run `make generate`

### Debugging Provider Issues
- Enable Terraform logging: `export TF_LOG=DEBUG`
- Use `tflog.Debug(ctx, "message", map[string]any{"key": value})` in provider code
- Check API request/response in sendgrid-go library (may need to add logging)
- For acceptance test failures, check that all required environment variables are set