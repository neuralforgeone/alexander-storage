# Quick start

Get Alexander running and put a file in a bucket in a few minutes.

## What you will use

1. **Server** — `alexander-server`  
2. **Admin CLI** — `alexander-admin` (users + S3 keys)  
3. **Web console** — browser at `/dashboard`  
4. Optional — `mc` (MinIO Client) or `aws` CLI for S3  

Download binaries from [Releases](https://github.com/neuralforgeone/alexander-storage/releases), use the [install scripts](../../scripts/), or build with `make build`.

---

## Path A — Single machine (recommended first)

No PostgreSQL or Redis. SQLite + local disk.

### 1. Prepare directories and config

```bash
mkdir -p data/objects data/temp
cp configs/config.embedded.yaml.example config.yaml
```

### 2. Encryption key (required)

Must be **exactly 32 characters**:

```bash
export ALEXANDER_AUTH_ENCRYPTION_KEY="$(openssl rand -hex 16)"
# optional: write the same string into config.yaml → auth.encryption_key
```

### 3. Start

```bash
./alexander-server
```

Default embedded example listens on **port 8080** (see `config.yaml`).

Check:

```bash
curl -s http://127.0.0.1:8080/healthz
# {"status":"healthy"}
```

### 4. Admin user (web console)

```bash
./alexander-admin user create --username admin --email admin@example.com --admin
```

Copy the one-time password.

### 5. Web console

Open: **http://127.0.0.1:8080/dashboard**

- Sign in as `admin`  
- Create a bucket  
- Upload a file  
- Open it to preview or download  

Only **admin** users can sign in to the console.

### 6. S3 access keys (apps / CLI)

```bash
./alexander-admin accesskey create --user-id 1
```

Save Access Key ID and Secret.

**MinIO Client:**

```bash
mc alias set alexander http://127.0.0.1:8080 "$ACCESS_KEY" "$SECRET_KEY" --api S3v4 --path on
mc mb alexander/demo
mc cp ./hello.txt alexander/demo/
mc ls alexander/demo/
```

**AWS CLI:**

```bash
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
export AWS_DEFAULT_REGION=us-east-1

aws --endpoint-url http://127.0.0.1:8080 s3 mb s3://demo
aws --endpoint-url http://127.0.0.1:8080 s3 cp ./hello.txt s3://demo/
aws --endpoint-url http://127.0.0.1:8080 s3 ls s3://demo/
```

---

## Path B — Install script

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/neuralforgeone/alexander-storage/main/scripts/install.sh | sudo bash
```

### Windows (Administrator PowerShell)

```powershell
irm https://raw.githubusercontent.com/neuralforgeone/alexander-storage/main/scripts/install.ps1 | iex
```

Then open the console URL printed by the installer (or `http://localhost:8080/dashboard`) and create keys with `alexander-admin` if needed.

---

## Path C — Docker (quick)

```bash
export ALEXANDER_AUTH_ENCRYPTION_KEY="$(openssl rand -hex 16)"

docker run -d --name alexander \
  -p 8080:8080 \
  -e ALEXANDER_AUTH_ENCRYPTION_KEY="$ALEXANDER_AUTH_ENCRYPTION_KEY" \
  -v alexander_data:/data \
  ghcr.io/neuralforgeone/alexander-storage:latest
```

Create the admin user and access keys with `alexander-admin` against the same data volume/config as the container (or an exec into the image if the CLI is included). Prefer Path A if you want the fewest moving parts.

For Postgres + Redis, see [`configs/docker-compose.yaml`](../../configs/docker-compose.yaml) and [production.md](production.md).

---

## Mental model

| Client | URL | Auth |
|--------|-----|------|
| Browser console | `http://HOST:PORT/dashboard` | Username / password (admin) |
| S3 tools & SDKs | `http://HOST:PORT` | Access key + secret (SigV4) |
| Load balancers | `http://HOST:PORT/healthz` | None |

---

## Next

- Full usage overview: [README](../../README.md)  
- Production checklist: [production.md](production.md)  
- Common failures: [troubleshooting.md](troubleshooting.md)  
- API sketch: [openapi.yaml](../api/openapi.yaml)  
