package batch

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Processor handles batch processing of operations
type Processor struct {
	batchSize     int
	flushInterval time.Duration
	queue         chan interface{}
	handler       BatchHandler
	mu            sync.Mutex
	buffer        []interface{}
	stopChan      chan struct{}
	wg            sync.WaitGroup

	// Statistics
	totalProcessed uint64
	totalBatches   uint64
	mustats        sync.RWMutex
}

// BatchHandler processes a batch of items
type BatchHandler func(ctx context.Context, items []interface{}) error

// Config configures the batch processor
type Config struct {
	BatchSize     int
	FlushInterval time.Duration
	QueueSize     int
}

// NewProcessor creates a new batch processor
func NewProcessor(config Config, handler BatchHandler) *Processor {
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 100 * time.Millisecond
	}
	if config.QueueSize <= 0 {
		config.QueueSize = config.BatchSize * 10
	}

	return &Processor{
		batchSize:     config.BatchSize,
		flushInterval: config.FlushInterval,
		queue:         make(chan interface{}, config.QueueSize),
		handler:       handler,
		buffer:        make([]interface{}, 0, config.BatchSize),
		stopChan:      make(chan struct{}),
	}
}

// Start begins processing batches
func (p *Processor) Start(ctx context.Context) {
	p.wg.Add(1)
	go p.run(ctx)
}

// Stop stops the batch processor and flushes remaining items
func (p *Processor) Stop() {
	close(p.stopChan)
	p.wg.Wait()
	close(p.queue)
}

// Submit adds an item to the processing queue
func (p *Processor) Submit(item interface{}) error {
	select {
	case p.queue <- item:
		return nil
	default:
		return fmt.Errorf("queue is full")
	}
}

// run executes the batch processing loop
func (p *Processor) run(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			// Flush remaining items before stopping
			p.flush(ctx)
			return

		case <-ticker.C:
			// Flush on timer
			p.flush(ctx)

		case item := <-p.queue:
			// Add to buffer
			p.mu.Lock()
			p.buffer = append(p.buffer, item)
			shouldFlush := len(p.buffer) >= p.batchSize
			p.mu.Unlock()

			// Flush if batch is full
			if shouldFlush {
				p.flush(ctx)
			}
		}
	}
}

// flush processes all items in the current buffer
func (p *Processor) flush(ctx context.Context) {
	p.mu.Lock()
	if len(p.buffer) == 0 {
		p.mu.Unlock()
		return
	}

	// Copy buffer and clear
	batch := make([]interface{}, len(p.buffer))
	copy(batch, p.buffer)
	p.buffer = p.buffer[:0]
	p.mu.Unlock()

	// Process batch
	if err := p.handler(ctx, batch); err != nil {
		log.Printf("Failed to process batch of %d items: %v", len(batch), err)
	} else {
		// Update statistics
		p.mustats.Lock()
		p.totalProcessed += uint64(len(batch))
		p.totalBatches++
		p.mustats.Unlock()
	}
}

// Stats returns processing statistics
func (p *Processor) Stats() ProcessorStats {
	p.mustats.RLock()
	defer p.mustats.RUnlock()

	return ProcessorStats{
		TotalProcessed: p.totalProcessed,
		TotalBatches:   p.totalBatches,
		QueueSize:      len(p.queue),
		BufferSize:     len(p.buffer),
	}
}

// ProcessorStats represents batch processor statistics
type ProcessorStats struct {
	TotalProcessed uint64
	TotalBatches   uint64
	QueueSize      int
	BufferSize     int
}
