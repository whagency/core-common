# Common Testkit

`testkit` contains reusable test infrastructure for app Go services.

It is part of the main `github.com/whagency/core-common` module, not a separate
nested Go module. Services import helpers through
`github.com/whagency/core-common/testkit/...`.

Use these packages for cross-service test setup that should not be copied into
every repository:

- `loggertest` for minimal shared logger initialization.
- `envtest` for service-local test config defaults.
- `httptestx` for Gin request contexts and JSON response decoding.
- `postgrestest` for disposable PostgreSQL repository integration tests backed
  by real migrations and `testcontainers-go`.
- `dbtest` for small transaction-manager fakes.
- `decimaltest` for decimal fixtures.

Service-specific domain fixtures, generated mocks, repository seed data, and
controller wiring should stay inside the owning service repository.
