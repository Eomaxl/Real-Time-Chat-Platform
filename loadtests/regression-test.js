import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend, Rate, Counter } from 'k6/metrics';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.1/index.js';
import { config, randomString, generateUUID } from './config.js';

// Performance baseline metrics
const baselineMetrics = {
  messageLatencyP95: 200,      // ms - from requirement 8.1
  signalingLatencyP95: 150,    // ms - from requirement 8.2
  databaseQueryP95: 5,         // ms - from requirement 8.3
  errorRate: 0.001,            // 0.1%
  throughputRPS: 1000,         // requests per second
};

// Custom metrics for regression detection
const regressionDetected = new Rate('regression_detected');
const performanceScore = new Trend('performance_score');
const baselineDeviation = new Trend('baseline_deviation');

// Test configuration - validates against performance baselines
export const options = {
  stages: [
    { duration: '1m', target: 100 },   // Warm up
    { duration: '5m', target: 500 },   // Steady state
    { duration: '1m', target: 0 },     // Cool down
  ],
  thresholds: {
    'http_req_duration{endpoint:send_message}': [`p(95)<${baselineMetrics.messageLatencyP95}`],
    'http_req_duration{endpoint:signaling}': [`p(95)<${baselineMetrics.signalingLatencyP95}`],
    'http_req_failed': [`rate<${baselineMetrics.errorRate}`],
    'regression_detected': ['rate<0.1'], // Less than 10% of tests should detect regression
    'performance_score': ['avg>0.8'],    // Average score should be > 80%
  },
};

// Setup: Create test data
export function setup() {
  const baseUrl = config.apiUrl;
  
  // Create test user
  const username = `regression_user_${Date.now()}`;
  const password = 'test123';
  
  const registerRes = http.post(`${baseUrl}/v1/auth/register`, JSON.stringify({
    username: username,
    password: password,
    email: `${username}@example.com`,
  }), {
    headers: { 'Content-Type': 'application/json' },
  });
  
  if (registerRes.status !== 200 && registerRes.status !== 201) {
    console.error('Failed to create test user');
    return null;
  }
  
  const loginRes = http.post(`${baseUrl}/v1/auth/login`, JSON.stringify({
    username: username,
    password: password,
  }), {
    headers: { 'Content-Type': 'application/json' },
  });
  
  if (loginRes.status !== 200) {
    console.error('Failed to login');
    return null;
  }
  
  const loginBody = JSON.parse(loginRes.body);
  const token = loginBody.token;
  const userId = loginBody.user_id;
  
  // Create test channel
  const channelRes = http.post(`${baseUrl}/v1/channels`, JSON.stringify({
    name: `regression_channel_${Date.now()}`,
    type: 'public',
  }), {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
    },
  });
  
  if (channelRes.status !== 200 && channelRes.status !== 201) {
    console.error('Failed to create channel');
    return null;
  }
  
  const channelBody = JSON.parse(channelRes.body);
  
  return {
    token: token,
    userId: userId,
    channelId: channelBody.id,
    username: username,
  };
}

// Main test scenario - performance regression detection
export default function(data) {
  if (!data) {
    console.error('Setup failed');
    return;
  }
  
  const baseUrl = config.apiUrl;
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${data.token}`,
  };
  
  let testsPassed = 0;
  let totalTests = 0;
  let regressionFound = false;
  
  // Test 1: Message sending latency
  totalTests++;
  const messageStart = Date.now();
  const messageRes = http.post(
    `${baseUrl}/v1/channels/${data.channelId}/messages`,
    JSON.stringify({
      content: `Regression test: ${randomString(50)}`,
      idempotency_key: generateUUID(),
      message_type: 'text',
    }),
    {
      headers: headers,
      tags: { endpoint: 'send_message', test: 'regression' },
    }
  );
  const messageLatency = Date.now() - messageStart;
  
  if (check(messageRes, {
    'message sent': (r) => r.status === 200 || r.status === 201,
  })) {
    testsPassed++;
  }
  
  // Check against baseline
  if (messageLatency > baselineMetrics.messageLatencyP95 * 1.2) {
    console.warn(`Message latency regression: ${messageLatency}ms > ${baselineMetrics.messageLatencyP95}ms baseline`);
    regressionFound = true;
  }
  
  sleep(0.5);
  
  // Test 2: Message history retrieval
  totalTests++;
  const historyStart = Date.now();
  const historyRes = http.get(
    `${baseUrl}/v1/channels/${data.channelId}/messages?limit=50`,
    {
      headers: headers,
      tags: { endpoint: 'get_history', test: 'regression' },
    }
  );
  const historyLatency = Date.now() - historyStart;
  
  if (check(historyRes, {
    'history retrieved': (r) => r.status === 200,
  })) {
    testsPassed++;
  }
  
  // Database query should be fast
  if (historyLatency > baselineMetrics.databaseQueryP95 * 20) { // 100ms threshold
    console.warn(`History query regression: ${historyLatency}ms`);
    regressionFound = true;
  }
  
  sleep(0.5);
  
  // Test 3: Presence heartbeat
  totalTests++;
  const heartbeatStart = Date.now();
  const heartbeatRes = http.post(
    `${baseUrl}/v1/presence/heartbeat`,
    JSON.stringify({
      user_id: data.userId,
      status: 'online',
    }),
    {
      headers: headers,
      tags: { endpoint: 'heartbeat', test: 'regression' },
    }
  );
  const heartbeatLatency = Date.now() - heartbeatStart;
  
  if (check(heartbeatRes, {
    'heartbeat sent': (r) => r.status === 200 || r.status === 204,
  })) {
    testsPassed++;
  }
  
  if (heartbeatLatency > 50) { // 50ms threshold for heartbeat
    console.warn(`Heartbeat latency regression: ${heartbeatLatency}ms`);
    regressionFound = true;
  }
  
  sleep(0.5);
  
  // Test 4: Call creation
  totalTests++;
  const callStart = Date.now();
  const callRes = http.post(
    `${baseUrl}/v1/channels/${data.channelId}/calls`,
    JSON.stringify({
      call_type: 'video',
    }),
    {
      headers: headers,
      tags: { endpoint: 'create_call', test: 'regression' },
    }
  );
  const callLatency = Date.now() - callStart;
  
  let callId = null;
  if (check(callRes, {
    'call created': (r) => r.status === 200 || r.status === 201,
  })) {
    testsPassed++;
    const callBody = JSON.parse(callRes.body);
    callId = callBody.id;
  }
  
  if (callLatency > 200) {
    console.warn(`Call creation regression: ${callLatency}ms`);
    regressionFound = true;
  }
  
  sleep(0.5);
  
  // Test 5: Signaling latency (if call was created)
  if (callId) {
    totalTests++;
    const signalingStart = Date.now();
    const signalingRes = http.post(
      `${baseUrl}/v1/calls/${callId}/signaling`,
      JSON.stringify({
        from_user_id: data.userId,
        to_user_id: data.userId,
        message_type: 'offer',
        payload: { type: 'offer', sdp: 'test-sdp' },
      }),
      {
        headers: headers,
        tags: { endpoint: 'signaling', test: 'regression' },
      }
    );
    const signalingLatency = Date.now() - signalingStart;
    
    if (check(signalingRes, {
      'signaling sent': (r) => r.status === 200 || r.status === 204,
    })) {
      testsPassed++;
    }
    
    // Check against baseline (requirement 8.2: p95 < 150ms)
    if (signalingLatency > baselineMetrics.signalingLatencyP95 * 1.2) {
      console.warn(`Signaling latency regression: ${signalingLatency}ms > ${baselineMetrics.signalingLatencyP95}ms baseline`);
      regressionFound = true;
    }
  }
  
  // Calculate performance score
  const score = testsPassed / totalTests;
  performanceScore.add(score);
  
  // Calculate deviation from baseline
  const avgLatency = (messageLatency + historyLatency + heartbeatLatency + callLatency) / 4;
  const baselineAvg = (baselineMetrics.messageLatencyP95 + 100 + 50 + 200) / 4;
  const deviation = ((avgLatency - baselineAvg) / baselineAvg) * 100;
  baselineDeviation.add(deviation);
  
  regressionDetected.add(regressionFound ? 1 : 0);
  
  sleep(1);
}

// Custom summary with regression report
export function handleSummary(data) {
  const regressionRate = data.metrics.regression_detected.values.rate;
  const avgScore = data.metrics.performance_score.values.avg;
  const avgDeviation = data.metrics.baseline_deviation.values.avg;
  
  let regressionReport = '\n=== Performance Regression Report ===\n\n';
  regressionReport += `Performance Score: ${(avgScore * 100).toFixed(2)}%\n`;
  regressionReport += `Baseline Deviation: ${avgDeviation.toFixed(2)}%\n`;
  regressionReport += `Regression Detection Rate: ${(regressionRate * 100).toFixed(2)}%\n\n`;
  
  regressionReport += 'Performance Baselines (from requirements 8.1, 8.2, 8.3):\n';
  regressionReport += `  - Message Latency p95: ${baselineMetrics.messageLatencyP95}ms\n`;
  regressionReport += `  - Signaling Latency p95: ${baselineMetrics.signalingLatencyP95}ms\n`;
  regressionReport += `  - Database Query p95: ${baselineMetrics.databaseQueryP95}ms\n`;
  regressionReport += `  - Error Rate: ${(baselineMetrics.errorRate * 100).toFixed(2)}%\n\n`;
  
  if (regressionRate > 0.1) {
    regressionReport += '⚠️  WARNING: Performance regression detected!\n';
    regressionReport += 'More than 10% of tests showed performance degradation.\n';
  } else if (avgScore < 0.8) {
    regressionReport += '⚠️  WARNING: Low performance score!\n';
    regressionReport += 'Average test success rate is below 80%.\n';
  } else {
    regressionReport += '✅ All performance metrics within acceptable range.\n';
  }
  
  return {
    'stdout': textSummary(data, { indent: ' ', enableColors: true }) + regressionReport,
    'regression-report.txt': regressionReport,
    'summary.json': JSON.stringify(data, null, 2),
  };
}

// Teardown
export function teardown(data) {
  if (data) {
    console.log('Regression test completed for user:', data.username);
  }
}
