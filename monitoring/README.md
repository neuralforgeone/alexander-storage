# Alexander Storage Monitoring

This directory contains monitoring configurations for Alexander Storage.

## Contents

```
monitoring/
├── grafana/
│   └── dashboard.json      # Grafana dashboard for visualization
├── prometheus/
│   └── alerts.yaml         # Prometheus alerting rules
└── README.md
```

## Grafana Dashboard

Import the dashboard from `grafana/dashboard.json` into your Grafana instance.

### Features

- **Overview**: Request rate, P95 latency, error rate, in-flight requests
- **HTTP Requests**: Request rate by method, latency percentiles
- **Storage Operations**: Operations by type, throughput
- **Authentication**: Auth attempts, failure rate
- **Rate Limiting**: Rate limited requests over time
- **Garbage Collection**: Blobs deleted, bytes freed

### Variables

- `datasource`: Prometheus data source
- `job`: Job name (default: `alexander`)

### Import

1. Open Grafana → Dashboards → Import
2. Upload `dashboard.json` or paste its contents
3. Select your Prometheus data source
4. Click Import

## Prometheus Alerts

Add the alerting rules from `prometheus/alerts.yaml` to your Prometheus configuration.

### Alert Categories

#### Availability
- `AlexanderDown`: Instance is unreachable
- `AlexanderHighErrorRate`: >5% error rate (warning)
- `AlexanderCriticalErrorRate`: >20% error rate (critical)

#### Latency
- `AlexanderHighLatency`: P95 > 1s (warning)
- `AlexanderVeryHighLatency`: P99 > 5s (critical)

#### Rate Limiting
- `AlexanderHighRateLimiting`: >10% requests rate limited

#### Authentication
- `AlexanderHighAuthFailures`: >20% auth failures
- `AlexanderPossibleBruteForce`: >100 failed attempts/minute

#### Storage
- `AlexanderStorageErrors`: Storage operation failures

#### Garbage Collection
- `AlexanderGCFailed`: GC run failed
- `AlexanderGCNotRunning`: GC hasn't run in 2+ hours

#### Health
- `AlexanderUnhealthy`: Overall health check failing
- `AlexanderDatabaseUnhealthy`: Database component unhealthy
- `AlexanderStorageUnhealthy`: Storage component unhealthy

#### Resources (Kubernetes)
- `AlexanderHighMemoryUsage`: >85% memory limit
- `AlexanderHighCPUUsage`: >85% CPU limit

### Configuration

Add to your Prometheus configuration:

```yaml
rule_files:
  - /etc/prometheus/rules/alexander-alerts.yaml

# Or if using Prometheus Operator
# Copy alerts.yaml content to a PrometheusRule CR
```

## Prometheus Scrape Config

Add Alexander to your Prometheus scrape targets:

```yaml
scrape_configs:
  - job_name: 'alexander'
    static_configs:
      - targets: ['alexander-storage:9091']
    # Or for Kubernetes service discovery:
    # kubernetes_sd_configs:
    #   - role: endpoints
    # relabel_configs:
    #   - source_labels: [__meta_kubernetes_service_name]
    #     regex: alexander-storage
    #     action: keep
```

## Metrics Reference

### HTTP Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `alexander_http_requests_total` | Counter | Total HTTP requests by method, path, code |
| `alexander_http_request_duration_seconds` | Histogram | Request duration |
| `alexander_http_requests_in_flight` | Gauge | Current in-flight requests |
| `alexander_http_response_size_bytes` | Histogram | Response size |

### Storage Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `alexander_storage_operations_total` | Counter | Storage operations by type and status |
| `alexander_storage_operation_duration_seconds` | Histogram | Storage operation duration |
| `alexander_storage_bytes_total` | Counter | Bytes read/written |

### Auth Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `alexander_auth_attempts_total` | Counter | Auth attempts by result |
| `alexander_auth_failures_total` | Counter | Auth failures by reason |

### GC Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `alexander_gc_runs_total` | Counter | GC runs by status |
| `alexander_gc_blobs_deleted_total` | Counter | Blobs deleted |
| `alexander_gc_bytes_freed_total` | Counter | Bytes freed |
| `alexander_gc_duration_seconds` | Histogram | GC duration |
| `alexander_gc_last_run_timestamp` | Gauge | Last GC run timestamp |

### Rate Limit Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `alexander_rate_limit_requests_total` | Counter | Rate limit decisions |

### Health Metrics
| Metric | Type | Description |
|--------|------|-------------|
| `alexander_health_status` | Gauge | Overall health (1=healthy, 0=unhealthy) |
| `alexander_health_component_status` | Gauge | Per-component health |
