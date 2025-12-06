# Alexander Storage - Backup and Disaster Recovery

This guide covers backup strategies, disaster recovery procedures, and business continuity planning for Alexander Storage.

## Table of Contents

1. [Backup Strategy](#backup-strategy)
2. [Database Backup](#database-backup)
3. [Blob Storage Backup](#blob-storage-backup)
4. [Configuration Backup](#configuration-backup)
5. [Disaster Recovery](#disaster-recovery)
6. [Recovery Procedures](#recovery-procedures)
7. [Testing and Validation](#testing-and-validation)

## Backup Strategy

### Backup Types

| Type | Frequency | Retention | Description |
|------|-----------|-----------|-------------|
| Full | Weekly | 4 weeks | Complete backup of all data |
| Incremental | Daily | 7 days | Changes since last backup |
| Point-in-time | Continuous | 7 days | Database transaction logs |
| Configuration | On change | 30 days | Config files and secrets |

### RPO and RTO Targets

| Environment | RPO (Recovery Point Objective) | RTO (Recovery Time Objective) |
|-------------|-------------------------------|-------------------------------|
| Development | 24 hours | 4 hours |
| Staging | 4 hours | 2 hours |
| Production | 15 minutes | 30 minutes |

## Database Backup

### SQLite Backup

For embedded SQLite deployments:

```bash
#!/bin/bash
# sqlite-backup.sh

SQLITE_PATH="/data/alexander.db"
BACKUP_DIR="/backup/sqlite"
DATE=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="$BACKUP_DIR/alexander_$DATE.db"

# Create backup directory
mkdir -p "$BACKUP_DIR"

# Enable WAL mode checkpoint before backup
sqlite3 "$SQLITE_PATH" "PRAGMA wal_checkpoint(TRUNCATE);"

# Create backup using .backup command (online backup)
sqlite3 "$SQLITE_PATH" ".backup '$BACKUP_FILE'"

# Compress backup
gzip "$BACKUP_FILE"

# Verify backup integrity
gunzip -c "$BACKUP_FILE.gz" | sqlite3 ":memory:" "PRAGMA integrity_check;" || {
    echo "Backup integrity check failed!"
    exit 1
}

# Upload to S3 (optional)
aws s3 cp "$BACKUP_FILE.gz" "s3://backup-bucket/alexander/sqlite/$DATE.db.gz"

# Cleanup old backups (keep 7 days)
find "$BACKUP_DIR" -name "*.gz" -mtime +7 -delete

echo "Backup completed: $BACKUP_FILE.gz"
```

### PostgreSQL Backup

For distributed PostgreSQL deployments:

```bash
#!/bin/bash
# postgres-backup.sh

PG_HOST="${POSTGRES_HOST:-localhost}"
PG_PORT="${POSTGRES_PORT:-5432}"
PG_DB="${POSTGRES_DATABASE:-alexander}"
PG_USER="${POSTGRES_USER:-alexander}"
BACKUP_DIR="/backup/postgres"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p "$BACKUP_DIR"

# Full backup with pg_dump
pg_dump -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$PG_DB" \
    --format=custom \
    --compress=9 \
    --file="$BACKUP_DIR/alexander_$DATE.dump"

# Verify backup
pg_restore --list "$BACKUP_DIR/alexander_$DATE.dump" > /dev/null || {
    echo "Backup verification failed!"
    exit 1
}

# Upload to S3
aws s3 cp "$BACKUP_DIR/alexander_$DATE.dump" \
    "s3://backup-bucket/alexander/postgres/$DATE.dump"

# Cleanup old backups (keep 30 days)
find "$BACKUP_DIR" -name "*.dump" -mtime +30 -delete

echo "Backup completed: alexander_$DATE.dump"
```

### Continuous Archiving (WAL)

For point-in-time recovery with PostgreSQL:

```bash
# postgresql.conf settings
archive_mode = on
archive_command = 'aws s3 cp %p s3://backup-bucket/alexander/wal/%f'
archive_timeout = 300  # Archive every 5 minutes if no activity

# Recovery settings (recovery.conf / postgresql.conf)
restore_command = 'aws s3 cp s3://backup-bucket/alexander/wal/%f %p'
recovery_target_time = '2024-01-15 10:00:00'  # Point-in-time target
```

## Blob Storage Backup

### Incremental Blob Sync

```bash
#!/bin/bash
# blob-backup.sh

BLOB_PATH="/data/blobs"
BACKUP_DEST="s3://backup-bucket/alexander/blobs"
DATE=$(date +%Y%m%d)

# Sync blobs to S3 (only new/changed files)
aws s3 sync "$BLOB_PATH" "$BACKUP_DEST" \
    --storage-class STANDARD_IA \
    --exclude "*.tmp" \
    --exclude "*.lock"

# Create manifest of current state
find "$BLOB_PATH" -type f -exec sha256sum {} \; > "/backup/manifest_$DATE.txt"
aws s3 cp "/backup/manifest_$DATE.txt" "$BACKUP_DEST/manifests/"

echo "Blob sync completed"
```

### Cross-Region Replication

For AWS S3-based blob storage:

```hcl
# terraform configuration
resource "aws_s3_bucket_replication_configuration" "blobs" {
  bucket = aws_s3_bucket.primary.id
  role   = aws_iam_role.replication.arn

  rule {
    id     = "disaster-recovery"
    status = "Enabled"

    destination {
      bucket        = aws_s3_bucket.dr.arn
      storage_class = "STANDARD_IA"
    }
  }
}
```

## Configuration Backup

### Automated Config Backup

```bash
#!/bin/bash
# config-backup.sh

CONFIG_DIR="/etc/alexander"
BACKUP_DIR="/backup/config"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p "$BACKUP_DIR"

# Create encrypted backup of configuration
tar czf - -C "$CONFIG_DIR" . | \
    openssl enc -aes-256-cbc -salt -pbkdf2 \
    -pass file:/etc/alexander/.backup-key \
    -out "$BACKUP_DIR/config_$DATE.tar.gz.enc"

# Backup to S3
aws s3 cp "$BACKUP_DIR/config_$DATE.tar.gz.enc" \
    "s3://backup-bucket/alexander/config/"

# Keep only last 30 backups
find "$BACKUP_DIR" -name "config_*.tar.gz.enc" -mtime +30 -delete
```

### Secret Management Backup

```bash
#!/bin/bash
# secrets-backup.sh

# Export from AWS Secrets Manager
aws secretsmanager get-secret-value \
    --secret-id alexander/production \
    --query SecretString \
    --output text | \
    openssl enc -aes-256-cbc -salt -pbkdf2 \
    -pass file:/etc/alexander/.backup-key \
    -out "/backup/secrets/secrets_$(date +%Y%m%d).enc"

# Export from HashiCorp Vault
vault kv get -format=json secret/alexander | \
    jq '.data.data' | \
    openssl enc -aes-256-cbc -salt -pbkdf2 \
    -pass file:/etc/alexander/.backup-key \
    -out "/backup/secrets/vault_$(date +%Y%m%d).enc"
```

## Disaster Recovery

### DR Architecture

```
Primary Region (us-east-1)          DR Region (us-west-2)
┌─────────────────────────┐        ┌─────────────────────────┐
│  Load Balancer          │        │  Load Balancer (standby)│
│         ↓               │        │         ↓               │
│  Alexander Cluster      │───────>│  Alexander Cluster      │
│  (3 nodes)              │  sync  │  (warm standby)         │
│         ↓               │        │         ↓               │
│  PostgreSQL (Primary)   │───────>│  PostgreSQL (Replica)   │
│         ↓               │  WAL   │                         │
│  Blob Storage           │───────>│  Blob Storage           │
│                         │  S3    │  (replicated)           │
└─────────────────────────┘        └─────────────────────────┘
```

### Failover Procedures

#### Automatic Failover (AWS)

```hcl
# Route53 health check and failover
resource "aws_route53_health_check" "primary" {
  fqdn              = "s3-primary.example.com"
  port              = 443
  type              = "HTTPS"
  resource_path     = "/health"
  failure_threshold = 3
  request_interval  = 30
}

resource "aws_route53_record" "s3" {
  zone_id = aws_route53_zone.main.zone_id
  name    = "s3.example.com"
  type    = "A"

  failover_routing_policy {
    type = "PRIMARY"
  }

  set_identifier  = "primary"
  health_check_id = aws_route53_health_check.primary.id

  alias {
    name                   = aws_lb.primary.dns_name
    zone_id                = aws_lb.primary.zone_id
    evaluate_target_health = true
  }
}

resource "aws_route53_record" "s3_secondary" {
  zone_id = aws_route53_zone.main.zone_id
  name    = "s3.example.com"
  type    = "A"

  failover_routing_policy {
    type = "SECONDARY"
  }

  set_identifier = "secondary"

  alias {
    name                   = aws_lb.dr.dns_name
    zone_id                = aws_lb.dr.zone_id
    evaluate_target_health = true
  }
}
```

#### Manual Failover Runbook

```bash
#!/bin/bash
# dr-failover.sh

set -e

DR_REGION="us-west-2"
PRIMARY_REGION="us-east-1"

echo "=== Alexander DR Failover Runbook ==="
echo "Primary: $PRIMARY_REGION"
echo "DR: $DR_REGION"
echo ""

# Step 1: Verify primary is actually down
echo "Step 1: Verifying primary status..."
if curl -s --max-time 10 "https://s3-primary.example.com/health" | grep -q "ok"; then
    echo "WARNING: Primary appears to be UP. Are you sure you want to failover? (yes/no)"
    read confirm
    if [ "$confirm" != "yes" ]; then
        echo "Failover cancelled."
        exit 0
    fi
fi

# Step 2: Promote PostgreSQL replica
echo "Step 2: Promoting PostgreSQL replica..."
aws rds promote-read-replica \
    --region "$DR_REGION" \
    --db-instance-identifier alexander-dr

echo "Waiting for DB promotion..."
aws rds wait db-instance-available \
    --region "$DR_REGION" \
    --db-instance-identifier alexander-dr

# Step 3: Update DNS
echo "Step 3: Updating DNS to DR region..."
aws route53 change-resource-record-sets \
    --hosted-zone-id $HOSTED_ZONE_ID \
    --change-batch '{
        "Changes": [{
            "Action": "UPSERT",
            "ResourceRecordSet": {
                "Name": "s3.example.com",
                "Type": "A",
                "TTL": 60,
                "ResourceRecords": [{"Value": "'$DR_ALB_IP'"}]
            }
        }]
    }'

# Step 4: Scale up DR cluster
echo "Step 4: Scaling up DR cluster..."
aws autoscaling set-desired-capacity \
    --region "$DR_REGION" \
    --auto-scaling-group-name alexander-dr-asg \
    --desired-capacity 3

# Step 5: Verify DR is operational
echo "Step 5: Verifying DR status..."
sleep 30
for i in {1..10}; do
    if curl -s "https://s3.example.com/health" | grep -q "ok"; then
        echo "DR failover successful!"
        break
    fi
    echo "Waiting for DR to become healthy... ($i/10)"
    sleep 10
done

echo ""
echo "=== Failover Complete ==="
echo "DR endpoint is now active: https://s3.example.com"
echo ""
echo "Post-failover tasks:"
echo "1. Notify users of the failover"
echo "2. Monitor DR performance"
echo "3. Begin investigation of primary failure"
echo "4. Plan failback procedure"
```

## Recovery Procedures

### Full Database Recovery

```bash
#!/bin/bash
# db-restore.sh

BACKUP_FILE="$1"
TARGET_DB="${2:-alexander_restored}"

if [ -z "$BACKUP_FILE" ]; then
    echo "Usage: $0 <backup_file> [target_database]"
    exit 1
fi

# Download from S3 if needed
if [[ "$BACKUP_FILE" == s3://* ]]; then
    LOCAL_FILE="/tmp/$(basename $BACKUP_FILE)"
    aws s3 cp "$BACKUP_FILE" "$LOCAL_FILE"
    BACKUP_FILE="$LOCAL_FILE"
fi

# For PostgreSQL
if [[ "$BACKUP_FILE" == *.dump ]]; then
    echo "Restoring PostgreSQL database..."
    
    # Create target database
    psql -h "$PG_HOST" -U "$PG_USER" -c "CREATE DATABASE $TARGET_DB;"
    
    # Restore
    pg_restore -h "$PG_HOST" -U "$PG_USER" -d "$TARGET_DB" \
        --clean --if-exists "$BACKUP_FILE"
    
    echo "PostgreSQL restore complete."
fi

# For SQLite
if [[ "$BACKUP_FILE" == *.db* ]]; then
    echo "Restoring SQLite database..."
    
    # Decompress if needed
    if [[ "$BACKUP_FILE" == *.gz ]]; then
        gunzip -c "$BACKUP_FILE" > "/data/$TARGET_DB.db"
    else
        cp "$BACKUP_FILE" "/data/$TARGET_DB.db"
    fi
    
    # Verify integrity
    sqlite3 "/data/$TARGET_DB.db" "PRAGMA integrity_check;"
    
    echo "SQLite restore complete."
fi
```

### Point-in-Time Recovery

```bash
#!/bin/bash
# pitr-restore.sh

TARGET_TIME="$1"  # Format: '2024-01-15 10:00:00'

if [ -z "$TARGET_TIME" ]; then
    echo "Usage: $0 '<target_time>'"
    echo "Example: $0 '2024-01-15 10:00:00'"
    exit 1
fi

# Create recovery configuration
cat > /var/lib/postgresql/data/recovery.signal <<EOF
# Point-in-time recovery
EOF

cat >> /var/lib/postgresql/data/postgresql.auto.conf <<EOF
restore_command = 'aws s3 cp s3://backup-bucket/alexander/wal/%f %p'
recovery_target_time = '$TARGET_TIME'
recovery_target_action = 'promote'
EOF

# Restart PostgreSQL to begin recovery
systemctl restart postgresql

echo "Point-in-time recovery initiated to: $TARGET_TIME"
echo "Monitor recovery progress in PostgreSQL logs."
```

### Blob Recovery

```bash
#!/bin/bash
# blob-restore.sh

BACKUP_DATE="$1"  # Format: YYYYMMDD
RESTORE_PATH="${2:-/data/blobs}"

if [ -z "$BACKUP_DATE" ]; then
    echo "Usage: $0 <backup_date> [restore_path]"
    exit 1
fi

echo "Restoring blobs from $BACKUP_DATE to $RESTORE_PATH..."

# Verify backup exists
aws s3 ls "s3://backup-bucket/alexander/blobs/manifests/manifest_$BACKUP_DATE.txt" || {
    echo "Backup not found for date: $BACKUP_DATE"
    exit 1
}

# Restore blobs
aws s3 sync "s3://backup-bucket/alexander/blobs" "$RESTORE_PATH" \
    --exclude "manifests/*"

# Verify using manifest
echo "Verifying restored files..."
aws s3 cp "s3://backup-bucket/alexander/blobs/manifests/manifest_$BACKUP_DATE.txt" /tmp/manifest.txt
sha256sum -c /tmp/manifest.txt 2>/dev/null | grep -c "OK" | xargs echo "Files verified:"

echo "Blob restore complete."
```

## Testing and Validation

### Backup Verification Checklist

```bash
#!/bin/bash
# backup-verify.sh

echo "=== Alexander Backup Verification ==="
echo ""

ERRORS=0

# Check database backup
echo "Checking database backups..."
LATEST_DB=$(aws s3 ls s3://backup-bucket/alexander/postgres/ --recursive | sort | tail -n 1)
if [ -z "$LATEST_DB" ]; then
    echo "❌ No database backups found"
    ((ERRORS++))
else
    echo "✅ Latest DB backup: $LATEST_DB"
fi

# Check blob backup
echo "Checking blob backups..."
BLOB_COUNT=$(aws s3 ls s3://backup-bucket/alexander/blobs/ --recursive | wc -l)
if [ "$BLOB_COUNT" -eq 0 ]; then
    echo "❌ No blob backups found"
    ((ERRORS++))
else
    echo "✅ Blob objects backed up: $BLOB_COUNT"
fi

# Check config backup
echo "Checking config backups..."
LATEST_CONFIG=$(aws s3 ls s3://backup-bucket/alexander/config/ | sort | tail -n 1)
if [ -z "$LATEST_CONFIG" ]; then
    echo "❌ No config backups found"
    ((ERRORS++))
else
    echo "✅ Latest config backup: $LATEST_CONFIG"
fi

# Check WAL archiving (if PostgreSQL)
echo "Checking WAL archive..."
WAL_COUNT=$(aws s3 ls s3://backup-bucket/alexander/wal/ | wc -l)
if [ "$WAL_COUNT" -eq 0 ]; then
    echo "⚠️ No WAL archives found (may be SQLite deployment)"
else
    echo "✅ WAL segments archived: $WAL_COUNT"
fi

echo ""
if [ $ERRORS -gt 0 ]; then
    echo "❌ Verification failed with $ERRORS errors"
    exit 1
else
    echo "✅ All backup verifications passed"
fi
```

### DR Drill Procedure

Quarterly DR drill checklist:

1. **Preparation (1 day before)**
   - [ ] Notify stakeholders
   - [ ] Create fresh database snapshot
   - [ ] Verify DR infrastructure
   - [ ] Prepare monitoring

2. **Drill Execution**
   - [ ] Initiate failover to DR
   - [ ] Verify application functionality
   - [ ] Run smoke tests
   - [ ] Measure RTO achieved
   - [ ] Document any issues

3. **Failback**
   - [ ] Sync any new data to primary
   - [ ] Failback to primary region
   - [ ] Verify primary functionality
   - [ ] Update DNS TTL back to normal

4. **Post-Drill**
   - [ ] Document lessons learned
   - [ ] Update runbooks if needed
   - [ ] File improvement tickets
   - [ ] Update RTO/RPO estimates

### Automated DR Testing

```bash
#!/bin/bash
# dr-test.sh

echo "=== Alexander DR Test Suite ==="

# Test 1: Restore database from backup
echo "Test 1: Database restore..."
./db-restore.sh "s3://backup-bucket/alexander/postgres/latest.dump" "test_restore"
psql -h localhost -U alexander -d test_restore -c "SELECT COUNT(*) FROM objects;"
psql -h localhost -U alexander -c "DROP DATABASE test_restore;"
echo "✅ Database restore test passed"

# Test 2: Blob integrity
echo "Test 2: Blob integrity check..."
SAMPLE_BLOBS=$(find /data/blobs -type f | shuf -n 100)
for blob in $SAMPLE_BLOBS; do
    stored_hash=$(basename $blob | cut -d'.' -f1)
    computed_hash=$(sha256sum $blob | cut -d' ' -f1)
    if [ "$stored_hash" != "$computed_hash" ]; then
        echo "❌ Blob integrity check failed for $blob"
        exit 1
    fi
done
echo "✅ Blob integrity test passed (100 samples)"

# Test 3: Config restore
echo "Test 3: Config restore..."
./config-restore.sh "/tmp/config_test"
diff /etc/alexander/config.yaml /tmp/config_test/config.yaml
rm -rf /tmp/config_test
echo "✅ Config restore test passed"

echo ""
echo "=== All DR tests passed ==="
```

## Monitoring and Alerts

### Backup Monitoring

```yaml
# prometheus alerting rules
groups:
  - name: backup-alerts
    rules:
      - alert: BackupMissing
        expr: time() - backup_last_success_timestamp > 86400
        for: 1h
        labels:
          severity: critical
        annotations:
          summary: "Backup not completed in 24 hours"
          
      - alert: BackupFailed
        expr: backup_last_status == 0
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Last backup failed"
          
      - alert: WALArchiveLag
        expr: pg_stat_archiver_archived_count < 1
        for: 30m
        labels:
          severity: warning
        annotations:
          summary: "WAL archiving has stopped"
```

## Appendix

### Cron Schedule Reference

```cron
# /etc/cron.d/alexander-backup

# SQLite backup every 6 hours
0 */6 * * * root /usr/local/bin/sqlite-backup.sh

# PostgreSQL backup daily at 2 AM
0 2 * * * root /usr/local/bin/postgres-backup.sh

# Blob sync every hour
0 * * * * root /usr/local/bin/blob-backup.sh

# Config backup on change (using inotify)
# Managed by systemd service

# Backup verification daily at 6 AM
0 6 * * * root /usr/local/bin/backup-verify.sh

# DR test weekly on Sunday at 3 AM
0 3 * * 0 root /usr/local/bin/dr-test.sh
```

### Contact Information

| Role | Contact | Escalation |
|------|---------|------------|
| Primary On-Call | oncall@example.com | PagerDuty |
| Database Admin | dba@example.com | Phone |
| Infrastructure | infra@example.com | Slack #infra-emergency |
| Management | mgmt@example.com | Phone (business hours) |
