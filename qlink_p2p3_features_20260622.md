# P2+P3 Feature Implementation - 2026-06-22

## Objective
Implement qlink-server P2 (business features) and P3 (enterprise features) in Go.

## P2: Business Features ✅

### P2-1: API Token
- Model: `account/model/api_token.go` — ApiTokenDO (token, scopes, expiry)
- Service: `account/service/api_token_service.go` — Create/List/Delete/ValidateToken
- Controller: `account/controller/api_token.go` — 3 endpoints
- Routes: `/api/account/v1/api_token/create|list|delete` (auth required)
- Token format: 64-char hex, masked when listed (first6\*\*\*\*last6)

### P2-2: Operation Log (Audit)
- Model: `link/model/operation_log.go` — OperationLogDO (action, resource, IP, user-agent, details)
- Service: `link/service/operation_log_service.go` — Record + Page query
- Controller: `link/controller/operation_log.go` — Page + ActionTypes
- Logged operations: link:create|update|delete|status, domain:*, group:*, abuse:*
- Integrated into ShortLinkController (Add/Del/Update/Status) + DomainController + BatchController

### P2-3: Batch Operations
- Controller: `link/controller/batch.go`
- `batch_delete` — accepts `[ids]` via MQ async + sync group_code_mapping soft-delete
- `batch_status` — single link state toggle (one-at-a-time UX)
- Routes: `/api/link/v1/batch_delete|batch_status`

### P2-4: Multi-Domain CRUD
- Updated `link/model/domain.go` — added `State` field (ACTIVE/INACTIVE)
- Controller: `link/controller/domain.go` — full Create/Update/Delete/Status/List
- Routes: `/api/domain/v1/create|update|delete|status|list`

## P3: Enterprise Features ✅

### P3-1: URL Safety Check
- Service: `link/service/p3_service.go` — URLSafeChecker
- Checks: protocol validation, malicious pattern scan, HTTPS reachability, redirect loops
- Route: `POST /api/link/v1/url_safe_check`

### P3-2: Abuse Report
- Model: `link/model/abuse_report.go` — AbuseReportDO
- Controller: `link/controller/p3.go` — AbuseReportController
- Routes: `POST /api/abuse_report/v1/create|list`
- Status flow: PENDING → RESOLVED / DISMISSED

### P3-3: Brand Customization
- Model: `link/model/brand_config.go` — BrandConfigDO (site_name, logo, favicon, primary_color, copyright)
- Controller: `link/controller/p3.go` — BrandConfigController
- Routes: `/api/brand_config/v1/save|get|delete` (auth), `/api/public/brand_config/:account_no` (public)

### P3-4: OIDC SSO — deferred (depends on workspace model, OIDC provider config)
### P3-5: Workspace — deferred (complex, needs workspace model redesign)

## Files Changed

### New files (14):
- `internal/account/model/api_token.go`
- `internal/account/request/api_token.go`
- `internal/account/vo/api_token.go`
- `internal/account/service/api_token_service.go`
- `internal/account/controller/api_token.go`
- `internal/link/model/operation_log.go`
- `internal/link/model/domain.go` (updated with State)
- `internal/link/model/abuse_report.go`
- `internal/link/model/brand_config.go`
- `internal/link/request/operation.go`
- `internal/link/request/p3.go`
- `internal/link/vo/operation.go`
- `internal/link/vo/p3.go`
- `internal/link/service/operation_log_service.go`
- `internal/link/service/p3_service.go`
- `internal/link/controller/domain.go`
- `internal/link/controller/operation_log.go`
- `internal/link/controller/batch.go`
- `internal/link/controller/p3.go`

### Modified files (4):
- `cmd/link/main.go` — new services + controllers + routes
- `cmd/account/main.go` — apiTokenSvc + routes
- `internal/link/controller/short_link.go` — opLog recording
- `internal/link/vo/domain.go` — added State+CreatedAt

### SQL migrations:
- `sql/p2_migration.sql` — api_token, operation_log, domain state column
- `sql/p3_migration.sql` — abuse_report, brand_config tables

## Build Status
✅ `go build ./cmd/link/... ./cmd/account/... ./cmd/data/...` — all 3 services compile cleanly

## Deferred
- OIDC SSO: requires workspace model + external IdP configuration (Keycloak/Auth0/etc.)
- Workspace management: needs model rethink (workspace ≠ link group, separate entity with member roles)
