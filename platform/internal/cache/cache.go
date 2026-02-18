// Package cache provides a generic in-memory TTL cache for ratd.
// Used to reduce redundant Postgres queries for slow-changing data like
// namespace lists and pipeline metadata. Thread-safe via sync.RWMutex.
//
// Not intended for run data (changes frequently) or file content (too large).
package cache

import (
	"sync"
	"time"
)

// DefaultTTL is the default time-to-live for cache entries (30 seconds).
const DefaultTTL = 30 * time.Second

// DefaultMaxEntries is the default maximum number of cache entries.
const DefaultMaxEntries = 1000

// Options configures a Cache instance.
type Options struct {
	// TTL is the time-to-live for each entry. Zero uses DefaultTTL (30s).
	TTL time.Duration

	// MaxEntries is the maximum number of entries before eviction. Zero uses DefaultMaxEntries (1000).
	MaxEntries int
}

// entry holds a cached value and its expiration time.
type entry[V any] struct {
	value     V
	expiresAt time.Time
}

// Cache is a generic in-memory cache with TTL expiration and max-entries eviction.
// Keys must be comparable; values can be any type.
//
// Eviction policy: when max entries is reached, expired entries are cleaned first.
// If still at capacity, the oldest entry by insertion order is evicted.
type Cache[K comparable, V any] struct {
	mu         sync.RWMutex
	entries    map[K]entry[V]
	order      []K // insertion order for eviction
	ttl        time.Duration
	maxEntries int
}

// New creates a new Cache with the given options.
// Zero-value TTL defaults to 30s; zero-value MaxEntries defaults to 1000.
func New[K comparable, V any](opts Options) *Cache[K, V] {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	maxEntries := opts.MaxEntries
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	return &Cache[K, V]{
		entries:    make(map[K]entry[V]),
		order:      make([]K, 0),
		ttl:        ttl,
		maxEntries: maxEntries,
	}
}

// Get retrieves a value by key. Returns the value and true if found and not expired.
// Returns the zero value and false if the key is missing or expired.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		var zero V
		return zero, false
	}

	if time.Now().After(e.expiresAt) {
		// Entry expired — remove it lazily.
		c.mu.Lock()
		c.removeLocked(key)
		c.mu.Unlock()
		var zero V
		return zero, false
	}

	return e.value, true
}

// Set adds or updates a cache entry. If the cache is at capacity, it first
// cleans expired entries, then evicts the oldest entry if still full.
func (c *Cache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// If key already exists, update in place (don't change order).
	if _, exists := c.entries[key]; exists {
		c.entries[key] = entry[V]{
			value:     value,
			expiresAt: time.Now().Add(c.ttl),
		}
		return
	}

	// Ensure capacity: clean expired first, then evict oldest if needed.
	if len(c.entries) >= c.maxEntries {
		c.cleanExpiredLocked()
	}
	if len(c.entries) >= c.maxEntries {
		c.evictOldestLocked()
	}

	c.entries[key] = entry[V]{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.order = append(c.order, key)
}

// Delete removes a single entry by key. No-op if the key doesn't exist.
func (c *Cache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.removeLocked(key)
}

// Clear removes all entries from the cache.
func (c *Cache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[K]entry[V])
	c.order = c.order[:0]
}

// Len returns the number of entries currently in the cache (including expired but not yet cleaned).
func (c *Cache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// TTL returns the configured time-to-live for cache entries.
func (c *Cache[K, V]) TTL() time.Duration {
	return c.ttl
}

// MaxEntries returns the configured maximum number of entries.
func (c *Cache[K, V]) MaxEntries() int {
	return c.maxEntries
}

// removeLocked removes a key from both the map and the order slice.
// Caller must hold c.mu (write lock).
func (c *Cache[K, V]) removeLocked(key K) {
	if _, ok := c.entries[key]; !ok {
		return
	}
	delete(c.entries, key)
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// cleanExpiredLocked removes all expired entries.
// Caller must hold c.mu (write lock).
func (c *Cache[K, V]) cleanExpiredLocked() {
	now := time.Now()
	var remaining []K
	for _, k := range c.order {
		if e, ok := c.entries[k]; ok && now.After(e.expiresAt) {
			delete(c.entries, k)
		} else {
			remaining = append(remaining, k)
		}
	}
	c.order = remaining
}

// evictOldestLocked removes the oldest entry by insertion order.
// Caller must hold c.mu (write lock).
func (c *Cache[K, V]) evictOldestLocked() {
	if len(c.order) == 0 {
		return
	}
	oldest := c.order[0]
	c.order = c.order[1:]
	delete(c.entries, oldest)
}
