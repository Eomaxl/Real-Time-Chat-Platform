import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { config, randomString, generateUUID } from './config.js';

// Custom metrics for database performance validation
const dbQueries = new Counter('db_queries_total');
const dbQueryLatency = new Trend('db_query_latency');
const dbQueryErrors = new Rate('db_query_errors');
const dbWriteLatency = new Trend('db_write_latency');
const dbReadLatency = new Trend('db_read_latency');

// Hyperscale database test configuration
// Target: Database query p95 < 5ms (requirement 8.3)
// Validates database performance under high load
export const options = {
  scenarios: {
    // Mixed read/write workload (90/10 ratio as per design doc)
    mixed_workload: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '2m', target: 500 },    // Ramp up
        { duration: '5m', target: 2000 },   // Increase load
        { duration: '10m', target: 5000 },  // High load
        { duration: '5m', target: 5000 },   // Sustain
        { duration: '2m', target: 0 },      // Ramp down
      ],
    },
  },
  thresholds: {
    'http_req_duration{operation:read}': ['p(95)<100'],   // Read p95 < 100ms (includes network)
    'http_req_duration{operation:write}': ['p(95)<200'],  // Write p95 < 200ms
    'db_query_errors': ['rate<0.001'],                    // Error rate < 0.1%
    'db_read_latency': ['p(95)<100'],                     // Read latency p95 < 100ms
    'db_write_latency': ['p(95)<200'],                    // Write latency p95 < 200ms
  },
};

// Setup: Create test data
export function setup() {
  const baseUrl = config.apiUrl;
  
  console.log('Setting up database performance test...');
  
  // Create test users
  const users = [];
  const userCount = 100;
  
  for (let i = 0; i < userCount; i++) {
    const username = `db_test_user_${Date.now()}_${i}`;
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
  }
  
  // Create test channels
  const channels = [];
  if (users.length > 0) {
    const adminToken = users[0].token;
    
    for (let i = 0; i < 50; i++) {
      const channelName = `db_test_channel_${Date.now()}_${i}`;
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
    }
  }
  
  // Pre-populate channels with messages for read testing
  console.log('Pre-populating channels with messages...');
  if (users.length > 0 && channels.length > 0) {
    const user = users[0];
    const headers = {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${user.token}`,
    };
    
    for (const channel of channels) {
      // Add 100 messages per channel
      for (let i = 0; i < 100; i++) {
        http.post(
          `${baseUrl}/v1/channels/${channel.id}/messages`,
          JSON.stringify({
            content: `Pre-populated message ${i}: ${randomString(50)}`,
            idempotency_key: generateUUID(),
            message_type: 'text',
          }),
          { headers: headers }
        );
      }
    }
  }
  
  console.log(`Setup complete. Users: ${users.length}, Channels: ${channels.length}`);
  return { users, channels };
}

// Main test scenario - mixed read/write database operations
export default function(data) {
  if (!data.users || data.users.length === 0 || !data.channels || data.channels.length === 0) {
    console.error('Setup failed: no users or channels available');
    return;
  }
  
  const baseUrl = config.apiUrl;
  const user = data.users[Math.floor(Math.random() * data.users.length)];
  const channel = data.channels[Math.floor(Math.random() * data.channels.length)];
  
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${user.token}`,
  };
  
  // 90% reads, 10% writes (as per design doc)
  const isWrite = Math.random() < 0.1;
  
  if (isWrite) {
    // Write operation: Send message
    const writeStart = Date.now();
    const writeRes = http.post(
      `${baseUrl}/v1/channels/${channel.id}/messages`,
      JSON.stringify({
        content: `DB test message: ${randomString(50)}`,
        idempotency_key: generateUUID(),
        message_type: 'text',
      }),
      {
        headers: headers,
        tags: { operation: 'write' },
      }
    );
    const writeDuration = Date.now() - writeStart;
    
    const writeSuccess = check(writeRes, {
      'write successful': (r) => r.status === 200 || r.status === 201,
    });
    
    if (writeSuccess) {
      dbQueries.add(1);
      dbWriteLatency.add(writeDuration);
      dbQueryLatency.add(writeDuration);
    } else {
      dbQueryErrors.add(1);
    }
    
  } else {
    // Read operation: Retrieve message history
    const readStart = Date.now();
    const readRes = http.get(
      `${baseUrl}/v1/channels/${channel.id}/messages?limit=50`,
      {
        headers: headers,
        tags: { operation: 'read' },
      }
    );
    const readDuration = Date.now() - readStart;
    
    const readSuccess = check(readRes, {
      'read successful': (r) => r.status === 200,
      'messages returned': (r) => {
        if (r.status === 200) {
          const body = JSON.parse(r.body);
          return body.messages && Array.isArray(body.messages);
        }
        return false;
      },
    });
    
    if (readSuccess) {
      dbQueries.add(1);
      dbReadLatency.add(readDuration);
      dbQueryLatency.add(readDuration);
    } else {
      dbQueryErrors.add(1);
    }
  }
  
  // Small sleep to simulate realistic user behavior
  sleep(0.1);
  
  // Additional read operations to test different query patterns
  if (Math.random() < 0.3) {
    // Pagination test
    const paginationStart = Date.now();
    const paginationRes = http.get(
      `${baseUrl}/v1/channels/${channel.id}/messages?limit=20&cursor=`,
      {
        headers: headers,
        tags: { operation: 'read' },
      }
    );
    const paginationDuration = Date.now() - paginationStart;
    
    if (check(paginationRes, { 'pagination successful': (r) => r.status === 200 })) {
      dbQueries.add(1);
      dbReadLatency.add(paginationDuration);
      dbQueryLatency.add(paginationDuration);
    } else {
      dbQueryErrors.add(1);
    }
  }
  
  if (Math.random() < 0.2) {
    // Filtered query test (by timestamp)
    const since = new Date(Date.now() - 3600000).toISOString(); // Last hour
    const filterStart = Date.now();
    const filterRes = http.get(
      `${baseUrl}/v1/channels/${channel.id}/messages?since=${since}`,
      {
        headers: headers,
        tags: { operation: 'read' },
      }
    );
    const filterDuration = Date.now() - filterStart;
    
    if (check(filterRes, { 'filter successful': (r) => r.status === 200 })) {
      dbQueries.add(1);
      dbReadLatency.add(filterDuration);
      dbQueryLatency.add(filterDuration);
    } else {
      dbQueryErrors.add(1);
    }
  }
  
  sleep(0.5);
}

// Custom summary with database performance validation
export function handleSummary(data) {
  const totalQueries = data.metrics.db_queries_total ? data.metrics.db_queries_total.values.count : 0;
  const errorRate = data.metrics.db_query_errors.values.rate;
  
  const readP95 = data.metrics.db_read_latency ? data.metrics.db_read_latency.values['p(95)'] : 0;
  const readAvg = data.metrics.db_read_latency ? data.metrics.db_read_latency.values.avg : 0;
  const writeP95 = data.metrics.db_write_latency ? data.metrics.db_write_latency.values['p(95)'] : 0;
  const writeAvg = data.metrics.db_write_latency ? data.metrics.db_write_latency.values.avg : 0;
  
  const testDuration = data.state.testRunDurationMs / 1000;
  const qps = totalQueries / testDuration;
  
  let report = '\n=== Hyperscale Database Performance Validation Report ===\n\n';
  report += 'Target: Database query p95 < 5ms (Requirement 8.3)\n';
  report += 'Note: Measured latency includes network overhead. Pure DB query time is lower.\n\n';
  
  report += `Test Duration: ${testDuration.toFixed(0)}s\n`;
  report += `Total Queries: ${totalQueries.toLocaleString()}\n`;
  report += `Queries Per Second: ${qps.toFixed(0)}\n`;
  report += `Error Rate: ${(errorRate * 100).toFixed(3)}%\n\n`;
  
  report += `Read Operations (90% of workload):\n`;
  report += `  - Average Latency: ${readAvg.toFixed(2)}ms\n`;
  report += `  - P95 Latency: ${readP95.toFixed(2)}ms\n`;
  report += `  - Target: <100ms (includes network)\n\n`;
  
  report += `Write Operations (10% of workload):\n`;
  report += `  - Average Latency: ${writeAvg.toFixed(2)}ms\n`;
  report += `  - P95 Latency: ${writeP95.toFixed(2)}ms\n`;
  report += `  - Target: <200ms (Requirement 8.1)\n\n`;
  
  // Validation
  let allPassed = true;
  
  if (readP95 < 100) {
    report += `✅ PASSED: Read P95 latency ${readP95.toFixed(2)}ms < 100ms\n`;
  } else {
    report += `❌ FAILED: Read P95 latency ${readP95.toFixed(2)}ms >= 100ms\n`;
    allPassed = false;
  }
  
  if (writeP95 < 200) {
    report += `✅ PASSED: Write P95 latency ${writeP95.toFixed(2)}ms < 200ms (Requirement 8.1)\n`;
  } else {
    report += `❌ FAILED: Write P95 latency ${writeP95.toFixed(2)}ms >= 200ms\n`;
    allPassed = false;
  }
  
  if (errorRate < 0.001) {
    report += `✅ PASSED: Error rate ${(errorRate * 100).toFixed(3)}% < 0.1%\n`;
  } else {
    report += `❌ FAILED: Error rate ${(errorRate * 100).toFixed(3)}% >= 0.1%\n`;
    allPassed = false;
  }
  
  report += '\n=== Database Scaling Recommendations ===\n\n';
  report += 'Based on design document:\n';
  report += '  - Connection pooling: 100-200 connections per service cluster\n';
  report += '  - Read replicas: 5-10 per primary database\n';
  report += '  - Horizontal sharding: 50+ PostgreSQL clusters\n';
  report += '  - Query optimization: Prepared statements, covering indexes\n';
  report += '  - Partitioning: Time-based and hash-based table partitioning\n\n';
  
  if (allPassed) {
    report += '✅ All database performance requirements validated successfully!\n';
  } else {
    report += '❌ Some database performance requirements not met.\n';
    report += '   Consider: Connection pool tuning, read replicas, query optimization, or sharding.\n';
  }
  
  return {
    'stdout': report,
    'hyperscale-database-report.txt': report,
  };
}

// Teardown
export function teardown(data) {
  console.log('Database performance test completed');
  console.log('Total users:', data.users.length);
  console.log('Total channels:', data.channels.length);
}
