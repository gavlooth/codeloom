package httpclient

import (
	"container/list"
	"net/http"
	"sync"
	"time"
)

var (
	transportOnce   sync.Once
	sharedTransport *http.Transport
)

type clientEntry struct {
	client    *http.Client
	timestamp time.Time
	element   *list.Element
}

type clientCache struct {
	maxSize int
	ll      *list.List
	entries map[int64]*list.Element
	mu      sync.Mutex
}

var (
	// Global LRU cache for HTTP clients
	// maxSize is the maximum number of clients to cache
	// Default to 10, which covers most common timeout values
	cache *clientCache
)

func init() {
	cache = &clientCache{
		maxSize: 10,
		ll:      list.New(),
		entries: make(map[int64]*list.Element),
	}
}

// getSharedTransport returns a shared HTTP transport with connection pooling
func getSharedTransport() *http.Transport {
	transportOnce.Do(func() {
		sharedTransport = &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DisableKeepAlives:     false,
			ForceAttemptHTTP2:     false,
		}
	})
	return sharedTransport
}

// GetSharedClient returns a shared HTTP client with connection pooling
// suitable for making multiple requests to external APIs.
// Clients are cached per timeout value to balance connection pooling with timeout flexibility.
// Uses LRU eviction with a maximum cache size to prevent memory leaks.
func GetSharedClient(timeout time.Duration) *http.Client {
	// Use timeout as key; zero timeout means no timeout
	timeoutKey := timeout.Milliseconds()

	// Try to get from LRU cache first
	cache.mu.Lock()
	entry, exists := cache.entries[timeoutKey]
	if exists {
		// Move to front (most recently used)
		cache.ll.MoveToFront(entry)
		client := entry.Value.(*clientEntry).client
		cache.mu.Unlock()
		return client
	}
	cache.mu.Unlock()

	// Create new client
	client := &http.Client{
		Timeout:   timeout,
		Transport: getSharedTransport(),
	}

	newEntry := &clientEntry{
		client:    client,
		timestamp: time.Now(),
	}

	// Add to cache with LRU eviction
	cache.mu.Lock()

	// Check again in case another goroutine added it while we were unlocked
	if entry, exists := cache.entries[timeoutKey]; exists {
		cache.ll.MoveToFront(entry)
		cache.mu.Unlock()
		return entry.Value.(*clientEntry).client
	}

	// Create new list element
	elem := cache.ll.PushFront(newEntry)
	newEntry.element = elem
	cache.entries[timeoutKey] = elem

	// Evict oldest entries if we've exceeded max size
	for cache.ll.Len() > cache.maxSize {
		oldest := cache.ll.Back()
		if oldest != nil {
			// Remove from entries map (need to iterate to find key)
			for k, v := range cache.entries {
				if v == oldest {
					delete(cache.entries, k)
					break
				}
			}
			// Remove from list
			cache.ll.Remove(oldest)
		}
	}

	cache.mu.Unlock()

	return client
}

// ClearCache removes all cached HTTP clients
// Useful for testing or when you want to force reconnection
func ClearCache() {
	cache.mu.Lock()
	cache.ll.Init()
	cache.entries = make(map[int64]*list.Element)
	cache.mu.Unlock()
}

// CacheSize returns the current number of cached clients
// Useful for monitoring and debugging
func CacheSize() int {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.ll.Len()
}

// SetMaxCacheSize sets the maximum number of clients to cache
// This can be called to adjust cache size at runtime
// Smaller values use less memory but may reduce cache hit rate
func SetMaxCacheSize(size int) {
	if size < 1 {
		size = 1
	}
	cache.mu.Lock()
	cache.maxSize = size

	// Evict excess entries if needed
	for cache.ll.Len() > cache.maxSize {
		oldest := cache.ll.Back()
		if oldest != nil {
			for k, v := range cache.entries {
				if v == oldest {
					delete(cache.entries, k)
					break
				}
			}
			cache.ll.Remove(oldest)
		}
	}

	cache.mu.Unlock()
}
