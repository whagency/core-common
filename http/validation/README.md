# HTTP Validation

`common/http/validation` is the shared package for HTTP transport validation.

It contains:

- validator construction
- Gin bind + validate helpers for JSON, query, and URI params
- conversion of `validator.ValidationErrors` to `common/errorsx.ValidationData`
- reusable HTTP-oriented validation rules
- transport tag aliases from internal validator tags to the public frontend contract

It does not contain:

- service-specific error templates
- business allowlists such as supported modes, statuses, or category codes
- service-specific semantic validation
- generic decode helpers for non-HTTP transports

## Responsibilities

Use this package for HTTP transport-level concerns:

- binding request DTOs
- running struct validation
- normalizing field names from `json`, `form`, and `uri` tags
- mapping validator errors to the shared validation response shape
- registering reusable HTTP transport rules such as `not_blank`
- mapping validator tags such as `uuid4` or `http_url` to stable frontend-facing tags

Keep these things in the service itself:

- validator bootstrap singleton
- service-specific custom validators
- business decode / semantic validation after binding succeeds
- service-specific HTTP policy when it differs from the shared helpers

## Package Boundaries

Use the shared packages like this:

- `common/http/validation`
  - HTTP bind + struct validation
  - field-name resolution from tags
  - validator tag alias mapping to public validation tags
- `common/decode`
  - imperative trim / required / decimal / positive checks
  - transport-agnostic helpers for gRPC, queue, or manual HTTP decode
- `common/grpc`
  - gRPC connection management, middleware, status/details, and transport error decoding
- `common/errorsx`
  - public error codes and public validation tag contract

Rule of thumb:

- if it depends on Gin or validator tags, it belongs here
- if it is generic trim / parse / require logic, it belongs in `common/decode`
- if it depends on gRPC status/details, it belongs in `common/grpc`

## Exported API

Validator construction:

- `New(opts ...Option) (*validator.Validate, error)`
- `MustNew(opts ...Option) *validator.Validate`
- `WithValidation(tag string, fn validator.Func) Option`
- `WithTagAlias(tag string, alias string) Option`
- `WithValidationAlias(tag string, alias string, fn validator.Func) Option`

High-level Gin helpers:

- `BindAndValidateJSON(c, v, dst, logPrefix)`
- `BindAndValidateQuery(c, v, dst, logPrefix)`
- `BindAndValidateURI(c, v, dst, logPrefix)`

Error conversion:

- `ToValidationData(err)`

## Field Name Resolution

Validation entries resolve field names in this order:

1. `json`
2. `form`
3. `uri`
4. Go struct field name

This keeps validation payloads aligned with the HTTP contract instead of Go field names.

## Shared Transport Policy

The high-level helpers use only `common/errorsx` codes.

JSON body:

- bind error -> `BodyBadFormatError`
- validation error -> `ValidationError` with structured `errors[]`

Query params:

- bind error -> `QueryParamsBadFormatError`
- validation error -> `QueryParamsBadFormatError`

URI params:

- bind error -> `URLParamsBadFormatError`
- validation error -> `URLParamsBadFormatError`

This is transport-level behavior only.
Business validation should still happen in the service after binding succeeds.

## Public Validation Tags

The public validation contract lives in `common/errorsx`.
A service should return those tags consistently across all HTTP APIs.

Common examples:

- `required`
- `oneof`
- `format`
- `uuid`
- `min`
- `max`
- `gt`
- `gte`
- `weak`
- `invalid_value`
- `max_size_bytes`
- `max_fractional_digits`
- `mime_extension_mismatch`

## Default Alias Mapping

Built-in aliases applied by the shared package:

- `alpha` -> `format`
- `datetime` -> `format`
- `email` -> `format`
- `e164` -> `format`
- `http_url` -> `format`
- `iso3166_1_alpha2` -> `format`
- `not_blank` -> `required`
- `uuid4` -> `uuid`

Use `WithTagAlias(...)` or `WithValidationAlias(...)` for service-specific validators.

## Example: Service Bootstrap

```go
package validation

import (
    "sync"

    commonValidation "github.com/whagency/core-common/http/validation"
    appErrors "github.com/whagency/core-common/errorsx"
    "github.com/go-playground/validator/v10"
)

var (
    once sync.Once
    v    *validator.Validate
)

type Option = commonValidation.Option

func Validator() *validator.Validate {
    once.Do(func() {
        v = commonValidation.MustNew(
            commonValidation.WithValidationAlias("doc_sha256", appErrors.ValidationTagFormat, func(fl validator.FieldLevel) bool {
                return len(fl.Field().String()) == 64
            }),
        )
    })

    return v
}
```

## Example: Handler Usage

```go
func Handle(c *gin.Context) {
    var req createRequest
    if !commonValidation.BindAndValidateJSON(c, validation.Validator(), &req, "[CreateHandler:Handle]") {
        return
    }

    // Business validation starts here.
}
```

## Example: Business Decode Stays Local

The shared package should not decide whether a specific value is allowed by a concrete endpoint.
That remains local to the service.

```go
func decodeCredentialMode(value string) (domain.CredentialMode, *appErrors.ErrorResponse) {
    mode, err := domain.NewCredentialModeFromString(value)
    if err != nil {
        return 0, svcErrors.NewOneOfValidationError(
            "[UpdateCredentialHandler:Handle]",
            "mode",
            value,
            domain.SupportedCredentialModeStrings(),
        )
    }

    return mode, nil
}
```

## not_blank

`not_blank` is registered by default in the shared HTTP validator. It treats strings containing only whitespace as invalid.
Externally it maps to the public `required` tag.

```go
type request struct {
    DisplayName string `json:"displayName" validate:"required,not_blank,min=2,max=48"`
}
```

For slices, use it with `dive` when you want per-element validation.

```go
type request struct {
    Categories []string `form:"categories" validate:"omitempty,dive,not_blank"`
}
```

## Design Rule

Move logic into `common/http/validation` only when it is truly shared HTTP transport behavior.

Keep logic in the service when it depends on:

- endpoint-specific allowlists
- business meaning
- service-specific field formats
- service-specific error wording
