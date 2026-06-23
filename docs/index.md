# Alexander Storage Documentation

Welcome to the Alexander Storage documentation. Alexander is a production-ready,
S3-compatible object storage server written in Go.

## Quick Links

| Section | Description |
|---------|-------------|
| [Quick Start](viewer.html?doc=guides/quickstart.md) | Get running in 5 minutes |
| [Production Deployment](viewer.html?doc=guides/production.md) | Production best practices |
| [Troubleshooting](viewer.html?doc=guides/troubleshooting.md) | Common issues and solutions |
| [Performance Tuning](viewer.html?doc=operations/performance-tuning.md) | Optimization guide |
| [Backup & Recovery](viewer.html?doc=operations/backup-dr.md) | Disaster recovery |
| [Runbooks](viewer.html?doc=operations/runbooks.md) | Operational procedures |
| [API Reference](api/openapi.yaml) | OpenAPI 3.0 specification |

## Architecture Highlights

- **S3 API compatibility** — works with `aws-cli`, `boto3`, and Terraform
- **Content-addressable storage** — SHA-256 deduplication
- **Multiple backends** — filesystem and S3
- **Cluster support** — gRPC inter-node communication
- **Embedded or PostgreSQL** — SQLite for homelab, PostgreSQL for production

## Storage Backends

Configure via `storage.backend`:

- `filesystem` — local disk with CAS sharding (default)
- `s3` — S3-compatible remote storage (MinIO, AWS S3)

See [Production Deployment](guides/production.md) for configuration examples.

---

[View styled site](index.html)