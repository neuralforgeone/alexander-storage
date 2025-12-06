# Alexander Storage - k6 Load Tests

Performance and load testing scripts for Alexander Storage using [k6](https://k6.io/).

## Prerequisites

1. Install k6:
   ```bash
   # macOS
   brew install k6
   
   # Windows
   choco install k6
   
   # Linux
   sudo gpg -k
   sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg \
     --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
   echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | \
     sudo tee /etc/apt/sources.list.d/k6.list
   sudo apt-get update
   sudo apt-get install k6
   ```

2. Start Alexander Storage server:
   ```bash
   make run
   # or
   ./bin/alexander-server -config configs/config.yaml
   ```

## Test Scenarios

### Basic Operations
Tests core S3 operations (PUT, GET, HEAD, DELETE) under load.

```bash
# Smoke test (1 VU, 1 minute)
k6 run -e SCENARIO=smoke scenarios/basic-operations.js

# Load test (ramping to 100 VUs)
k6 run -e SCENARIO=load scenarios/basic-operations.js

# Stress test (ramping to 400 VUs)
k6 run -e SCENARIO=stress scenarios/basic-operations.js

# Spike test (sudden traffic surge)
k6 run -e SCENARIO=spike scenarios/basic-operations.js
```

### Large Objects
Tests upload/download performance with large files (1MB - 50MB).

```bash
k6 run scenarios/large-objects.js
```

### Concurrent Access
Tests concurrent read/write access to the same objects.

```bash
k6 run scenarios/concurrent-access.js
```

### Listing Performance
Tests bucket listing with many objects.

```bash
k6 run scenarios/listing-performance.js
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `ENV` | Environment (local, staging, production) | `local` |
| `SCENARIO` | Test scenario type | `load` |
| `ALEXANDER_URL` | Server URL | `http://localhost:8080` |
| `AWS_ACCESS_KEY_ID` | Access key ID | - |
| `AWS_SECRET_ACCESS_KEY` | Secret access key | - |

### Running Against Different Environments

```bash
# Local
k6 run -e ENV=local scenarios/basic-operations.js

# Staging
k6 run -e ENV=staging \
  -e ALEXANDER_URL=http://staging.example.com \
  -e AWS_ACCESS_KEY_ID=xxx \
  -e AWS_SECRET_ACCESS_KEY=xxx \
  scenarios/basic-operations.js

# Production
k6 run -e ENV=production \
  -e ALEXANDER_URL=https://s3.example.com \
  -e AWS_ACCESS_KEY_ID=xxx \
  -e AWS_SECRET_ACCESS_KEY=xxx \
  scenarios/basic-operations.js
```

## Test Scenarios Explained

### Smoke Test
- 1 VU for 1 minute
- Verify the system works under minimal load
- Quick sanity check

### Load Test
- Ramps from 0 to 100 VUs over 16 minutes
- Simulates average expected load
- Validates performance under normal conditions

### Stress Test
- Ramps from 0 to 400 VUs over 38 minutes
- Finds the system's breaking point
- Identifies performance degradation thresholds

### Spike Test
- Sudden spike from 100 to 500 VUs
- Tests system's ability to handle sudden traffic surges
- Validates auto-scaling and recovery

### Soak Test
- 50 VUs for 4 hours
- Tests system stability over extended periods
- Identifies memory leaks and resource exhaustion

## Thresholds

Default thresholds are configured in `config.js`:

| Metric | Threshold |
|--------|-----------|
| HTTP errors | < 1% |
| Request duration (p95) | < 500ms |
| Request duration (p99) | < 1000ms |
| PUT object (p95) | < 1000ms |
| GET object (p95) | < 200ms |
| List objects (p95) | < 500ms |
| Requests/sec | > 100 |

## Outputting Results

### JSON Output
```bash
k6 run --out json=results.json scenarios/basic-operations.js
```

### CSV Output
```bash
k6 run --out csv=results.csv scenarios/basic-operations.js
```

### InfluxDB Output
```bash
k6 run --out influxdb=http://localhost:8086/k6 scenarios/basic-operations.js
```

### Grafana Cloud
```bash
K6_CLOUD_TOKEN=xxx k6 cloud scenarios/basic-operations.js
```

## Custom Metrics

Each test defines custom metrics for detailed analysis:

- `put_object_duration` - PUT operation latency
- `get_object_duration` - GET operation latency
- `delete_object_duration` - DELETE operation latency
- `upload_throughput_mbps` - Upload throughput in MB/s
- `download_throughput_mbps` - Download throughput in MB/s
- `concurrent_reads` - Number of concurrent read operations
- `concurrent_writes` - Number of concurrent write operations
- `success_rate` - Overall operation success rate

## Troubleshooting

### Connection Errors
- Verify Alexander server is running
- Check firewall settings
- Validate endpoint URL

### Authentication Failures
- Verify access key and secret key are correct
- Check AWS Signature V4 signing

### High Error Rates
- Check server logs for errors
- Monitor server resources (CPU, memory, disk)
- Consider scaling down VUs

### Slow Performance
- Check network latency
- Monitor server metrics
- Review disk I/O performance

## Contributing

When adding new test scenarios:

1. Create a new file in `scenarios/`
2. Use the `S3Client` helper from `helpers/s3-client.js`
3. Define appropriate thresholds
4. Include setup and teardown functions
5. Document the scenario in this README
