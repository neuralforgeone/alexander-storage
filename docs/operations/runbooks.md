# Alexander Storage - Operational Runbooks

Collection of operational procedures for managing Alexander Storage in production.

## Table of Contents

1. [Daily Operations](#daily-operations)
2. [Incident Response](#incident-response)
3. [Scaling Procedures](#scaling-procedures)
4. [Maintenance Windows](#maintenance-windows)
5. [Security Procedures](#security-procedures)

## Daily Operations

### Health Check Procedure

```bash
#!/bin/bash
# daily-health-check.sh

echo "=== Alexander Daily Health Check ==="
echo "Date: $(date)"
echo ""

# Service Health
echo "1. Service Health"
for host in node1 node2 node3; do
    status=$(curl -s "http://$host:8080/health" | jq -r '.status // "FAILED"')
    echo "   $host: $status"
done

# Database Status
echo ""
echo "2. Database Status"
psql -h db.internal -U alexander -c "SELECT pg_is_in_recovery();" 2>/dev/null && echo "   Database: OK" || echo "   Database: FAILED"

# Storage Utilization
echo ""
echo "3. Storage Utilization"
df -h /data/blobs | tail -1 | awk '{print "   Used: "$3" / "$2" ("$5")"}'

# Object Counts
echo ""
echo "4. Object Statistics"
psql -h db.internal -U alexander -t -c "SELECT COUNT(*) FROM objects WHERE deleted_at IS NULL;" | xargs echo "   Total objects:"
psql -h db.internal -U alexander -t -c "SELECT COUNT(*) FROM buckets;" | xargs echo "   Total buckets:"

# Error Rate (last hour)
echo ""
echo "5. Error Rate (last hour)"
curl -s "http://prometheus:9090/api/v1/query?query=rate(alexander_http_requests_total{status=~'5..'}[1h])" | \
    jq -r '.data.result[0].value[1] // "0"' | xargs echo "   5xx rate:"

# Recent Alerts
echo ""
echo "6. Active Alerts"
curl -s "http://alertmanager:9093/api/v2/alerts?active=true" | jq -r '.[].labels.alertname' | sort -u

echo ""
echo "=== Health Check Complete ==="
```

### Log Analysis

```bash
#!/bin/bash
# analyze-logs.sh

LOG_FILE="/var/log/alexander/alexander.log"
HOURS=${1:-1}

echo "=== Log Analysis (last $HOURS hour(s)) ==="

# Error summary
echo ""
echo "Error Summary:"
journalctl -u alexander --since "$HOURS hours ago" | grep -i error | \
    sed 's/.*error[": ]*//i' | sort | uniq -c | sort -rn | head -10

# Slow requests (>1s)
echo ""
echo "Slow Requests (>1000ms):"
journalctl -u alexander --since "$HOURS hours ago" | \
    grep -oP 'duration[":=]+\K[0-9]+' | \
    awk '$1 > 1000 {count++} END {print count " requests"}'

# Top accessed buckets
echo ""
echo "Top Accessed Buckets:"
journalctl -u alexander --since "$HOURS hours ago" | \
    grep -oP 'bucket[":=]+\K[a-zA-Z0-9-]+' | sort | uniq -c | sort -rn | head -10

# Authentication failures
echo ""
echo "Authentication Failures:"
journalctl -u alexander --since "$HOURS hours ago" | \
    grep -i "auth.*fail\|unauthorized\|forbidden" | wc -l | xargs echo "Count:"
```

## Incident Response

### Severity Levels

| Level | Description | Response Time | Escalation |
|-------|-------------|---------------|------------|
| SEV1 | Service down, data loss risk | 15 min | Immediate page |
| SEV2 | Major feature broken | 30 min | Page during hours |
| SEV3 | Minor issue, workaround exists | 4 hours | Slack notification |
| SEV4 | Low impact, cosmetic | 24 hours | Ticket |

### SEV1 Response Runbook

```markdown
# SEV1 Incident Response

## Initial Response (0-15 minutes)
1. [ ] Acknowledge alert
2. [ ] Join incident channel (#incident-active)
3. [ ] Assign Incident Commander (IC)
4. [ ] Initial assessment - what's broken?

## Triage (15-30 minutes)
1. [ ] Check service health: `curl http://localhost:8080/health`
2. [ ] Check database: `pg_isready -h db.internal`
3. [ ] Check storage: `df -h /data/blobs`
4. [ ] Check recent deployments: `kubectl rollout history`
5. [ ] Check recent changes: `git log --oneline -10`

## Mitigation
- [ ] If recent deploy: `kubectl rollout undo deployment/alexander`
- [ ] If database: Check `docs/operations/database-recovery.md`
- [ ] If storage: Check `docs/operations/storage-recovery.md`
- [ ] If network: Check load balancer and DNS

## Communication
- [ ] Update status page
- [ ] Notify affected customers (if >30 min)
- [ ] Update incident channel every 15 min

## Post-Incident
- [ ] Write incident summary
- [ ] Schedule post-mortem (within 48 hours)
- [ ] Create follow-up tickets
```

### Common Issues and Fixes

#### High Memory Usage

```bash
# Check memory usage
free -h
ps aux --sort=-%mem | head -20

# If Alexander is consuming too much memory
# 1. Check connection count
curl -s http://localhost:9100/metrics | grep alexander_connections

# 2. Check for memory leaks
go tool pprof http://localhost:6060/debug/pprof/heap

# 3. Restart with memory limit
systemctl stop alexander
systemctl set-property alexander MemoryMax=4G
systemctl start alexander
```

#### High Latency

```bash
# Check request latency
curl -s http://localhost:9100/metrics | grep alexander_http_request_duration

# Check database latency
psql -h db.internal -U alexander -c "SELECT * FROM pg_stat_activity;"

# Check disk I/O
iostat -x 1 5

# Possible fixes:
# 1. Enable query caching (if not enabled)
# 2. Add read replicas
# 3. Scale horizontally
```

#### Storage Full

```bash
# Check usage
df -h /data/blobs

# Find large files
du -sh /data/blobs/* | sort -h | tail -20

# Run garbage collection
curl -X POST http://localhost:8080/admin/gc

# If emergency, identify orphaned blobs
psql -h db.internal -U alexander -c "
SELECT b.hash, b.size 
FROM blobs b 
LEFT JOIN objects o ON b.hash = o.blob_hash 
WHERE o.id IS NULL 
ORDER BY b.size DESC 
LIMIT 10;"
```

## Scaling Procedures

### Horizontal Scaling

```bash
#!/bin/bash
# scale-out.sh

CURRENT=$(kubectl get deployment alexander -o jsonpath='{.spec.replicas}')
NEW=$((CURRENT + 1))

echo "Scaling from $CURRENT to $NEW replicas..."

kubectl scale deployment alexander --replicas=$NEW

# Wait for pod to be ready
kubectl rollout status deployment/alexander --timeout=300s

# Verify
kubectl get pods -l app=alexander
```

### Vertical Scaling

```bash
#!/bin/bash
# scale-up.sh

# 1. Update resource limits
kubectl patch deployment alexander -p '{
  "spec": {
    "template": {
      "spec": {
        "containers": [{
          "name": "alexander",
          "resources": {
            "requests": {"memory": "2Gi", "cpu": "1"},
            "limits": {"memory": "4Gi", "cpu": "2"}
          }
        }]
      }
    }
  }
}'

# 2. Rolling restart
kubectl rollout restart deployment/alexander
kubectl rollout status deployment/alexander
```

### Database Scaling

```bash
# Add read replica (AWS RDS)
aws rds create-db-instance-read-replica \
    --db-instance-identifier alexander-read-1 \
    --source-db-instance-identifier alexander-primary \
    --db-instance-class db.r6g.large

# Update application to use read replica for GET requests
# (requires config change)
```

## Maintenance Windows

### Pre-Maintenance Checklist

```markdown
# Maintenance Preparation

## 1 Week Before
- [ ] Schedule announced to users
- [ ] Status page updated
- [ ] Maintenance window created in PagerDuty
- [ ] Runbook reviewed and updated

## 1 Day Before
- [ ] Fresh backup verified
- [ ] Rollback plan documented
- [ ] On-call team briefed
- [ ] Communication templates ready

## 1 Hour Before
- [ ] Final backup snapshot
- [ ] Health check passed
- [ ] Team assembled
- [ ] Communication sent

## Start of Window
- [ ] Enable maintenance mode (if applicable)
- [ ] Stop accepting new requests (drain)
- [ ] Begin maintenance procedure
```

### Rolling Update Procedure

```bash
#!/bin/bash
# rolling-update.sh

VERSION=$1
if [ -z "$VERSION" ]; then
    echo "Usage: $0 <version>"
    exit 1
fi

echo "Rolling update to version $VERSION"

# 1. Update image
kubectl set image deployment/alexander \
    alexander=ghcr.io/neuralforgeone/alexander-storage:$VERSION

# 2. Monitor rollout
kubectl rollout status deployment/alexander --timeout=600s

# 3. Verify health
for pod in $(kubectl get pods -l app=alexander -o name); do
    kubectl exec $pod -- curl -s localhost:8080/health
done

# 4. Run smoke tests
./tests/smoke-test.sh

echo "Rollout complete"
```

### Database Migration Procedure

```bash
#!/bin/bash
# db-migrate.sh

MIGRATION_DIR="/app/migrations"

# 1. Create backup
./postgres-backup.sh

# 2. Run migrations
alexander-migrate -config /etc/alexander/config.yaml -direction up

# 3. Verify schema
psql -h db.internal -U alexander -c "\dt"

# 4. Run validation queries
psql -h db.internal -U alexander -c "SELECT COUNT(*) FROM objects;"
```

## Security Procedures

### Credential Rotation

```bash
#!/bin/bash
# rotate-credentials.sh

# Generate new admin credentials
NEW_ACCESS_KEY="AK$(openssl rand -hex 10 | tr '[:lower:]' '[:upper:]')"
NEW_SECRET_KEY=$(openssl rand -base64 30)

# Update in Secrets Manager
aws secretsmanager update-secret \
    --secret-id alexander/admin-credentials \
    --secret-string "{\"access_key\":\"$NEW_ACCESS_KEY\",\"secret_key\":\"$NEW_SECRET_KEY\"}"

# Update Kubernetes secret
kubectl create secret generic alexander-admin-creds \
    --from-literal=access-key="$NEW_ACCESS_KEY" \
    --from-literal=secret-key="$NEW_SECRET_KEY" \
    --dry-run=client -o yaml | kubectl apply -f -

# Rolling restart to pick up new credentials
kubectl rollout restart deployment/alexander

echo "Credentials rotated. New access key: $NEW_ACCESS_KEY"
echo "Update client applications with new credentials."
```

### Security Audit

```bash
#!/bin/bash
# security-audit.sh

echo "=== Alexander Security Audit ==="

# Check for exposed ports
echo "1. Exposed Ports"
netstat -tlnp | grep alexander

# Check TLS configuration
echo ""
echo "2. TLS Configuration"
openssl s_client -connect localhost:443 -brief 2>/dev/null

# Check for default credentials
echo ""
echo "3. Default Credentials Check"
curl -s -X GET "http://localhost:8080/" \
    -H "Authorization: AWS4-HMAC-SHA256 ..." 2>&1 | grep -i "unauthorized" && \
    echo "OK: Default credentials rejected"

# Check access logs for suspicious activity
echo ""
echo "4. Suspicious Activity (last 24h)"
journalctl -u alexander --since "24 hours ago" | \
    grep -iE "unauthorized|forbidden|invalid.*signature" | wc -l | \
    xargs echo "Auth failures:"

# Check file permissions
echo ""
echo "5. File Permissions"
ls -la /etc/alexander/
ls -la /data/blobs/ | head -5

echo ""
echo "=== Audit Complete ==="
```

### Access Review

```sql
-- List all access keys and their last use
SELECT 
    ak.access_key_id,
    u.username,
    ak.created_at,
    ak.last_used_at,
    ak.is_active
FROM access_keys ak
JOIN users u ON ak.user_id = u.id
ORDER BY ak.last_used_at DESC NULLS LAST;

-- Find inactive keys (not used in 90 days)
SELECT 
    ak.access_key_id,
    u.username
FROM access_keys ak
JOIN users u ON ak.user_id = u.id
WHERE ak.last_used_at < NOW() - INTERVAL '90 days'
   OR ak.last_used_at IS NULL;
```

## Appendix

### Useful Commands

```bash
# Check Alexander version
alexander-server --version

# Validate configuration
alexander-server -config /etc/alexander/config.yaml -validate

# Debug mode
alexander-server -config /etc/alexander/config.yaml -debug

# Profile CPU
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof

# Profile memory
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof heap.prof

# Export metrics
curl http://localhost:9100/metrics > metrics.txt
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `ALEXANDER_CONFIG` | Config file path | `/etc/alexander/config.yaml` |
| `ALEXANDER_DEBUG` | Enable debug mode | `false` |
| `ALEXANDER_LOG_LEVEL` | Log level | `info` |
| `GOMAXPROCS` | Go runtime threads | (auto) |
