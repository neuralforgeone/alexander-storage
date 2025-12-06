# Troubleshooting Guide

Common issues and solutions for Alexander Storage.

## Table of Contents

1. [Connection Issues](#connection-issues)
2. [Authentication Errors](#authentication-errors)
3. [Storage Errors](#storage-errors)
4. [Performance Issues](#performance-issues)
5. [Database Issues](#database-issues)
6. [Debugging Tips](#debugging-tips)

## Connection Issues

### Cannot connect to server

**Symptoms:**
- `connection refused` errors
- Timeout when accessing endpoints

**Possible Causes & Solutions:**

1. **Server not running**
   ```bash
   # Check if server is running
   ps aux | grep alexander
   
   # Check logs
   journalctl -u alexander -f
   ```

2. **Wrong port or host**
   ```bash
   # Verify server configuration
   grep -E "host|port" /etc/alexander/config.yaml
   
   # Test connection
   curl -v http://localhost:8080/healthz
   ```

3. **Firewall blocking**
   ```bash
   # Check firewall rules
   ufw status
   iptables -L -n
   
   # Allow port
   ufw allow 8080/tcp
   ```

### TLS/SSL errors

**Symptoms:**
- `certificate verify failed`
- `SSL handshake failure`

**Solutions:**

1. **Self-signed certificate**
   ```bash
   # For aws-cli, disable cert verification (not for production!)
   aws --no-verify-ssl --endpoint-url https://... s3 ls
   
   # Or add CA to trust store
   ```

2. **Certificate expired**
   ```bash
   # Check certificate expiry
   openssl s_client -connect server:443 2>/dev/null | openssl x509 -noout -dates
   ```

## Authentication Errors

### SignatureDoesNotMatch

**Symptoms:**
```xml
<Error>
  <Code>SignatureDoesNotMatch</Code>
  <Message>The request signature we calculated does not match...</Message>
</Error>
```

**Possible Causes & Solutions:**

1. **Wrong secret key**
   - Verify the secret key matches what was created
   - Ensure no extra whitespace in credentials

2. **Clock skew**
   ```bash
   # Check server time
   date
   
   # Sync time
   timedatectl set-ntp true
   ```
   AWS Signature V4 requires time within 15 minutes of server time.

3. **Incorrect region**
   ```bash
   # Ensure region matches (default: us-east-1)
   aws configure set region us-east-1 --profile alexander
   ```

### InvalidAccessKeyId

**Symptoms:**
```xml
<Error>
  <Code>InvalidAccessKeyId</Code>
</Error>
```

**Solutions:**

1. **Access key doesn't exist**
   ```bash
   # List access keys
   ./alexander-admin accesskey list
   ```

2. **Access key revoked**
   ```bash
   # Check if active
   ./alexander-admin accesskey list --user-id 1
   ```

### AccessDenied

**Symptoms:**
```xml
<Error>
  <Code>AccessDenied</Code>
</Error>
```

**Solutions:**

1. **Bucket ACL restricts access**
   ```bash
   # Check bucket ACL
   aws --endpoint-url http://localhost:8080 s3api get-bucket-acl --bucket mybucket
   ```

2. **User doesn't own the bucket**
   - Verify bucket ownership
   - Check user permissions

## Storage Errors

### NoSuchBucket

**Symptoms:**
```xml
<Error>
  <Code>NoSuchBucket</Code>
</Error>
```

**Solutions:**
```bash
# List all buckets
aws --endpoint-url http://localhost:8080 s3 ls

# Create bucket if missing
aws --endpoint-url http://localhost:8080 s3 mb s3://mybucket
```

### NoSuchKey

**Symptoms:**
```xml
<Error>
  <Code>NoSuchKey</Code>
</Error>
```

**Solutions:**
```bash
# List objects in bucket
aws --endpoint-url http://localhost:8080 s3 ls s3://mybucket/

# Check exact key name (case-sensitive)
```

### Storage full

**Symptoms:**
- Upload failures
- `no space left on device` in logs

**Solutions:**
```bash
# Check disk space
df -h /var/lib/alexander

# Run garbage collection
./alexander-admin gc run

# Clean up old versions if versioning enabled
```

## Performance Issues

### Slow uploads

**Possible Causes & Solutions:**

1. **Disk I/O bottleneck**
   ```bash
   # Monitor I/O
   iostat -x 1
   
   # Consider SSD for metadata
   ```

2. **Single-threaded uploads**
   ```bash
   # Use multipart uploads for large files
   # aws-cli does this automatically for files >8MB
   ```

3. **Network bottleneck**
   ```bash
   # Test network throughput
   iperf3 -c server
   ```

### High latency

**Possible Causes & Solutions:**

1. **Database slow**
   ```bash
   # For SQLite, check WAL size
   ls -la /var/lib/alexander/*.db*
   
   # Checkpoint WAL
   sqlite3 /var/lib/alexander/alexander.db "PRAGMA wal_checkpoint(TRUNCATE);"
   ```

2. **Too many concurrent requests**
   - Adjust rate limiting
   - Scale horizontally

### Memory usage growing

**Possible Causes & Solutions:**

1. **Large request bodies buffered**
   - Check for streaming issues in logs
   - Monitor with `top` or `htop`

2. **Cache not expiring**
   - Verify cache TTL settings
   - Restart if necessary

## Database Issues

### SQLite: database is locked

**Symptoms:**
```
database is locked
```

**Solutions:**

1. **Multiple writers**
   - SQLite only supports one writer
   - Ensure only one instance is running
   ```bash
   ps aux | grep alexander
   ```

2. **Long-running transaction**
   - Check for stuck processes
   - Increase busy_timeout:
   ```yaml
   database:
     busy_timeout: 10000  # 10 seconds
   ```

### PostgreSQL: connection refused

**Solutions:**

1. **PostgreSQL not running**
   ```bash
   systemctl status postgresql
   ```

2. **Wrong host/port**
   ```bash
   # Test connection
   psql -h postgres-host -U alexander -d alexander
   ```

3. **pg_hba.conf restrictions**
   - Add entry for Alexander server IP

### Migration failed

**Solutions:**
```bash
# Check migration status
./alexander-migrate status

# Retry migration
./alexander-migrate up

# Manual intervention if needed
./alexander-migrate down 1  # Rollback one step
./alexander-migrate up
```

## Debugging Tips

### Enable debug logging

```yaml
log:
  level: debug
  format: json
```

### View logs

```bash
# Systemd
journalctl -u alexander -f

# Docker
docker logs -f alexander

# File
tail -f /var/log/alexander/server.log
```

### Request tracing

Each request has a unique ID in response headers:

```bash
curl -v http://localhost:8080/healthz 2>&1 | grep x-amz-request-id
```

Search logs for this ID to trace the request.

### Health check details

```bash
# Full health check
curl http://localhost:8080/health | jq

# Output:
# {
#   "status": "healthy",
#   "components": {
#     "database": {"status": "healthy", "latency_ms": 1},
#     "storage": {"status": "healthy", "latency_ms": 2}
#   }
# }
```

### Common log messages

| Message | Meaning | Action |
|---------|---------|--------|
| `signature mismatch` | Auth failed | Check credentials |
| `bucket not found` | 404 for bucket | Create bucket |
| `blob not found` | Missing data | Check storage path |
| `rate limited` | Too many requests | Reduce request rate |
| `gc: deleted N blobs` | Normal GC | No action needed |

### Getting help

1. Check [GitHub Issues](https://github.com/neuralforgeone/alexander-storage/issues)
2. Search closed issues for similar problems
3. Open new issue with:
   - Alexander version
   - Configuration (redact secrets!)
   - Error messages
   - Steps to reproduce
