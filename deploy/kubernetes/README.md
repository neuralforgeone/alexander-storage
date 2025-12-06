# Alexander Storage Kubernetes Deployment

This directory contains Kubernetes manifests for deploying Alexander Storage.

## Quick Start

```bash
# Create namespace
kubectl create namespace alexander

# Apply all manifests
kubectl apply -f . -n alexander

# Wait for deployment
kubectl rollout status deployment/alexander-storage -n alexander
```

## Files

| File | Description |
|------|-------------|
| `deployment.yaml` | Main deployment with container specs, probes, and volumes |
| `service.yaml` | ClusterIP service for internal access |
| `configmap.yaml` | Configuration via environment variables and config file |
| `secret.yaml` | Secrets for master keys (MUST be replaced before deploying) |
| `pvc.yaml` | Persistent volume claim for data storage |
| `ingress.yaml` | Ingress rules for external access |
| `rbac.yaml` | Service account and RBAC rules |

## Prerequisites

1. Kubernetes cluster (v1.21+)
2. kubectl configured
3. Storage class available for PVCs
4. (Optional) Ingress controller (nginx-ingress recommended)
5. (Optional) cert-manager for TLS certificates

## Configuration

### 1. Generate Master Keys

```bash
# Generate secure master keys
MASTER_KEY=$(openssl rand -hex 32)
SSE_MASTER_KEY=$(openssl rand -hex 32)

# Update secret.yaml with these values
sed -i "s/REPLACE_WITH_SECURE_64_CHAR_HEX_VALUE_FOR_ACCESS_KEY_ENCRYPTION/$MASTER_KEY/" secret.yaml
sed -i "s/REPLACE_WITH_SECURE_64_CHAR_HEX_VALUE_FOR_SSE_ENCRYPTION_KEY/$SSE_MASTER_KEY/" secret.yaml
```

### 2. Configure Storage

Edit `pvc.yaml` to set:
- Storage size based on your needs
- Storage class if using a specific one

### 3. Configure Ingress

Edit `ingress.yaml` to set:
- Your domain name
- TLS configuration
- Ingress class (nginx, alb, etc.)

## Deployment Modes

### Single-Node (Default)

The default configuration uses SQLite with local storage, suitable for:
- Development
- Small deployments
- Homelabs

### PostgreSQL Backend

For production with PostgreSQL:

1. Update `configmap.yaml`:
```yaml
ALEXANDER_DATABASE_DRIVER: "postgres"
ALEXANDER_DATABASE_HOST: "postgres-host"
ALEXANDER_DATABASE_PORT: "5432"
ALEXANDER_DATABASE_NAME: "alexander"
```

2. Add PostgreSQL credentials to `secret.yaml`:
```yaml
ALEXANDER_DATABASE_USER: "alexander"
ALEXANDER_DATABASE_PASSWORD: "your-secure-password"
```

### With Redis (Distributed)

For multi-node deployments with distributed locking:

1. Update `configmap.yaml`:
```yaml
ALEXANDER_REDIS_ENABLED: "true"
ALEXANDER_REDIS_HOST: "redis-host"
ALEXANDER_REDIS_PORT: "6379"
```

2. Add Redis password to `secret.yaml` if authentication is enabled.

## Monitoring

The deployment exposes metrics on port 9091. To scrape with Prometheus:

```yaml
# ServiceMonitor for Prometheus Operator
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: alexander-storage
spec:
  selector:
    matchLabels:
      app: alexander-storage
  endpoints:
    - port: metrics
      interval: 30s
```

## Scaling

### Horizontal Scaling

For horizontal scaling with PostgreSQL backend:

```yaml
# Update deployment.yaml
spec:
  replicas: 3
```

**Note**: SQLite mode does NOT support horizontal scaling.

### Vertical Scaling

Adjust resource limits in `deployment.yaml`:

```yaml
resources:
  requests:
    cpu: 500m
    memory: 512Mi
  limits:
    cpu: 2000m
    memory: 4Gi
```

## Troubleshooting

### Check Logs

```bash
kubectl logs -f deployment/alexander-storage -n alexander
```

### Check Health

```bash
kubectl exec -it deployment/alexander-storage -n alexander -- wget -qO- http://localhost:8080/health
```

### Check PVC Status

```bash
kubectl get pvc -n alexander
```

## Backup

### Database Backup (SQLite)

```bash
kubectl exec -it deployment/alexander-storage -n alexander -- \
  sqlite3 /var/lib/alexander/alexander.db ".backup /tmp/backup.db"

kubectl cp alexander/alexander-storage-xxx:/tmp/backup.db ./backup.db
```

### Blob Storage Backup

Use your storage provider's backup mechanism or rsync the data directory.

## Security Considerations

1. **Secrets Management**: Use external secret management (Vault, AWS Secrets Manager) for production
2. **Network Policies**: Implement network policies to restrict pod-to-pod communication
3. **TLS**: Always use TLS in production (configure in Ingress)
4. **RBAC**: Review and restrict RBAC permissions as needed
5. **Pod Security**: The deployment uses security contexts for non-root execution
