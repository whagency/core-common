# Merchant Shared Contracts

`services/merchant` contains the shared contract surface used by internal
services that integrate with `merchant-service`.

This package does not own merchant business logic. It owns reusable DTOs,
client interfaces, and transport adapters that let backend services talk to
`merchant-service` consistently.

## What lives here

- shared request and response DTOs for merchant auth/profile/webhook flows
- the `Service` interface used by internal consumers
- gRPC client implementation under `services/merchant/grpc/`
- shared queue contract under `services/merchant/queue/`

The merchant projection sync queue contract uses the RabbitMQ message type
`pay.merchant_operation.sync` as the event-family identifier. Its payload keeps
only the projection snapshot fields, without redundant source or event-type
fields inside the JSON body.

## Current shared service methods

- `AuthenticateMerchant`
  - verifies merchant credentials, signature, timestamp skew, and client IP
  - can optionally return a reusable merchant profile payload
- `GetMerchantProfile`
  - returns reusable merchant identity and settlement metadata for internal
    flows
- `PrepareWebhookDelivery`
  - returns webhook delivery data prepared by `merchant-service` while keeping
    webhook config ownership there

## Merchant profile contract

`MerchantProfile` currently includes:

- `merchant_id`
- `merchant_code`
- `display_name`
- `merchant_type`
- `merchant_status`
- `merchant_url`
- `owner_user_id`
- `settlement_hold_days`
- `fee_rules`

This data is intended for internal service use. Treat it as shared API surface
and change it conservatively.

## Source of truth

- Go DTOs and interfaces:
  - `services/merchant/domain.go`
- gRPC source schema:
  - `grpc/proto/merchant/v1/merchant.proto`
- generated protobuf:
  - `grpc/pb/merchantv1/...`

If the Go DTOs and generated protobuf disagree, treat the `.proto` file as the
source of truth and regenerate.

## Regeneration

After changing the merchant gRPC schema, regenerate protobuf with:

```bash
make gen-proto
```

Then verify the main consumers that rely on this shared contract.
