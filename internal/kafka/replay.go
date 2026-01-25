package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

// ReplayManager handles event replay for regional synchronization
type ReplayManager struct {
	reader     *kafka.Reader
	eventStore *EventStore
	mu         sync.RWMutex
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// EventStore stores events for replay
type EventStore struct {
	events              map[string][]*Event // channelID -> events
	sequenceIndex       map[string]int64    // channelID -> last sequence number
	mu                  sync.RWMutex
	maxEventsPerChannel int
	ttl                 time.Duration
}

// ReplayRequest represents a request to replay events
type ReplayRequest struct {
	ChannelID     string
	FromSequence  int64
	ToSequence    int64
	FromTimestamp time.Time
	ToTimestamp   time.Time
	MaxEvents     int
}

// ReplayResponse contains replayed events
type ReplayResponse struct {
	Events       []*Event
	TotalEvents  int
	FromSequence int64
	ToSequence   int64
	HasMore      bool
}

// NewReplayManager creates a new replay manager
func NewReplayManager(brokers []string, topic string) (*ReplayManager, error) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokers,
		Topic:       topic,
		MinBytes:    1,
		MaxBytes:    10e6,
		StartOffset: kafka.FirstOffset, // Start from beginning for replay
	})

	rm := &ReplayManager{
		reader:     reader,
		eventStore: NewEventStore(10000, 24*time.Hour),
		stopCh:     make(chan struct{}),
	}

	return rm, nil
}

// NewEventStore creates a new event store
func NewEventStore(maxEventsPerChannel int, ttl time.Duration) *EventStore {
	return &EventStore{
		events:              make(map[string][]*Event),
		sequenceIndex:       make(map[string]int64),
		maxEventsPerChannel: maxEventsPerChannel,
		ttl:                 ttl,
	}
}

// Start begins the replay manager
func (rm *ReplayManager) Start(ctx context.Context) {
	rm.wg.Add(1)
	go rm.indexWorker(ctx)
}

// Stop stops the replay manager
func (rm *ReplayManager) Stop() {
	close(rm.stopCh)
	rm.wg.Wait()
	rm.reader.Close()
}

// indexWorker indexes events for replay
func (rm *ReplayManager) indexWorker(ctx context.Context) {
	defer rm.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case <-rm.stopCh:
			return
		default:
			msg, err := rm.reader.FetchMessage(ctx)
			if err != nil {
				if err == context.Canceled {
					return
				}
				continue
			}

			// Parse and store event
			var event Event
			if err := json.Unmarshal(msg.Value, &event); err != nil {
				continue
			}

			rm.eventStore.StoreEvent(&event)
		}
	}
}

// ReplayEvents replays events based on the request
func (rm *ReplayManager) ReplayEvents(ctx context.Context, req ReplayRequest) (*ReplayResponse, error) {
	if req.ChannelID == "" {
		return nil, fmt.Errorf("channel ID is required")
	}

	events := rm.eventStore.GetEvents(req)

	response := &ReplayResponse{
		Events:      events,
		TotalEvents: len(events),
	}

	if len(events) > 0 {
		response.FromSequence = events[0].SequenceNum
		response.ToSequence = events[len(events)-1].SequenceNum

		// Check if there are more events
		lastSeq := rm.eventStore.GetLastSequence(req.ChannelID)
		response.HasMore = response.ToSequence < lastSeq
	}

	return response, nil
}

// ReplayFromTimestamp replays events from a specific timestamp
func (rm *ReplayManager) ReplayFromTimestamp(ctx context.Context, channelID string, fromTime time.Time, maxEvents int) (*ReplayResponse, error) {
	req := ReplayRequest{
		ChannelID:     channelID,
		FromTimestamp: fromTime,
		MaxEvents:     maxEvents,
	}
	return rm.ReplayEvents(ctx, req)
}

// ReplayFromSequence replays events from a specific sequence number
func (rm *ReplayManager) ReplayFromSequence(ctx context.Context, channelID string, fromSeq int64, maxEvents int) (*ReplayResponse, error) {
	req := ReplayRequest{
		ChannelID:    channelID,
		FromSequence: fromSeq,
		MaxEvents:    maxEvents,
	}
	return rm.ReplayEvents(ctx, req)
}

// StoreEvent stores an event in the event store
func (es *EventStore) StoreEvent(event *Event) {
	es.mu.Lock()
	defer es.mu.Unlock()

	channelID := event.ChannelID
	if channelID == "" {
		channelID = event.UserID // Use user ID for DMs
	}

	// Initialize channel events if needed
	if _, ok := es.events[channelID]; !ok {
		es.events[channelID] = make([]*Event, 0)
	}

	// Add event
	es.events[channelID] = append(es.events[channelID], event)

	// Update sequence index
	if event.SequenceNum > es.sequenceIndex[channelID] {
		es.sequenceIndex[channelID] = event.SequenceNum
	}

	// Trim if exceeds max
	if len(es.events[channelID]) > es.maxEventsPerChannel {
		// Remove oldest events
		excess := len(es.events[channelID]) - es.maxEventsPerChannel
		es.events[channelID] = es.events[channelID][excess:]
	}
}

// GetEvents retrieves events based on the request
func (es *EventStore) GetEvents(req ReplayRequest) []*Event {
	es.mu.RLock()
	defer es.mu.RUnlock()

	channelID := req.ChannelID
	events, ok := es.events[channelID]
	if !ok {
		return []*Event{}
	}

	result := make([]*Event, 0)
	maxEvents := req.MaxEvents
	if maxEvents == 0 {
		maxEvents = 100 // Default
	}

	for _, event := range events {
		// Filter by sequence number
		if req.FromSequence > 0 && event.SequenceNum < req.FromSequence {
			continue
		}
		if req.ToSequence > 0 && event.SequenceNum > req.ToSequence {
			continue
		}

		// Filter by timestamp
		if !req.FromTimestamp.IsZero() && event.Timestamp.Before(req.FromTimestamp) {
			continue
		}
		if !req.ToTimestamp.IsZero() && event.Timestamp.After(req.ToTimestamp) {
			continue
		}

		// Check TTL
		if time.Since(event.Timestamp) > es.ttl {
			continue
		}

		result = append(result, event)

		if len(result) >= maxEvents {
			break
		}
	}

	return result
}

// GetLastSequence returns the last sequence number for a channel
func (es *EventStore) GetLastSequence(channelID string) int64 {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return es.sequenceIndex[channelID]
}

// CleanupExpired removes expired events
func (es *EventStore) CleanupExpired() {
	es.mu.Lock()
	defer es.mu.Unlock()

	now := time.Now()
	for channelID, events := range es.events {
		filtered := make([]*Event, 0)
		for _, event := range events {
			if now.Sub(event.Timestamp) <= es.ttl {
				filtered = append(filtered, event)
			}
		}
		es.events[channelID] = filtered
	}
}

// GetStats returns event store statistics
func (es *EventStore) GetStats() map[string]interface{} {
	es.mu.RLock()
	defer es.mu.RUnlock()

	totalEvents := 0
	for _, events := range es.events {
		totalEvents += len(events)
	}

	return map[string]interface{}{
		"total_channels":  len(es.events),
		"total_events":    totalEvents,
		"max_per_channel": es.maxEventsPerChannel,
		"ttl_hours":       es.ttl.Hours(),
	}
}
