package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"real-time-chat-system/internal/events"
	"real-time-chat-system/internal/pool"
	redisclient "real-time-chat-system/internal/redis"

	"github.com/gorilla/websocket"
)

// Hub manages WebSocket connections and message distribution
type Hub struct {
	// Connection management
	connections map[string]*Connection
	register    chan *Connection
	unregister  chan *Connection
	broadcast   chan []byte

	// Sharding for scalability
	shards     []*ConnectionShard
	shardCount int

	// Redis client for pub/sub
	redis *redisclient.Client

	// Event system
	publisher *events.Publisher
	replayMgr *events.ReplayManager

	// Object pools for memory optimization
	messagePool *pool.MessagePool
	bufferPool  *pool.ByteBufferPool

	// Synchronization
	mutex sync.RWMutex

	// Context for graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
}

// ConnectionShard represents a shard of connections for scalability
type ConnectionShard struct {
	connections map[string]*Connection
	register    chan *Connection
	unregister  chan *Connection
	broadcast   chan []byte
	mutex       sync.RWMutex
	shardID     int
}

// Connection represents a WebSocket connection
type Connection struct {
	// WebSocket connection
	conn *websocket.Conn

	// Connection metadata
	userID    string
	sessionID string
	channels  map[string]bool // subscribed channels

	// Message channels
	send chan []byte

	// Hub reference
	hub   *Hub
	shard *ConnectionShard

	// Authentication
	authenticated bool

	// Connection state
	lastPong time.Time

	// Synchronization
	mutex sync.RWMutex
}

// WebSocketEvent represents an event sent over WebSocket
type WebSocketEvent struct {
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
	ChannelID *string         `json:"channel_id,omitempty"`
	CallID    *string         `json:"call_id,omitempty"`
}

// WebSocketMessage represents a message received from client
type WebSocketMessage struct {
	Type    string          `json:"type"`
	Data    json.RawMessage `json:"data"`
	Channel *string         `json:"channel,omitempty"`
}

// Upgrader for WebSocket connections
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// In production, implement proper origin checking
		return true
	},
}

// NewHub creates a new WebSocket hub
func NewHub(redis *redisclient.Client, shardCount int) *Hub {
	ctx, cancel := context.WithCancel(context.Background())

	hub := &Hub{
		connections: make(map[string]*Connection),
		register:    make(chan *Connection, 256),
		unregister:  make(chan *Connection, 256),
		broadcast:   make(chan []byte, 256),
		shards:      make([]*ConnectionShard, shardCount),
		shardCount:  shardCount,
		redis:       redis,
		publisher:   events.NewPublisher(redis),
		replayMgr:   events.NewReplayManager(redis),
		messagePool: pool.NewMessagePool(),
		bufferPool:  pool.NewByteBufferPool(4096), // 4KB buffers
		ctx:         ctx,
		cancel:      cancel,
	}

	// Initialize shards
	for i := 0; i < shardCount; i++ {
		hub.shards[i] = &ConnectionShard{
			connections: make(map[string]*Connection),
			register:    make(chan *Connection, 256),
			unregister:  make(chan *Connection, 256),
			broadcast:   make(chan []byte, 256),
			shardID:     i,
		}
	}

	return hub
}

// Start starts the hub and all shards
func (h *Hub) Start() {
	// Start all shards
	for _, shard := range h.shards {
		go shard.run(h.ctx)
	}

	// Start main hub
	go h.run()

	// Start Redis pub/sub listener
	go h.listenRedis()
}

// Stop stops the hub gracefully
func (h *Hub) Stop() {
	h.cancel()
}

// run is the main hub loop
func (h *Hub) run() {
	for {
		select {
		case conn := <-h.register:
			h.registerConnection(conn)

		case conn := <-h.unregister:
			h.unregisterConnection(conn)

		case message := <-h.broadcast:
			h.broadcastMessage(message)

		case <-h.ctx.Done():
			return
		}
	}
}

// registerConnection registers a new connection
func (h *Hub) registerConnection(conn *Connection) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	// Add to global connections map
	h.connections[conn.sessionID] = conn

	// Route to appropriate shard
	shardIndex := h.getShardIndex(conn.userID)
	shard := h.shards[shardIndex]
	conn.shard = shard

	// Register with shard
	select {
	case shard.register <- conn:
	default:
		log.Printf("Failed to register connection %s with shard %d", conn.sessionID, shardIndex)
	}

	log.Printf("Connection registered: user=%s, session=%s, shard=%d",
		conn.userID, conn.sessionID, shardIndex)
}

// unregisterConnection unregisters a connection
func (h *Hub) unregisterConnection(conn *Connection) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	// Remove from global connections map
	delete(h.connections, conn.sessionID)

	// Unregister from shard
	if conn.shard != nil {
		select {
		case conn.shard.unregister <- conn:
		default:
			log.Printf("Failed to unregister connection %s from shard", conn.sessionID)
		}
	}

	// Close connection
	conn.close()

	log.Printf("Connection unregistered: user=%s, session=%s",
		conn.userID, conn.sessionID)
}

// broadcastMessage broadcasts a message to all connections
func (h *Hub) broadcastMessage(message []byte) {
	// Parse the message to determine routing
	var event WebSocketEvent
	if err := json.Unmarshal(message, &event); err != nil {
		log.Printf("Failed to parse broadcast message: %v", err)
		return
	}

	// Route to appropriate shards based on event type and target
	if event.ChannelID != nil {
		h.broadcastToChannel(*event.ChannelID, message)
	} else {
		// Broadcast to all shards
		for _, shard := range h.shards {
			select {
			case shard.broadcast <- message:
			default:
				log.Printf("Failed to broadcast to shard %d", shard.shardID)
			}
		}
	}
}

// broadcastToChannel broadcasts a message to all connections subscribed to a channel
func (h *Hub) broadcastToChannel(channelID string, message []byte) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	// Find all connections subscribed to this channel
	for _, conn := range h.connections {
		conn.mutex.RLock()
		subscribed := conn.channels[channelID]
		conn.mutex.RUnlock()

		if subscribed {
			select {
			case conn.send <- message:
			default:
				// Connection is blocked, close it
				h.unregisterConnection(conn)
			}
		}
	}
}

// getShardIndex returns the shard index for a user
func (h *Hub) getShardIndex(userID string) int {
	// Simple hash-based sharding
	hash := 0
	for _, c := range userID {
		hash = hash*31 + int(c)
	}
	return (hash%h.shardCount + h.shardCount) % h.shardCount
}

// listenRedis listens for Redis pub/sub messages
func (h *Hub) listenRedis() {
	// Subscribe to relevant channels
	pubsub := h.redis.Subscribe(h.ctx, "events:*", "presence:events", "calls:events", "signaling:*")
	defer pubsub.Close()

	ch := pubsub.Channel()

	for {
		select {
		case msg := <-ch:
			if msg == nil {
				continue
			}

			// Parse the event
			var event events.Event
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				log.Printf("Failed to parse Redis event: %v", err)
				continue
			}

			// Store event for replay
			if err := h.replayMgr.StoreEventForReplay(h.ctx, event); err != nil {
				log.Printf("Failed to store event for replay: %v", err)
			}

			// Forward Redis message to WebSocket connections
			h.broadcast <- []byte(msg.Payload)

		case <-h.ctx.Done():
			return
		}
	}
}

// HandleWebSocket handles WebSocket upgrade and connection
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request, userID string) {
	// Upgrade connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	// Create connection object
	sessionID := generateSessionID()
	wsConn := &Connection{
		conn:          conn,
		userID:        userID,
		sessionID:     sessionID,
		channels:      make(map[string]bool),
		send:          make(chan []byte, 256),
		hub:           h,
		authenticated: true, // Assume authenticated if we got here
		lastPong:      time.Now(),
	}

	// Register connection
	h.register <- wsConn

	// Handle event replay if requested
	lastEventID := r.URL.Query().Get("last_event_id")
	if lastEventID != "" {
		go h.handleEventReplay(wsConn, lastEventID)
	}

	// Start connection handlers
	go wsConn.writePump()
	go wsConn.readPump()
}

// GetConnectionCount returns the total number of connections
func (h *Hub) GetConnectionCount() int {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return len(h.connections)
}

// GetShardConnectionCounts returns connection counts per shard
func (h *Hub) GetShardConnectionCounts() []int {
	counts := make([]int, h.shardCount)
	for i, shard := range h.shards {
		shard.mutex.RLock()
		counts[i] = len(shard.connections)
		shard.mutex.RUnlock()
	}
	return counts
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	return fmt.Sprintf("session_%d_%d", time.Now().UnixNano(), time.Now().Nanosecond())
}

// handleEventReplay handles event replay for reconnecting clients
func (h *Hub) handleEventReplay(conn *Connection, lastEventID string) {
	// Wait a bit for the connection to be fully established
	time.Sleep(100 * time.Millisecond)

	// Get user's subscribed channels (for now, we'll replay from all channels)
	// In a real implementation, this would come from the user's session state
	channels := conn.GetSubscriptions()
	if len(channels) == 0 {
		// If no channels subscribed yet, skip replay
		return
	}

	replayReq := events.ReplayRequest{
		UserID:      conn.userID,
		SessionID:   conn.sessionID,
		LastEventID: lastEventID,
		LastSeen:    time.Now().Add(-5 * time.Minute), // Default to 5 minutes ago
		Channels:    channels,
		MaxEvents:   50, // Limit replay to 50 events
	}

	response, err := h.replayMgr.ReplayEvents(h.ctx, replayReq)
	if err != nil {
		log.Printf("Failed to replay events for user %s: %v", conn.userID, err)
		return
	}

	// Send replayed events to the client
	for _, event := range response.Events {
		eventData, err := json.Marshal(WebSocketEvent{
			Type:      event.Type,
			Timestamp: event.Timestamp,
			Data:      json.RawMessage(mustMarshal(event.Data)),
			ChannelID: &event.ChannelID,
			CallID:    &event.CallID,
		})
		if err != nil {
			log.Printf("Failed to marshal replay event: %v", err)
			continue
		}

		select {
		case conn.send <- eventData:
		default:
			log.Printf("Failed to send replay event to connection %s", conn.sessionID)
			return
		}
	}

	log.Printf("Replayed %d events for user %s", len(response.Events), conn.userID)
}

// GetPublisher returns the event publisher
func (h *Hub) GetPublisher() *events.Publisher {
	return h.publisher
}

// GetReplayManager returns the replay manager
func (h *Hub) GetReplayManager() *events.ReplayManager {
	return h.replayMgr
}

// mustMarshal marshals data and panics on error (for internal use)
func mustMarshal(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
