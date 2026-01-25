package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"real-time-chat-system/internal/config"

	"github.com/segmentio/kafka-go"
)

// Producer handles publishing events to Kafka
type Producer struct {
	writer      *kafka.Writer
	config      *config.KafkaConfig
	eventQueue  chan *Event
	batchBuffer []*Event
	mu          sync.Mutex
	stopCh      chan struct{}
	wg          sync.WaitGroup
	metrics     *ProducerMetrics
}

// Event represents a cross-region event
type Event struct {
	ID              string                 `json:"id"`
	Type            string                 `json:"type"`
	SourceRegion    string                 `json:"sourceRegion"`
	Timestamp       time.Time              `json:"timestamp"`
	Data            map[string]interface{} `json:"data"`
	ChannelID       string                 `json:"channelId,omitempty"`
	UserID          string                 `json:"userId,omitempty"`
	SequenceNum     int64                  `json:"sequenceNum"`
	DeduplicationID string                 `json:"deduplicationId"`
}

// ProducerMetrics tracks producer metrics
type ProducerMetrics struct {
	mu               sync.RWMutex
	totalEvents      int64
	successfulEvents int64
	failedEvents     int64
	batchesSent      int64
	averageBatchSize float64
	lastPublishTime  time.Time
}

// NewProducer creates a new Kafka producer
func NewProducer(cfg *config.KafkaConfig) (*Producer, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("Kafka is not enabled")
	}

	// Configure compression
	var compression kafka.Compression
	switch cfg.Compression {
	case "gzip":
		compression = kafka.Gzip
	case "snappy":
		compression = kafka.Snappy
	case "lz4":
		compression = kafka.Lz4
	case "zstd":
		compression = kafka.Zstd
	default:
		compression = kafka.Snappy // Default
	}

	writer := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Topic:        cfg.Topic,
		Balancer:     &kafka.Hash{}, // Hash by key for ordering
		Compression:  compression,
		BatchSize:    cfg.BatchSize,
		BatchTimeout: cfg.GetFlushInterval(),
		RequiredAcks: kafka.RequireAll, // Wait for all replicas
		Async:        false,            // Synchronous for reliability
	}

	p := &Producer{
		writer:      writer,
		config:      cfg,
		eventQueue:  make(chan *Event, cfg.BatchSize*2),
		batchBuffer: make([]*Event, 0, cfg.BatchSize),
		stopCh:      make(chan struct{}),
		metrics:     &ProducerMetrics{},
	}

	return p, nil
}

// Start begins the producer worker
func (p *Producer) Start(ctx context.Context) {
	p.wg.Add(1)
	go p.batchWorker(ctx)
}

// Stop stops the producer
func (p *Producer) Stop() {
	close(p.stopCh)
	p.wg.Wait()
	p.writer.Close()
}

// PublishEvent publishes an event to Kafka
func (p *Producer) PublishEvent(ctx context.Context, event *Event) error {
	// Add timestamp if not set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Generate deduplication ID if not set
	if event.DeduplicationID == "" {
		event.DeduplicationID = fmt.Sprintf("%s-%d", event.ID, event.Timestamp.UnixNano())
	}

	select {
	case p.eventQueue <- event:
		p.metrics.incrementTotal()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-p.stopCh:
		return fmt.Errorf("producer stopped")
	}
}

// batchWorker processes events in batches
func (p *Producer) batchWorker(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.GetFlushInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.flushBatch(ctx)
			return
		case <-p.stopCh:
			p.flushBatch(ctx)
			return
		case event := <-p.eventQueue:
			p.addToBatch(event)
			if len(p.batchBuffer) >= p.config.BatchSize {
				p.flushBatch(ctx)
			}
		case <-ticker.C:
			if len(p.batchBuffer) > 0 {
				p.flushBatch(ctx)
			}
		}
	}
}

// addToBatch adds an event to the batch buffer
func (p *Producer) addToBatch(event *Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.batchBuffer = append(p.batchBuffer, event)
}

// flushBatch sends the current batch to Kafka
func (p *Producer) flushBatch(ctx context.Context) {
	p.mu.Lock()
	if len(p.batchBuffer) == 0 {
		p.mu.Unlock()
		return
	}

	batch := p.batchBuffer
	p.batchBuffer = make([]*Event, 0, p.config.BatchSize)
	p.mu.Unlock()

	// Convert events to Kafka messages
	messages := make([]kafka.Message, len(batch))
	for i, event := range batch {
		data, err := json.Marshal(event)
		if err != nil {
			p.metrics.incrementFailed()
			continue
		}

		// Use channel ID or user ID as key for ordering
		key := event.ChannelID
		if key == "" {
			key = event.UserID
		}

		messages[i] = kafka.Message{
			Key:   []byte(key),
			Value: data,
			Headers: []kafka.Header{
				{Key: "event-type", Value: []byte(event.Type)},
				{Key: "source-region", Value: []byte(event.SourceRegion)},
				{Key: "dedup-id", Value: []byte(event.DeduplicationID)},
			},
		}
	}

	// Write batch to Kafka
	if err := p.writer.WriteMessages(ctx, messages...); err != nil {
		p.metrics.incrementFailed()
		// In production, implement retry logic here
		return
	}

	p.metrics.recordBatch(len(batch))
	p.metrics.incrementSuccessful(int64(len(batch)))
}

// GetMetrics returns producer metrics
func (p *Producer) GetMetrics() map[string]interface{} {
	p.metrics.mu.RLock()
	defer p.metrics.mu.RUnlock()

	return map[string]interface{}{
		"total_events":       p.metrics.totalEvents,
		"successful_events":  p.metrics.successfulEvents,
		"failed_events":      p.metrics.failedEvents,
		"batches_sent":       p.metrics.batchesSent,
		"average_batch_size": p.metrics.averageBatchSize,
		"last_publish_time":  p.metrics.lastPublishTime,
	}
}

// incrementTotal increments total events counter
func (m *ProducerMetrics) incrementTotal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.totalEvents++
}

// incrementSuccessful increments successful events counter
func (m *ProducerMetrics) incrementSuccessful(count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.successfulEvents += count
	m.lastPublishTime = time.Now()
}

// incrementFailed increments failed events counter
func (m *ProducerMetrics) incrementFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failedEvents++
}

// recordBatch records batch statistics
func (m *ProducerMetrics) recordBatch(size int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batchesSent++

	// Update running average
	if m.batchesSent == 1 {
		m.averageBatchSize = float64(size)
	} else {
		m.averageBatchSize = (m.averageBatchSize*float64(m.batchesSent-1) + float64(size)) / float64(m.batchesSent)
	}
}
