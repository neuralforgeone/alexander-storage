# Alexander Storage docs

Self-hosted **S3-compatible** object storage with a **web console**.

## Start here

| Guide | When |
|-------|------|
| [Quick start](guides/quickstart.md) | First install, first bucket, first upload |
| [Main README](../README.md) | Everyday use: console, `mc`, AWS CLI, config |
| [Production](guides/production.md) | Deploy for real traffic and retention |
| [Troubleshooting](guides/troubleshooting.md) | Login, signatures, keys, health |

## Operate

| Guide | Topic |
|-------|--------|
| [Backup & DR](operations/backup-dr.md) | Metadata + blob backup |
| [Performance](operations/performance-tuning.md) | Capacity and tuning |
| [Runbooks](operations/runbooks.md) | Incident-style procedures |

## Reference

| Doc | Content |
|-----|---------|
| [OpenAPI](api/openapi.yaml) | HTTP / S3-style operations |

## How people usually use it

1. Run **embedded mode** (SQLite + disk) or a packaged install.  
2. Open **`/dashboard`**, sign in as admin, create a bucket, upload files.  
3. Create **access keys** and point apps at the same host with `aws` / `mc` / SDKs.

S3 lives at the server root; the console is only under `/dashboard`.
