// Basic Operations Load Test
// Tests core S3 operations under load

import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { S3Client, generateRandomData, generateKey } from '../helpers/s3-client.js';
import { getConfig, thresholds, scenarios } from '../config.js';

// Custom metrics
const putObjectDuration = new Trend('put_object_duration');
const getObjectDuration = new Trend('get_object_duration');
const deleteObjectDuration = new Trend('delete_object_duration');
const operationErrors = new Counter('operation_errors');
const successRate = new Rate('success_rate');

// Configuration
const env = __ENV.ENV || 'local';
const scenario = __ENV.SCENARIO || 'load';
const config = getConfig(env);

export const options = {
  scenarios: {
    basic_operations: scenarios[scenario],
  },
  thresholds: thresholds,
};

// Shared test bucket name
const testBucket = `k6-test-${Date.now()}`;

// Setup: Create test bucket
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

// Main test function
export default function(data) {
  if (!data.bucket) {
    console.error('Test bucket not available');
    return;
  }
  
  const client = new S3Client(config);
  const bucket = data.bucket;
  
  // Generate test data (1KB - 1MB random)
  const sizes = [1024, 10240, 102400, 1048576];
  const dataSize = sizes[Math.floor(Math.random() * sizes.length)];
  const testData = generateRandomData(dataSize);
  const key = generateKey(`vu-${__VU}`);
  
  // PUT Object
  let startTime = Date.now();
  let response = client.putObject(bucket, key, testData, 'text/plain');
  putObjectDuration.add(Date.now() - startTime);
  
  let success = check(response, {
    'PUT status is 200': (r) => r.status === 200,
  });
  
  if (!success) {
    operationErrors.add(1);
    successRate.add(0);
    console.error(`PUT failed: ${response.status} - ${response.body}`);
    return;
  }
  successRate.add(1);
  
  sleep(0.1);
  
  // GET Object
  startTime = Date.now();
  response = client.getObject(bucket, key);
  getObjectDuration.add(Date.now() - startTime);
  
  success = check(response, {
    'GET status is 200': (r) => r.status === 200,
    'GET body matches': (r) => r.body === testData,
  });
  
  if (!success) {
    operationErrors.add(1);
    successRate.add(0);
    console.error(`GET failed: ${response.status}`);
  } else {
    successRate.add(1);
  }
  
  sleep(0.1);
  
  // HEAD Object
  response = client.headObject(bucket, key);
  check(response, {
    'HEAD status is 200': (r) => r.status === 200,
    'HEAD content-length correct': (r) => r.headers['Content-Length'] === testData.length.toString(),
  });
  
  sleep(0.1);
  
  // DELETE Object
  startTime = Date.now();
  response = client.deleteObject(bucket, key);
  deleteObjectDuration.add(Date.now() - startTime);
  
  success = check(response, {
    'DELETE status is 204': (r) => r.status === 204 || r.status === 200,
  });
  
  if (!success) {
    operationErrors.add(1);
    successRate.add(0);
    console.error(`DELETE failed: ${response.status}`);
  } else {
    successRate.add(1);
  }
  
  // Random sleep between iterations (0.5-2s)
  sleep(0.5 + Math.random() * 1.5);
}

// Teardown: Delete test bucket
export function teardown(data) {
  if (!data.bucket) return;
  
  const client = new S3Client(config);
  
  // List and delete all objects first
  const listResponse = client.listObjects(data.bucket);
  if (listResponse.status === 200) {
    // Note: In real scenario, parse XML and delete each object
    console.log('Cleaning up test objects...');
  }
  
  // Delete bucket
  const response = client.deleteBucket(data.bucket);
  if (response.status === 204 || response.status === 200) {
    console.log(`Deleted test bucket: ${data.bucket}`);
  } else {
    console.error(`Failed to delete test bucket: ${response.status}`);
  }
}
