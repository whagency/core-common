# GRPC

`common/grpc` contains shared gRPC-specific infrastructure.

Use it for:

- client connection management
- interceptors / observability
- gRPC middleware
- transport error decoding
- status/details helpers
- HTTP-status to gRPC-code mapping

## This Package Owns

- `ClientConnManager`
- `DecodeResponseError(...)`
- `MapHTTPStatusToCode(...)`
- `ExtractErrorInfoReason(...)`
- middleware and observability helpers

## This Package Does Not Own

- generic trim / required / decimal decode helpers
- HTTP struct validation
- frontend validation tag mapping
- service-specific domain allowlists

Use instead:

- `common/decode` for imperative server-side transport decode helpers
- `common/http/validation` for HTTP bind + struct validation
- service-local code for business decode and semantic checks

## Rule Of Thumb

If the code still makes sense outside gRPC, it probably does not belong here.

Examples that do not belong in `common/grpc`:

- `ParseRequiredString(...)`
- `ParseRequiredDecimal(...)`
- `RequirePositiveUint64(...)`

Those belong in `common/decode`.

Examples that do belong here:

- converting `status.Status` details into typed client errors
- mapping server responses into `codes.InvalidArgument` / `codes.NotFound` / `codes.Internal`
- shared client connection lifecycle and interceptors
