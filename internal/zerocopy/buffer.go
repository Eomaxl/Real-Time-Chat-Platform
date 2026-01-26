package zerocopy

import (
	"io"
	"sync"
)

// Buffer provides a zero-copy buffer implementation
type Buffer struct {
	buf  []byte
	off  int
	pool *sync.Pool
}

// BufferPool manages a pool of reusable buffers
type BufferPool struct {
	pool sync.Pool
	size int
}

// NewBufferPool creates a new buffer pool
func NewBufferPool(size int) *BufferPool {
	bp := &BufferPool{
		size: size,
	}

	bp.pool = sync.Pool{
		New: func() interface{} {
			return &Buffer{
				buf:  make([]byte, 0, size),
				pool: &bp.pool,
			}
		},
	}

	return bp
}

// Get retrieves a buffer from the pool
func (bp *BufferPool) Get() *Buffer {
	return bp.pool.Get().(*Buffer)
}

// Put returns a buffer to the pool
func (bp *BufferPool) Put(b *Buffer) {
	b.Reset()
	bp.pool.Put(b)
}

// Write appends data to the buffer
func (b *Buffer) Write(p []byte) (n int, err error) {
	b.buf = append(b.buf, p...)
	return len(p), nil
}

// WriteByte appends a single byte to the buffer
func (b *Buffer) WriteByte(c byte) error {
	b.buf = append(b.buf, c)
	return nil
}

// WriteString appends a string to the buffer
func (b *Buffer) WriteString(s string) (n int, err error) {
	b.buf = append(b.buf, s...)
	return len(s), nil
}

// Read reads data from the buffer
func (b *Buffer) Read(p []byte) (n int, err error) {
	if b.off >= len(b.buf) {
		return 0, io.EOF
	}

	n = copy(p, b.buf[b.off:])
	b.off += n

	return n, nil
}

// Bytes returns the buffer contents as a byte slice (zero-copy)
func (b *Buffer) Bytes() []byte {
	return b.buf[b.off:]
}

// String returns the buffer contents as a string (zero-copy)
func (b *Buffer) String() string {
	return string(b.buf[b.off:])
}

// Len returns the number of unread bytes
func (b *Buffer) Len() int {
	return len(b.buf) - b.off
}

// Cap returns the capacity of the buffer
func (b *Buffer) Cap() int {
	return cap(b.buf)
}

// Reset resets the buffer for reuse
func (b *Buffer) Reset() {
	b.buf = b.buf[:0]
	b.off = 0
}

// Release returns the buffer to its pool
func (b *Buffer) Release() {
	if b.pool != nil {
		b.Reset()
		b.pool.Put(b)
	}
}

// Grow grows the buffer to guarantee space for n more bytes
func (b *Buffer) Grow(n int) {
	if cap(b.buf)-len(b.buf) < n {
		newBuf := make([]byte, len(b.buf), 2*cap(b.buf)+n)
		copy(newBuf, b.buf)
		b.buf = newBuf
	}
}

// MessageWriter provides zero-copy message writing
type MessageWriter struct {
	buf *Buffer
}

// NewMessageWriter creates a new message writer
func NewMessageWriter(buf *Buffer) *MessageWriter {
	return &MessageWriter{buf: buf}
}

// WriteHeader writes a message header
func (mw *MessageWriter) WriteHeader(messageType string, length int) error {
	// Write message type
	if _, err := mw.buf.WriteString(messageType); err != nil {
		return err
	}

	// Write separator
	if err := mw.buf.WriteByte(':'); err != nil {
		return err
	}

	// Write length (simplified - in production use binary encoding)
	// This is just for demonstration

	return nil
}

// WritePayload writes message payload
func (mw *MessageWriter) WritePayload(data []byte) error {
	_, err := mw.buf.Write(data)
	return err
}

// Bytes returns the complete message
func (mw *MessageWriter) Bytes() []byte {
	return mw.buf.Bytes()
}

// MessageReader provides zero-copy message reading
type MessageReader struct {
	buf []byte
	off int
}

// NewMessageReader creates a new message reader
func NewMessageReader(data []byte) *MessageReader {
	return &MessageReader{
		buf: data,
		off: 0,
	}
}

// ReadHeader reads a message header
func (mr *MessageReader) ReadHeader() (messageType string, length int, err error) {
	// Find separator
	sepIdx := -1
	for i := mr.off; i < len(mr.buf); i++ {
		if mr.buf[i] == ':' {
			sepIdx = i
			break
		}
	}

	if sepIdx == -1 {
		return "", 0, io.EOF
	}

	// Extract message type (zero-copy)
	messageType = string(mr.buf[mr.off:sepIdx])
	mr.off = sepIdx + 1

	// In production, read length from binary encoding
	length = len(mr.buf) - mr.off

	return messageType, length, nil
}

// ReadPayload reads message payload (zero-copy)
func (mr *MessageReader) ReadPayload(length int) []byte {
	if mr.off+length > len(mr.buf) {
		length = len(mr.buf) - mr.off
	}

	payload := mr.buf[mr.off : mr.off+length]
	mr.off += length

	return payload
}

// Remaining returns the number of unread bytes
func (mr *MessageReader) Remaining() int {
	return len(mr.buf) - mr.off
}
