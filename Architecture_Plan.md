You are a senior Go backend engineer.

Inspect this entire repository and refactor its architecture toward a simple, idiomatic, maintainable Go backend.

The current architecture may be over-engineered. Simplify it where doing so improves clarity, cohesion, maintainability, and idiomatic Go.

Do not merely provide recommendations. Inspect the repository, formulate a plan based on what you actually find, and then execute the refactor.

## Principles

Prefer:

* idiomatic Go
* simple, cohesive packages
* feature-oriented organization where useful
* explicit dependency wiring
* concrete types
* small interfaces only where they solve a real problem
* composition
* straightforward control flow
* standard-library conventions
* minimal indirection
* colocating strongly related code

Avoid:

* Domain-Driven Design ceremony
* Clean/Hexagonal/Onion Architecture layering
* `domain/application/infrastructure` package structures
* repository interfaces for every model
* service interfaces for every service
* DTOs that duplicate identical structs
* pointless mapping layers
* generic repositories
* dependency-injection frameworks
* factories that only call constructors
* pass-through `manager`, `provider`, `adapter`, `usecase`, repository, or service types
* `utils`, `helpers`, `common`, `shared`, or `core` dumping grounds
* abstractions created for hypothetical future requirements

Every abstraction must earn its existence.

When choosing between two designs, prefer fewer concepts and less indirection unless the additional structure solves a concrete problem in this codebase today.

## Inspect first

Before changing the architecture, inspect the entire repository.

Understand:

* package structure and dependency graph
* executable entry points
* routing and middleware
* database code and schema
* migrations
* SQL queries
* business logic
* authentication/authorization
* configuration
* background jobs
* external integrations
* tests
* generated code
* build, lint, generation, and migration commands

Identify concrete architectural problems before moving files.

Look especially for:

* excessive layering
* unnecessary interfaces
* unnecessary repository abstractions
* duplicated models and DTOs
* pointless mappings
* pass-through services
* tiny packages without meaningful boundaries
* god packages or god structs
* business logic in HTTP handlers
* HTTP concerns leaking into business code
* database concerns leaking throughout the application
* awkward or circular dependencies
* global mutable state
* constructors with excessive dependencies
* abstractions with one implementation and no useful boundary

Then create a refactoring plan based on what actually exists and execute it.

## Target architecture

Do not mechanically impose a predefined directory tree.

Adapt the architecture to the actual application.

A reasonable shape might be:

```text
cmd/
  server/
    main.go

internal/
  app/
    app.go
    routes.go

  user/
    user.go
    service.go

  project/
    project.go
    service.go

  sqlite/
    sqlite.go
    migrations/
    queries/
    generated/

  http/
    router.go
    middleware/
    user/
    project/
```

This is only an example.

Do not create packages or files merely to match this structure.

If a feature only needs one or two files, keep it simple.

A package with three straightforward files is often better than six packages connected through interfaces.

## Application wiring

Keep `cmd/server/main.go` small.

It should primarily:

* load configuration
* construct the application
* start the server
* handle startup failures and graceful shutdown

Keep application construction in a small composition root such as `internal/app`.

Use explicit constructor calls.

Do not introduce a dependency-injection framework.

A developer should be able to understand how the application is assembled by reading a relatively small amount of code.

## Feature packages

Use cohesive packages for real application concepts where useful.

Feature packages may contain:

* core application types
* business operations
* feature-specific validation
* small dependency interfaces genuinely required by the feature

Do not automatically create a service or repository for every model.

If a service only forwards calls to another object, remove it unless it provides a meaningful boundary.

## HTTP

Keep HTTP transport concerns together, preferably under `internal/http`.

Handlers should generally:

1. parse/decode input
2. perform transport-level validation
3. call application functionality
4. translate results and errors into HTTP responses

Keep substantial business logic out of handlers.

Keep HTTP-specific concepts such as status codes, headers, cookies, request parsing, and serialization out of core application packages.

Use consistent HTTP error translation.

Do not expose raw database errors to clients.

## SQLite

The application uses SQLite.

Keep SQLite-specific persistence concerns together.

The SQLite package should own things such as:

* opening/configuring the database
* migrations
* SQL queries
* transactions
* persistence implementations
* SQLite-specific error handling

Review SQLite configuration specifically for:

* foreign-key enforcement
* busy timeout/handling
* transaction boundaries
* context propagation
* resource cleanup
* indexes
* constraints
* concurrency assumptions

Do not apply client/server database assumptions blindly to SQLite.

Do not create elaborate abstractions merely because SQLite might theoretically be replaced someday.

## sqlc

Use `sqlc` for SQLite query code generation where it improves the persistence layer.

Prefer SQL queries as the source of truth over handwritten query boilerplate.

A reasonable organization is:

```text
internal/sqlite/
  migrations/
  queries/
  generated/
```

Adapt this to the repository.

Guidelines:

* keep SQL queries in a clear database-owned location
* keep `sqlc.yaml` at the repository root unless there is a good reason not to
* treat sqlc output as generated code
* never hand-edit generated files
* use generated concrete query types directly when practical
* do not wrap every generated method in a repository/service that merely forwards calls
* preserve `context.Context`
* use sqlc transaction support such as `WithTx` for transactional operations
* avoid unnecessary mapping between sqlc-generated types and application types when their representations are effectively identical
* introduce separate application types when they represent genuinely different concepts or protect useful boundaries
* configure nullable values and type overrides deliberately where useful

Review existing handwritten SQL/database code and migrate it to sqlc where doing so reduces boilerplate and improves compile-time query/type safety.

Do not force sqlc onto operations where it makes the implementation less clear.

## goose

Use `goose` for database migrations.

Guidelines:

* keep migrations together under the SQLite/database package
* use ordered, reviewable migration files
* do not maintain a second competing migration system
* preserve existing migration history and production data where applicable
* account for SQLite-specific DDL limitations
* keep migration execution explicit
* do not hide goose behind unnecessary interfaces or wrappers

If an existing custom migration runner can be cleanly replaced by goose, remove the custom machinery while preserving required behavior.

Test migrations against a fresh database and, where practical, representative upgrade paths.

## Transactions

Make transaction boundaries explicit and driven by application operations rather than individual repository methods.

Use sqlc's generated query types with transactions where appropriate.

Avoid introducing transaction-manager abstractions unless the codebase demonstrates a concrete need for one.

A multi-step operation that must succeed atomically should clearly own its transaction boundary.

## Interfaces

Be skeptical of interfaces.

Prefer concrete dependencies by default.

Do not create interfaces:

* simply because a concrete implementation exists
* solely for mocking
* for every repository or service
* to prepare for hypothetical future implementations

When an interface genuinely helps:

* keep it small
* define it from the consumer's needs
* place it near the consumer when practical
* let implementations satisfy it implicitly

## Errors

Use idiomatic Go error handling:

* `%w` wrapping
* `errors.Is`
* `errors.As`
* sentinel or typed errors only where callers genuinely need classification

Avoid:

* comparing error strings
* swallowing errors
* leaking SQLite errors to HTTP clients
* logging the same error repeatedly at multiple layers

Prefer logging an error once at an appropriate boundary with enough context to diagnose it.

## Context

Pass `context.Context` explicitly through request-scoped operations.

Do not store contexts inside long-lived structs.

Database and other I/O operations should honor cancellation where supported.

## Execute the refactor

After inspection and planning, perform the refactor.

Move, merge, rename, simplify, or delete code where justified.

Update all affected:

* imports
* constructors
* dependency wiring
* routes
* handlers
* database code
* SQL queries
* migrations
* configuration
* generated code
* tests

Preserve existing application behavior unless changing behavior is necessary to fix a clear bug.

Do not stop after producing a proposed architecture.

## Validation

Continuously run the repository's relevant checks.

At minimum:

```bash
sqlc generate
gofmt -w .
go test ./...
go ve
```
