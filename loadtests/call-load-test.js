import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import { config, generateUUID } from './config.js';

// Custom metrics
const callCreationLatency = new Trend('call_creation_latency');
const signalingLatency = new Trend('signaling_latency');
const callErrors = new Rate('call_errors');
const activeCalls = new Counter('active_calls');
const signalingMessages = new Counter('signaling_messages');

// Test configuration - validates signaling p95 < 150ms
export const options = {
  stages: [
    { duration: '2m', target: 50 },    // Ramp up to 50 concurrent calls
    { duration: '5m', target: 200 },   // Ramp up to 200 concurrent calls
    { duration: '10m', target: 500 },  // Ramp up to 500 concurrent calls
    { duration: '5m', target: 500 },   // Hold at 500 concurrent calls
    { duration: '2m', target: 0 },     // Ramp down
  ],
  thresholds: {
    'http_req_duration{endpoint:create_call}': ['p(95)<200'],     // Call creation < 200ms
    'http_req_duration{endpoint:signaling}': ['p(95)<150'],       // Signaling < 150ms (req 8.2)
    'call_errors': ['rate<0.001'],                                // Error rate < 0.1%
    'signaling_latency': ['p(95)<150'],                           // Signaling p95 < 150ms
  },
};

// Setup: Create test users and channels
export function setup() {
  const baseUrl = config.apiUrl;
  
  const users = [];
  for (let i = 0; i < 50; i++) {
    const username = `call_user_${Date.now()}_${i}`;
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
      const channelName = `call_channel_${Date.now()}_${i}`;
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

// Main test scenario - simulates WebRTC call lifecycle
export default function(data) {
  if (!data.users || data.users.length === 0 || !data.channels || data.channels.length === 0) {
    console.error('Setup failed: no users or channels available');
    return;
  }
  
  const baseUrl = config.apiUrl;
  const caller = data.users[Math.floor(Math.random() * data.users.length)];
  const channel = data.channels[Math.floor(Math.random() * data.channels.length)];
  
  const headers = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${caller.token}`,
  };
  
  // Scenario 1: Create call session
  const createCallStart = Date.now();
  const createCallRes = http.post(
    `${baseUrl}/v1/channels/${channel.id}/calls`,
    JSON.stringify({
      call_type: 'video',
    }),
    {
      headers: headers,
      tags: { endpoint: 'create_call' },
    }
  );
  const createCallDuration = Date.now() - createCallStart;
  
  const callCreated = check(createCallRes, {
    'call created successfully': (r) => r.status === 200 || r.status === 201,
    'call has id': (r) => {
      if (r.status === 200 || r.status === 201) {
        const body = JSON.parse(r.body);
        return body.id !== undefined;
      }
      return false;
    },
  });
  
  if (!callCreated) {
    callErrors.add(1);
    return;
  }
  
  const callBody = JSON.parse(createCallRes.body);
  const callId = callBody.id;
  
  callCreationLatency.add(createCallDuration);
  activeCalls.add(1);
  
  sleep(1);
  
  // Scenario 2: Another user joins the call
  const joiner = data.users[Math.floor(Math.random() * data.users.length)];
  const joinerHeaders = {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${joiner.token}`,
  };
  
  const joinCallRes = http.post(
    `${baseUrl}/v1/calls/${callId}/join`,
    JSON.stringify({}),
    {
      headers: joinerHeaders,
      tags: { endpoint: 'join_call' },
    }
  );
  
  check(joinCallRes, {
    'user joined call': (r) => r.status === 200,
  });
  
  sleep(1);
  
  // Scenario 3: Exchange signaling messages (SDP offer)
  const offerPayload = {
    type: 'offer',
    sdp: 'v=0\r\no=- 123456789 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n',
  };
  
  const signalingStart = Date.now();
  const offerRes = http.post(
    `${baseUrl}/v1/calls/${callId}/signaling`,
    JSON.stringify({
      from_user_id: caller.userId,
      to_user_id: joiner.userId,
      message_type: 'offer',
      payload: offerPayload,
    }),
    {
      headers: headers,
      tags: { endpoint: 'signaling' },
    }
  );
  const signalingDuration = Date.now() - signalingStart;
  
  const offerSent = check(offerRes, {
    'offer sent successfully': (r) => r.status === 200 || r.status === 204,
  });
  
  signalingLatency.add(signalingDuration);
  signalingMessages.add(1);
  
  if (!offerSent) {
    callErrors.add(1);
  }
  
  sleep(0.5);
  
  // Scenario 4: Send SDP answer
  const answerPayload = {
    type: 'answer',
    sdp: 'v=0\r\no=- 987654321 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n',
  };
  
  const answerStart = Date.now();
  const answerRes = http.post(
    `${baseUrl}/v1/calls/${callId}/signaling`,
    JSON.stringify({
      from_user_id: joiner.userId,
      to_user_id: caller.userId,
      message_type: 'answer',
      payload: answerPayload,
    }),
    {
      headers: joinerHeaders,
      tags: { endpoint: 'signaling' },
    }
  );
  const answerDuration = Date.now() - answerStart;
  
  check(answerRes, {
    'answer sent successfully': (r) => r.status === 200 || r.status === 204,
  });
  
  signalingLatency.add(answerDuration);
  signalingMessages.add(1);
  
  sleep(0.5);
  
  // Scenario 5: Exchange ICE candidates (trickle ICE)
  for (let i = 0; i < 3; i++) {
    const iceCandidate = {
      candidate: `candidate:${i} 1 udp 2122260223 192.168.1.${i} ${50000 + i} typ host`,
      sdpMLineIndex: 0,
      sdpMid: 'audio',
    };
    
    const iceStart = Date.now();
    const iceRes = http.post(
      `${baseUrl}/v1/calls/${callId}/signaling`,
      JSON.stringify({
        from_user_id: caller.userId,
        to_user_id: joiner.userId,
        message_type: 'ice-candidate',
        payload: iceCandidate,
      }),
      {
        headers: headers,
        tags: { endpoint: 'signaling' },
      }
    );
    const iceDuration = Date.now() - iceStart;
    
    check(iceRes, {
      'ICE candidate sent': (r) => r.status === 200 || r.status === 204,
    });
    
    signalingLatency.add(iceDuration);
    signalingMessages.add(1);
    
    sleep(0.2);
  }
  
  sleep(2);
  
  // Scenario 6: Test ICE restart (connection recovery)
  const iceRestartRes = http.post(
    `${baseUrl}/v1/calls/${callId}/signaling`,
    JSON.stringify({
      from_user_id: caller.userId,
      to_user_id: joiner.userId,
      message_type: 'ice-restart',
      payload: {},
    }),
    {
      headers: headers,
      tags: { endpoint: 'signaling' },
    }
  );
  
  check(iceRestartRes, {
    'ICE restart signaled': (r) => r.status === 200 || r.status === 204,
  });
  
  sleep(1);
  
  // Scenario 7: Leave call
  const leaveRes = http.post(
    `${baseUrl}/v1/calls/${callId}/leave`,
    JSON.stringify({}),
    {
      headers: headers,
      tags: { endpoint: 'leave_call' },
    }
  );
  
  check(leaveRes, {
    'left call successfully': (r) => r.status === 200 || r.status === 204,
  });
  
  sleep(1);
}

// Teardown
export function teardown(data) {
  console.log('Call load test completed');
  console.log('Total users:', data.users.length);
  console.log('Total channels:', data.channels.length);
}
