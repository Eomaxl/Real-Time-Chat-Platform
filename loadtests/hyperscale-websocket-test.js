import ws from 'k6/ws';
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { config } from './config.js';

// Custom metrics for hyperscale validation
const wsConnections = new Counter('ws_connections_total');
const wsConnectionErrors = new Rate('ws_connection_errors');
const wsStableConnections = new Counter('ws_stable_connections');
const wsConnectionDuration = new Trend('ws_connection_duration');
const wsMemoryPerConnection = new Trend('ws_memory_per_connection');

// Hyperscale test configuration
// Target: 50K concurrent connections per node (requirement 8.3)
// This test validates connection capacity and stability
export const options = {
  scenarios: {
    // Scenario 1: Ramp up to 50K connections
    ramp_to_50k: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10m', target: 10000 },  // Ramp to 10K
        { duration: '10m', target: 20000 },  // Ramp to 20K
        { duration: '10m', target: 30000 },  // Ramp to 30K
        { duration: '10m', target: 40000 },  // Ramp to 40K
        { duration: '10m', target: 50000 },  // Ramp to 50K (target)
        { duration: '30m', target: 50000 },  // Hold at 50K for 30 minutes
        { duration: '10m', target: 0 },      // Ramp down
      ],
      gracefulRampDown: '5m',
    },
  },
  thresholds: {
    'ws_connecting': ['p(95)<2000'],              // Connection time < 2s at scale
    'ws_connection_errors': ['rate<0.01'],        // Error rate < 1%
    'ws_stable_connections': ['count>45000'],     // At least 45K stable connections (90% of target)
    'ws_connection_duration': ['p(50)>1800000'],  // Median connection duration > 30 minutes
  },
};

// Setup: Create test users (batch creation for efficiency)
export function setup() {
  const baseUrl = config.apiUrl;
  const batchSize = 100;
  const totalUsers = 1000; // Create 1000 users, reuse across VUs
  
  console.log(`Creating ${totalUsers} test users in batches of ${batchSize}...`);
  
  const users = [];
  for (let batch = 0; batch < totalUsers / batchSize; batch++) {
    const batchUsers = [];
    
    for (let i = 0; i < batchSize; i++) {
      const userIndex = batch * batchSize + i;
      const username = `hyperscale_ws_user_${userIndex}`;
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
          batchUsers.push({
            username: username,
            token: body.token,
            userId: body.user_id,
          });
        }
      }
    }
    
    users.push(...batchUsers);
    console.log(`Created batch ${batch + 1}/${totalUsers / batchSize}, total users: ${users.length}`);
    
    // Small delay between batches to avoid overwhelming the system
    sleep(1);
  }
  
  console.log(`Setup complete. Created ${users.length} users.`);
  return { users };
}

// Main test scenario - establish and maintain WebSocket connection
export default function(data) {
  if (!data.users || data.users.length === 0) {
    console.error('Setup failed: no users available');
    return;
  }
  
  // Select a user (round-robin across available users)
  const user = data.users[__VU % data.users.length];
  const wsUrl = `${config.wsUrl}/v1/ws`;
  
  const connectionStart = Date.now();
  let connectionEstablished = false;
  let messagesReceived = 0;
  let connectionStable = false;
  
  const res = ws.connect(wsUrl, {
    headers: {
      'Authorization': `Bearer ${user.token}`,
    },
  }, function(socket) {
    connectionEstablished = true;
    wsConnections.add(1);
    
    socket.on('open', function() {
      console.log(`[VU ${__VU}] WebSocket connected for user ${user.username}`);
      
      // Subscribe to a test channel (distribute across multiple channels)
      const channelId = `test_channel_${__VU % 100}`;
      socket.send(JSON.stringify({
        type: 'subscribe',
        channel_id: channelId,
      }));
      
      // Send periodic heartbeats to keep connection alive
      socket.setInterval(function() {
        socket.send(JSON.stringify({
          type: 'heartbeat',
          timestamp: Date.now(),
        }));
      }, 30000); // Every 30 seconds
      
      // Mark as stable after 5 minutes
      socket.setTimeout(function() {
        connectionStable = true;
        wsStableConnections.add(1);
        console.log(`[VU ${__VU}] Connection stable for ${user.username}`);
      }, 300000); // 5 minutes
    });
    
    socket.on('message', function(message) {
      messagesReceived++;
      
      try {
        const data = JSON.parse(message);
        
        // Handle different message types
        if (data.type === 'heartbeat_ack') {
          // Heartbeat acknowledged
        } else if (data.type === 'message') {
          // Chat message received
        } else if (data.type === 'presence_change') {
          // Presence update received
        }
      } catch (e) {
        // Ignore parse errors
      }
    });
    
    socket.on('error', function(e) {
      console.error(`[VU ${__VU}] WebSocket error:`, e);
      wsConnectionErrors.add(1);
    });
    
    socket.on('close', function() {
      const connectionDuration = Date.now() - connectionStart;
      wsConnectionDuration.add(connectionDuration);
      
      console.log(`[VU ${__VU}] WebSocket closed after ${(connectionDuration / 1000).toFixed(0)}s, received ${messagesReceived} messages`);
      
      // Estimate memory per connection (rough approximation)
      // Actual memory usage should be monitored on the server side
      const estimatedMemory = 4096; // 4KB per connection (from design doc)
      wsMemoryPerConnection.add(estimatedMemory);
    });
    
    // Keep connection alive for the duration of the test
    // Connection will be closed when VU ramps down
    socket.setTimeout(function() {
      // This timeout is very long to keep connections alive
      // Connections will naturally close during ramp-down
    }, 7200000); // 2 hours
  });
  
  check(res, {
    'WebSocket connected': () => connectionEstablished,
  });
  
  if (!connectionEstablished) {
    wsConnectionErrors.add(1);
  }
}

// Custom summary with hyperscale validation
export function handleSummary(data) {
  const totalConnections = data.metrics.ws_connections_total.values.count;
  const stableConnections = data.metrics.ws_stable_connections ? data.metrics.ws_stable_connections.values.count : 0;
  const errorRate = data.metrics.ws_connection_errors.values.rate;
  const p95ConnectionTime = data.metrics.ws_connecting ? data.metrics.ws_connecting.values['p(95)'] : 0;
  
  let report = '\n=== Hyperscale WebSocket Validation Report ===\n\n';
  report += 'Target: 50,000 concurrent connections per node (Requirement 8.3)\n\n';
  report += `Total Connections Attempted: ${totalConnections}\n`;
  report += `Stable Connections (>5min): ${stableConnections}\n`;
  report += `Connection Error Rate: ${(errorRate * 100).toFixed(2)}%\n`;
  report += `P95 Connection Time: ${p95ConnectionTime.toFixed(0)}ms\n\n`;
  
  // Validation against requirements
  const targetConnections = 50000;
  const minStableConnections = targetConnections * 0.9; // 90% threshold
  
  if (stableConnections >= minStableConnections) {
    report += `✅ PASSED: Achieved ${stableConnections} stable connections (target: ${targetConnections})\n`;
  } else {
    report += `❌ FAILED: Only ${stableConnections} stable connections (target: ${targetConnections})\n`;
  }
  
  if (errorRate < 0.01) {
    report += `✅ PASSED: Error rate ${(errorRate * 100).toFixed(2)}% < 1%\n`;
  } else {
    report += `❌ FAILED: Error rate ${(errorRate * 100).toFixed(2)}% >= 1%\n`;
  }
  
  if (p95ConnectionTime < 2000) {
    report += `✅ PASSED: P95 connection time ${p95ConnectionTime.toFixed(0)}ms < 2000ms\n`;
  } else {
    report += `❌ FAILED: P95 connection time ${p95ConnectionTime.toFixed(0)}ms >= 2000ms\n`;
  }
  
  report += '\nMemory Estimation:\n';
  const estimatedMemoryMB = (stableConnections * 4) / 1024; // 4KB per connection
  report += `Estimated Memory Usage: ${estimatedMemoryMB.toFixed(0)}MB for ${stableConnections} connections\n`;
  report += `Target Memory Usage: ~200MB for 50K connections (from design doc)\n`;
  
  return {
    'stdout': report,
    'hyperscale-websocket-report.txt': report,
  };
}

// Teardown
export function teardown(data) {
  console.log('Hyperscale WebSocket test completed');
  console.log('Total users created:', data.users.length);
}
