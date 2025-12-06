# Alexander Storage Quick Start Guide

Get Alexander Storage running in 5 minutes.

## Option 1: Docker (Recommended)

```bash
# Generate master key
MASTER_KEY=$(openssl rand -hex 32)

# Run Alexander Storage
docker run -d \
  --name alexander \
  -p 8080:8080 \
  -p 9091:9091 \
  -v alexander_data:/var/lib/alexander \
  -e ALEXANDER_AUTH_MASTER_KEY=$MASTER_KEY \
  ghcr.io/neuralforgeone/alexander-storage:latest
```

## Option 2: Install Script

### Linux/macOS

```bash
curl -fsSL https://raw.githubusercontent.com/neuralforgeone/alexander-storage/main/scripts/install.sh | sudo bash
```

### Windows (PowerShell as Administrator)

```powershell
irm https://raw.githubusercontent.com/neuralforgeone/alexander-storage/main/scripts/install.ps1 | iex
```

## Option 3: Binary Download

1. Download from [GitHub Releases](https://github.com/neuralforgeone/alexander-storage/releases)
2. Extract and run:

```bash
# Generate config
./alexander-server --init-config

# Edit config.yaml to set master_key

# Run server
./alexander-server --config config.yaml
```

## Create Your First User

```bash
# Using Admin CLI
./alexander-admin user create --username admin --email admin@example.com
# Note the generated password

# Create access key
./alexander-admin accesskey create --user-id 1
# Note the access_key_id and secret_access_key
```

## Test with AWS CLI

```bash
# Configure aws-cli
aws configure --profile alexander
# AWS Access Key ID: <access_key_id from above>
# AWS Secret Access Key: <secret_access_key from above>
# Default region: us-east-1
# Default output format: json

# Create a bucket
aws --endpoint-url http://localhost:8080 --profile alexander s3 mb s3://my-bucket

# Upload a file
aws --endpoint-url http://localhost:8080 --profile alexander s3 cp README.md s3://my-bucket/

# List files
aws --endpoint-url http://localhost:8080 --profile alexander s3 ls s3://my-bucket/

# Download file
aws --endpoint-url http://localhost:8080 --profile alexander s3 cp s3://my-bucket/README.md downloaded.md
```

## Access Dashboard

Open http://localhost:8080/dashboard in your browser and login with the credentials created above.

## Next Steps

- [Production Deployment Guide](production.md)
- [Configuration Reference](../config/README.md)
- [API Reference](../api/openapi.yaml)
