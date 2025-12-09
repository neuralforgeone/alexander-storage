# Alexander Storage

[![CI](https://github.com/neuralforgeone/alexander-storage/actions/workflows/ci.yml/badge.svg)](https://github.com/neuralforgeone/alexander-storage/actions/workflows/ci.yml)
[![Release](https://github.com/neuralforgeone/alexander-storage/actions/workflows/release.yml/badge.svg)](https://github.com/neuralforgeone/alexander-storage/actions/workflows/release.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/neuralforgeone/alexander-storage)](https://goreportcard.com/report/github.com/neuralforgeone/alexander-storage)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Go Version](https://img.shields.io/github/go-mod/go-version/neuralforgeone/alexander-storage)](https://go.dev/)

**A production-ready, S3-compatible object storage server written in Go.**

Alexander Storage is a self-hosted object storage system designed for **archival**, **backups**, **homelab**, and **enterprise** environments. It provides full compatibility with the AWS S3 API, allowing you to use existing tools like `aws-cli`, `boto3`, and Terraform without modification.

> ✅ **Best For**: Archival storage, backup solutions, home labs, development/testing, and self-hosted cloud storage.  
> 🎯 **Unique Features**: Content-addressable storage (CAS), automatic deduplication, SQLite/PostgreSQL support, web dashboard, and enterprise-grade encryption.

---

## Architecture Overview

```mermaid
flowchart TB
    subgraph Clients["🖥️ S3 Clients"]
        CLI["aws-cli"]
        SDK["boto3 / AWS SDK"]
        TF["Terraform"]
    end

    subgraph Alexander["⚡ Alexander Server"]
        AUTH["🔐 Auth Middleware<br/>AWS Signature V4"]
        HANDLERS["📡 API Handlers<br/>chi router"]
        SERVICES["⚙️ Service Layer<br/>Business Logic"]
    end

    subgraph Storage["💾 Storage Layer"]
        PG["🐘 PostgreSQL<br/>Metadata + ACID"]
        REDIS["📦 Redis<br/>Cache (Optional)"]
        CAS["📁 Blob Storage<br/>Content-Addressable"]
    end

    Clients --> AUTH
    AUTH --> HANDLERS
    HANDLERS --> SERVICES
    SERVICES --> PG
    SERVICES -.-> REDIS
    SERVICES --> CAS

    style Alexander fill:#e1f5fe
    style Storage fill:#fff3e0
```

### Data Flow: Object Upload

```mermaid
sequenceDiagram
    participant C as Client
    participant A as Auth Middleware
    participant H as Object Handler
    participant S as ObjectService
    participant DB as PostgreSQL
    participant FS as Blob Storage

    C->>A: PUT /bucket/key + AWS v4 Signature
    A->>A: Verify HMAC-SHA256
    A->>H: Authenticated Request
    H->>S: PutObject(key, body)
    S->>S: Stream & Hash (io.TeeReader)
    S->>DB: UPSERT blob (ref_count++)
    S->>FS: Store if not duplicate
    S->>DB: INSERT object record
    S-->>C: 200 OK + ETag
```

---

## Table of Contents

- [Features](#features)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Web Dashboard](#web-dashboard)
- [API Compatibility](#api-compatibility)
- [Development](#development)
- [Documentation](#documentation)
- [Contributing](#contributing)
- [License](#license)

---

## Features

### Core S3 Operations ✅

- **Bucket Operations**: CreateBucket, DeleteBucket, ListBuckets, HeadBucket
- **Object Operations**: PutObject, GetObject, HeadObject, DeleteObject, CopyObject
- **List Operations**: ListObjectsV1, ListObjectsV2 with pagination
- **Multipart Uploads**: InitiateMultipartUpload, UploadPart, CompleteMultipartUpload, AbortMultipartUpload, ListParts
- **Versioning**: Full S3-compatible versioning with ListObjectVersions
- **Presigned URLs**: Generate time-limited URLs for secure sharing

### Storage Features ✅

- **Content-Addressable Storage (CAS)**: Automatic deduplication using SHA-256 hashing
- **Two-Level Directory Sharding**: Optimized filesystem layout for millions of objects
- **Reference Counting**: Efficient blob management with automatic cleanup
- **Streaming Hash Calculation**: SHA-256 computed during upload via `io.TeeReader` — no extra disk reads

### Security ✅

- **AWS Signature V4**: Industry-standard request signing
- **AES-256-GCM Encryption**: Secure secret key storage
- **Server-Side Encryption (SSE-S3)**: AES-256-GCM + HKDF per-object encryption
- **Access Key Management**: Create and manage multiple access keys per user
- **Bucket ACL**: Support for private, public-read, public-read-write policies

### Enterprise Features ✅

- **Web Dashboard**: Built-in HTMX + Tailwind CSS management interface
- **Object Lifecycle Rules**: Automatic object expiration based on policies
- **Garbage Collection**: Background cleanup of orphan blobs with configurable grace period
- **Prometheus Metrics**: Full observability with request, storage, auth, and GC metrics
- **Health Endpoints**: Kubernetes-compatible liveness and readiness probes
- **Rate Limiting**: Token bucket algorithm per client IP

### Database Support ✅

- **PostgreSQL**: ACID-compliant metadata storage for production deployments
- **SQLite**: Embedded database for single-node/homelab deployments (no external dependencies)
- **Redis**: Optional distributed cache and locking for multi-node clusters

### Advanced Features (Fusion Engine v2.0) ✅

- **Per-Hash Sharded Locking**: 256-bucket lock pool for parallel uploads
- **ChaCha20-Poly1305 Streaming Encryption**: 16MB chunks with per-chunk nonce derivation
- **Composite Blobs**: Part references for efficient multipart storage
- **Delta Versioning**: FastCDC content-defined chunking (20-90% storage savings)
- **Multi-Node Cluster**: gRPC inter-node communication with hot/warm/cold tiers
- **Automatic Tiering**: Policy-based data movement between storage tiers

---

## Benchmarks

> 📊 **Benchmark results from testing suite. Run your own benchmarks for your specific workload.**

### Performance Characteristics

| Metric | Single Node (SQLite) | Production (PostgreSQL) | Notes |
|--------|---------------------|-------------------------|-------|
| Memory (idle) | ~20MB | ~30MB | Low memory footprint |
| Memory (1k concurrent) | ~150MB | ~200MB | Efficient under load |
| Startup time | <100ms | <500ms | Fast cold start |
| PUT throughput | ~100 req/s | ~500 req/s | Depends on disk I/O |
| GET throughput | ~200 req/s | ~1000 req/s | CAS enables caching |

> Alexander prioritizes **low resource usage** and **data integrity** over raw throughput.

### Running Benchmarks

```bash
# Run all benchmarks
go test -bench=. -benchmem ./...

# Run specific benchmark
go test -bench=BenchmarkPutObject -benchmem ./internal/service/

# Load testing with k6
k6 run tests/load/k6/scenarios.js
```

---

## Architecture

> 📐 See the [Architecture Overview](#architecture-overview) diagrams at the top of this document.

### Component Overview

| Component | Technology | Purpose |
|-----------|------------|---------|
| **API Layer** | chi router | S3-compatible HTTP endpoints |
| **Auth** | AWS Sig V4 | Request authentication & signing |
| **Metadata** | PostgreSQL | ACID-compliant object/bucket metadata |
| **Cache** | Redis | Optional distributed cache & locking |
| **Storage** | Filesystem (CAS) | Content-addressable blob storage |

### Storage Layout

Objects are stored using content-addressable storage with two-level directory sharding:

```
/data
  /ab
    /cd
      /abcdef1234567890...  (SHA-256 hash as filename)
```

This approach provides:
- **Automatic deduplication**: Identical files stored once
- **Built-in integrity verification**: Hash = checksum
- **Efficient distribution**: Balanced filesystem for millions of files
- **No extra I/O for hashing**: Hash computed during upload stream via `io.TeeReader`

### Deployment Modes

| Mode | Database | Cache/Lock | Use Case |
|------|----------|------------|----------|
| **Single Node** | SQLite | Memory | Homelab, dev/test, small deployments |
| **Production** | PostgreSQL | Memory | Medium deployments, single server |
| **Distributed** | PostgreSQL | Redis | High availability, multi-node clusters |

---

## Quick Start

### Using Docker Compose

The fastest way to get started is with Docker Compose:

```bash
# Clone the repository
git clone https://github.com/neuralforgeone/alexander-storage.git
cd alexander-storage

# Start all services (PostgreSQL, Redis, Alexander)
docker-compose -f configs/docker-compose.yaml up -d

# Generate a master encryption key
export ALEXANDER_AUTH_ENCRYPTION_KEY=$(openssl rand -hex 32)

# Run database migrations
./alexander-migrate up

# Start the server
./alexander-server
```

### Using aws-cli

Once running, configure aws-cli to use Alexander:

```bash
aws configure set aws_access_key_id YOUR_ACCESS_KEY
aws configure set aws_secret_access_key YOUR_SECRET_KEY
aws configure set default.region us-east-1

# Create a bucket
aws --endpoint-url http://localhost:9000 s3 mb s3://my-bucket

# Upload a file
aws --endpoint-url http://localhost:9000 s3 cp myfile.txt s3://my-bucket/

# List objects
aws --endpoint-url http://localhost:9000 s3 ls s3://my-bucket/
```

---

## Installation

### Quick Install (Recommended)

#### Linux / macOS

```bash
# One-line installer (as root/sudo)
curl -fsSL https://raw.githubusercontent.com/neuralforgeone/alexander-storage/main/scripts/install.sh | sudo bash
```

This will:
- Download the latest release
- Install binaries to `/usr/local/bin`
- Create configuration in `/etc/alexander`
- Set up systemd/launchd service
- Generate master keys and initial admin user
- Create S3 access keys

#### Windows (PowerShell as Administrator)

```powershell
# Option 1: One-line installer (download and run)
irm https://raw.githubusercontent.com/neuralforgeone/alexander-storage/main/scripts/install.ps1 | iex

# Option 2: If you downloaded the release, run with bypass
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

> **Note**: If you get a "script is not digitally signed" error, use Option 2 with `-ExecutionPolicy Bypass`.

This will:
- Download the latest release
- Install binaries to `C:\Program Files\Alexander`
- Create configuration in `C:\ProgramData\Alexander`
- Set up Windows service
- Configure firewall rules
- Generate master keys and initial admin user

### Manual Installation

#### Prerequisites

- Go 1.21 or later (for building from source)
- PostgreSQL 14 or later (for production mode)
- Redis 7 or later (optional, for distributed deployments)

#### Download Release

```bash
# Linux amd64
curl -LO https://github.com/neuralforgeone/alexander-storage/releases/latest/download/alexander-linux-amd64.tar.gz
tar -xzf alexander-linux-amd64.tar.gz
sudo mv alexander-* /usr/local/bin/

# macOS arm64 (Apple Silicon)
curl -LO https://github.com/neuralforgeone/alexander-storage/releases/latest/download/alexander-darwin-arm64.tar.gz
tar -xzf alexander-darwin-arm64.tar.gz
sudo mv alexander-* /usr/local/bin/
```

#### Build from Source

```bash
# Clone the repository
git clone https://github.com/neuralforgeone/alexander-storage.git
cd alexander-storage

# Download dependencies
go mod download

# Build all binaries
make build

# The binaries will be in ./bin/
ls -la bin/
# alexander-server   - Main storage server
# alexander-admin    - Admin CLI tool
# alexander-migrate  - Database migration tool
```

### Docker

```bash
# Using Docker Compose (recommended)
curl -LO https://raw.githubusercontent.com/neuralforgeone/alexander-storage/main/configs/docker-compose.yaml
docker-compose up -d

# Or run standalone
docker run -d \
  -p 8080:8080 \
  -e ALEXANDER_AUTH_MASTER_KEY=$(openssl rand -hex 32) \
  -v alexander_data:/var/lib/alexander \
  ghcr.io/neuralforgeone/alexander-storage:latest
```

### Uninstall

```bash
# Linux/macOS
curl -fsSL https://raw.githubusercontent.com/neuralforgeone/alexander-storage/main/scripts/uninstall.sh | sudo bash

# Windows (PowerShell as Administrator)
irm https://raw.githubusercontent.com/neuralforgeone/alexander-storage/main/scripts/uninstall.ps1 | iex

# Or if downloaded locally:
powershell -ExecutionPolicy Bypass -File .\uninstall.ps1
```

---

## Configuration

Alexander can be configured via YAML file or environment variables.

### Configuration File

Copy the example configuration:

```bash
cp configs/config.yaml.example configs/config.yaml
```

### Environment Variables

All configuration options can be set via environment variables with the `ALEXANDER_` prefix:

| Variable | Description | Default |
|----------|-------------|---------|
| `ALEXANDER_SERVER_HOST` | Server bind address | `0.0.0.0` |
| `ALEXANDER_SERVER_PORT` | Server port | `9000` |
| `ALEXANDER_DATABASE_HOST` | PostgreSQL host | `localhost` |
| `ALEXANDER_DATABASE_PORT` | PostgreSQL port | `5432` |
| `ALEXANDER_DATABASE_USER` | PostgreSQL user | `alexander` |
| `ALEXANDER_DATABASE_PASSWORD` | PostgreSQL password | (required) |
| `ALEXANDER_DATABASE_DATABASE` | Database name | `alexander` |
| `ALEXANDER_REDIS_HOST` | Redis host | `localhost` |
| `ALEXANDER_REDIS_PORT` | Redis port | `6379` |
| `ALEXANDER_REDIS_ENABLED` | Enable Redis caching | `true` |
| `ALEXANDER_AUTH_ENCRYPTION_KEY` | 32-byte hex key for AES-256 | (required) |
| `ALEXANDER_AUTH_REGION` | Default AWS region | `us-east-1` |
| `ALEXANDER_STORAGE_DATA_DIR` | Blob storage directory | `/data` |

### Generate Encryption Key

The encryption key must be exactly 32 bytes (64 hex characters):

```bash
openssl rand -hex 32
```

---

## Usage

### Create Access Keys

Use the admin CLI to manage users and access keys:

```bash
# Create a new user
./alexander-admin user create --username myuser --email user@example.com

# Create an access key for the user
./alexander-admin accesskey create --username myuser

# Output:
# Access Key ID: AKIAIOSFODNN7EXAMPLE
# Secret Key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
# 
# IMPORTANT: Save these credentials. The secret key cannot be retrieved later.
```

### Bucket Operations

```bash
# Create bucket
aws --endpoint-url http://localhost:9000 s3 mb s3://my-bucket

# List buckets
aws --endpoint-url http://localhost:9000 s3 ls

# Delete bucket
aws --endpoint-url http://localhost:9000 s3 rb s3://my-bucket
```

### Object Operations

```bash
# Upload file
aws --endpoint-url http://localhost:9000 s3 cp file.txt s3://my-bucket/

# Download file
aws --endpoint-url http://localhost:9000 s3 cp s3://my-bucket/file.txt ./

# List objects
aws --endpoint-url http://localhost:9000 s3 ls s3://my-bucket/

# Delete object
aws --endpoint-url http://localhost:9000 s3 rm s3://my-bucket/file.txt
```

### Presigned URLs

Generate presigned URLs for temporary access:

```bash
# Generate a presigned URL valid for 1 hour
aws --endpoint-url http://localhost:9000 s3 presign s3://my-bucket/file.txt --expires-in 3600
```

### Versioning

```bash
# Enable versioning on a bucket
aws --endpoint-url http://localhost:9000 s3api put-bucket-versioning \
  --bucket my-bucket \
  --versioning-configuration Status=Enabled

# List object versions
aws --endpoint-url http://localhost:9000 s3api list-object-versions --bucket my-bucket
```

### Multipart Uploads

```bash
# Upload large file (aws-cli handles multipart automatically for files > 8MB)
aws --endpoint-url http://localhost:9000 s3 cp large-file.zip s3://my-bucket/

# Manual multipart upload
aws --endpoint-url http://localhost:9000 s3api create-multipart-upload \
  --bucket my-bucket --key large-file.zip
```

---

## Web Dashboard

Alexander includes a built-in web dashboard for easy management:

- **URL**: `http://localhost:9000/dashboard`
- **Features**:
  - Bucket management (create, delete, configure)
  - Object browsing and management
  - User and access key management
  - Lifecycle rule configuration
  - Real-time metrics overview

### Dashboard Authentication

The dashboard uses session-based authentication. Create a dashboard user:

```bash
./alexander-admin user create --username admin --email admin@example.com --dashboard
```

---

## API Compatibility

Alexander implements the following S3 API operations:

### Bucket Operations

| Operation | Status |
|-----------|--------|
| CreateBucket | ✅ Implemented |
| DeleteBucket | ✅ Implemented |
| ListBuckets | ✅ Implemented |
| HeadBucket | ✅ Implemented |
| GetBucketVersioning | ✅ Implemented |
| PutBucketVersioning | ✅ Implemented |

### Object Operations

| Operation | Status |
|-----------|--------|
| PutObject | ✅ Implemented |
| GetObject | ✅ Implemented |
| HeadObject | ✅ Implemented |
| DeleteObject | ✅ Implemented |
| ListObjects (v1) | ✅ Implemented |
| ListObjectsV2 | ✅ Implemented |
| CopyObject | ✅ Implemented |
| ListObjectVersions | ✅ Implemented |

### Multipart Upload

| Operation | Status |
|-----------|--------|
| CreateMultipartUpload | ✅ Implemented |
| UploadPart | ✅ Implemented |
| CompleteMultipartUpload | ✅ Implemented |
| AbortMultipartUpload | ✅ Implemented |
| ListMultipartUploads | ✅ Implemented |
| ListParts | ✅ Implemented |

### Additional Features

| Feature | Status |
|---------|--------|
| Presigned URLs | ✅ Implemented |
| Server-Side Encryption (SSE-S3) | ✅ Implemented |
| Object Lifecycle Rules | ✅ Implemented |
| Bucket ACL | ✅ Implemented |
| Web Dashboard | ✅ Implemented |

---

## Development

### Project Structure

```
alexander-storage/
├── cmd/
│   ├── alexander-server/     # Main server entry point
│   ├── alexander-admin/      # Admin CLI (user/key management, encryption)
│   └── alexander-migrate/    # Database migration tool
├── internal/
│   ├── auth/                 # AWS v4 signature authentication
│   ├── cache/
│   │   ├── memory/           # In-memory cache (single-node)
│   │   └── redis/            # Redis caching layer (distributed)
│   ├── cluster/              # Multi-node gRPC communication
│   ├── config/               # Configuration management
│   ├── delta/                # CDC chunking and delta versioning
│   ├── domain/               # Domain models
│   ├── handler/              # HTTP handlers and web dashboard
│   │   └── templates/        # HTMX dashboard templates
│   ├── lock/                 # Distributed and memory locking
│   ├── metrics/              # Prometheus metrics
│   ├── middleware/           # Rate limiting, tracing, CSRF
│   ├── migration/            # Background migration system
│   ├── pkg/crypto/           # Cryptographic utilities (AES, ChaCha20)
│   ├── repository/
│   │   ├── postgres/         # PostgreSQL implementation
│   │   └── sqlite/           # SQLite implementation
│   ├── service/              # Business logic
│   ├── storage/
│   │   └── filesystem/       # CAS filesystem backend
│   └── tiering/              # Automatic data tiering
├── migrations/
│   └── postgres/             # SQL migrations
├── configs/                  # Configuration examples
├── deploy/
│   ├── kubernetes/           # Kubernetes manifests
│   ├── helm/                 # Helm chart
│   └── terraform/            # Terraform modules
├── monitoring/
│   ├── grafana/              # Grafana dashboards
│   └── prometheus/           # Prometheus alerts
├── docs/
│   ├── api/                  # OpenAPI specification
│   ├── guides/               # User guides
│   └── operations/           # Operations documentation
└── tests/
    ├── integration/          # End-to-end tests
    ├── load/                 # Performance benchmarks
    └── compatibility/        # S3 SDK compatibility tests
```

### Running Tests

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run linter
make lint
```

### Building

```bash
# Build all binaries
make build

# Build Docker image
make docker-build

# Run locally
make run
```

---

## Documentation

Comprehensive documentation is available in the `docs/` directory:

| Document | Description |
|----------|-------------|
| [Quick Start Guide](docs/guides/quickstart.md) | Get running in 5 minutes |
| [Production Deployment](docs/guides/production.md) | Production best practices |
| [Performance Tuning](docs/operations/performance-tuning.md) | Optimization guide |
| [Backup & Recovery](docs/operations/backup-dr.md) | Disaster recovery procedures |
| [Troubleshooting](docs/guides/troubleshooting.md) | Common issues and solutions |
| [API Reference](docs/api/openapi.yaml) | OpenAPI 3.0 specification |
| [Runbooks](docs/operations/runbooks.md) | Operational procedures |

### Deployment Resources

| Resource | Description |
|----------|-------------|
| [Kubernetes](deploy/kubernetes/) | Kubernetes manifests |
| [Helm Chart](deploy/helm/alexander/) | Helm chart for Kubernetes |
| [Terraform](deploy/terraform/) | Infrastructure as code modules |
| [Grafana Dashboards](monitoring/grafana/) | Pre-built monitoring dashboards |
| [Prometheus Alerts](monitoring/prometheus/) | Alerting rules |

---

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Quick Links

- [Report a Bug](https://github.com/neuralforgeone/alexander-storage/issues/new?template=bug_report.md)
- [Request a Feature](https://github.com/neuralforgeone/alexander-storage/issues/new?template=feature_request.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Security Policy](SECURITY.md)

---

## License

Alexander Storage is licensed under the [Apache License 2.0](LICENSE).

```
Copyright 2025 Alexander Storage Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
```

---

## Acknowledgments

- Inspired by [MinIO](https://github.com/minio/minio) for S3 API patterns
- Built with [chi](https://github.com/go-chi/chi) router
- Uses [pgx](https://github.com/jackc/pgx) PostgreSQL driver
- Logging powered by [zerolog](https://github.com/rs/zerolog)
- SQLite support via [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)
- Dashboard built with [HTMX](https://htmx.org/) and [Tailwind CSS](https://tailwindcss.com/)
