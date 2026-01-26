// Shared configuration for load tests
export const config = {
  // API endpoints
  apiUrl: __ENV.API_URL || 'http://localhost:8080',
  wsUrl: __ENV.WS_URL || 'ws://localhost:8080',
  
  // Test parameters
  testDuration: __ENV.TEST_DURATION || '5m',
  vus: parseInt(__ENV.VUS) || 100,
  targetRPS: parseInt(__ENV.TARGET_RPS) || 1000,
  
  // Performance thresholds (from requirements 8.1, 8.2, 8.3)
  thresholds: {
    messagingP95: 200, // ms
    signalingP95: 150, // ms
    databaseQueryP95: 5, // ms
    errorRate: 0.001, // 0.1%
  },
  
  // Test users
  testUsers: [
    { username: 'loadtest_user_1', password: 'test123' },
    { username: 'loadtest_user_2', password: 'test123' },
    { username: 'loadtest_user_3', password: 'test123' },
  ],
  
  // Test channels
  testChannels: [
    'loadtest_channel_1',
    'loadtest_channel_2',
    'loadtest_channel_3',
  ],
};

// Helper function to generate random string
export function randomString(length) {
  const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
  let result = '';
  for (let i = 0; i < length; i++) {
    result += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return result;
}

// Helper function to generate UUID
export function generateUUID() {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function(c) {
    const r = Math.random() * 16 | 0;
    const v = c === 'x' ? r : (r & 0x3 | 0x8);
    return v.toString(16);
  });
}

// Helper function to sleep with jitter
export function sleepWithJitter(baseMs, jitterMs) {
  const jitter = Math.random() * jitterMs;
  return baseMs + jitter;
}
