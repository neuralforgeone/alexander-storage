// k6 Load Test Configuration for Alexander Storage
// https://k6.io/docs/

export const environments = {
  local: {
    baseUrl: 'http://localhost:8080',
    accessKeyId: 'test-access-key',
    secretAccessKey: 'test-secret-key',
  },
  staging: {
    baseUrl: __ENV.ALEXANDER_URL || 'http://staging.alexander.local',
    accessKeyId: __ENV.AWS_ACCESS_KEY_ID,
    secretAccessKey: __ENV.AWS_SECRET_ACCESS_KEY,
  },
  production: {
    baseUrl: __ENV.ALEXANDER_URL || 'http://alexander.local',
    accessKeyId: __ENV.AWS_ACCESS_KEY_ID,
    secretAccessKey: __ENV.AWS_SECRET_ACCESS_KEY,
  },
};

export const scenarios = {
  // Smoke test - verify system works
  smoke: {
    executor: 'constant-vus',
    vus: 1,
    duration: '1m',
  },
  // Load test - average expected load
  load: {
    executor: 'ramping-vus',
    startVUs: 0,
    stages: [
      { duration: '2m', target: 50 },   // Ramp up
      { duration: '5m', target: 50 },   // Stay at 50 users
      { duration: '2m', target: 100 },  // Ramp up more
      { duration: '5m', target: 100 },  // Stay at 100 users
      { duration: '2m', target: 0 },    // Ramp down
    ],
  },
  // Stress test - find breaking point
  stress: {
    executor: 'ramping-vus',
    startVUs: 0,
    stages: [
      { duration: '2m', target: 100 },
      { duration: '5m', target: 100 },
      { duration: '2m', target: 200 },
      { duration: '5m', target: 200 },
      { duration: '2m', target: 300 },
      { duration: '5m', target: 300 },
      { duration: '2m', target: 400 },
      { duration: '5m', target: 400 },
      { duration: '10m', target: 0 },
    ],
  },
  // Spike test - sudden traffic surge
  spike: {
    executor: 'ramping-vus',
    startVUs: 0,
    stages: [
      { duration: '10s', target: 100 },  // Fast ramp up
      { duration: '1m', target: 100 },   // Hold
      { duration: '10s', target: 500 },  // Spike!
      { duration: '3m', target: 500 },   // Hold spike
      { duration: '10s', target: 100 },  // Scale down
      { duration: '3m', target: 100 },   // Hold
      { duration: '10s', target: 0 },    // Ramp down
    ],
  },
  // Soak test - extended duration
  soak: {
    executor: 'constant-vus',
    vus: 50,
    duration: '4h',
  },
};

export const thresholds = {
  // HTTP errors should be less than 1%
  http_req_failed: ['rate<0.01'],
  
  // 95% of requests should be below 500ms
  http_req_duration: ['p(95)<500', 'p(99)<1000'],
  
  // Specific operation thresholds
  'http_req_duration{operation:put_object}': ['p(95)<1000'],
  'http_req_duration{operation:get_object}': ['p(95)<200'],
  'http_req_duration{operation:list_objects}': ['p(95)<500'],
  'http_req_duration{operation:delete_object}': ['p(95)<300'],
  
  // Throughput targets
  http_reqs: ['rate>100'],
};

export function getConfig(env = 'local') {
  return environments[env] || environments.local;
}

export function getScenario(name = 'load') {
  return { [name]: scenarios[name] } || scenarios.load;
}
