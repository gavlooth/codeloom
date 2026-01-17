package httpclient

import (
	"net/http"
	"sync"
	"time"
)

var (
	clientsMutex sync.RWMutex
	sharedClients = make(map[int64]*http.Client)
	transportOnce sync.Once
	sharedTransport *http.Transport
)

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
func GetSharedClient(timeout time.Duration) *http.Client {
	// Use timeout as key; zero timeout means no timeout
	timeoutKey := timeout.Milliseconds()

	// Fast path: read lock to check cache
	clientsMutex.RLock()
	client, exists := sharedClients[timeoutKey]
	clientsMutex.RUnlock()

	if exists {
		return client
	}

	// Need to create a new client, acquire write lock
	clientsMutex.Lock()
	defer clientsMutex.Unlock()

	// Double-check in case another goroutine created the client while we waited
	if client, exists := sharedClients[timeoutKey]; exists {
		return client
	}

	// Create and cache new client
	client = &http.Client{
		Timeout:   timeout,
		Transport: getSharedTransport(),
	}
	sharedClients[timeoutKey] = client
	return client
}
