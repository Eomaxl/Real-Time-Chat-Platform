package kafka

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// OrderingManager ensures event ordering across regions
type OrderingManager struct {
	sequenceTrackers map[string]*SequenceTracker // channelID -> tracker
	mu               sync.RWMutex
	bufferSize       int
	bufferTimeout    time.Duration
}

// SequenceTracker tracks sequence numbers for a channel
type SequenceTracker struct {
	channelID       string
	expectedSeq     int64
	buffer          map[int64]*Event // Out-of-order events
	deliveredEvents chan *Event
	mu              sync.RWMutex
	lastDelivered   time.Time
}

// NewOrderingManager creates a new ordering manager
func NewOrderingManager(bufferSize int, bufferTimeout time.Duration) *OrderingManager {
	return &OrderingManager{
		sequenceTrackers: make(map[string]*SequenceTracker),
		bufferSize:       bufferSize,
		bufferTimeout:    bufferTimeout,
	}
}

// NewSequenceTracker creates a new sequence tracker
func NewSequenceTracker(channelID string, bufferSize int) *SequenceTracker {
	return &SequenceTracker{
		channelID:       channelID,
		expectedSeq:     1,
		buffer:          make(map[int64]*Event),
		deliveredEvents: make(chan *Event, bufferSize),
		lastDelivered:   time.Now(),
	}
}

// ProcessEvent processes an event and ensures ordering
func (om *OrderingManager) ProcessEvent(ctx context.Context, event *Event) error {
	channelID := event.ChannelID
	if channelID == "" {
		channelID = event.UserID
	}

	tracker := om.getOrCreateTracker(channelID)
	return tracker.ProcessEvent(ctx, event)
}

// getOrCreateTracker gets or creates a sequence tracker for a channel
func (om *OrderingManager) getOrCreateTracker(channelID string) *SequenceTracker {
	om.mu.Lock()
	defer om.mu.Unlock()

	tracker, ok := om.sequenceTrackers[channelID]
	if !ok {
		tracker = NewSequenceTracker(channelID, om.bufferSize)
		om.sequenceTrackers[channelID] = tracker
		go tracker.bufferTimeoutWorker(om.bufferTimeout)
	}

	return tracker
}

// ProcessEvent processes an event with sequence ordering
func (st *SequenceTracker) ProcessEvent(ctx context.Context, event *Event) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	// Check if this is the expected sequence number
	if event.SequenceNum == st.expectedSeq {
		// Deliver this event
		st.deliverEvent(event)
		st.expectedSeq++

		// Check buffer for subsequent events
		st.deliverBufferedEvents()
		return nil
	}

	// Out of order - check if it's a future event
	if event.SequenceNum > st.expectedSeq {
		// Buffer for later delivery
		st.buffer[event.SequenceNum] = event
		return nil
	}

	// Duplicate or old event - ignore
	return fmt.Errorf("duplicate or old event: got %d, expected %d", event.SequenceNum, st.expectedSeq)
}

// deliverEvent delivers an event to the output channel
func (st *SequenceTracker) deliverEvent(event *Event) {
	select {
	case st.deliveredEvents <- event:
		st.lastDelivered = time.Now()
	default:
		// Channel full - this shouldn't happen with proper buffer sizing
	}
}

// deliverBufferedEvents delivers any buffered events that are now in order
func (st *SequenceTracker) deliverBufferedEvents() {
	for {
		event, ok := st.buffer[st.expectedSeq]
		if !ok {
			break
		}

		st.deliverEvent(event)
		delete(st.buffer, st.expectedSeq)
		st.expectedSeq++
	}
}

// bufferTimeoutWorker handles buffer timeout to prevent indefinite waiting
func (st *SequenceTracker) bufferTimeoutWorker(timeout time.Duration) {
	ticker := time.NewTicker(timeout)
	defer ticker.Stop()

	for range ticker.C {
		st.checkBufferTimeout(timeout)
	}
}

// checkBufferTimeout checks for events that have been buffered too long
func (st *SequenceTracker) checkBufferTimeout(timeout time.Duration) {
	st.mu.Lock()
	defer st.mu.Unlock()

	if len(st.buffer) == 0 {
		return
	}

	// If we haven't delivered anything recently and have buffered events,
	// we might have a gap - deliver what we have
	if time.Since(st.lastDelivered) > timeout {
		// Find the smallest sequence number in buffer
		minSeq := int64(-1)
		for seq := range st.buffer {
			if minSeq == -1 || seq < minSeq {
				minSeq = seq
			}
		}

		if minSeq > st.expectedSeq {
			// We have a gap - skip to the next available sequence
			st.expectedSeq = minSeq
			st.deliverBufferedEvents()
		}
	}
}

// GetDeliveredEvents returns the channel of delivered events
func (st *SequenceTracker) GetDeliveredEvents() <-chan *Event {
	return st.deliveredEvents
}

// GetStats returns tracker statistics
func (st *SequenceTracker) GetStats() map[string]interface{} {
	st.mu.RLock()
	defer st.mu.RUnlock()

	return map[string]interface{}{
		"channel_id":      st.channelID,
		"expected_seq":    st.expectedSeq,
		"buffered_events": len(st.buffer),
		"last_delivered":  st.lastDelivered,
	}
}

// GetAllStats returns statistics for all trackers
func (om *OrderingManager) GetAllStats() map[string]interface{} {
	om.mu.RLock()
	defer om.mu.RUnlock()

	stats := map[string]interface{}{
		"total_trackers": len(om.sequenceTrackers),
		"trackers":       make([]map[string]interface{}, 0),
	}

	for _, tracker := range om.sequenceTrackers {
		stats["trackers"] = append(stats["trackers"].([]map[string]interface{}), tracker.GetStats())
	}

	return stats
}

// CleanupInactiveTrackers removes trackers that haven't been used recently
func (om *OrderingManager) CleanupInactiveTrackers(inactiveThreshold time.Duration) {
	om.mu.Lock()
	defer om.mu.Unlock()

	now := time.Now()
	for channelID, tracker := range om.sequenceTrackers {
		tracker.mu.RLock()
		lastDelivered := tracker.lastDelivered
		tracker.mu.RUnlock()

		if now.Sub(lastDelivered) > inactiveThreshold {
			delete(om.sequenceTrackers, channelID)
		}
	}
}
