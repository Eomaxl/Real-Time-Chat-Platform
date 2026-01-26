import http from 'k6/http';
import ws from 'k6/ws';
import { check, sleep, group } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import { config, randomString, generateUUID } from './config.js';

// Custom metrics
const workflowSuccess = new Rate('workflow_success');
const workflowDuration = new Trend('workflow_duration');
const e2eLatency = new Trend('e2e_latency');

// Test configuration - realistic user behavior
export const options = {
  stages: [
    { duration: '2m', target: 50 },   // Ramp up to 50 users
    { duration: '5m', target: 200 },  // Ramp up to 200 users
    { duration: '10m', target: 500 }, // Ramp up to 500 users
    { duration: '5m', target: 500 },  // Hold at 500 users
    { duration: '2m', target: 0 },    // Ramp down
  ],
  thresholds: {
    'workflow_success': ['rate>0.95'],        // 95% success rate
    'workflow_duration': ['p(95)<10000'],     // Complete workflow < 10s
    'e2e_latency': ['p(95)<500'],             // End-to-end latency < 500ms
  },
};

// Main test scenario - complete user workflow
export default function() {
  const baseUrl = config.apiUrl;
  const wsUrl = config.wsUrl;
  const workflowStart = Date.now();
  let workflowSuccessful = true;
  
  // Step 1: User Registration
  const username = `e2e_user_${Date.now()}_${__VU}`;
  const password = 'test123';
  const email = `${username}@example.com`;
  
  group('User Registration', function() {
    const registerRes = http.post(`${baseUrl}/v1/auth/register`, JSON.stringify({
      username: username,
      password: password,
      email: email,
    }), {
      headers: { 'Content-Type': 'application/json' },
      tags: { workflow_step: 'register' },
    });
    
    if (!check(registerRes, {
      'registration successful': (r) => r.status === 200 || r.status === 201,
    })) {
      workflowSuccessful = false;
      return;
    }
  });
  
  sleep(1);
  
  // Step 2: User Login
  let token, userId;
  group('User Login', function() {
    const loginRes = http.post(`${baseUrl}/v1/auth/login`, JSON.stringify({
      username: username,
      password: password,
    }), {
      headers: { 'Content-Type': 'application/json' },
      tags: { workflow_step: 'login' },
    });
    
    if (!check(loginRes, {
      'login successful': (r) => r.status === 200,
      'token received': (r) => {
        if (r.status === 200) {
          const body = JSON.parse(r.body);
          return body.token !== undefined;
        }
        return false;
      },
    })) {
      workflowSuccessful = false;
      return;
    }
    
    const body = JSON.parse(loginRes.body);
    token = body.token;
    userId = body.user_id;
  });
  
  if (!token) {
    workflowSuccess.add(0);
    return;
  }
  
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${token}`,
  };
  
  sleep(1);
  
  // Step 3: Create or Join Channel
  let channelId;
  group('Channel Management', function() {
    const channelName = `e2e_channel_${Date.now()}_${__VU}`;
    const createChannelRes = http.post(`${baseUrl}/v1/channels`, JSON.stringify({
      name: channelName,
      type: 'public',
    }), {
      headers: headers,
      tags: { workflow_step: 'create_channel' },
    });
    
    if (!check(createChannelRes, {
      'channel created': (r) => r.status === 200 || r.status === 201,
    })) {
      workflowSuccessful = false;
      return;
    }
    
    const body = JSON.parse(createChannelRes.body);
    channelId = body.id;
  });
  
  if (!channelId) {
    workflowSuccess.add(0);
    return;
  }
  
  sleep(1);
  
  // Step 4: Connect WebSocket and Subscribe
  let wsConnected = false;
  let messagesReceived = 0;
  
  group('WebSocket Connection', function() {
    const wsStart = Date.now();
    
    ws.connect(`${wsUrl}/v1/ws`, {
      headers: {
        'Authorization': `Bearer ${token}`,
      },
    }, function(socket) {
      socket.on('open', function() {
        wsConnected = true;
        
        // Subscribe to channel
        socket.send(JSON.stringify({
          type: 'subscribe',
          channel_id: channelId,
        }));
        
        // Send heartbeat
        socket.send(JSON.stringify({
          type: 'heartbeat',
          timestamp: Date.now(),
        }));
      });
      
      socket.on('message', function(message) {
        messagesReceived++;
        const receiveTime = Date.now();
        
        try {
          const data = JSON.parse(message);
          if (data.timestamp) {
            const latency = receiveTime - data.timestamp;
            e2eLatency.add(latency);
          }
        } catch (e) {
          // Ignore parse errors
        }
      });
      
      socket.on('error', function(e) {
        console.error('WebSocket error:', e);
        workflowSuccessful = false;
      });
      
      // Keep connection for 5 seconds
      socket.setTimeout(function() {
        socket.close();
      }, 5000);
    });
    
    check(null, {
      'WebSocket connected': () => wsConnected,
    });
  });
  
  sleep(2);
  
  // Step 5: Send Messages
  group('Send Messages', function() {
    for (let i = 0; i < 5; i++) {
      const messageContent = `E2E test message ${i}: ${randomString(30)}`;
      const idempotencyKey = generateUUID();
      
      const sendRes = http.post(
        `${baseUrl}/v1/channels/${channelId}/messages`,
        JSON.stringify({
          content: messageContent,
          idempotency_key: idempotencyKey,
          message_type: 'text',
        }),
        {
          headers: headers,
          tags: { workflow_step: 'send_message' },
        }
      );
      
      if (!check(sendRes, {
        'message sent': (r) => r.status === 200 || r.status === 201,
      })) {
        workflowSuccessful = false;
      }
      
      sleep(0.5);
    }
  });
  
  sleep(1);
  
  // Step 6: Retrieve Message History
  group('Message History', function() {
    const historyRes = http.get(
      `${baseUrl}/v1/channels/${channelId}/messages?limit=20`,
      {
        headers: headers,
        tags: { workflow_step: 'get_history' },
      }
    );
    
    if (!check(historyRes, {
      'history retrieved': (r) => r.status === 200,
      'messages present': (r) => {
        if (r.status === 200) {
          const body = JSON.parse(r.body);
          return body.messages && body.messages.length > 0;
        }
        return false;
      },
    })) {
      workflowSuccessful = false;
    }
  });
  
  sleep(1);
  
  // Step 7: Update Presence
  group('Presence Update', function() {
    const heartbeatRes = http.post(
      `${baseUrl}/v1/presence/heartbeat`,
      JSON.stringify({
        user_id: userId,
        status: 'online',
      }),
      {
        headers: headers,
        tags: { workflow_step: 'heartbeat' },
      }
    );
    
    check(heartbeatRes, {
      'heartbeat sent': (r) => r.status === 200 || r.status === 204,
    });
  });
  
  sleep(1);
  
  // Step 8: Start a Call (optional)
  group('Call Initiation', function() {
    const createCallRes = http.post(
      `${baseUrl}/v1/channels/${channelId}/calls`,
      JSON.stringify({
        call_type: 'audio',
      }),
      {
        headers: headers,
        tags: { workflow_step: 'create_call' },
      }
    );
    
    if (check(createCallRes, {
      'call created': (r) => r.status === 200 || r.status === 201,
    })) {
      const callBody = JSON.parse(createCallRes.body);
      const callId = callBody.id;
      
      // Send a signaling message
      sleep(0.5);
      
      const signalingRes = http.post(
        `${baseUrl}/v1/calls/${callId}/signaling`,
        JSON.stringify({
          from_user_id: userId,
          to_user_id: userId, // Self for testing
          message_type: 'offer',
          payload: { type: 'offer', sdp: 'test-sdp' },
        }),
        {
          headers: headers,
          tags: { workflow_step: 'signaling' },
        }
      );
      
      check(signalingRes, {
        'signaling sent': (r) => r.status === 200 || r.status === 204,
      });
    }
  });
  
  // Calculate workflow duration
  const workflowEnd = Date.now();
  const duration = workflowEnd - workflowStart;
  workflowDuration.add(duration);
  workflowSuccess.add(workflowSuccessful ? 1 : 0);
  
  sleep(2);
}
