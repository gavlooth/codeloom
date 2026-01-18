# HTTP Client Cache Memory Leak Fix

## Issue

**Location**: `internal/httpclient/client.go` (GetSharedClient function)

**Severity**: Medium - Memory Leak in Long-Running Processes

**Problem**: The HTTP client caching mechanism had unbounded memory growth. Clients were cached per timeout value but never cleaned up, causing memory leaks in long-running processes that use diverse timeout values.

### Example of the Problem

```go
// Before fix: Clients accumulated without limit
sharedClients = make(map[int64]*http.Client)  // No cleanup!

// If an application created clients with many different timeouts:
// 1000ms, 2000ms, 5000ms, 10000ms, 30000ms, etc.
// Each would create a new cached client, never to be removed.
```

With this behavior:
- ✗ Memory grows unbounded in long-running processes
- ✗ Connection pools are duplicated across clients
- ✗ No way to control memory usage
- ✗ In applications with dynamic timeout configuration, memory pressure increases over time

## Solution

Implemented LRU (Least Recently Used) cache with maximum size limits to prevent unbounded memory growth while maintaining connection pooling benefits.

### Key Changes

1. **LRU Cache Structure**:
   - Uses `container/list` doubly-linked list for O(1) LRU operations
   - Tracks access order to evict least recently used clients
   - Maintains O(1) lookup using map keyed by timeout value

2. **Maximum Cache Size**:
   - Default: 10 clients (covers most common timeout values)
   - Configurable via `SetMaxCacheSize()` function
   - Enforces minimum size of 1 to prevent cache being disabled

3. **Cache Management Functions**:
   - `ClearCache()`: Remove all cached clients (useful for testing)
   - `CacheSize()`: Get current number of cached clients (monitoring)
   - `SetMaxCacheSize()`: Adjust max size at runtime

4. **Concurrent-Safe Operations**:
   - Uses `sync.Mutex` to protect cache operations
   - Double-checked locking pattern for thread-safe lookups
   - Prevents race conditions during concurrent access

### Code Structure

```go
type clientCache struct {
    maxSize int           // Maximum number of clients to cache
    ll      *list.List  // Doubly-linked list for LRU tracking
    entries map[int64]*list.Element  // Map for O(1) lookup
    mu      sync.Mutex    // Protects cache operations
}
```

### Eviction Strategy

1. When requesting a client:
   - If client exists in cache → Move to front (most recently used)
   - If client doesn't exist → Create new client, add to front

2. When adding new client exceeds max size:
   - Evict oldest entry (back of list)
   - Remove from both list and map
   - Repeat until cache size <= max size

## Benefits

1. **Memory Bounded**: Cache size never exceeds configured maximum
2. **Performance Maintained**: Most recently used clients stay in cache
3. **Connection Pooling Preserved**: Shared transport still used across all clients
4. **Configurable**: Can adjust cache size based on application needs
5. **Monitoring Ready**: `CacheSize()` allows observation of cache behavior

## Configuration

### Default Behavior
- Max cache size: 10 clients
- Eviction: LRU (Least Recently Used)
- Automatic: No manual management required

### Adjusting Cache Size

```go
// Increase cache size for applications with many diverse timeouts
httpclient.SetMaxCacheSize(50)

// Decrease cache size for memory-constrained environments
httpclient.SetMaxCacheSize(5)

// Clear all cached clients (e.g., force new connections)
httpclient.ClearCache()
```

### Recommended Values

| Use Case | Recommended Size | Rationale |
|-----------|------------------|-------------|
| Standard LLM/Embedding clients | 5-10 | Few timeout values (30s, 60s, 120s) |
| Dynamic timeout configuration | 20-50 | Diverse timeouts from user settings |
| Memory-constrained environments | 3-5 | Prioritize memory over cache hits |
| High-throughput services | 50-100 | Many timeout values, prioritize performance |

## Testing

Added comprehensive unit tests (`internal/httpclient/client_test.go`):

1. **TestCacheLRUEviction**: Verifies LRU eviction works correctly
2. **TestCacheConcurrency**: Tests concurrent access safety
3. **TestCacheSizeEviction**: Validates size limits are enforced
4. **TestCacheSameTimeout**: Ensures identical timeouts reuse same client
5. **TestClearCache**: Tests cache clearing functionality
6. **TestSetMaxCacheSize**: Validates dynamic size adjustment

Run tests:
```bash
go test ./internal/httpclient -v
```

## Verification

Created demonstration script (`verify_http_cache_fix.go`) that shows:

- ✓ Cache size stays bounded regardless of how many clients created
- ✓ LRU eviction removes oldest clients when cache is full
- ✓ Same timeout values reuse cached clients
- ✓ Concurrent access doesn't cause race conditions
- ✓ Dynamic cache size adjustment works correctly

Run verification:
```bash
go run verify_http_cache_fix.go
```

## Impact Analysis

### Before Fix
- ❌ Unbounded memory growth
- ❌ Duplicate connection pools for same hosts
- ❌ No way to control memory usage
- ❌ Long-running processes would eventually exhaust memory

### After Fix
- ✅ Cache size bounded to configurable maximum
- ✅ Shared transport maintains connection pooling benefits
- ✅ LRU eviction keeps most-used clients cached
- ✅ Memory usage predictable and controllable
- ✅ Safe for long-running processes

## Migration Guide

### For Existing Users

No changes required! The fix is backward compatible and maintains default behavior for most applications.

To customize:
```go
// In application startup or config loading
import "github.com/heefoo/codeloom/internal/httpclient"

// Adjust based on your needs
httpclient.SetMaxCacheSize(20)
```

### Monitoring

Add to monitoring dashboard:
```go
import (
    "github.com/heefoo/codeloom/internal/httpclient"
    "runtime/metrics"
)

// Report cache size regularly
func reportCacheMetrics() {
    size := httpclient.CacheSize()
    // Send to metrics system
}
```

## Tradeoffs and Alternatives

### Alternatives Considered

1. **Time-based expiration (TTL)**
   - Pros: Automatically removes old clients
   - Cons: Adds complexity; race conditions with in-flight requests
   - Decision: LRU with size limit is simpler and predictable

2. **Disable caching entirely**
   - Pros: No memory leak concerns
   - Cons: Loses connection pooling benefits
   - Decision: Caching provides real performance benefits

3. **Unbounded cache**
   - Pros: Maximum cache hit rate
   - Cons: Memory leaks in long-running processes
   - Decision: Memory safety is more important

### Tradeoffs of Chosen Solution

**Advantages**:
- Simple, predictable implementation
- Bounded memory usage
- Maintains connection pooling
- Configurable for different needs
- Low overhead (O(1) operations)

**Limitations**:
- Maximum cache size must be pre-configured
- Least recently used client might still be needed
- Adds some complexity to client lookup

## Related Code

This fix affects all code that uses `httpclient.GetSharedClient()`:
- `internal/llm/openai.go`: LLM clients
- `internal/llm/ollama.go`: Ollama LLM clients
- `internal/embedding/openai.go`: OpenAI embeddings
- `internal/embedding/ollama.go`: Ollama embeddings

All these providers continue to work without modification, benefiting from the bounded cache automatically.

## Conclusion

This fix addresses a genuine memory leak in long-running Go processes by implementing an LRU cache with size limits. The solution maintains the performance benefits of connection pooling while preventing unbounded memory growth. It's fully backward compatible, well-tested, and configurable for different application needs.
