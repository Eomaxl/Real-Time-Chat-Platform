package websocket

import (
	"context"
	"log"
)

// run is the main loop for a connection shard
func (s *ConnectionShard) run(ctx context.Context) {
	for {
		select {
		case conn := <-s.register:
			s.registerConnection(conn)

		case conn := <-s.unregister:
			s.unregisterConnection(conn)

		case message := <-s.broadcast:
			s.broadcastMessage(message)

		case <-ctx.Done():
			s.cleanup()
			return
		}
	}
}

// registerConnection registers a connection with this shard
func (s *ConnectionShard) registerConnection(conn *Connection) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.connections[conn.sessionID] = conn
	log.Printf("Connection registered with shard %d: session=%s, total=%d",
		s.shardID, conn.sessionID, len(s.connections))
}

// unregisterConnection unregisters a connection from this shard
func (s *ConnectionShard) unregisterConnection(conn *Connection) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, exists := s.connections[conn.sessionID]; exists {
		delete(s.connections, conn.sessionID)
		log.Printf("Connection unregistered from shard %d: session=%s, total=%d",
			s.shardID, conn.sessionID, len(s.connections))
	}
}

// broadcastMessage broadcasts a message to all connections in this shard
func (s *ConnectionShard) broadcastMessage(message []byte) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	for sessionID, conn := range s.connections {
		select {
		case conn.send <- message:
		default:
			// Connection is blocked, it will be cleaned up by the hub
			log.Printf("Failed to send message to connection %s in shard %d",
				sessionID, s.shardID)
		}
	}
}

// cleanup closes all connections in this shard
func (s *ConnectionShard) cleanup() {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	for _, conn := range s.connections {
		conn.close()
	}

	s.connections = make(map[string]*Connection)
	log.Printf("Shard %d cleaned up", s.shardID)
}

// GetConnectionCount returns the number of connections in this shard
func (s *ConnectionShard) GetConnectionCount() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return len(s.connections)
}

// GetConnections returns all connections in this shard (for testing/debugging)
func (s *ConnectionShard) GetConnections() map[string]*Connection {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	connections := make(map[string]*Connection)
	for sessionID, conn := range s.connections {
		connections[sessionID] = conn
	}
	return connections
}

// BroadcastToChannel broadcasts a message to connections subscribed to a specific channel
func (s *ConnectionShard) BroadcastToChannel(channelID string, message []byte) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	for sessionID, conn := range s.connections {
		if conn.IsSubscribed(channelID) {
			select {
			case conn.send <- message:
			default:
				log.Printf("Failed to send channel message to connection %s in shard %d",
					sessionID, s.shardID)
			}
		}
	}
}

// GetChannelSubscribers returns all connections subscribed to a channel in this shard
func (s *ConnectionShard) GetChannelSubscribers(channelID string) []*Connection {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var subscribers []*Connection
	for _, conn := range s.connections {
		if conn.IsSubscribed(channelID) {
			subscribers = append(subscribers, conn)
		}
	}
	return subscribers
}

// GetUserConnections returns all connections for a specific user in this shard
func (s *ConnectionShard) GetUserConnections(userID string) []*Connection {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var userConnections []*Connection
	for _, conn := range s.connections {
		if conn.GetUserID() == userID {
			userConnections = append(userConnections, conn)
		}
	}
	return userConnections
}
