import ws from 'k6/ws';
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Rate, Trend } from 'k6/metrics';
import { config, generateUUID } from './config.js';

// Custom metrics
const wsConnections = new Counter('ws_connections');
const wsConnectionErrors = new Rate('ws_connection_errors');
const wsMessageLatency = new Trend('ws_message_latency');
const wsMessagesReceived = new Counter('ws_messages_received');
const wsConnectionDuration = new Trend('ws_connection_duration');

// Test configuration - validates 50K connections per node
export const options = {
  stages: [
    { duration: '5m', target: 1000 },   // Ramp up to 1K connections
    { duration: '10m', target: 5000 },  // Ramp up to 5K connections
    { duration: '10m', target: 10000 }, // Ramp up to 10K connections
    { duration: '10m', target: 10000 }, // Hold at 10K connections
    { duration: '5m', target: 0 },      // Ramp down
  ],
  thresholds: {
    'ws_connecting': ['p(95)<1000'],           // Connection time < 1s
    'ws_connection_errors': ['rate<0.01'],     // Error rate < 1%
    'ws_message_latency': ['p(95)<100'],       // Message latency < 100ms
    'ws_connection_duration': ['p(95)<60000'], // Connections stable for 60s
  },
};

// Setup: Create test users
export function setup() {
  const baseUrl = config.apiUrl;
  
  const users = [];
  for (let i = 0; i < 100; i++) {
    const username = `ws_loadtest_user_${Date.now()}_${i}`;
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
  
  return { users };
}

// Main test scenario
export default function(data) {
  if (!data.users || data.users.length === 0) {
    console.error('Setup failed: no users available');
    return;
  }
  
  const user = data.users[Math.floor(Math.random() * data.users.length)];
  const wsUrl = `${config.wsUrl}/v1/ws`;
  
  const connectionStart = Date.now();
  let messagesReceived = 0;
  let connectionEstablished = false;
  
  const res = ws.connect(wsUrl, {
    headers: {
      'Authorization': `Bearer ${user.token}`,
    },
  }, function(socket) {
    connectionEstablished = true;
    wsConnections.add(1);
    
    socket.on('open', function() {
      console.log(`WebSocket connected for user ${user.username}`);
      
      // Subscribe to a test channel
      socket.send(JSON.stringify({
        type: 'subscribe',
        channel_id: 'test_channel_1',
      }));
      
      // Send periodic heartbeats
      socket.setInterval(function() {
        socket.send(JSON.stringify({
          type: 'heartbeat',
          timestamp: Date.now(),
        }));
      }, 30000); // Every 30 seconds
    });
    
    socket.on('message', function(message) {
      const receiveTime = Date.now();
      messagesReceived++;
      wsMessagesReceived.add(1);
      
      try {
        const data = JSON.parse(message);
        
        // Calculate latency if message has timestamp
        if (data.timestamp) {
          const latency = receiveTime - data.timestamp;
          wsMessageLatency.add(latency);
        }
        
        // Handle different message types
        if (data.type === 'message') {
          console.log(`Received message: ${data.content}`);
        } else if (data.type === 'presence_change') {
          console.log(`Presence change: ${data.user_id} is ${data.status}`);
        } else if (data.type === 'call_started') {
          console.log(`Call started: ${data.call_id}`);
        }
      } catch (e) {
        console.error('Failed to parse message:', e);
      }
    });
    
    socket.on('error', function(e) {
      console.error('WebSocket error:', e);
      wsConnectionErrors.add(1);
    });
    
    socket.on('close', function() {
      const connectionDuration = Date.now() - connectionStart;
      wsConnectionDuration.add(connectionDuration);
      console.log(`WebSocket closed after ${connectionDuration}ms, received ${messagesReceived} messages`);
    });
    
    // Keep connection alive for test duration
    socket.setTimeout(function() {
      console.log('Closing WebSocket connection');
      socket.close();
    }, 60000); // Keep alive for 60 seconds
  });
  
  check(res, {
    'WebSocket connected': () => connectionEstablished,
  });
  
  if (!connectionEstablished) {
    wsConnectionErrors.add(1);
  }
}

// Teardown
export function teardown(data) {
  console.log('WebSocket load test completed');
  console.log('Total users created:', data.users.length);
}
