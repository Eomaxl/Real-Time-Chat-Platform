import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { config, sleepWithJitter } from './config.js';

// Custom metrics
const heartbeatLatency = new Trend('heartbeat_latency');
const heartbeatErrors = new Rate('heartbeat_errors');
const presenceQueryLatency = new Trend('presence_query_latency');
const presenceUpdates = new Counter('presence_updates');

// Test configuration - simulates millions of users sending heartbeats
export const options = {
  stages: [
    { duration: '2m', target: 500 },    // Ramp up to 500 users
    { duration: '5m', target: 2000 },   // Ramp up to 2K users
    { duration: '10m', target: 5000 },  // Ramp up to 5K users
    { duration: '10m', target: 5000 },  // Hold at 5K users
    { duration: '2m', target: 0 },      // Ramp down
  ],
  thresholds: {
    'http_req_duration{endpoint:heartbeat}': ['p(95)<50'],      // Heartbeat p95 < 50ms
    'http_req_duration{endpoint:get_presence}': ['p(95)<100'],  // Query p95 < 100ms
    'heartbeat_errors': ['rate<0.001'],                         // Error rate < 0.1%
  },
};

// Setup: Create test users
export function setup() {
  const baseUrl = config.apiUrl;
  
  const users = [];
  for (let i = 0; i < 100; i++) {
    const username = `presence_user_${Date.now()}_${i}`;
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
    for (let i = 0; i < 10; i++) {
      const channelName = `presence_channel_${Date.now()}_${i}`;
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
  
  return { users, channels };
}

// Main test scenario - simulates realistic presence behavior
export default function(data) {
  if (!data.users || data.users.length === 0) {
    console.error('Setup failed: no users available');
    return;
  }
  
  const baseUrl = config.apiUrl;
  const user = data.users[Math.floor(Math.random() * data.users.length)];
  
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${user.token}`,
  };
  
  // Scenario 1: Send heartbeat (simulates active user)
  const heartbeatStart = Date.now();
  const heartbeatRes = http.post(
    `${baseUrl}/v1/presence/heartbeat`,
    JSON.stringify({
      user_id: user.userId,
      status: 'online',
    }),
    {
      headers: headers,
      tags: { endpoint: 'heartbeat' },
    }
  );
  const heartbeatDuration = Date.now() - heartbeatStart;
  
  const heartbeatSuccess = check(heartbeatRes, {
    'heartbeat sent successfully': (r) => r.status === 200 || r.status === 204,
  });
  
  heartbeatLatency.add(heartbeatDuration);
  heartbeatErrors.add(!heartbeatSuccess);
  presenceUpdates.add(1);
  
  // Sleep with jitter to simulate realistic heartbeat intervals (20-30 seconds)
  sleep(sleepWithJitter(20, 10) / 1000);
  
  // Scenario 2: Query own presence
  const selfPresenceRes = http.get(
    `${baseUrl}/v1/presence/users/${user.userId}`,
    {
      headers: headers,
      tags: { endpoint: 'get_presence' },
    }
  );
  
  check(selfPresenceRes, {
    'self presence retrieved': (r) => r.status === 200,
    'presence status is online': (r) => {
      if (r.status === 200) {
        const body = JSON.parse(r.body);
        return body.status === 'online';
      }
      return false;
    },
  });
  
  sleep(2);
  
  // Scenario 3: Query channel presence (if channels available)
  if (data.channels && data.channels.length > 0) {
    const channel = data.channels[Math.floor(Math.random() * data.channels.length)];
    
    const channelPresenceStart = Date.now();
    const channelPresenceRes = http.get(
      `${baseUrl}/v1/presence/channels/${channel.id}`,
      {
        headers: headers,
        tags: { endpoint: 'get_channel_presence' },
      }
    );
    const channelPresenceDuration = Date.now() - channelPresenceStart;
    
    check(channelPresenceRes, {
      'channel presence retrieved': (r) => r.status === 200,
      'channel presence is array': (r) => {
        if (r.status === 200) {
          const body = JSON.parse(r.body);
          return Array.isArray(body.users);
        }
        return false;
      },
    });
    
    presenceQueryLatency.add(channelPresenceDuration);
  }
  
  sleep(3);
  
  // Scenario 4: Batch presence query (multiple users)
  const userIds = data.users.slice(0, 10).map(u => u.userId);
  const batchPresenceRes = http.post(
    `${baseUrl}/v1/presence/batch`,
    JSON.stringify({
      user_ids: userIds,
    }),
    {
      headers: headers,
      tags: { endpoint: 'get_batch_presence' },
    }
  );
  
  check(batchPresenceRes, {
    'batch presence retrieved': (r) => r.status === 200,
  });
  
  sleep(5);
  
  // Scenario 5: Simulate going offline (stop sending heartbeats)
  // In real scenario, TTL would expire after 30 seconds
  const offlineRes = http.post(
    `${baseUrl}/v1/presence/offline`,
    JSON.stringify({
      user_id: user.userId,
    }),
    {
      headers: headers,
      tags: { endpoint: 'set_offline' },
    }
  );
  
  check(offlineRes, {
    'offline status set': (r) => r.status === 200 || r.status === 204,
  });
  
  sleep(2);
}

// Teardown
export function teardown(data) {
  console.log('Presence load test completed');
  console.log('Total users:', data.users.length);
  console.log('Total channels:', data.channels.length);
}
