package pool

import (
	"strings"
	"sync"
)

// ObjectPool provides a generic object pool for reusing objects
type ObjectPool struct {
	pool sync.Pool
	new  func() interface{}
}

// NewObjectPool creates a new object pool
func NewObjectPool(newFunc func() interface{}) *ObjectPool {
	return &ObjectPool{
		pool: sync.Pool{
			New: newFunc,
		},
		new: newFunc,
	}
}

// Get retrieves an object from the pool
func (p *ObjectPool) Get() interface{} {
	return p.pool.Get()
}

// Put returns an object to the pool
func (p *ObjectPool) Put(obj interface{}) {
	p.pool.Put(obj)
}

// ByteBufferPool provides a pool for byte buffers
type ByteBufferPool struct {
	pool sync.Pool
	size int
}

// NewByteBufferPool creates a new byte buffer pool
func NewByteBufferPool(size int) *ByteBufferPool {
	return &ByteBufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, size)
				return &buf
			},
		},
		size: size,
	}
}

// Get retrieves a byte buffer from the pool
func (p *ByteBufferPool) Get() *[]byte {
	return p.pool.Get().(*[]byte)
}

// Put returns a byte buffer to the pool
func (p *ByteBufferPool) Put(buf *[]byte) {
	// Reset buffer before returning to pool
	*buf = (*buf)[:0]
	p.pool.Put(buf)
}

// MessagePool provides a pool for message objects
type MessagePool struct {
	pool sync.Pool
}

// Message represents a pooled message object
type Message struct {
	ID        string
	ChannelID string
	UserID    string
	Content   string
	Type      string
	Data      []byte
}

// NewMessagePool creates a new message pool
func NewMessagePool() *MessagePool {
	return &MessagePool{
		pool: sync.Pool{
			New: func() interface{} {
				return &Message{}
			},
		},
	}
}

// Get retrieves a message from the pool
func (p *MessagePool) Get() *Message {
	msg := p.pool.Get().(*Message)
	return msg
}

// Put returns a message to the pool after resetting it
func (p *MessagePool) Put(msg *Message) {
	// Reset message fields
	msg.ID = ""
	msg.ChannelID = ""
	msg.UserID = ""
	msg.Content = ""
	msg.Type = ""
	msg.Data = nil

	p.pool.Put(msg)
}

// ConnectionPool provides a pool for connection objects
type ConnectionPool struct {
	pool sync.Pool
}

// Connection represents a pooled connection object
type Connection struct {
	ID       string
	UserID   string
	Channels []string
	Data     map[string]interface{}
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool() *ConnectionPool {
	return &ConnectionPool{
		pool: sync.Pool{
			New: func() interface{} {
				return &Connection{
					Channels: make([]string, 0, 10),
					Data:     make(map[string]interface{}),
				}
			},
		},
	}
}

// Get retrieves a connection from the pool
func (p *ConnectionPool) Get() *Connection {
	conn := p.pool.Get().(*Connection)
	return conn
}

// Put returns a connection to the pool after resetting it
func (p *ConnectionPool) Put(conn *Connection) {
	// Reset connection fields
	conn.ID = ""
	conn.UserID = ""
	conn.Channels = conn.Channels[:0]

	// Clear map
	for k := range conn.Data {
		delete(conn.Data, k)
	}

	p.pool.Put(conn)
}

// StringBuilderPool provides a pool for string builders
type StringBuilderPool struct {
	pool sync.Pool
}

// NewStringBuilderPool creates a new string builder pool
func NewStringBuilderPool() *StringBuilderPool {
	return &StringBuilderPool{
		pool: sync.Pool{
			New: func() interface{} {
				return new(strings.Builder)
			},
		},
	}
}

// Get retrieves a string builder from the pool
func (p *StringBuilderPool) Get() *strings.Builder {
	return p.pool.Get().(*strings.Builder)
}

// Put returns a string builder to the pool after resetting it
func (p *StringBuilderPool) Put(sb *strings.Builder) {
	sb.Reset()
	p.pool.Put(sb)
}
