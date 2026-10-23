# errorsx: Error Specification for Frontend

This document describes the `errorsx` error contract used in backend HTTP responses.

## 0) Shared Package Boundaries

Use the shared transport packages like this:

| Package | Responsibility | Public contract it defines |
|---|---|---|
| `common/errorsx` | Shared error codes, messages, validation tags, and HTTP error payload shape. | Error `code`, `message`, `data`, `meta`, and public validation tags such as `required`, `format`, `oneof`, `uuid`. |
| `common/http/validation` | HTTP bind + struct validation for JSON, query, and URI params. | HTTP transport behavior for bind/validation failures and mapping validator tags to public validation tags. |
| `common/decode` | Transport-agnostic imperative decode helpers. | No direct frontend contract. Internal trim/parse/require helpers that callers wrap into HTTP/gRPC/queue transport errors. |
| `common/grpc` | gRPC-specific infra, status/details helpers, and transport error decoding. | gRPC status mapping, shared client connection behavior, and `ErrorInfo` decoding for internal service-to-service calls. |

Rule of thumb:

- if it defines public HTTP error payloads or validation tags, it belongs in `common/errorsx`
- if it binds Gin requests and runs struct validation, it belongs in `common/http/validation`
- if it only trims/parses/requires primitive values, it belongs in `common/decode`
- if it depends on gRPC status/details or client connection behavior, it belongs in `common/grpc`

## 1) Standard Error Response Structure

Errors are always returned in a unified response format:

```json
{
  "code": 11,
  "message": "data validation error.",
  "data": {
    "errors": [
      {
        "field": "email",
        "tag": "required",
        "params": []
      }
    ]
  },
  "meta": {
    "requestId": "010a0998-4b1a-4e70-a951-d27a40b49c0c",
    "timestamp": "2025-10-07T12:00:45Z"
  }
}
```

### Response Fields

- `code` (`number`) — internal business error code (`> 0` indicates error).
- `message` (`string`) — human-readable backend message (English).
- `data` (`object`) — additional error details. If no details are provided, backend returns `{}`.
- `meta.requestId` (`string`) — request identifier for tracing/support.
- `meta.timestamp` (`string`, RFC3339) — response timestamp.

## 2) Recommended Frontend Error Handling Strategy

Recommended priority order:

1. **Primary UI branching by `code`** (most stable contract).
2. **Fallback by HTTP status** (for unknown `code`).
3. For user-facing text:
    - Prefer FE-localized message mapped by `code`.
    - Use backend `message` only as fallback.
4. Log/forward `meta.requestId` for support and diagnostics.

> Important: `message` text may evolve and should not be the only routing key in frontend logic.

## 3) Error Code Reference

> Range `1..99999` is reserved for common/shared errors.

| Code | Constant | HTTP | Message | FE handling guidance |
|---:|---|---:|---|---|
| 1 | `InvalidRequestError` | 400 | `bad request, see logs for more details.` | Generic malformed request fallback (client-side 4xx category). |
| 2 | `InternalServerError` | 500 | `internal error, see logs for more details.` | Generic server failure UI (toast/screen: “Please try again later”). |
| 3 | `DatabaseError` | 500 | `error in database, see logs for more details.` | Server storage failure; use generic retry/fallback UX. |
| 4 | `BodyBadFormatError` | 400 | `body request format is not valid.` | FE sent invalid JSON/body; verify payload serialization. |
| 5 | `QueryParamsBadFormatError` | 400 | `query params request format is not valid.` | Invalid query parameter format/types/required values. |
| 21 | `URLParamsBadFormatError` | 400 | `url params request format is not valid.` | Invalid path parameter format/types/required values. |
| 6 | `RequestTimeoutError` | 504 | `request timeout in third party service.` | Upstream timeout; suggest retry option. |
| 7 | `AccessForbiddenError` | 403 | `access forbidden, invalid security token.` | Access denied/invalid security token; verify auth/security flow. |
| 8 | `RecordNotFoundError` | 404 | `record not found.` | Entity not found; show empty/not-found state. |
| 9 | `RecordAlreadyExistsError` | 409 | `record already exists.` | Uniqueness conflict; prompt user to change input. |
| 10 | `CacheError` | 500 | `error in cache, see logs for more details.` | Internal infrastructure/cache issue; generic fallback UI. |
| 11 | `ValidationError` | 422 | `data validation error.` | Parse and render field errors from `data.errors`. |
| 12 | `RouteNotFoundError` | 404 | `route not found.` | Endpoint not found; usually FE integration/config issue. |
| 13 | `ConcurrencyConflictError` | 409 | `concurrency conflict.` | Concurrent update conflict (optimistic lock/version mismatch). |
| 14 | `ExternalServiceError` | 502 | `external service error, see logs for more details.` | Upstream dependency failure; retry/fallback UX. |
| 15 | `RateLimitExceededError` | 429 | `rate limit exceeded, please try again later.` | Rate limit exceeded; throttle UI retries and show cooldown. |
| 16 | `NoContentError` | 204 | `no content to return.` | No data available; response body may be empty by API design. |
| 17 | `UserSessionTokenInvalidError` | 401 | `user session token is invalid.` | Invalid token; trigger logout/reauth flow. |
| 18 | `UserSessionExpiredError` | 401 | `user session has expired.` | Session expired; prompt user to sign in again. |
| 19 | `RestrictedOperationError` | 403 | `operation is restricted for this account.` | Operation restricted for account (policy/compliance/business limits). |
| 20 | `InsufficientFundsError` | 400 | `insufficient funds for this operation.` | Insufficient balance; highlight required amount vs available funds. |

## 4) `data` Format for Validation Errors (`code = 11`)

For `ValidationError`, backend provides an `errors` array in `data`:

```json
{
  "code": 11,
  "message": "data validation error.",
  "data": {
    "errors": [
      {
        "field": "amount",
        "tag": "gte",
        "params": ["100"]
      },
      {
        "field": "currency",
        "tag": "oneof",
        "params": ["USD", "EUR", "KZT"]
      }
    ]
  }
}
```

### Field Semantics

- `field` — DTO/request field name.
- `tag` — validation rule (e.g. `required`, `email`, `gte`, `lte`, `oneof`).
- `params` — validation parameters (if any).

Frontend recommendation:
- map `field` to a specific UI control,
- map `tag` + `params` to localized helper/error text.

## 5) Practical UX Guidelines

- For `401` (`17`, `18`) — handle centrally in interceptor (logout/refresh/redirect).
- For `403` (`7`, `19`) — show permission/restriction state, avoid blind infinite retries.
- For `409` (`9`, `13`) — ask user to refresh state or adjust input.
- For `429` (`15`) — apply backoff and temporarily disable repeated actions.
- For `5xx` (`2`, `3`, `10`, `14`) — show generic error and provide retry action.

## 6) Frontend Integration Checklist

- [ ] Keep a centralized enum/map for error codes (`code -> UI behavior`).
- [ ] Add a fallback branch for unknown `code` values.
- [ ] Implement special handling for `ValidationError` and `data.errors`.
- [ ] Log and expose `meta.requestId` for diagnostics/support.
- [ ] Add i18n mappings for key error code scenarios.
