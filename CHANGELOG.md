# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.1.0] - 2026-08-11

### Added

- **Web console** at `/dashboard`: session login (admin), bucket browser, object list by prefix, upload, text/image preview, download, ACL and lifecycle management, user directory
- CSRF protection for console forms (including multipart upload)
- SQLite and PostgreSQL wiring for **sessions** and **lifecycle** repositories
- S3 **multi-object delete** (`POST /{bucket}?delete=`) for MinIO Client / AWS CLI `rm`
- **AWS chunked** body decoding for streaming SigV4 uploads (`STREAMING-AWS4-HMAC-SHA256-PAYLOAD`)
- Operator-focused README and quickstart (install, console, `mc` / AWS CLI)

### Fixed

- CI/toolchain alignment with `go 1.25` (lint + tests + Docker)
- SigV4 verification using `r.Host` (fixes `SignatureDoesNotMatch` for real clients)
- CreateBucket default ACL `private` (SQLite CHECK constraint)
- Cluster gRPC `NotFound` mapped to domain blob not found

### Changed

- golangci-lint v2 config and action v9
- Docker builder image `golang:1.26-alpine`
- GitHub Actions: `setup-go@v6`, `upload-artifact@v7`
- Dependency bumps:
  - `aws-sdk-go-v2` → v1.41.0
  - `aws-sdk-go-v2/config` → v1.32.5
  - `aws-sdk-go-v2/credentials` → v1.19.5
  - `aws-sdk-go-v2/service/s3` → v1.93.2
  - `golang.org/x/crypto` → v0.55.0

## [2.0.0] - 2025-12-06

Production readiness release (Fusion Engine surfaces, deploy assets, security hardening). See git history for `v2.0.0`.

## [1.0.0] - 2025-12-06

Initial tagged release.

---

## Versioning

- **MAJOR**: incompatible API or behavior changes
- **MINOR**: new functionality, backwards compatible
- **PATCH**: bug fixes, backwards compatible
