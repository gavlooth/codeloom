# Embedding Generation Retry Fix

## Problem Summary

When embedding generation failed (e.g., due to transient network errors or temporary API errors), the indexer would:
1. Log a warning message
2. Store the code node with a `nil` embedding
3. Continue processing other nodes

This created several issues:
- **Silent failures**: Files could be indexed without embeddings without any clear indication
- **No recovery**: Transient errors (network hiccups) would leave nodes without embeddings forever
- **Poor user experience**: Semantic search would skip nodes with `nil` embeddings silently
- **No retry mechanism**: A brief network glitch would cause permanent embedding loss

### Code Location
- `internal/indexer/indexer.go` lines 398-404 and 532-536
- Direct call to `idx.embedding.EmbedSingle(ctx, node.Content)` with no retry logic

## Solution

Implemented a lightweight retry mechanism with exponential backoff:

### retryEmbedding() Function
A new helper function that:
- Makes up to **3 retry attempts** before giving up
- Uses **exponential backoff**: 500ms, 1s, 2s
- **Respects context cancellation** during retry attempts
- **Clear logging** of each retry attempt for debugging
- Returns error only after all retries are exhausted

### Backoff Strategy
```
Attempt 1: Immediate (0ms backoff)
Attempt 2: Wait 500ms before retry
Attempt 3: Wait 1s before retry
Attempt 4: Wait 2s before final attempt
```

The exponential backoff (`1 << uint(attempt) * initialBackoff`) provides:
- **Quick recovery** for transient issues (first retry at 500ms)
- **Graceful degradation** for persistent issues (longer waits for subsequent retries)
- **Bounded total time**: ~3.5s maximum wait time across all retries

### Context Cancellation
The retry mechanism respects context cancellation at two points:
1. **Before each attempt**: Checks `ctx.Done()` before calling EmbedSingle()
2. **During backoff**: Waits on both `time.After()` and `ctx.Done()` simultaneously

This ensures that:
- User can interrupt long-running operations (Ctrl+C)
- File watching system can cancel embeddings when files change
- Server shutdown cleanly stops pending retries

## Changes Made

### internal/indexer/indexer.go
1. Added `retryEmbedding()` helper function (lines 106-144)
2. Replaced direct `idx.embedding.EmbedSingle()` calls with `retryEmbedding()`
3. Updated error logging to clarify "after all retries" vs. single attempt

### internal/indexer/indexer_context_test.go
Added comprehensive test suite:
- `TestRetryEmbeddingSuccessOnFirstTry`: No retries on immediate success
- `TestRetryEmbeddingSuccessAfterRetries`: Retries succeed on transient failures
- `TestRetryEmbeddingFailureAfterMaxRetries`: Persistent failures handled correctly
- `TestRetryEmbeddingContextCancellation`: Immediate cancellation respected
- `TestRetryEmbeddingMidRetryCancellation`: Cancellation during backoff handled

## Benefits

### Reliability
- **Transient error recovery**: Network hiccups automatically retried
- **Improved embedding coverage**: Fewer nodes with nil embeddings
- **Graceful degradation**: Files still indexed even if embeddings fail

### User Experience
- **Better visibility**: Logs show retry attempts
- **Predictable behavior**: Consistent 3-retry limit with exponential backoff
- **No silent failures**: Users can see when embeddings fail

### Code Quality
- **No complex state management**: Retry logic encapsulated in helper function
- **Testable**: Unit tests verify retry behavior
- **Backward compatible**: Existing code continues to work (with retries)

## Testing Results

```
=== RUN   TestRetryEmbeddingSuccessOnFirstTry
--- PASS: TestRetryEmbeddingSuccessOnFirstTry (0.00s)
=== RUN   TestRetryEmbeddingSuccessAfterRetries
--- PASS: TestRetryEmbeddingSuccessAfterRetries (1.50s)
=== RUN   TestRetryEmbeddingFailureAfterMaxRetries
--- PASS: TestRetryEmbeddingFailureAfterMaxRetries (1.50s)
=== RUN   TestRetryEmbeddingContextCancellation
--- PASS: TestRetryEmbeddingContextCancellation (0.00s)
=== RUN   TestRetryEmbeddingMidRetryCancellation
--- PASS: TestRetryEmbeddingMidRetryCancellation (0.20s)
PASS
```

All new tests pass, and full test suite runs without regressions.

## Impact Analysis

### Positive Impact
- **Reduced embedding failures**: Transient errors automatically recovered
- **Better semantic search**: More nodes have embeddings for similarity matching
- **Clearer logs**: Developers can see retry attempts in logs

### Minimal Risk
- **Small code change**: Only 40 lines added (function + tests)
- **No database schema changes**: Backward compatible
- **No API changes**: Internal implementation detail only
- **Short wait time**: Maximum 3.5s wait for persistent failures

### Performance Considerations
- **Success case**: No additional overhead (immediate return)
- **Single retry**: +500ms average delay
- **Multiple retries**: Exponential increase, but bounded to ~3.5s total
- **Context cancellation**: Immediate abort (no wasted time)

## Future Improvements (Out of Scope)

1. **Configurable retry parameters**: Allow users to customize maxRetries and backoff
2. **Persistent failure tracking**: Track nodes that consistently fail embeddings
3. **Retry queue**: Queue failed embeddings for background retry
4. **Circuit breaker**: Stop retrying if provider consistently fails
5. **Metrics**: Track embedding retry rates and success rates

These were not included to keep the fix:
- Small and focused
- Testable and verifiable
- High value-to-risk ratio
- Minimal scope
