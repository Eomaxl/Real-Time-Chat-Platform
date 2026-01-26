package tenant

import (
	"container/heap"
	"context"
	"sync"
	"time"
)

// Priority levels for different tenant tiers
const (
	PriorityPlatform   = 4
	PriorityEnterprise = 3
	PriorityPro        = 2
	PriorityFree       = 1
)

// GetPriority returns the priority level for a tenant tier
func GetPriority(tier string) int {
	switch tier {
	case "platform":
		return PriorityPlatform
	case "enterprise":
		return PriorityEnterprise
	case "pro":
		return PriorityPro
	case "free":
		return PriorityFree
	default:
		return PriorityFree
	}
}

// PriorityMessage represents a message with priority
type PriorityMessage struct {
	TenantID  string
	Priority  int
	Timestamp time.Time
	Data      interface{}
	Index     int // Index in the heap
}

// PriorityQueue implements a priority queue for tenant messages
type PriorityQueue struct {
	items []*PriorityMessage
	mu    sync.RWMutex
}

// NewPriorityQueue creates a new priority queue
func NewPriorityQueue() *PriorityQueue {
	pq := &PriorityQueue{
		items: make([]*PriorityMessage, 0),
	}
	heap.Init(pq)
	return pq
}

// Len returns the number of items in the queue
func (pq *PriorityQueue) Len() int {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	return len(pq.items)
}

// Less compares two items in the queue
func (pq *PriorityQueue) Less(i, j int) bool {
	// Higher priority comes first
	if pq.items[i].Priority != pq.items[j].Priority {
		return pq.items[i].Priority > pq.items[j].Priority
	}
	// If same priority, older messages come first (FIFO within priority)
	return pq.items[i].Timestamp.Before(pq.items[j].Timestamp)
}

// Swap swaps two items in the queue
func (pq *PriorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[i].Index = i
	pq.items[j].Index = j
}

// Push adds an item to the queue
func (pq *PriorityQueue) Push(x interface{}) {
	n := len(pq.items)
	item := x.(*PriorityMessage)
	item.Index = n
	pq.items = append(pq.items, item)
}

// Pop removes and returns the highest priority item
func (pq *PriorityQueue) Pop() interface{} {
	old := pq.items
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // avoid memory leak
	item.Index = -1 // for safety
	pq.items = old[0 : n-1]
	return item
}

// Enqueue adds a message to the queue with thread safety
func (pq *PriorityQueue) Enqueue(msg *PriorityMessage) {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	heap.Push(pq, msg)
}

// Dequeue removes and returns the highest priority message
func (pq *PriorityQueue) Dequeue() *PriorityMessage {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if len(pq.items) == 0 {
		return nil
	}

	return heap.Pop(pq).(*PriorityMessage)
}

// Peek returns the highest priority message without removing it
func (pq *PriorityQueue) Peek() *PriorityMessage {
	pq.mu.RLock()
	defer pq.mu.RUnlock()

	if len(pq.items) == 0 {
		return nil
	}

	return pq.items[0]
}

// IsEmpty returns true if the queue is empty
func (pq *PriorityQueue) IsEmpty() bool {
	pq.mu.RLock()
	defer pq.mu.RUnlock()
	return len(pq.items) == 0
}

// PriorityQueueManager manages multiple priority queues
type PriorityQueueManager struct {
	queues map[string]*PriorityQueue // One queue per resource type
	mu     sync.RWMutex
}

// NewPriorityQueueManager creates a new priority queue manager
func NewPriorityQueueManager() *PriorityQueueManager {
	return &PriorityQueueManager{
		queues: make(map[string]*PriorityQueue),
	}
}

// GetQueue returns the queue for a specific resource type
func (m *PriorityQueueManager) GetQueue(resourceType string) *PriorityQueue {
	m.mu.Lock()
	defer m.mu.Unlock()

	if queue, exists := m.queues[resourceType]; exists {
		return queue
	}

	queue := NewPriorityQueue()
	m.queues[resourceType] = queue
	return queue
}

// EnqueueMessage adds a message to the appropriate queue
func (m *PriorityQueueManager) EnqueueMessage(resourceType string, tenantID string, tier string, data interface{}) {
	queue := m.GetQueue(resourceType)

	msg := &PriorityMessage{
		TenantID:  tenantID,
		Priority:  GetPriority(tier),
		Timestamp: time.Now(),
		Data:      data,
	}

	queue.Enqueue(msg)
}

// DequeueMessage removes and returns the highest priority message from a queue
func (m *PriorityQueueManager) DequeueMessage(resourceType string) *PriorityMessage {
	queue := m.GetQueue(resourceType)
	return queue.Dequeue()
}

// ProcessQueue processes messages from a queue with a worker function
func (m *PriorityQueueManager) ProcessQueue(ctx context.Context, resourceType string, workerFn func(context.Context, *PriorityMessage) error) {
	queue := m.GetQueue(resourceType)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg := queue.Dequeue()
			if msg == nil {
				// Queue is empty, wait a bit
				time.Sleep(10 * time.Millisecond)
				continue
			}

			// Process message
			if err := workerFn(ctx, msg); err != nil {
				// Log error but continue processing
				// In production, you might want to retry or move to DLQ
				continue
			}
		}
	}
}

// GetQueueStats returns statistics for a queue
func (m *PriorityQueueManager) GetQueueStats(resourceType string) map[string]interface{} {
	queue := m.GetQueue(resourceType)

	queue.mu.RLock()
	defer queue.mu.RUnlock()

	// Count messages by priority
	priorityCounts := make(map[int]int)
	for _, msg := range queue.items {
		priorityCounts[msg.Priority]++
	}

	return map[string]interface{}{
		"total_messages":  len(queue.items),
		"priority_counts": priorityCounts,
		"oldest_message":  getOldestMessage(queue.items),
	}
}

// getOldestMessage returns the timestamp of the oldest message
func getOldestMessage(items []*PriorityMessage) *time.Time {
	if len(items) == 0 {
		return nil
	}

	oldest := items[0].Timestamp
	for _, item := range items {
		if item.Timestamp.Before(oldest) {
			oldest = item.Timestamp
		}
	}

	return &oldest
}
