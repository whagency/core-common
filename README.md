# Common Go Packages

`common` is the shared Go module used by app backend services.

Module path:

- `github.com/whagency/core-common`

This repository provides cross-cutting infrastructure, transport helpers, and
shared service contracts so backend services can reuse consistent building
blocks instead of re-implementing them repository by repository.

This repository ships library code only. There is no standalone service binary
entrypoint here.

## What this repository contains

### Infrastructure packages

- `logger/`
  - structured logging, request/internal trace IDs, Logstash support
- `tracer/`
  - OpenTelemetry initialization, span helpers, HTTP client instrumentation
- `cache/`
  - typed cache repositories, local/remote cache support, distributed locking
- `db/`
  - transaction propagation via context
- `db/gormlog/`
  - tracing-aware GORM logger
- `db/migrator/`
  - migration execution wrapper
- `queue/`
  - publisher/consumer abstractions and lifecycle manager
- `queue/rabbitmq/`
  - RabbitMQ implementation built on `go-rabbitmq`
- `cron/`
  - scheduled job manager with observability hooks
- `outbox/`
  - transactional outbox: the platform standard for side effects that must not
    be lost when a business transaction commits
  - `outbox/postgres` — repository implementation
  - `outbox/postgres/migrations` — canonical DDL as an `embed.FS`; services apply
    it as a second migrator source, they do not copy it
  - `outbox/metrics` — standard Prometheus collectors so every adopting service
    publishes the same metric names
  - `outbox/cronjobs` — the prune and lag maintenance jobs
  - `outbox/testkit` — `FakeRepository` for unit tests
  - adopted by `account-service`, `pay-service`, `user-service` and
    `restriction-service`; see `outbox/README.md` and `outbox/ARCHITECTURE.md`
    (Russian versions alongside)
- `ledgercompensation/`
  - guards an account-service ledger posting whose local transaction may not
    commit: the row is written outside that transaction and cleared inside it,
    so a surviving row means money moved with nothing accounting for it
  - the mirror image of `outbox/`, which fires on commit rather than on rollback
  - `ledgercompensation/postgres` — repository and its `embed.FS` DDL
  - `ledgercompensation/metrics`, `.../cronjobs`, `.../testkit` — same shape as
    the outbox subpackages
  - adopted by `pay-service`; see `ledgercompensation/README.md`

### Transport support packages

- `grpc/`
  - shared connection management, status helpers, error decoding
- `grpc/middleware/`
  - gRPC observability and timeout middleware
- `grpc/proto/`
  - source protobuf schemas
- `grpc/pb/`
  - generated protobuf packages
- `http/`
  - shared HTTP helpers and response helpers
- `http/middleware/`
  - Gin middleware for observability, rate limiting, security token checks
- `http/validation/`
  - binding and validation helpers

### Shared contract packages

- `services/account`
- `services/currency`
- `services/merchant`
- `services/notification`
- `services/restriction`
- `services/user`

These packages are especially important because they define reusable DTOs,
shared transport clients, and queue payloads used across multiple service
repositories.

### Support packages

- `errorsx/`
  - shared error model and response helpers
- `decode/`
  - reusable imperative decode helpers
- `cryptox/`
  - crypto helpers
- `validation/`
  - reusable validation helpers
- `ratelimit/`
  - rate limiting primitives
- `pagination/`
  - pagination helpers
- `appenv/`
  - shared runtime environment names and validation helpers
- `tools/`
  - low-level utility helpers
- `testkit/`
  - reusable test infrastructure for backend services

## How backend services use `common`

Backend services typically use this repository in three ways:

1. infrastructure reuse
   - logging, tracing, cache, queue, DB helpers, validation
2. transport reuse
   - HTTP/gRPC middleware, shared protobufs, shared transport clients
3. contract reuse
   - shared enums, DTOs, queue payloads, and cross-service client interfaces

If a change is only useful to one service and is not a reusable contract or
cross-cutting helper, it usually does **not** belong in `common`.

Shared test infrastructure belongs under `testkit/` only when it is reusable
across services. Service-specific fixtures, generated mocks, seed data, and
controller wiring should stay in the owning service repository.

`testkit/` is part of the main `github.com/whagency/core-common` module. Do not
add a nested `testkit/go.mod`; services should import helpers as
`github.com/whagency/core-common/testkit/...` through their normal `common`
dependency.

## Requirements

- Use the Go version declared in `go.mod` as the source of truth.
- In the current repository state, `go.mod` declares `go 1.25.8`.
- Optional infrastructure depends on the package you are using:
  - Hazelcast for remote cache features
  - Logstash for remote log shipping
  - PostgreSQL or MySQL for migration and DB helpers
  - RabbitMQ for queue packages
  - an OpenTelemetry collector for tracing export

## Development workflows

### Run tests

```bash
go test ./...
```

### Run lint

```bash
make lint
```

### Check dependencies for vulnerabilities

```bash
make vulnerable-check
```

### Generate protobuf code

```bash
make gen-proto
```

### Remove generated protobuf output

```bash
make clean-pb
```

## Consuming this module from services

Services usually consume this module as a versioned Go dependency:

```bash
go get github.com/whagency/core-common@<version>
```

Local workspace consumption depends on the consumer repository setup. A service
may use:

- a published module version
- a local `replace` directive
- a shared `go.work`

Do not assume local edits in this repository are automatically active in every
consumer service.

## Package-level docs

Additional package documentation already exists for several important packages:

- `cache/README.md`
- `db/README.md`
- `grpc/README.md`
- `logger/README.md`
- `outbox/README.md` and `outbox/ARCHITECTURE.md`
- `queue/README.md`
- `services/merchant/README.md`
- `tracer/README.md`

When changing public behavior in one of these packages, update the relevant
package README as well.

## Repository structure

```text
common/
├── cache/
├── cron/
├── cryptox/
├── db/
│   ├── gormlog/
│   └── migrator/
├── decode/
├── errorsx/
├── grpc/
│   ├── middleware/
│   ├── pb/
│   └── proto/
├── http/
│   ├── handler/
│   ├── middleware/
│   └── validation/
├── logger/
├── outbox/
│   ├── cronjobs/
│   ├── metrics/
│   ├── postgres/
│   │   └── migrations/
│   └── testkit/
├── pagination/
├── queue/
│   └── rabbitmq/
├── ratelimit/
├── appenv/
├── services/
│   ├── account/
│   ├── currency/
│   ├── merchant/
│   ├── notification/
│   ├── restriction/
│   └── user/
├── tools/
├── tracer/
├── validation/
├── AGENTS.md
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

## Important repository rules

- Treat exported symbols as shared public API.
- Prefer additive changes over breaking changes.
- Do not hand-edit generated protobuf code under `grpc/pb/...`.
- Update package-level docs when public/shared behavior changes.
- Read `AGENTS.md` before making non-trivial changes.
