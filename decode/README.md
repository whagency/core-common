# Decode

`common/decode` contains small imperative transport-decode helpers.

Use it when you already have primitive values and need to:

- trim required strings
- normalize required upper-case transport codes
- parse decimals
- validate positive / non-negative numeric inputs

This package is transport-agnostic.
It is not tied to HTTP, gRPC, queue, or any specific framework.

## Use This Package For

- gRPC server request decode helpers
- queue consumer payload decode helpers
- manual HTTP decode after bind succeeds
- other server-side transport adapters that need imperative validation

Typical examples:

- `ParseRequiredString`
- `ParseRequiredUpperString`
- `RequirePositiveUint64`
- `RequirePositiveInt64`
- `ParseRequiredDecimal`
- `RequirePositiveDecimal`
- `RequireNonNegativeDecimal`

## Do Not Use This Package For

- struct validation tags
- Gin bind / abort helpers
- frontend validation contract tags
- gRPC status mapping
- thin shared gRPC clients that only forward normalized payloads
- service-specific business allowlists
- service-specific semantic validation

Those belong elsewhere:

- `common/http/validation` for HTTP bind + struct validation
- `common/grpc` for gRPC infra and status handling
- `common/errorsx` for public validation tags and error codes
- service-local code for business decode and semantic rules

## Error Semantics

Errors returned by this package are internal decode errors.
They are meant to be wrapped by the caller into the correct transport error:

- HTTP validation / bad-format response
- gRPC `codes.InvalidArgument`
- queue `NonRetryable` malformed-message error

Do not return these raw strings as a public API contract.

## Example

```go
func decodeRequest(req *pb.MakeTransactionRequest) (Params, error) {
    if req == nil {
        return Params{}, fmt.Errorf("[err: request is required]")
    }

    assetCode, err := commonDecode.ParseRequiredUpperString("asset_code", req.GetAssetCode())
    if err != nil {
        return Params{}, err
    }

    amount, err := commonDecode.ParseRequiredDecimal("amount", req.GetAmount())
    if err != nil {
        return Params{}, err
    }

    if err := commonDecode.RequirePositiveDecimal("amount", amount); err != nil {
        return Params{}, err
    }

    return Params{
        AssetCode: assetCode,
        Amount:    amount,
    }, nil
}
```

## Design Rule

Keep this package small.
Only move helpers here when they are:

- transport-level
- framework-agnostic
- reused in at least two places

If logic depends on HTTP/Gin, keep it in `common/http/validation`.
If logic depends on gRPC status/details, keep it in `common/grpc`.
If logic depends on business meaning, keep it in the service.
