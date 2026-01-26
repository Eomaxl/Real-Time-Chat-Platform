import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import { config, randomString, generateUUID } from './config.js';

// Custom metrics
const messageLatency = new Trend('message_latency');
const messageErrorRate = new Rate('message_errors');
const historyLatency = new Trend('history_latency');

// Test configuration
export const options = {
  stages: [
    { duration: '2m', target: 100 },   // Ramp up to 100 users
    { duration: '5m', target: 500 },   // Ramp up to 500 users
    { duration: '10m', target: 1000 }, // Ramp up to 1000 users
    { duration: '5m', target: 1000 },  // Stay at 1000 users
    { duration: '2m', target: 0 },     // Ramp down
  ],
  thresholds: {
    'http_req_duration{endpoint:send_message}': ['p(95)<200'], // p95 < 200ms
    'http_req_duration{endpoint:get_history}': ['p(95)<100'],  // p95 < 100ms
    'http_req_failed': ['rate<0.001'],                         // Error rate < 0.1%
    'message_latency': ['p(95)<200'],
    'message_errors': ['rate<0.001'],
  },
};

// Setup: Create test users and channels
export function setup() {
  const baseUrl = config.apiUrl;
  
  // Register test users
  const users = [];
  for (let i = 0; i < 10; i++) {
    const username = `loadtest_user_${Date.now()}_${i}`;
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
    for (let i = 0; i < 5; i++) {
      const channelName = `loadtest_channel_${Date.now()}_${i}`;
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

// Main test scenario
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
  
  // Scenario 1: Send message with idempotency
  const idempotencyKey = generateUUID();
  const messageContent = `Load test message: ${randomString(50)}`;
  
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
  
  const sendSuccess = check(sendRes, {
    'message sent successfully': (r) => r.status === 200 || r.status === 201,
    'message has id': (r) => {
      if (r.status === 200 || r.status === 201) {
        const body = JSON.parse(r.body);
        return body.id !== undefined;
      }
      return false;
    },
  });
  
  messageLatency.add(sendDuration);
  messageErrorRate.add(!sendSuccess);
  
  sleep(1);
  
  // Scenario 2: Test idempotency (retry with same key)
  const retryRes = http.post(
    `${baseUrl}/v1/channels/${channel.id}/messages`,
    JSON.stringify({
      content: messageContent,
      idempotency_key: idempotencyKey,
      message_type: 'text',
    }),
    {
      headers: headers,
      tags: { endpoint: 'send_message_idempotent' },
    }
  );
  
  check(retryRes, {
    'idempotent request handled': (r) => r.status === 200 || r.status === 409,
  });
  
  sleep(1);
  
  // Scenario 3: Retrieve message history
  const historyStart = Date.now();
  const historyRes = http.get(
    `${baseUrl}/v1/channels/${channel.id}/messages?limit=50`,
    {
      headers: headers,
      tags: { endpoint: 'get_history' },
    }
  );
  const historyDuration = Date.now() - historyStart;
  
  check(historyRes, {
    'history retrieved successfully': (r) => r.status === 200,
    'history contains messages': (r) => {
      if (r.status === 200) {
        const body = JSON.parse(r.body);
        return body.messages !== undefined && Array.isArray(body.messages);
      }
      return false;
    },
  });
  
  historyLatency.add(historyDuration);
  
  sleep(2);
  
  // Scenario 4: Pagination test
  const paginationRes = http.get(
    `${baseUrl}/v1/channels/${channel.id}/messages?limit=20&cursor=`,
    {
      headers: headers,
      tags: { endpoint: 'get_history_paginated' },
    }
  );
  
  check(paginationRes, {
    'pagination works': (r) => r.status === 200,
  });
  
  sleep(1);
  
  // Scenario 5: Filter messages by timestamp
  const since = new Date(Date.now() - 3600000).toISOString(); // Last hour
  const filterRes = http.get(
    `${baseUrl}/v1/channels/${channel.id}/messages?since=${since}`,
    {
      headers: headers,
      tags: { endpoint: 'get_history_filtered' },
    }
  );
  
  check(filterRes, {
    'filtered history retrieved': (r) => r.status === 200,
  });
  
  sleep(2);
}

// Teardown: Clean up test data
export function teardown(data) {
  // Optional: Clean up test channels and users
  console.log('Test completed. Created', data.users.length, 'users and', data.channels.length, 'channels');
}
