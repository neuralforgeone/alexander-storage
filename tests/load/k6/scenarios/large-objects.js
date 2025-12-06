// Large Object Upload Load Test
// Tests performance with larger files (1MB - 100MB)

import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { S3Client, generateRandomData, generateKey } from '../helpers/s3-client.js';
import { getConfig, thresholds } from '../config.js';

// Custom metrics
const uploadThroughput = new Trend('upload_throughput_mbps');
const downloadThroughput = new Trend('download_throughput_mbps');
const largeUploadDuration = new Trend('large_upload_duration');
const largeDownloadDuration = new Trend('large_download_duration');
const operationErrors = new Counter('operation_errors');

const env = __ENV.ENV || 'local';
const config = getConfig(env);

export const options = {
  scenarios: {
    large_objects: {
      executor: 'constant-vus',
      vus: 10,
      duration: '10m',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.05'],  // Allow 5% errors for large files
    'large_upload_duration': ['p(95)<60000'],  // 60s for large uploads
    'large_download_duration': ['p(95)<30000'], // 30s for large downloads
    'upload_throughput_mbps': ['avg>10'],  // At least 10 MB/s average
    'download_throughput_mbps': ['avg>20'], // At least 20 MB/s average
  },
};

const testBucket = `k6-large-${Date.now()}`;

export function setup() {
  const client = new S3Client(config);
  const response = client.createBucket(testBucket);
  
  if (response.status !== 200 && response.status !== 409) {
    console.error(`Failed to create test bucket: ${response.status}`);
    return { bucket: null };
  }
  
  console.log(`Created test bucket: ${testBucket}`);
  return { bucket: testBucket };
}

export default function(data) {
  if (!data.bucket) return;
  
  const client = new S3Client(config);
  const bucket = data.bucket;
  
  // Generate large test data (1MB, 5MB, 10MB, 50MB)
  const sizes = [
    { size: 1 * 1024 * 1024, name: '1MB' },
    { size: 5 * 1024 * 1024, name: '5MB' },
    { size: 10 * 1024 * 1024, name: '10MB' },
    { size: 50 * 1024 * 1024, name: '50MB' },
  ];
  
  const selectedSize = sizes[Math.floor(Math.random() * sizes.length)];
  const testData = generateRandomData(selectedSize.size);
  const key = generateKey(`large-${selectedSize.name}`);
  
  console.log(`Testing ${selectedSize.name} object...`);
  
  // Upload
  let startTime = Date.now();
  let response = client.putObject(bucket, key, testData, 'application/octet-stream');
  let duration = Date.now() - startTime;
  
  largeUploadDuration.add(duration);
  
  let success = check(response, {
    'Large PUT status is 200': (r) => r.status === 200,
  });
  
  if (success) {
    // Calculate throughput in MB/s
    const throughput = (selectedSize.size / (1024 * 1024)) / (duration / 1000);
    uploadThroughput.add(throughput);
    console.log(`Uploaded ${selectedSize.name} in ${duration}ms (${throughput.toFixed(2)} MB/s)`);
  } else {
    operationErrors.add(1);
    console.error(`Large upload failed: ${response.status}`);
    return;
  }
  
  sleep(1);
  
  // Download
  startTime = Date.now();
  response = client.getObject(bucket, key);
  duration = Date.now() - startTime;
  
  largeDownloadDuration.add(duration);
  
  success = check(response, {
    'Large GET status is 200': (r) => r.status === 200,
    'Large GET size correct': (r) => r.body && r.body.length === selectedSize.size,
  });
  
  if (success) {
    const throughput = (selectedSize.size / (1024 * 1024)) / (duration / 1000);
    downloadThroughput.add(throughput);
    console.log(`Downloaded ${selectedSize.name} in ${duration}ms (${throughput.toFixed(2)} MB/s)`);
  } else {
    operationErrors.add(1);
    console.error(`Large download failed: ${response.status}`);
  }
  
  sleep(1);
  
  // Cleanup
  response = client.deleteObject(bucket, key);
  check(response, {
    'Large DELETE status is 204': (r) => r.status === 204 || r.status === 200,
  });
  
  sleep(2);
}

export function teardown(data) {
  if (!data.bucket) return;
  
  const client = new S3Client(config);
  const response = client.deleteBucket(data.bucket);
  
  if (response.status === 204 || response.status === 200) {
    console.log(`Deleted test bucket: ${data.bucket}`);
  }
}
