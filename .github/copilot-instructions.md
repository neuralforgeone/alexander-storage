# Copilot Instructions for Alexander Storage

This is an S3-compatible object storage server written in Go. Reference `MEMORY_BANK.md` for architecture details, decision log, and current development phase.

## Architecture Overview

```
Client (aws-cli/boto3) → Auth Middleware → Handlers → Services → Repositories → PostgreSQL/Redis
                                                          ↓
                                                    Storage Backend (CAS)
```

**Key Design Decisions:**
- Content-Addressable Storage (CAS) with SHA-256 hashes for deduplication
- Single-table versioning with `is_latest` partial index
- AES-256-GCM encryption for secret keys at rest
- AWS Signature V4 for request authentication

## Package Structure

```
internal/
  domain/      # Pure Go structs, no dependencies (User, Bucket, Object, Blob)
  repository/  # Data access interfaces + PostgreSQL implementations
  service/     # Business logic (IAMService, UserService, PresignService)
  auth/        # AWS v4 signature parsing and verification
  storage/     # Blob storage abstraction (filesystem backend)
  config/      # Viper-based configuration
  cache/redis/ # Optional Redis caching layer
```

## Code Patterns

### Service Layer Pattern
Services use Input/Output structs and wrap repository errors:
```go
// Always wrap errors with context
return nil, fmt.Errorf("%w: %v", ErrInternalError, err)

// Use sentinel errors from internal/service/errors.go
if err == repository.ErrNotFound {
    return nil, ErrUserNotFound
}
```

### Repository Interface Pattern
Define interfaces in `internal/repository/interfaces.go`, implement in `postgres/`:
```go
type BucketRepository interface {
    Create(ctx context.Context, bucket *domain.Bucket) error
    GetByName(ctx context.Context, name string) (*domain.Bucket, error)
}
```

### Adapter Pattern for Auth
`AccessKeyStoreAdapter` bridges `IAMService` to `auth.AccessKeyStore` interface.

## Development Commands

```bash
make build          # Build all binaries to ./bin/
make test           # Run tests with race detector
make lint           # Run golangci-lint
make migrate-up     # Apply database migrations
make docker-up      # Start PostgreSQL + Redis via docker-compose
```

## Key Files Reference

| Purpose | File |
|---------|------|
| Domain models | `internal/domain/*.go` |
| Repository interfaces | `internal/repository/interfaces.go` |
| Service errors | `internal/service/errors.go` |
| Auth middleware | `internal/auth/middleware.go` |
| Config structure | `internal/config/config.go` |
| Storage interface | `internal/storage/interfaces.go` |

## Conventions

- **Logging**: Use zerolog with structured fields: `logger.Info().Str("key", val).Msg("message")`
- **Context**: Always pass `context.Context` as first parameter
- **Errors**: Define sentinel errors, wrap with `fmt.Errorf("%w: ...", err)`
- **Tests**: Table-driven tests, use `testify/require` for assertions
- **Imports**: Group as stdlib, external, internal (enforced by goimports)

## Current Phase

Check `MEMORY_BANK.md` Section 4 for active development phase and pending tasks. As of last update: **Phase 3 - Bucket Operations**.
