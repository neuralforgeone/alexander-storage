// Listing Performance Load Test
// Tests bucket listing with many objects

import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { S3Client, generateRandomData, generateKey } from '../helpers/s3-client.js';
import { getConfig } from '../config.js';

// Custom metrics
const listLatency = new Trend('list_latency');
const listWithPrefixLatency = new Trend('list_with_prefix_latency');
const paginationLatency = new Trend('pagination_latency');
const objectsListed = new Counter('objects_listed');

const env = __ENV.ENV || 'local';
const config = getConfig(env);

export const options = {
  scenarios: {
    list_performance: {
      executor: 'constant-vus',
      vus: 20,
      duration: '10m',
    },
  },
  thresholds: {
    'list_latency': ['p(95)<1000'],
    'list_with_prefix_latency': ['p(95)<500'],
    'pagination_latency': ['p(95)<500'],
    http_req_failed: ['rate<0.01'],
  },
};

const testBucket = `k6-listing-${Date.now()}`;

// Pre-defined prefixes for organized testing
const prefixes = ['images/', 'documents/', 'videos/', 'backups/', 'logs/'];

export function setup() {
  const client = new S3Client(config);
  
  // Create test bucket
  let response = client.createBucket(testBucket);
  if (response.status !== 200 && response.status !== 409) {
    console.error(`Failed to create test bucket: ${response.status}`);
    return { bucket: null };
  }
  
  // Create many objects with different prefixes
  console.log('Creating test objects for listing tests...');
  
  const objectCount = 1000;  // Create 1000 objects
  let created = 0;
  
  for (let i = 0; i < objectCount; i++) {
    const prefix = prefixes[i % prefixes.length];
    const key = `${prefix}object-${i.toString().padStart(5, '0')}`;
    const data = generateRandomData(256);  // Small objects
    
    response = client.putObject(testBucket, key, data);
    if (response.status === 200) {
      created++;
    }
    
    // Progress logging
    if ((i + 1) % 100 === 0) {
      console.log(`Created ${i + 1}/${objectCount} objects...`);
    }
  }
  
  console.log(`Created ${created} test objects in ${testBucket}`);
  return { bucket: testBucket, objectCount: created };
}

export default function(data) {
  if (!data.bucket) return;
  
  const client = new S3Client(config);
  
  // Test 1: List all objects (no pagination)
  let startTime = Date.now();
  let response = client.listObjects(data.bucket, '', 1000);
  listLatency.add(Date.now() - startTime);
  
  check(response, {
    'List all status is 200': (r) => r.status === 200,
  });
  
  sleep(1);
  
  // Test 2: List with prefix
  const prefix = prefixes[Math.floor(Math.random() * prefixes.length)];
  startTime = Date.now();
  response = client.listObjects(data.bucket, prefix, 1000);
  listWithPrefixLatency.add(Date.now() - startTime);
  
  check(response, {
    'List with prefix status is 200': (r) => r.status === 200,
  });
  
  sleep(1);
  
  // Test 3: Paginated listing (small page size)
  startTime = Date.now();
  response = client.listObjects(data.bucket, '', 100);
  paginationLatency.add(Date.now() - startTime);
  
  let success = check(response, {
    'Paginated list status is 200': (r) => r.status === 200,
  });
  
  if (success) {
    objectsListed.add(100);
  }
  
  sleep(1);
  
  // Test 4: Multiple prefix listing (simulate directory browsing)
  for (const p of prefixes) {
    response = client.listObjects(data.bucket, p, 100);
    check(response, {
      'Prefix browse status is 200': (r) => r.status === 200,
    });
    sleep(0.2);
  }
  
  sleep(2);
}

export function teardown(data) {
  if (!data.bucket) return;
  
  const client = new S3Client(config);
  
  console.log('Cleaning up test objects (this may take a while)...');
  
  // Delete all objects
  for (const prefix of prefixes) {
    for (let i = 0; i < 200; i++) {
      const key = `${prefix}object-${(i + prefixes.indexOf(prefix) * 200).toString().padStart(5, '0')}`;
      client.deleteObject(data.bucket, key);
    }
  }
  
  // Delete bucket
  const response = client.deleteBucket(data.bucket);
  if (response.status === 204 || response.status === 200) {
    console.log(`Deleted test bucket: ${data.bucket}`);
  }
}
