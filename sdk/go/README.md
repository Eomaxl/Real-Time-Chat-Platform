# Real-Time Chat Platform Go SDK

Official Go SDK for the Real-Time Chat Platform, providing comprehensive API coverage for chat messaging, presence management, and WebRTC calls.

## Features

- **Chat Messaging**: Send messages, retrieve history, pagination support
- **Presence Management**: Heartbeat updates, channel presence tracking
- **WebRTC Calls**: Call session management, signaling message relay
- **WebSocket Support**: Real-time events with automatic reconnection
- **Connection Management**: Automatic retry logic and exponential backoff
- **Idempotency**: Built-in support for idempotent message sending

## Installation

```bash
go get real-time-chat-platform/sdk/go
```

## Quick Start

```go
package main

import (
    "context"
    "log"
    "time"
    
    chatplatform "real-time-chat-platform/sdk/go"
)

func main() {
    // Create client
    client := chatplatform.NewClient(chatplatform.Config{
        BaseURL: "http://localhost:8080",
        APIKey:  "your-api-key",
        UserID:  "user-123",
        Timeout: 30 * time.Second,
    })
    
    ctx := context.Background()
    
    // Send a message
    message, err := client.Chat.SendMessage(ctx, chatplatform.SendMessageRequest{
        ChannelID: "channel-456",
        Content:   "Hello, World!",
    })
    if err != nil {
        log.Fatal(err)
    }
    
    log.Printf("Message sent: %s", message.ID)
}
```

## Chat API

### Send Message

```go
message, err := client.Chat.SendMessage(ctx, chatplatform.SendMessageRequest{
    ChannelID:   "channel-456",
    Content:     "Hello!",
    MessageType: "text",
})
```

### Send Message with Idempotency

```go
idempotencyKey := "unique-key-123"
message, err := client.Chat.SendMessage(ctx, chatplatform.SendMessageRequest{
    ChannelID:      "channel-456",
    Content:        "This won't be duplicated",
    IdempotencyKey: &idempotencyKey,
})
```

### Get Message History

```go
history, err := client.Chat.GetMessageHistory(ctx, "channel-456", chatplatform.HistoryOptions{
    Limit: 50,
})
```

### Get Messages Since Timestamp

```go
since := time.Now().Add(-1 * time.Hour)
messages, err := client.Chat.GetMessagesSince(ctx, "channel-456", since, 50)
```

### Pagination

```go
var allMessages []chatplatform.Message
var cursor *string

for {
    opts := chatplatform.HistoryOptions{
        Limit: 10,
    }
    if cursor != nil {
        opts.Cursor = *cursor
    }
    
    page, err := client.Chat.GetMessageHistory(ctx, "channel-456", opts)
    if err != nil {
        return err
    }
    
    allMessages = append(allMessages, page.Messages...)
    
    if !page.HasMore {
        break
    }
    
    cursor = page.NextCursor
}
```

## Presence API

### Send Heartbeat

```go
err := client.Presence.SendHeartbeat(ctx, []string{"channel-456"}, "online")
```

### Start Automatic Heartbeat Loop

```go
go func() {
    err := client.Presence.StartHeartbeatLoop(ctx, []string{"channel-456"}, 15*time.Second)
    if err != nil {
        log.Printf("Heartbeat error: %v", err)
    }
}()
```

### Get User Presence

```go
presence, err := client.Presence.GetPresence(ctx, "user-456")
fmt.Printf("User is %s\n", presence.Status)
```

### Get Channel Presence

```go
channelPresence, err := client.Presence.GetChannelPresence(ctx, "channel-456")
for _, user := range channelPresence.Users {
    fmt.Printf("%s: %s\n", user.UserID, user.Status)
}
```

## Call API

### Create Call

```go
call, err := client.Call.CreateCall(ctx, "channel-456", "audio")
```

### Join Call

```go
participant, err := client.Call.JoinCall(ctx, callID)
```

### Send WebRTC Signaling

```go
// Send offer
offer := map[string]interface{}{
    "type": "offer",
    "sdp":  "...",
}
err := client.Call.SendOffer(ctx, callID, "user-456", offer)

// Send answer
answer := map[string]interface{}{
    "type": "answer",
    "sdp":  "...",
}
err := client.Call.SendAnswer(ctx, callID, "user-456", answer)

// Send ICE candidate
candidate := map[string]interface{}{
    "candidate": "...",
}
err := client.Call.SendICECandidate(ctx, callID, "user-456", candidate)
```

### Connection Recovery

```go
// Reconnect after network failure
err := client.Call.ReconnectToCall(ctx, callID)

// Request ICE restart
err := client.Call.RequestICERestart(ctx, callID)

// Resume session
session, err := client.Call.ResumeSession(ctx, callID)
```

### Leave Call

```go
err := client.Call.LeaveCall(ctx, callID)
```

## WebSocket API

### Connect and Subscribe

```go
ws := client.NewWebSocketClient("ws://localhost:8080/v1/ws")

// Set up event handlers
ws.OnMessage(func(event chatplatform.WebSocketEvent) {
    fmt.Printf("Event: %s\n", event.Type)
})

ws.OnConnect(func() {
    fmt.Println("Connected")
})

ws.OnDisconnect(func() {
    fmt.Println("Disconnected")
})

// Connect
if err := ws.Connect(ctx); err != nil {
    log.Fatal(err)
}

// Subscribe to channel
if err := ws.SubscribeToChannel("channel-456"); err != nil {
    log.Fatal(err)
}
```

### Handle Events

```go
ws.OnMessage(func(event chatplatform.WebSocketEvent) {
    switch event.Type {
    case "message":
        // Handle new message
    case "presence_change":
        // Handle presence change
    case "call_started":
        // Handle call started
    case "participant_joined":
        // Handle participant joined
    }
})
```

### Automatic Reconnection

The WebSocket client automatically reconnects with exponential backoff when the connection is lost. All subscriptions are automatically restored after reconnection.

## Error Handling

```go
message, err := client.Chat.SendMessage(ctx, req)
if err != nil {
    if apiErr, ok := err.(*chatplatform.APIError); ok {
        fmt.Printf("API Error: %d - %s\n", apiErr.StatusCode, apiErr.Message)
    } else {
        fmt.Printf("Error: %v\n", err)
    }
}
```

## Configuration

```go
client := chatplatform.NewClient(chatplatform.Config{
    BaseURL:    "http://localhost:8080",  // API base URL
    APIKey:     "your-api-key",           // Authentication token
    UserID:     "user-123",               // User ID for requests
    Timeout:    30 * time.Second,         // HTTP timeout
    MaxRetries: 3,                        // Max retry attempts
})
```

## Examples

See the `examples/` directory for complete working examples:

- `chat_example.go` - Chat messaging examples
- `presence_example.go` - Presence management examples
- `call_example.go` - WebRTC call examples
- `websocket_example.go` - Real-time events examples

## Requirements

- Go 1.18 or higher
- `github.com/gorilla/websocket` for WebSocket support

## License

See LICENSE file for details.
