# Production Deployment Guide

Best practices and recommendations for deploying Alexander Storage in production.

## Table of Contents

1. [Prerequisites](#prerequisites)
2. [Architecture Options](#architecture-options)
3. [Security Configuration](#security-configuration)
4. [High Availability](#high-availability)
5. [Monitoring & Alerting](#monitoring--alerting)
6. [Backup & Recovery](#backup--recovery)
7. [Performance Tuning](#performance-tuning)

## Prerequisites

- Linux server (Ubuntu 20.04+, RHEL 8+, or equivalent)
- Minimum 2 CPU cores, 4GB RAM
- SSD storage recommended for metadata (SQLite/PostgreSQL)
- Sufficient storage for blob data
- TLS certificate for HTTPS

## Architecture Options

### Single-Node (Simple)

Best for: Small deployments, homelabs, development

```
┌─────────────────────────────────────┐
│         Alexander Server            │
│  ┌─────────┐  ┌─────────────────┐  │
│  │ SQLite  │  │  Blob Storage   │  │
│  │  (WAL)  │  │  (Filesystem)   │  │
│  └─────────┘  └─────────────────┘  │
└─────────────────────────────────────┘
```

Configuration:
```yaml
database:
  driver: sqlite
  path: /var/lib/alexander/alexander.db
  journal_mode: WAL

redis:
  enabled: false
```

### Distributed (Production)

Best for: High availability, horizontal scaling

```
┌─────────────┐     ┌─────────────┐
│  Alexander  │     │  Alexander  │
│   Node 1    │     │   Node 2    │
└──────┬──────┘     └──────┬──────┘
       │                   │
       └─────────┬─────────┘
                 │
    ┌────────────┼────────────┐
    │            │            │
┌───▼───┐  ┌─────▼─────┐  ┌───▼───┐
│ Redis │  │ PostgreSQL │  │  NFS  │
│       │  │            │  │ or S3 │
└───────┘  └───────────┘  └───────┘
```

Configuration:
```yaml
database:
  driver: postgres
  host: postgres.internal
  port: 5432
  name: alexander
  user: alexander
  password: ${POSTGRES_PASSWORD}
  sslmode: require

redis:
  enabled: true
  host: redis.internal
  port: 6379
  password: ${REDIS_PASSWORD}

storage:
  filesystem:
    base_path: /mnt/shared/alexander
```

## Security Configuration

### 1. Generate Strong Keys

```bash
# Master key for access key encryption (32 bytes = 64 hex chars)
openssl rand -hex 32

# SSE master key for server-side encryption
openssl rand -hex 32
```

### 2. TLS Configuration

Always use HTTPS in production. Options:

**Option A: Reverse Proxy (Recommended)**

Use nginx, Traefik, or cloud load balancer for TLS termination:

```nginx
server {
    listen 443 ssl http2;
    server_name s3.example.com;

    ssl_certificate /etc/ssl/certs/s3.example.com.crt;
    ssl_certificate_key /etc/ssl/private/s3.example.com.key;
    ssl_protocols TLSv1.2 TLSv1.3;

    # Disable request body size limit for large uploads
    client_max_body_size 0;

    # Increase timeouts for large transfers
    proxy_read_timeout 3600s;
    proxy_send_timeout 3600s;
    proxy_connect_timeout 60s;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # Disable buffering for streaming
        proxy_buffering off;
        proxy_request_buffering off;
    }
}
```

**Option B: Built-in TLS**

```yaml
server:
  tls:
    enabled: true
    cert_file: /etc/alexander/tls.crt
    key_file: /etc/alexander/tls.key
```

### 3. Firewall Rules

```bash
# Allow only necessary ports
ufw allow 443/tcp      # HTTPS
ufw allow 9091/tcp     # Metrics (internal only)
ufw deny 8080/tcp      # Block direct HTTP access
```

### 4. Run as Non-Root

```bash
# Create dedicated user
useradd -r -s /bin/false alexander

# Set permissions
chown -R alexander:alexander /var/lib/alexander
chmod 750 /var/lib/alexander

# Run as alexander user
sudo -u alexander ./alexander-server
```

## High Availability

### Load Balancing

For distributed deployments, use a load balancer:

```yaml
# Example: HAProxy configuration
frontend http_front
    bind *:80
    bind *:443 ssl crt /etc/ssl/certs/s3.pem
    default_backend alexander_back

backend alexander_back
    balance roundrobin
    option httpchk GET /healthz
    server node1 10.0.0.1:8080 check
    server node2 10.0.0.2:8080 check
```

### PostgreSQL HA

Use PostgreSQL replication for database high availability:
- Primary-Replica setup
- Connection pooling (PgBouncer)
- Automatic failover (Patroni, pg_auto_failover)

### Redis HA

Use Redis Sentinel or Redis Cluster for cache/lock high availability.

## Monitoring & Alerting

### Enable Metrics

```yaml
metrics:
  enabled: true
  port: 9091
  path: /metrics
```

### Prometheus Scrape Config

```yaml
scrape_configs:
  - job_name: 'alexander'
    static_configs:
      - targets: ['alexander:9091']
```

### Essential Alerts

See `monitoring/prometheus/alerts.yaml` for complete alerting rules. Key alerts:

- **AlexanderDown**: Service unavailable
- **AlexanderHighErrorRate**: >5% error rate
- **AlexanderHighLatency**: P95 > 1s
- **AlexanderGCNotRunning**: GC stalled

### Health Checks

```bash
# Kubernetes probes
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080

readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
```

## Backup & Recovery

### SQLite Backup

```bash
# Hot backup (WAL mode)
sqlite3 /var/lib/alexander/alexander.db ".backup /backup/alexander-$(date +%Y%m%d).db"

# Or using the CLI
./alexander-admin backup --output /backup/
```

### PostgreSQL Backup

```bash
# Using pg_dump
pg_dump -h postgres -U alexander alexander > backup.sql

# Point-in-time recovery with WAL archiving
# Configure in postgresql.conf
```

### Blob Storage Backup

```bash
# Filesystem: rsync
rsync -av /var/lib/alexander/data/ /backup/blobs/

# For large deployments, consider:
# - Incremental backups (rsync --link-dest)
# - Deduplication tools (restic, borg)
# - Object storage replication
```

### Disaster Recovery

1. **Recovery Time Objective (RTO)**: How quickly you need to recover
2. **Recovery Point Objective (RPO)**: How much data loss is acceptable

For critical deployments:
- Regular automated backups (hourly metadata, daily blobs)
- Off-site backup storage
- Documented recovery procedures
- Regular recovery testing

## Performance Tuning

### SQLite Tuning

```yaml
database:
  journal_mode: WAL
  busy_timeout: 5000
  cache_size: -8000      # 8MB cache
  synchronous_mode: NORMAL
```

### PostgreSQL Tuning

```sql
-- Connection pool settings
max_connections = 100
shared_buffers = 256MB
effective_cache_size = 1GB
work_mem = 16MB
```

### Rate Limiting

```yaml
rate_limit:
  enabled: true
  requests_per_second: 1000
  burst_size: 2000
```

### Garbage Collection

```yaml
gc:
  enabled: true
  interval: 1h
  grace_period: 24h
  batch_size: 1000
```

### Storage Performance

- Use SSD for metadata (SQLite/PostgreSQL)
- Use appropriate storage for blob data based on access patterns
- Consider separate volumes for metadata and blobs
- Monitor I/O metrics and adjust as needed

## Checklist

Before going to production:

- [ ] Strong master keys generated and securely stored
- [ ] TLS configured and verified
- [ ] Firewall rules applied
- [ ] Running as non-root user
- [ ] Monitoring and alerting configured
- [ ] Backup strategy implemented and tested
- [ ] Recovery procedure documented and tested
- [ ] Rate limiting configured
- [ ] Resource limits set (memory, CPU)
- [ ] Log rotation configured
- [ ] Security audit completed
