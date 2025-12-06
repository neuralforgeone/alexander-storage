// Concurrent Access Load Test
// Tests concurrent read/write access to the same objects

import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { SharedArray } from 'k6/data';
import { S3Client, generateRandomData, generateKey } from '../helpers/s3-client.js';
import { getConfig } from '../config.js';

// Custom metrics
const concurrentReads = new Counter('concurrent_reads');
const concurrentWrites = new Counter('concurrent_writes');
const readLatency = new Trend('concurrent_read_latency');
const writeLatency = new Trend('concurrent_write_latency');
const conflictErrors = new Counter('conflict_errors');
const consistencyErrors = new Counter('consistency_errors');
const successRate = new Rate('success_rate');

const env = __ENV.ENV || 'local';
const config = getConfig(env);

export const options = {
  scenarios: {
    // Writers - constantly updating objects
    writers: {
      executor: 'constant-vus',
      vus: 20,
      duration: '5m',
      exec: 'writeObjects',
    },
    // Readers - constantly reading objects
    readers: {
      executor: 'constant-vus',
      vus: 50,
      duration: '5m',
      exec: 'readObjects',
      startTime: '10s',  // Start after writers have created some objects
    },
    // Mixed workload - 70% read, 30% write
    mixed: {
      executor: 'constant-vus',
      vus: 30,
      duration: '5m',
      exec: 'mixedWorkload',
      startTime: '30s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.02'],
    'concurrent_read_latency': ['p(95)<200'],
    'concurrent_write_latency': ['p(95)<500'],
    'success_rate': ['rate>0.95'],
  },
};

const testBucket = `k6-concurrent-${Date.now()}`;

// Shared object keys that all VUs will access
const sharedKeys = [];
for (let i = 0; i < 100; i++) {
  sharedKeys.push(`shared-object-${i}`);
}

export function setup() {
  const client = new S3Client(config);
  
  // Create test bucket
  let response = client.createBucket(testBucket);
  if (response.status !== 200 && response.status !== 409) {
    console.error(`Failed to create test bucket: ${response.status}`);
    return { bucket: null, keys: [] };
  }
  
  // Pre-populate with some objects
  console.log('Pre-populating test objects...');
  for (let i = 0; i < 50; i++) {
    const data = generateRandomData(1024);
    response = client.putObject(testBucket, sharedKeys[i], data);
    if (response.status !== 200) {
      console.error(`Failed to create initial object: ${response.status}`);
    }
  }
  
  console.log(`Created test bucket with initial objects: ${testBucket}`);
  return { bucket: testBucket, keys: sharedKeys };
}

// Writer scenario
export function writeObjects(data) {
  if (!data.bucket) return;
  
  const client = new S3Client(config);
  const key = data.keys[Math.floor(Math.random() * data.keys.length)];
  const testData = generateRandomData(1024 + Math.floor(Math.random() * 10240));
  
  const startTime = Date.now();
  const response = client.putObject(data.bucket, key, testData);
  const duration = Date.now() - startTime;
  
  writeLatency.add(duration);
  concurrentWrites.add(1);
  
  const success = check(response, {
    'Write status is 200': (r) => r.status === 200,
  });
  
  if (!success) {
    if (response.status === 409) {
      conflictErrors.add(1);
    }
    successRate.add(0);
  } else {
    successRate.add(1);
  }
  
  sleep(0.5 + Math.random() * 0.5);
}

// Reader scenario
export function readObjects(data) {
  if (!data.bucket) return;
  
  const client = new S3Client(config);
  const key = data.keys[Math.floor(Math.random() * data.keys.length)];
  
  const startTime = Date.now();
  const response = client.getObject(data.bucket, key);
  const duration = Date.now() - startTime;
  
  readLatency.add(duration);
  concurrentReads.add(1);
  
  const success = check(response, {
    'Read status is 200 or 404': (r) => r.status === 200 || r.status === 404,
  });
  
  if (response.status === 200) {
    // Verify we got valid data
    if (!response.body || response.body.length === 0) {
      consistencyErrors.add(1);
      successRate.add(0);
    } else {
      successRate.add(1);
    }
  } else if (response.status === 404) {
    // Object might have been deleted, that's ok
    successRate.add(1);
  } else {
    successRate.add(0);
  }
  
  sleep(0.1 + Math.random() * 0.2);
}

// Mixed workload scenario
export function mixedWorkload(data) {
  if (!data.bucket) return;
  
  const client = new S3Client(config);
  const key = data.keys[Math.floor(Math.random() * data.keys.length)];
  
  // 70% read, 30% write
  if (Math.random() < 0.7) {
    // Read
    const startTime = Date.now();
    const response = client.getObject(data.bucket, key);
    readLatency.add(Date.now() - startTime);
    concurrentReads.add(1);
    
    check(response, {
      'Mixed read status ok': (r) => r.status === 200 || r.status === 404,
    });
  } else {
    // Write
    const testData = generateRandomData(2048);
    const startTime = Date.now();
    const response = client.putObject(data.bucket, key, testData);
    writeLatency.add(Date.now() - startTime);
    concurrentWrites.add(1);
    
    check(response, {
      'Mixed write status ok': (r) => r.status === 200,
    });
  }
  
  sleep(0.2 + Math.random() * 0.3);
}

export function teardown(data) {
  if (!data.bucket) return;
  
  const client = new S3Client(config);
  
  // Delete all shared objects
  console.log('Cleaning up shared objects...');
  for (const key of data.keys) {
    client.deleteObject(data.bucket, key);
  }
  
  // Delete bucket
  const response = client.deleteBucket(data.bucket);
  if (response.status === 204 || response.status === 200) {
    console.log(`Deleted test bucket: ${data.bucket}`);
  }
}
