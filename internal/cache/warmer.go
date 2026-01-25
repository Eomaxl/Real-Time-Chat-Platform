package cache

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Warmer handles cache warming and preloading
type Warmer struct {
	cache    *MultiLevelCache
	tasks    map[string]*WarmingTask
	mu       sync.RWMutex
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// WarmingTask represents a cache warming task
type WarmingTask struct {
	Key      string
	Loader   func(ctx context.Context) (interface{}, error)
	Interval time.Duration
	LastRun  time.Time
}

// NewWarmer creates a new cache warmer
func NewWarmer(cache *MultiLevelCache) *Warmer {
	return &Warmer{
		cache:    cache,
		tasks:    make(map[string]*WarmingTask),
		stopChan: make(chan struct{}),
	}
}

// RegisterTask registers a cache warming task
func (w *Warmer) RegisterTask(key string, loader func(ctx context.Context) (interface{}, error), interval time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.tasks[key] = &WarmingTask{
		Key:      key,
		Loader:   loader,
		Interval: interval,
		LastRun:  time.Time{},
	}
}

// Start begins the cache warming process
func (w *Warmer) Start(ctx context.Context) {
	w.wg.Add(1)
	go w.run(ctx)
}

// Stop stops the cache warming process
func (w *Warmer) Stop() {
	close(w.stopChan)
	w.wg.Wait()
}

// run executes the cache warming loop
func (w *Warmer) run(ctx context.Context) {
	defer w.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Run initial warming
	w.warmAll(ctx)

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			w.warmAll(ctx)
		}
	}
}

// warmAll warms all registered tasks that are due
func (w *Warmer) warmAll(ctx context.Context) {
	w.mu.RLock()
	tasks := make([]*WarmingTask, 0, len(w.tasks))
	for _, task := range w.tasks {
		tasks = append(tasks, task)
	}
	w.mu.RUnlock()

	now := time.Now()
	for _, task := range tasks {
		if task.LastRun.IsZero() || now.Sub(task.LastRun) >= task.Interval {
			if err := w.warmTask(ctx, task); err != nil {
				log.Printf("Failed to warm cache for key %s: %v", task.Key, err)
			}
		}
	}
}

// warmTask warms a single cache task
func (w *Warmer) warmTask(ctx context.Context, task *WarmingTask) error {
	// Load data
	value, err := task.Loader(ctx)
	if err != nil {
		return fmt.Errorf("failed to load data: %w", err)
	}

	// Store in cache
	if err := w.cache.Set(ctx, task.Key, value); err != nil {
		return fmt.Errorf("failed to cache data: %w", err)
	}

	// Update last run time
	w.mu.Lock()
	task.LastRun = time.Now()
	w.mu.Unlock()

	return nil
}

// WarmNow immediately warms a specific key
func (w *Warmer) WarmNow(ctx context.Context, key string) error {
	w.mu.RLock()
	task, ok := w.tasks[key]
	w.mu.RUnlock()

	if !ok {
		return fmt.Errorf("warming task not found for key: %s", key)
	}

	return w.warmTask(ctx, task)
}

// PreloadBatch preloads multiple keys in parallel
func (w *Warmer) PreloadBatch(ctx context.Context, keys []string, loader func(ctx context.Context, key string) (interface{}, error)) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(keys))

	for _, key := range keys {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()

			// Load data
			value, err := loader(ctx, k)
			if err != nil {
				errChan <- fmt.Errorf("failed to load key %s: %w", k, err)
				return
			}

			// Store in cache
			if err := w.cache.Set(ctx, k, value); err != nil {
				errChan <- fmt.Errorf("failed to cache key %s: %w", k, err)
			}
		}(key)
	}

	wg.Wait()
	close(errChan)

	// Check for errors
	if len(errChan) > 0 {
		return <-errChan
	}

	return nil
}
