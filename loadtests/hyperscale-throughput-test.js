import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { config, randomString, generateUUID } from './config.js';

// Custom metrics for throughput validation
const messagesProcessed = new Counter('messages_processed');
const messageLatency = new Trend('message_latency');
const messageErrors = new Rate('message_errors');
const throughputRPS = new Trend('throughput_rps');

// Hyperscale throughput test configuration
// Target: 2M messages/second globally (requirement 8.1, 8.2)
// This test validates message processing throughput
export const options = {
  scenarios: {
    // Constant arrival rate scenario for throughput testing
    constant_throughput: {
      executor: 'constant-arrival-rate',
      rate: 100000,              // 100K requests per second per region
      timeUnit: '1s',            // Per second
      duration: '10m',           // Run for 10 minutes
      preAllocatedVUs: 5000,     // Pre-allocate VUs
      maxVUs: 10000,             // Maximum VUs
    },
  },
  thresholds: {
    'http_req_duration{endpoint:send_message}': ['p(95)<200'],  // p95 < 200ms (req 8.1)
    'message_errors': ['rate<0.001'],                           // Error rate < 0.1%
    'messages_processed': ['count>50000000'],                   // Process > 50M messages in 10 min
    'throughput_rps': ['avg>90000'],                            // Average > 90K RPS
  },
};

// Setup: Create test infrastructure
export function setup() {
  const baseUrl = config.apiUrl;
  
  console.log('Setting up hyperscale throughput test...');
  
  // Create a pool of test users
  const users = [];
  const userCount = 1000;
  
  for (let i = 0; i < userCount; i++) {
    const username = `throughput_user_${Date.now()}_${i}`;
    const password = 'test123';
    
    const registerRes = http.post(`${baseUrl}/v1/auth/register`, JSON.stringify({
      username: username,
      password: password,
      email: `${username}@example.com`,
    }), {
      headers: { 'Content-Type': 'application/json' },
    });
    
    if (registerRes.status === 200 || registerRes.status === 201) {
      const loginRes = http.post(`${baseUrl}/v1/auth/login`, JSON.stringify({
        username: username,
        password: password,
      }), {
        headers: { 'Content-Type': 'application/json' },
      });
      
      if (loginRes.status === 200) {
        const body = JSON.parse(loginRes.body);
        users.push({
          username: username,
          token: body.token,
          userId: body.user_id,
        });
      }
    }
    
    if (i % 100 === 0) {
      console.log(`Created ${i + 1}/${userCount} users`);
      sleep(0.5);
    }
  }
  
  // Create test channels
  const channels = [];
  const channelCount = 100;
  
  if (users.length > 0) {
    const adminToken = users[0].token;
    
    for (let i = 0; i < channelCount; i++) {
      const channelName = `throughput_channel_${Date.now()}_${i}`;
      const createRes = http.post(`${baseUrl}/v1/channels`, JSON.stringify({
        name: channelName,
        type: 'public',
      }), {
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${adminToken}`,
        },
      });
      
      if (createRes.status === 200 || createRes.status === 201) {
        const body = JSON.parse(createRes.body);
        channels.push({
          id: body.id,
          name: channelName,
        });
      }
      
      if (i % 10 === 0) {
        console.log(`Created ${i + 1}/${channelCount} channels`);
      }
    }
  }
  
  console.log(`Setup complete. Users: ${users.length}, Channels: ${channels.length}`);
  return { users, channels };
}

// Main test scenario - high-throughput message sending
export default function(data) {
  if (!data.users || data.users.length === 0 || !data.channels || data.channels.length === 0) {
    console.error('Setup failed: no users or channels available');
    return;
  }
  
  const baseUrl = config.apiUrl;
  
  // Select random user and channel
  const user = data.users[Math.floor(Math.random() * data.users.length)];
  const channel = data.channels[Math.floor(Math.random() * data.channels.length)];
  
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${user.token}`,
  };
  
  // Send message with idempotency
  const idempotencyKey = generateUUID();
  const messageContent = `Throughput test: ${randomString(50)}`;
  
  const sendStart = Date.now();
  const sendRes = http.post(
    `${baseUrl}/v1/channels/${channel.id}/messages`,
    JSON.stringify({
      content: messageContent,
      idempotency_key: idempotencyKey,
      message_type: 'text',
    }),
    {
      headers: headers,
      tags: { endpoint: 'send_message' },
    }
  );
  const sendDuration = Date.now() - sendStart;
  
  const success = check(sendRes, {
    'message sent': (r) => r.status === 200 || r.status === 201,
    'response time acceptable': (r) => sendDuration < 500,
  });
  
  if (success) {
    messagesProcessed.add(1);
    messageLatency.add(sendDuration);
  } else {
    messageErrors.add(1);
  }
  
  // Calculate current throughput (messages per second)
  // This is an approximation based on iteration rate
  const iterationDuration = sendDuration / 1000; // Convert to seconds
  if (iterationDuration > 0) {
    const currentRPS = 1 / iterationDuration;
    throughputRPS.add(currentRPS);
  }
}

// Custom summary with throughput validation
export function handleSummary(data) {
  const totalMessages = data.metrics.messages_processed ? data.metrics.messages_processed.values.count : 0;
  const errorRate = data.metrics.message_errors.values.rate;
  const p95Latency = data.metrics.message_latency ? data.metrics.message_latency.values['p(95)'] : 0;
  const avgLatency = data.metrics.message_latency ? data.metrics.message_latency.values.avg : 0;
  const testDuration = data.state.testRunDurationMs / 1000; // Convert to seconds
  
  // Calculate actual throughput
  const actualThroughput = totalMessages / testDuration;
  
  let report = '\n=== Hyperscale Throughput Validation Report ===\n\n';
  report += 'Target: 2M messages/second globally (Requirement 8.1, 8.2)\n';
  report += 'Note: This test simulates a single region. Global throughput = sum of all regions.\n\n';
  
  report += `Test Duration: ${testDuration.toFixed(0)}s\n`;
  report += `Total Messages Processed: ${totalMessages.toLocaleString()}\n`;
  report += `Actual Throughput: ${actualThroughput.toFixed(0)} messages/second\n`;
  report += `Target Throughput (per region): 100,000 messages/second\n`;
  report += `Global Target: 2,000,000 messages/second (20 regions)\n\n`;
  
  report += `Message Latency:\n`;
  report += `  - Average: ${avgLatency.toFixed(2)}ms\n`;
  report += `  - P95: ${p95Latency.toFixed(2)}ms\n`;
  report += `  - Target P95: <200ms (Requirement 8.1)\n\n`;
  
  report += `Error Rate: ${(errorRate * 100).toFixed(3)}%\n`;
  report += `Target Error Rate: <0.1%\n\n`;
  
  // Validation against requirements
  let allPassed = true;
  
  if (actualThroughput >= 90000) { // 90K RPS = 90% of target
    report += `✅ PASSED: Throughput ${actualThroughput.toFixed(0)} RPS >= 90K RPS (90% of target)\n`;
  } else {
    report += `❌ FAILED: Throughput ${actualThroughput.toFixed(0)} RPS < 90K RPS\n`;
    allPassed = false;
  }
  
  if (p95Latency < 200) {
    report += `✅ PASSED: P95 latency ${p95Latency.toFixed(2)}ms < 200ms (Requirement 8.1)\n`;
  } else {
    report += `❌ FAILED: P95 latency ${p95Latency.toFixed(2)}ms >= 200ms\n`;
    allPassed = false;
  }
  
  if (errorRate < 0.001) {
    report += `✅ PASSED: Error rate ${(errorRate * 100).toFixed(3)}% < 0.1%\n`;
  } else {
    report += `❌ FAILED: Error rate ${(errorRate * 100).toFixed(3)}% >= 0.1%\n`;
    allPassed = false;
  }
  
  report += '\n=== Global Throughput Projection ===\n\n';
  report += 'Assuming 20 regions with similar performance:\n';
  const projectedGlobalThroughput = actualThroughput * 20;
  report += `Projected Global Throughput: ${(projectedGlobalThroughput / 1000000).toFixed(2)}M messages/second\n`;
  report += `Target: 2M messages/second\n`;
  
  if (projectedGlobalThroughput >= 2000000) {
    report += `✅ PASSED: Projected global throughput meets target\n`;
  } else {
    report += `⚠️  WARNING: Projected global throughput below target\n`;
    report += `   Consider: More regions, horizontal scaling, or performance optimization\n`;
  }
  
  if (allPassed) {
    report += '\n✅ All hyperscale throughput requirements validated successfully!\n';
  } else {
    report += '\n❌ Some hyperscale throughput requirements not met. Review results above.\n';
  }
  
  return {
    'stdout': report,
    'hyperscale-throughput-report.txt': report,
  };
}

// Teardown
export function teardown(data) {
  console.log('Hyperscale throughput test completed');
  console.log('Total users:', data.users.length);
  console.log('Total channels:', data.channels.length);
}
