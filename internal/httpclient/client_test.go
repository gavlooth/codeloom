package httpclient

import (
	"testing"
	"time"
)

func TestGetSharedClient(t *testing.T) {
	// Test that timeout is consistently applied
	client1 := GetSharedClient(30 * time.Second)
	if client1.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", client1.Timeout)
	}

	// Test that zero timeout (no timeout) is properly handled
	client2 := GetSharedClient(0)
	if client2.Timeout != 0 {
		t.Errorf("Expected timeout 0 (no timeout), got %v", client2.Timeout)
	}

	// Test that different timeouts work correctly
	client3 := GetSharedClient(120 * time.Second)
	if client3.Timeout != 120*time.Second {
		t.Errorf("Expected timeout 120s, got %v", client3.Timeout)
	}

	// Verify all clients share the same transport
	if client1.Transport != client2.Transport {
		t.Error("Expected all clients to share the same transport")
	}
	if client1.Transport != client3.Transport {
		t.Error("Expected all clients to share the same transport")
	}

	// Test concurrent access
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			client := GetSharedClient(time.Duration(i+1) * time.Second)
			if client.Transport == nil {
				t.Error("Expected non-nil transport")
			}
			done <- true
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestCacheLRUEviction(t *testing.T) {
	// Clear cache before test
	ClearCache()

	// Set a small max size for testing
	SetMaxCacheSize(3)

	// Create clients with 5 different timeouts
	timeout1 := 10 * time.Second
	timeout2 := 20 * time.Second
	timeout3 := 30 * time.Second
	timeout4 := 40 * time.Second
	timeout5 := 50 * time.Second

	GetSharedClient(timeout1)
	GetSharedClient(timeout2)
	GetSharedClient(timeout3)

	// Should have 3 clients
	if CacheSize() != 3 {
		t.Errorf("Expected cache size 3, got %d", CacheSize())
	}

	// Adding 4th client should evict oldest (timeout1)
	GetSharedClient(timeout4)
	if CacheSize() != 3 {
		t.Errorf("Expected cache size 3 after eviction, got %d", CacheSize())
	}

	// Adding 5th client should evict oldest (timeout2)
	GetSharedClient(timeout5)
	if CacheSize() != 3 {
		t.Errorf("Expected cache size 3 after eviction, got %d", CacheSize())
	}

	// Clean up
	ClearCache()
}

func TestCacheConcurrency(t *testing.T) {
	// Clear cache before test
	ClearCache()

	// Set reasonable cache size
	SetMaxCacheSize(20)

	// Concurrently create many clients with different timeouts
	done := make(chan bool)
	for i := 0; i < 50; i++ {
		go func(idx int) {
			timeout := time.Duration(idx+1) * time.Second
			client := GetSharedClient(timeout)
			if client == nil {
				t.Errorf("Expected non-nil client for timeout %v", timeout)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 50; i++ {
		<-done
	}

	// Verify cache size doesn't exceed max
	size := CacheSize()
	if size > 20 {
		t.Errorf("Expected cache size <= 20, got %d", size)
	}

	// Clean up
	ClearCache()
}

func TestCacheSizeEviction(t *testing.T) {
	// Clear cache before test
	ClearCache()

	// Test different cache sizes
	for maxSize := 1; maxSize <= 5; maxSize++ {
		ClearCache()

		SetMaxCacheSize(maxSize)

		// Create more clients than max size
		for i := 0; i < maxSize+3; i++ {
			GetSharedClient(time.Duration(i+1) * time.Second)
		}

		// Verify cache size doesn't exceed max
		size := CacheSize()
		if size > maxSize {
			t.Errorf("For max size %d, expected cache size <= %d, got %d", maxSize, maxSize, size)
		}
	}

	// Clean up
	ClearCache()
}

func TestCacheSameTimeout(t *testing.T) {
	// Clear cache before test
	ClearCache()

	SetMaxCacheSize(5)

	timeout := 30 * time.Second

	// Create multiple clients with same timeout
	client1 := GetSharedClient(timeout)
	client2 := GetSharedClient(timeout)
	client3 := GetSharedClient(timeout)

	// Should all be the same client instance
	if client1 != client2 || client2 != client3 {
		t.Error("Expected same client instance for identical timeout")
	}

	// Should only have 1 entry in cache
	if CacheSize() != 1 {
		t.Errorf("Expected cache size 1 for single timeout, got %d", CacheSize())
	}

	// Clean up
	ClearCache()
}

func TestClearCache(t *testing.T) {
	// Clear cache before test
	ClearCache()

	SetMaxCacheSize(10)

	// Create some clients
	GetSharedClient(10 * time.Second)
	GetSharedClient(20 * time.Second)
	GetSharedClient(30 * time.Second)

	if CacheSize() != 3 {
		t.Errorf("Expected cache size 3, got %d", CacheSize())
	}

	// Clear cache
	ClearCache()

	// Should be empty
	if CacheSize() != 0 {
		t.Errorf("Expected cache size 0 after clear, got %d", CacheSize())
	}
}

func TestSetMaxCacheSize(t *testing.T) {
	// Clear cache before test
	ClearCache()

	// Set initial size
	SetMaxCacheSize(5)
	if CacheSize() != 0 {
		t.Errorf("Expected empty cache, got size %d", CacheSize())
	}

	// Create some clients
	GetSharedClient(10 * time.Second)
	GetSharedClient(20 * time.Second)
	GetSharedClient(30 * time.Second)

	if CacheSize() != 3 {
		t.Errorf("Expected cache size 3, got %d", CacheSize())
	}

	// Reduce max size below current count
	SetMaxCacheSize(2)

	// Should evict down to max size
	if CacheSize() != 2 {
		t.Errorf("Expected cache size 2 after reduction, got %d", CacheSize())
	}

	// Test minimum size enforcement
	SetMaxCacheSize(0)
	if CacheSize() != 1 {
		t.Errorf("Expected cache size 1 (min allowed), got %d", CacheSize())
	}

	// Clean up
	ClearCache()
}
