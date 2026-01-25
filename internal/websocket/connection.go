package websocket

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer
	maxMessageSize = 512
)

// readPump pumps messages from the WebSocket connection to the hub
func (c *Connection) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.lastPong = time.Now()
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		// Handle incoming message
		c.handleMessage(message)
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Connection) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the current WebSocket message
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage handles incoming WebSocket messages
func (c *Connection) handleMessage(message []byte) {
	var msg WebSocketMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("Failed to parse WebSocket message: %v", err)
		c.sendError("Invalid message format")
		return
	}

	switch msg.Type {
	case "subscribe":
		c.handleSubscribe(msg)
	case "unsubscribe":
		c.handleUnsubscribe(msg)
	case "ping":
		c.handlePing()
	default:
		log.Printf("Unknown message type: %s", msg.Type)
		c.sendError("Unknown message type")
	}
}

// handleSubscribe handles channel subscription requests
func (c *Connection) handleSubscribe(msg WebSocketMessage) {
	if msg.Channel == nil {
		c.sendError("Channel required for subscription")
		return
	}

	channelID := *msg.Channel

	// TODO: Validate channel membership authorization
	// For now, allow all subscriptions

	c.mutex.Lock()
	c.channels[channelID] = true
	c.mutex.Unlock()

	// Send confirmation
	response := WebSocketEvent{
		Type:      "subscribed",
		Timestamp: time.Now(),
		ChannelID: &channelID,
		Data:      json.RawMessage(`{"status": "success"}`),
	}

	c.sendEvent(response)
	log.Printf("User %s subscribed to channel %s", c.userID, channelID)
}

// handleUnsubscribe handles channel unsubscription requests
func (c *Connection) handleUnsubscribe(msg WebSocketMessage) {
	if msg.Channel == nil {
		c.sendError("Channel required for unsubscription")
		return
	}

	channelID := *msg.Channel

	c.mutex.Lock()
	delete(c.channels, channelID)
	c.mutex.Unlock()

	// Send confirmation
	response := WebSocketEvent{
		Type:      "unsubscribed",
		Timestamp: time.Now(),
		ChannelID: &channelID,
		Data:      json.RawMessage(`{"status": "success"}`),
	}

	c.sendEvent(response)
	log.Printf("User %s unsubscribed from channel %s", c.userID, channelID)
}

// handlePing handles ping messages
func (c *Connection) handlePing() {
	response := WebSocketEvent{
		Type:      "pong",
		Timestamp: time.Now(),
		Data:      json.RawMessage(`{"status": "alive"}`),
	}

	c.sendEvent(response)
}

// sendEvent sends an event to the client
func (c *Connection) sendEvent(event WebSocketEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("Failed to marshal event: %v", err)
		return
	}

	select {
	case c.send <- data:
	default:
		// Connection is blocked, will be closed by hub
		log.Printf("Failed to send event to connection %s", c.sessionID)
	}
}

// sendError sends an error message to the client
func (c *Connection) sendError(message string) {
	errorEvent := WebSocketEvent{
		Type:      "error",
		Timestamp: time.Now(),
		Data:      json.RawMessage(`{"error": "` + message + `"}`),
	}

	c.sendEvent(errorEvent)
}

// close closes the connection
func (c *Connection) close() {
	close(c.send)
}

// IsSubscribed checks if the connection is subscribed to a channel
func (c *Connection) IsSubscribed(channelID string) bool {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return c.channels[channelID]
}

// GetSubscriptions returns all channel subscriptions
func (c *Connection) GetSubscriptions() []string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	channels := make([]string, 0, len(c.channels))
	for channelID := range c.channels {
		channels = append(channels, channelID)
	}
	return channels
}

// GetUserID returns the user ID for this connection
func (c *Connection) GetUserID() string {
	return c.userID
}

// GetSessionID returns the session ID for this connection
func (c *Connection) GetSessionID() string {
	return c.sessionID
}

// IsAuthenticated returns whether the connection is authenticated
func (c *Connection) IsAuthenticated() bool {
	return c.authenticated
}

// GetLastPong returns the last pong timestamp
func (c *Connection) GetLastPong() time.Time {
	return c.lastPong
}
