# Alexander Storage - Performance Tuning Guide

Comprehensive guide for optimizing Alexander Storage performance in production environments.

## Table of Contents

1. [Performance Baselines](#performance-baselines)
2. [Server Tuning](#server-tuning)
3. [Database Optimization](#database-optimization)
4. [Storage Optimization](#storage-optimization)
5. [Network Optimization](#network-optimization)
6. [Caching Strategies](#caching-strategies)
7. [Cluster Tuning](#cluster-tuning)
8. [Monitoring and Profiling](#monitoring-and-profiling)

## Performance Baselines

### Expected Performance (Single Node)

| Operation | Objects | Latency (p99) | Throughput |
|-----------|---------|---------------|------------|
| PUT Object | 1KB | 10ms | 5,000 ops/s |
| PUT Object | 1MB | 100ms | 500 ops/s |
| GET Object | 1KB | 5ms | 10,000 ops/s |
| GET Object | 1MB | 50ms | 1,000 ops/s |
| LIST Objects | 1000 | 50ms | 500 ops/s |
| DELETE Object | - | 5ms | 10,000 ops/s |

### Expected Performance (3-Node Cluster)

| Operation | Latency (p99) | Throughput |
|-----------|---------------|------------|
| PUT Object (1KB) | 15ms | 12,000 ops/s |
| GET Object (1KB) | 5ms | 25,000 ops/s |
| LIST Objects | 60ms | 1,200 ops/s |

## Server Tuning

### Go Runtime Configuration

```yaml
# config.yaml
runtime:
  # GOMAXPROCS - set to number of CPU cores
  # Use 0 for auto-detection
  gomaxprocs: 0
  
  # Memory ballast to reduce GC frequency
  # Set to ~30% of available memory
  memory_ballast_size: "2GB"
  
  # GC target percentage
  # Lower = more frequent GC, less memory
  # Higher = less frequent GC, more memory
  gogc: 100
```

Environment variables:
```bash
# Set explicitly if needed
export GOMAXPROCS=8
export GOGC=100
export GOMEMLIMIT=4GiB
```

### HTTP Server Tuning

```yaml
# config.yaml
server:
  address: "0.0.0.0:8080"
  
  # Timeouts
  read_timeout: 30s
  read_header_timeout: 10s
  write_timeout: 60s
  idle_timeout: 120s
  shutdown_timeout: 30s
  
  # Connection limits
  max_connections: 10000
  max_requests_per_conn: 1000
  
  # Request limits
  max_request_body_size: "5GB"
  max_header_size: "1MB"
  
  # Keep-alive
  keep_alive: true
  keep_alive_timeout: 75s
```

### Connection Pool Settings

```yaml
# config.yaml
connection_pool:
  # HTTP client pool for internal requests
  max_idle_conns: 100
  max_idle_conns_per_host: 10
  idle_conn_timeout: 90s
  
  # Database connection pool
  database:
    max_open_conns: 50
    max_idle_conns: 25
    conn_max_lifetime: 30m
    conn_max_idle_time: 5m
```

## Database Optimization

### PostgreSQL Configuration

```ini
# postgresql.conf

# Memory Settings
shared_buffers = 4GB                    # 25% of RAM
effective_cache_size = 12GB             # 75% of RAM
work_mem = 256MB                        # Per-operation memory
maintenance_work_mem = 1GB              # For VACUUM, CREATE INDEX

# Write Performance
wal_buffers = 64MB
checkpoint_completion_target = 0.9
max_wal_size = 4GB
min_wal_size = 1GB

# Query Planning
random_page_cost = 1.1                  # SSD storage
effective_io_concurrency = 200          # SSD storage
default_statistics_target = 100

# Parallelism
max_worker_processes = 8
max_parallel_workers_per_gather = 4
max_parallel_workers = 8
max_parallel_maintenance_workers = 4

# Connection Settings
max_connections = 200
```

### Index Optimization

```sql
-- Essential indexes (already in migrations)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_objects_bucket_key 
    ON objects(bucket_id, key) WHERE deleted_at IS NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_objects_bucket_prefix 
    ON objects(bucket_id, key text_pattern_ops) WHERE deleted_at IS NULL;

-- Performance indexes
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_objects_created 
    ON objects(created_at DESC) WHERE deleted_at IS NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_blobs_hash 
    ON blobs(hash);

-- Partial index for latest versions
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_objects_latest 
    ON objects(bucket_id, key) WHERE is_latest = true AND deleted_at IS NULL;

-- Analyze tables after index creation
ANALYZE objects;
ANALYZE blobs;
ANALYZE buckets;
```

### Query Optimization

```sql
-- Use prepared statements (automatic in Go)
-- Example: Batch operations for better performance

-- Instead of multiple single inserts:
INSERT INTO objects (bucket_id, key, ...) VALUES ($1, $2, ...);
INSERT INTO objects (bucket_id, key, ...) VALUES ($3, $4, ...);

-- Use batch insert:
INSERT INTO objects (bucket_id, key, ...)
VALUES 
    ($1, $2, ...),
    ($3, $4, ...),
    ($5, $6, ...);

-- Use CTEs for complex queries
WITH latest_objects AS (
    SELECT * FROM objects 
    WHERE bucket_id = $1 AND is_latest = true AND deleted_at IS NULL
)
SELECT * FROM latest_objects
WHERE key LIKE $2 || '%'
ORDER BY key
LIMIT $3;
```

### SQLite Optimization

```yaml
# config.yaml for SQLite
database:
  type: sqlite
  sqlite:
    path: "/data/alexander.db"
    # Performance pragmas
    pragmas:
      journal_mode: WAL
      synchronous: NORMAL
      cache_size: -64000      # 64MB cache
      busy_timeout: 5000
      foreign_keys: ON
      temp_store: MEMORY
      mmap_size: 268435456    # 256MB memory-mapped I/O
```

## Storage Optimization

### Filesystem Tuning

```bash
# For XFS (recommended for blob storage)
mkfs.xfs -f -d agcount=32 /dev/sdb
mount -o noatime,nodiratime,logbufs=8,logbsize=256k /dev/sdb /data/blobs

# For ext4
mkfs.ext4 -E stride=128,stripe-width=256 /dev/sdb
mount -o noatime,nodiratime,data=writeback,barrier=0 /dev/sdb /data/blobs

# Linux I/O scheduler (for SSDs)
echo noop > /sys/block/sdb/queue/scheduler
# Or for NVMe
echo none > /sys/block/nvme0n1/queue/scheduler
```

### Directory Structure

```yaml
# config.yaml
storage:
  backend: filesystem
  filesystem:
    path: "/data/blobs"
    
    # Sharding configuration
    # Creates directory structure: /data/blobs/ab/cd/abcd1234...
    shard_depth: 2        # Number of directory levels
    shard_width: 2        # Characters per level
    
    # Performance settings
    sync_writes: false    # Set true for durability, false for performance
    buffer_size: 65536    # 64KB buffer for reads/writes
```

### Content-Addressable Storage Settings

```yaml
# config.yaml
storage:
  cas:
    # Hash algorithm
    algorithm: sha256
    
    # Deduplication
    dedup_enabled: true
    
    # Chunk settings for FastCDC
    chunking:
      enabled: true
      min_chunk_size: 2048      # 2KB
      avg_chunk_size: 8192      # 8KB  
      max_chunk_size: 65536     # 64KB
```

## Network Optimization

### TCP Tuning

```bash
# /etc/sysctl.conf

# Increase socket buffer sizes
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
net.core.rmem_default = 1048576
net.core.wmem_default = 1048576
net.ipv4.tcp_rmem = 4096 1048576 16777216
net.ipv4.tcp_wmem = 4096 1048576 16777216

# Connection handling
net.core.somaxconn = 65535
net.core.netdev_max_backlog = 65535
net.ipv4.tcp_max_syn_backlog = 65535

# TIME_WAIT handling
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 15

# Keep-alive
net.ipv4.tcp_keepalive_time = 600
net.ipv4.tcp_keepalive_intvl = 60
net.ipv4.tcp_keepalive_probes = 3

# Apply changes
sysctl -p
```

### File Descriptor Limits

```bash
# /etc/security/limits.conf
alexander soft nofile 1048576
alexander hard nofile 1048576
alexander soft nproc 65535
alexander hard nproc 65535

# /etc/systemd/system/alexander.service.d/limits.conf
[Service]
LimitNOFILE=1048576
LimitNPROC=65535
```

## Caching Strategies

### Memory Cache Configuration

```yaml
# config.yaml
cache:
  type: memory
  memory:
    max_size: "1GB"
    ttl: 5m
    
    # Object metadata cache
    metadata:
      enabled: true
      max_entries: 100000
      ttl: 5m
    
    # Listing cache
    listing:
      enabled: true
      max_entries: 10000
      ttl: 30s
    
    # Authentication cache
    auth:
      enabled: true
      max_entries: 10000
      ttl: 1m
```

### Redis Cache Configuration

```yaml
# config.yaml
cache:
  type: redis
  redis:
    address: "redis:6379"
    password: ""
    database: 0
    
    # Connection pool
    pool_size: 100
    min_idle_conns: 10
    
    # Timeouts
    dial_timeout: 5s
    read_timeout: 3s
    write_timeout: 3s
    
    # Cache settings
    default_ttl: 5m
    
    # Key prefixes
    prefix: "alex:"
```

### Cache Warming

```bash
#!/bin/bash
# cache-warm.sh - Pre-warm cache with frequently accessed data

# Warm bucket listing cache
curl -s "http://localhost:8080/" > /dev/null

# Warm hot buckets
for bucket in $(cat /etc/alexander/hot-buckets.txt); do
    curl -s "http://localhost:8080/$bucket?list-type=2&max-keys=1000" > /dev/null
done

echo "Cache warming complete"
```

## Cluster Tuning

### Cluster Configuration

```yaml
# config.yaml
cluster:
  enabled: true
  
  # Node identity
  node_id: "node-1"
  advertise_address: "10.0.0.1:9090"
  
  # gRPC settings
  grpc:
    port: 9090
    max_recv_msg_size: 104857600  # 100MB
    max_send_msg_size: 104857600
    keepalive_time: 30s
    keepalive_timeout: 10s
  
  # Consensus settings
  raft:
    election_timeout: 1000ms
    heartbeat_timeout: 100ms
    snapshot_threshold: 10000
  
  # Replication
  replication:
    factor: 3
    min_sync_replicas: 2
    sync_timeout: 5s
```

### Load Balancing

```yaml
# HAProxy configuration example
frontend alexander_frontend
    bind *:80
    bind *:443 ssl crt /etc/haproxy/certs/
    default_backend alexander_backend

backend alexander_backend
    balance roundrobin
    option httpchk GET /health
    http-check expect status 200
    
    server node1 10.0.0.1:8080 check inter 5s fall 3 rise 2
    server node2 10.0.0.2:8080 check inter 5s fall 3 rise 2
    server node3 10.0.0.3:8080 check inter 5s fall 3 rise 2
```

## Monitoring and Profiling

### Key Metrics to Monitor

```yaml
# Prometheus queries for key performance indicators

# Request latency
histogram_quantile(0.99, 
  rate(alexander_http_request_duration_seconds_bucket[5m])
)

# Throughput
sum(rate(alexander_http_requests_total[5m]))

# Error rate
sum(rate(alexander_http_requests_total{status=~"5.."}[5m])) /
sum(rate(alexander_http_requests_total[5m]))

# Database latency
histogram_quantile(0.99,
  rate(alexander_db_query_duration_seconds_bucket[5m])
)

# Cache hit rate
sum(rate(alexander_cache_hits_total[5m])) /
(sum(rate(alexander_cache_hits_total[5m])) + sum(rate(alexander_cache_misses_total[5m])))

# Storage throughput
sum(rate(alexander_storage_bytes_written_total[5m]))
sum(rate(alexander_storage_bytes_read_total[5m]))
```

### Profiling

```bash
# Enable pprof endpoint
# config.yaml
debug:
  pprof_enabled: true
  pprof_address: "localhost:6060"

# CPU profiling
curl -o cpu.prof "http://localhost:6060/debug/pprof/profile?seconds=30"
go tool pprof -http=:8081 cpu.prof

# Memory profiling
curl -o heap.prof "http://localhost:6060/debug/pprof/heap"
go tool pprof -http=:8081 heap.prof

# Goroutine analysis
curl -o goroutine.prof "http://localhost:6060/debug/pprof/goroutine"
go tool pprof -http=:8081 goroutine.prof

# Block profiling (contention)
curl -o block.prof "http://localhost:6060/debug/pprof/block"
go tool pprof -http=:8081 block.prof

# Trace
curl -o trace.out "http://localhost:6060/debug/pprof/trace?seconds=5"
go tool trace trace.out
```

### Benchmarking

```bash
# Run built-in benchmarks
cd /path/to/alexander
go test -bench=. -benchmem ./...

# Run specific benchmarks
go test -bench=BenchmarkPutObject -benchtime=10s -count=5 ./internal/service/

# Memory benchmarks
go test -bench=. -benchmem -memprofile=mem.prof ./internal/service/
go tool pprof mem.prof

# CPU benchmarks
go test -bench=. -cpuprofile=cpu.prof ./internal/service/
go tool pprof cpu.prof
```

## Performance Checklist

### Before Production

- [ ] Database indexes verified
- [ ] Connection pools sized appropriately
- [ ] File descriptor limits increased
- [ ] TCP tuning applied
- [ ] Disk I/O optimized (scheduler, mount options)
- [ ] Cache configured and sized
- [ ] Monitoring dashboards ready
- [ ] Baseline performance documented

### Regular Review

- [ ] Weekly: Review slow query logs
- [ ] Weekly: Check cache hit rates
- [ ] Monthly: Run performance benchmarks
- [ ] Monthly: Review and optimize queries
- [ ] Quarterly: Load test with production traffic patterns

## Appendix: Quick Reference

### Recommended Configurations by Scale

| Users | Nodes | DB | Memory | Storage |
|-------|-------|-----|--------|---------|
| <1K | 1 | SQLite | 4GB | 100GB SSD |
| 1K-10K | 2-3 | PostgreSQL | 8GB | 500GB SSD |
| 10K-100K | 3-5 | PostgreSQL HA | 16GB | 2TB NVMe |
| >100K | 5+ | PostgreSQL HA + Redis | 32GB+ | Distributed |
