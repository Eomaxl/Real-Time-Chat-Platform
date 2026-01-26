package chatplatform

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocketClient handles WebSocket connections with automatic reconnection
type WebSocketClient struct {
	client          *Client
	conn            *websocket.Conn
	url             string
	reconnectDelay  time.Duration
	maxReconnectDelay time.Duration
	connected       bool
	mu              sync.RWMutex
	
	// Event handlers
	onMessage       func(event WebSocketEvent)
	onConnect       func()
	onDisconnect    func()
	onError         func(error)
	
	// Subscriptions
	subscriptions   map[string]bool
	subMu           sync.RWMutex
	
	// Control channels
	stopChan        chan struct{}
	reconnectChan   chan struct{}
	wg              sync.WaitGroup
}

// WebSocketEvent represents an event received via WebSocket
type WebSocketEvent struct {
	Type      string          `json:"type"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
	ChannelID *string         `json:"channel_id,omitempty"`
	CallID    *string         `json:"call_id,omitempty"`
}

// NewWebSocketClient creates a new WebSocket client
func (c *Client) NewWebSocketClient(wsURL string) *WebSocketClient {
	if wsURL == "" {
		// Default WebSocket URL based on base URL
		wsURL = "ws://localhost:8080/v1/ws"
	}

	return &WebSocketClient{
		client:            c,
		url:               wsURL,
		reconnectDelay:    1 * time.Second,
		maxReconnectDelay: 30 * time.Second,
		subscriptions:     make(map[string]bool),
		stopChan:          make(chan struct{}),
		reconnectChan:     make(chan struct{}, 1),
	}
}

// OnMessage sets the message event handler
func (ws *WebSocketClient) OnMessage(handler func(event WebSocketEvent)) {
	ws.onMessage = handler
}

// OnConnect sets the connect event handler
func (ws *WebSocketClient) OnConnect(handler func()) {
	ws.onConnect = handler
}

// OnDisconnect sets the disconnect event handler
func (ws *WebSocketClient) OnDisconnect(handler func()) {
	ws.onDisconnect = handler
}

// OnError sets the error event handler
func (ws *WebSocketClient) OnError(handler func(error)) {
	ws.onError = handler
}

// Connect establishes a WebSocket connection
func (ws *WebSocketClient) Connect(ctx context.Context) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.connected {
		return nil
	}

	// Create WebSocket connection with headers
	headers := make(map[string][]string)
	if ws.client.apiKey != "" {
		headers["Authorization"] = []string{"Bearer " + ws.client.apiKey}
	}

	dialer := websocket.DefaultDialer
	conn, _, err := dialer.Dial(ws.url, headers)
	if err != nil {
		return fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	ws.conn = conn
	ws.connected = true

	// Start message reader
	ws.wg.Add(1)
	go ws.readMessages()

	// Start reconnection handler
	ws.wg.Add(1)
	go ws.handleReconnection(ctx)

	// Call onConnect handler
	if ws.onConnect != nil {
		go ws.onConnect()
	}

	// Resubscribe to channels
	ws.resubscribe()

	return nil
}

// Disconnect closes the WebSocket connection
func (ws *WebSocketClient) Disconnect() error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if !ws.connected {
		return nil
	}

	close(ws.stopChan)
	
	if ws.conn != nil {
		ws.conn.Close()
	}

	ws.connected = false
	ws.wg.Wait()

	// Call onDisconnect handler
	if ws.onDisconnect != nil {
		go ws.onDisconnect()
	}

	return nil
}

// IsConnected returns whether the WebSocket is connected
func (ws *WebSocketClient) IsConnected() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.connected
}

// SubscribeToChannel subscribes to events for a specific channel
func (ws *WebSocketClient) SubscribeToChannel(channelID string) error {
	ws.subMu.Lock()
	ws.subscriptions[channelID] = true
	ws.subMu.Unlock()

	return ws.sendSubscription("subscribe", channelID)
}

// UnsubscribeFromChannel unsubscribes from events for a specific channel
func (ws *WebSocketClient) UnsubscribeFromChannel(channelID string) error {
	ws.subMu.Lock()
	delete(ws.subscriptions, channelID)
	ws.subMu.Unlock()

	return ws.sendSubscription("unsubscribe", channelID)
}

// sendSubscription sends a subscription message
func (ws *WebSocketClient) sendSubscription(action, channelID string) error {
	ws.mu.RLock()
	conn := ws.conn
	connected := ws.connected
	ws.mu.RUnlock()

	if !connected || conn == nil {
		return fmt.Errorf("not connected")
	}

	msg := map[string]string{
		"action":     action,
		"channel_id": channelID,
	}

	return conn.WriteJSON(msg)
}

// resubscribe resubscribes to all channels after reconnection
func (ws *WebSocketClient) resubscribe() {
	ws.subMu.RLock()
	channels := make([]string, 0, len(ws.subscriptions))
	for channelID := range ws.subscriptions {
		channels = append(channels, channelID)
	}
	ws.subMu.RUnlock()

	for _, channelID := range channels {
		if err := ws.sendSubscription("subscribe", channelID); err != nil {
			log.Printf("Failed to resubscribe to channel %s: %v", channelID, err)
		}
	}
}

// readMessages reads messages from the WebSocket connection
func (ws *WebSocketClient) readMessages() {
	defer ws.wg.Done()

	for {
		ws.mu.RLock()
		conn := ws.conn
		ws.mu.RUnlock()

		if conn == nil {
			return
		}

		var event WebSocketEvent
		err := conn.ReadJSON(&event)
		if err != nil {
			// Connection closed or error
			ws.handleDisconnection()
			return
		}

		// Call message handler
		if ws.onMessage != nil {
			go ws.onMessage(event)
		}
	}
}

// handleDisconnection handles disconnection and triggers reconnection
func (ws *WebSocketClient) handleDisconnection() {
	ws.mu.Lock()
	wasConnected := ws.connected
	ws.connected = false
	if ws.conn != nil {
		ws.conn.Close()
		ws.conn = nil
	}
	ws.mu.Unlock()

	if wasConnected {
		// Call onDisconnect handler
		if ws.onDisconnect != nil {
			go ws.onDisconnect()
		}

		// Trigger reconnection
		select {
		case ws.reconnectChan <- struct{}{}:
		default:
		}
	}
}

// handleReconnection handles automatic reconnection with exponential backoff
func (ws *WebSocketClient) handleReconnection(ctx context.Context) {
	defer ws.wg.Done()

	currentDelay := ws.reconnectDelay

	for {
		select {
		case <-ws.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ws.reconnectChan:
			// Wait before attempting reconnection
			time.Sleep(currentDelay)

			// Attempt to reconnect
			err := ws.Connect(ctx)
			if err != nil {
				if ws.onError != nil {
					go ws.onError(fmt.Errorf("reconnection failed: %w", err))
				}

				// Increase delay with exponential backoff
				currentDelay *= 2
				if currentDelay > ws.maxReconnectDelay {
					currentDelay = ws.maxReconnectDelay
				}

				// Trigger another reconnection attempt
				select {
				case ws.reconnectChan <- struct{}{}:
				default:
				}
			} else {
				// Reset delay on successful reconnection
				currentDelay = ws.reconnectDelay
			}
		}
	}
}

// SendMessage sends a message through the WebSocket connection
func (ws *WebSocketClient) SendMessage(msg interface{}) error {
	ws.mu.RLock()
	conn := ws.conn
	connected := ws.connected
	ws.mu.RUnlock()

	if !connected || conn == nil {
		return fmt.Errorf("not connected")
	}

	return conn.WriteJSON(msg)
}
