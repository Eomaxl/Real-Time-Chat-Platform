package cache

import (
	"container/list"
	"sync"
	"time"
)

// LRUCache implements a thread-safe LRU (Least Recently Used) cache
type LRUCache struct {
	capacity  int
	items     map[string]*list.Element
	evictList *list.List
	mu        sync.RWMutex

	// Statistics
	hits   uint64
	misses uint64
}

// entry represents a cache entry with value and metadata
type entry struct {
	key       string
	value     interface{}
	expiresAt time.Time
}

// NewLRUCache creates a new LRU cache with the specified capacity
func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 1000 // Default capacity
	}

	return &LRUCache{
		capacity:  capacity,
		items:     make(map[string]*list.Element),
		evictList: list.New(),
	}
}

// Get retrieves a value from the cache
func (c *LRUCache) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		c.misses++
		return nil, false
	}

	// Check if entry has expired
	ent := elem.Value.(*entry)
	if !ent.expiresAt.IsZero() && time.Now().After(ent.expiresAt) {
		c.removeElement(elem)
		c.misses++
		return nil, false
	}

	// Move to front (most recently used)
	c.evictList.MoveToFront(elem)
	c.hits++

	return ent.value, true
}

// Set adds or updates a value in the cache with optional TTL
func (c *LRUCache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if key already exists
	if elem, ok := c.items[key]; ok {
		// Update existing entry
		c.evictList.MoveToFront(elem)
		ent := elem.Value.(*entry)
		ent.value = value
		if ttl > 0 {
			ent.expiresAt = time.Now().Add(ttl)
		} else {
			ent.expiresAt = time.Time{}
		}
		return
	}

	// Create new entry
	ent := &entry{
		key:   key,
		value: value,
	}
	if ttl > 0 {
		ent.expiresAt = time.Now().Add(ttl)
	}

	// Add to front of list
	elem := c.evictList.PushFront(ent)
	c.items[key] = elem

	// Evict oldest if over capacity
	if c.evictList.Len() > c.capacity {
		c.removeOldest()
	}
}

// Delete removes a key from the cache
func (c *LRUCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.removeElement(elem)
	}
}

// Clear removes all entries from the cache
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element)
	c.evictList.Init()
}

// Len returns the number of items in the cache
func (c *LRUCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.evictList.Len()
}

// Stats returns cache statistics
func (c *LRUCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}

	return CacheStats{
		Hits:     c.hits,
		Misses:   c.misses,
		HitRate:  hitRate,
		Size:     c.evictList.Len(),
		Capacity: c.capacity,
	}
}

// removeOldest removes the oldest entry from the cache
func (c *LRUCache) removeOldest() {
	elem := c.evictList.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

// removeElement removes a specific element from the cache
func (c *LRUCache) removeElement(elem *list.Element) {
	c.evictList.Remove(elem)
	ent := elem.Value.(*entry)
	delete(c.items, ent.key)
}

// CleanupExpired removes all expired entries from the cache
func (c *LRUCache) CleanupExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	removed := 0

	// Iterate through all entries and remove expired ones
	for key, elem := range c.items {
		ent := elem.Value.(*entry)
		if !ent.expiresAt.IsZero() && now.After(ent.expiresAt) {
			c.evictList.Remove(elem)
			delete(c.items, key)
			removed++
		}
	}

	return removed
}

// CacheStats represents cache statistics
type CacheStats struct {
	Hits     uint64
	Misses   uint64
	HitRate  float64
	Size     int
	Capacity int
}
