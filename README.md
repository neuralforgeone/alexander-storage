# Alexander Storage

[![CI](https://github.com/neuralforgeone/alexander-storage/actions/workflows/ci.yml/badge.svg)](https://github.com/neuralforgeone/alexander-storage/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

**Self-hosted S3-compatible object storage** — run it on your machine or server, talk to it with the tools you already use (`aws` CLI, MinIO Client `mc`, boto3, Terraform), and manage files from a built-in web console.

Works for backups, media libraries, app attachments, homelabs, and private “S3” without an AWS account.

---

## What you get

| | |
|---|---|
| **S3 API** | Buckets, objects, list, multipart, versioning — path-style endpoint |
| **Web console** | Browse buckets, upload, preview text/images, download — `/dashboard` |
| **Simple mode** | One binary + SQLite + local disk (no Postgres/Redis required) |
| **Production mode** | PostgreSQL metadata, optional Redis, metrics, K8s-friendly health checks |
| **Dedup** | Content-addressable storage: identical blobs stored once |

---

## 5-minute start (embedded mode)

You need the `alexander-server` and `alexander-admin` binaries ([releases](https://github.com/neuralforgeone/alexander-storage/releases) or `make build`).

### 1. Config

```bash
mkdir -p data/objects data/temp
cp configs/config.embedded.yaml.example config.yaml
```

Set a **32-character** encryption key (required). Example:

```bash
# 32 hex characters
export ALEXANDER_AUTH_ENCRYPTION_KEY="$(openssl rand -hex 16)"
```

Or put the same value under `auth.encryption_key` in `config.yaml`.

### 2. Start the server

```bash
# Loads ./config.yaml or ./configs/config.yaml
./alexander-server
```

You should see something like:

- API / console: `http://127.0.0.1:8080` (port from config)
- Health: `http://127.0.0.1:8080/healthz`

### 3. Create an admin user

```bash
./alexander-admin user create --username admin --email admin@example.com --admin
```

**Save the printed password** — it is shown only once.

### 4. Open the web console

1. Go to **http://localhost:8080/dashboard**
2. Sign in with `admin` and the password from step 3
3. Create a bucket, upload a file, open it to preview or download

Console login is **admin-only**. Day-to-day object access for apps still uses S3 access keys (next section).

### 5. Use S3 clients (optional but usual)

Create an access key for API tools:

```bash
./alexander-admin accesskey create --user-id 1
```

Save **Access Key ID** and **Secret Access Key**.

#### MinIO Client (`mc`)

```bash
mc alias set alexander http://127.0.0.1:8080 ACCESS_KEY SECRET_KEY --api S3v4 --path on

mc mb alexander/photos
mc cp ./vacation.jpg alexander/photos/
mc ls alexander/photos/
mc cat alexander/photos/vacation.jpg
mc rm alexander/photos/vacation.jpg
```

#### AWS CLI

```bash
export AWS_ACCESS_KEY_ID=ACCESS_KEY
export AWS_SECRET_ACCESS_KEY=SECRET_KEY
export AWS_DEFAULT_REGION=us-east-1

aws --endpoint-url http://127.0.0.1:8080 s3 mb s3://photos
aws --endpoint-url http://127.0.0.1:8080 s3 cp ./file.txt s3://photos/
aws --endpoint-url http://127.0.0.1:8080 s3 ls s3://photos/
aws --endpoint-url http://127.0.0.1:8080 s3 cp s3://photos/file.txt ./file-downloaded.txt
```

Use the **same host and port** as the server. Region is typically `us-east-1` (configurable).

---

## Two ways to use Alexander

```text
  Browser  ──►  /dashboard     session login (admin)
  Apps/CLI ──►  /              S3 API + AWS Signature V4
```

| Goal | How |
|------|-----|
| Click around, upload, preview | Web console at `/dashboard` |
| App backups, scripts, SDKs | S3 endpoint + access keys |
| Check if the process is up | `GET /healthz` (no auth) |
| Deep readiness | `GET /health` or `GET /readyz` |

---

## Install options

### Binary release

Download from [GitHub Releases](https://github.com/neuralforgeone/alexander-storage/releases), extract `alexander-server` and `alexander-admin`, then follow [5-minute start](#5-minute-start-embedded-mode).

### Install script (Linux / macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/neuralforgeone/alexander-storage/main/scripts/install.sh | sudo bash
```

### Windows (PowerShell as Administrator)

```powershell
irm https://raw.githubusercontent.com/neuralforgeone/alexander-storage/main/scripts/install.ps1 | iex
```

### Docker

```bash
# Encryption key: exactly 32 characters for this build
export ALEXANDER_AUTH_ENCRYPTION_KEY="$(openssl rand -hex 16)"

docker run -d --name alexander \
  -p 8080:8080 \
  -e ALEXANDER_AUTH_ENCRYPTION_KEY="$ALEXANDER_AUTH_ENCRYPTION_KEY" \
  -v alexander_data:/data \
  ghcr.io/neuralforgeone/alexander-storage:latest
```

Compose (Postgres + Redis stack) lives at [`configs/docker-compose.yaml`](configs/docker-compose.yaml). Prefer **embedded config** for a first try; use compose when you want a multi-service layout.

---

## Everyday operations

### Users and keys

```bash
# Admin for web console
./alexander-admin user create --username admin --email admin@example.com --admin

# Extra user (API-oriented; dashboard still needs admin)
./alexander-admin user create --username app --email app@example.com

# S3 credentials
./alexander-admin accesskey create --user-id 1
./alexander-admin accesskey list --user-id 1
```

### Web console features

- Create and open buckets  
- Browse “folders” (key prefixes)  
- Upload from the browser  
- Preview text and images; download any object  
- Adjust bucket ACL  
- Lifecycle expire rules  
- User directory (admins)  

### S3-compatible surface (high level)

Supported for real client use: create/list/delete buckets; put/get/head/delete/copy objects; ListObjects / ListObjectsV2; multipart upload; versioning; multi-object delete; health endpoints.

Not a full clone of every AWS S3 feature (policies, notifications, Select, etc. may be missing or partial). If a client fails, check path-style addressing and SigV4 region.

### Configuration you care about

| Need | Setting |
|------|---------|
| Listen port | `server.port` or `ALEXANDER_SERVER_PORT` (example config often uses `8080`) |
| Encryption of stored secrets | `auth.encryption_key` / `ALEXANDER_AUTH_ENCRYPTION_KEY` (**exactly 32 characters**) |
| SQLite path | `database.driver: sqlite`, `database.path` |
| Object files on disk | `storage.data_dir`, `storage.temp_dir` |
| S3 region string | `auth.region` (default `us-east-1`) |
| Metrics | `metrics.enabled`, `metrics.port` (often `9091`) |

Examples:

- Single node: [`configs/config.embedded.yaml.example`](configs/config.embedded.yaml.example)  
- Full / Postgres-oriented: [`configs/config.yaml.example`](configs/config.yaml.example)  

Environment variables use the `ALEXANDER_` prefix and nested keys with `_` (for example `ALEXANDER_SERVER_PORT`).

---

## Production notes (short)

- Prefer PostgreSQL for multi-writer / larger deployments; keep Redis optional.  
- Put TLS in front (reverse proxy or enable TLS in config when you configure certs).  
- Back up both **metadata DB** and **blob data directory**.  
- Use `/healthz` and `/readyz` for orchestrators; scrape Prometheus metrics when enabled.  

Longer guides: [docs/guides/production.md](docs/guides/production.md), [docs/operations/](docs/operations/).

---

## Troubleshooting

| Symptom | What to check |
|---------|----------------|
| `auth.encryption_key must be exactly 32 characters` | Key length is 32 chars (e.g. `openssl rand -hex 16`), not 64 |
| `SignatureDoesNotMatch` | Correct access key/secret, path-style (`--path on` for `mc`), region matches server |
| Console login fails | User must be **admin** and active; use password from `user create` |
| Empty object / garbage on download with old builds | Update server (streaming/chunked uploads need a current build) |
| Port busy | Change `server.port` or free the port |

More: [docs/guides/troubleshooting.md](docs/guides/troubleshooting.md).

---

## Documentation

| Doc | For |
|-----|-----|
| [Quick start](docs/guides/quickstart.md) | Short install paths |
| [Production](docs/guides/production.md) | Deploying for real traffic |
| [OpenAPI](docs/api/openapi.yaml) | S3-style HTTP surface |
| [Operations](docs/operations/) | Backup, performance, runbooks |
| [Contributing](CONTRIBUTING.md) | Building and contributing code |

---

## License

Apache License 2.0 — see [LICENSE](LICENSE).
