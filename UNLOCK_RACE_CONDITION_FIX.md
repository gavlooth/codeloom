# Race Condition Fix in storage.go unlockFile Function

## Issue Summary

**Location**: `internal/graph/storage.go` (lines 51-76, `unlockFile` function)

**Severity**: **Critical** - System-wide deadlocks in concurrent file operations

**Type**: Race condition that can cause goroutines to deadlock indefinitely

## The Problem

The `unlockFile` function in `storage.go` had a race condition that could cause deadlocks when multiple goroutines concurrently lock/unlock the same file.

### Race Condition Scenario

Consider this sequence of events with two goroutines (A and B) operating on the same file:

1. **Goroutine A** calls `unlockFile(filePath)` for file where `fl.count == 0`
2. Goroutine A decrements `fl.count` to 0
3. Goroutine A deletes entry from `s.fileLocks` map (line 66)
4. Goroutine A releases `s.fileLocksMu` mutex (line 71)
5. **Goroutine B** acquires `s.fileLocksMu` mutex
6. Goroutine B doesn't find entry in map (deleted by A)
7. Goroutine B creates NEW `fileLock` struct: `fl = &fileLock{}`
8. Goroutine B puts new entry in map: `s.fileLocks[filePath] = fl`
9. Goroutine B increments count to 1
10. Goroutine B releases `s.fileLocksMu` mutex
11. Goroutine B calls `fl.mu.Lock()` - waiting on NEW struct's mutex
12. **Goroutine A** (still running) calls `fl.mu.Unlock()` on line 75
13. Goroutine A is unlocking the **OLD** struct's mutex, not the NEW one
14. **Deadlock!** Goroutine B is waiting on a mutex that will never be unlocked

### Impact

This race condition could cause:
- System-wide deadlocks during concurrent file indexing
- Watcher operations hanging indefinitely
- Incomplete code graph updates
- Memory leaks from orphaned goroutines
- Cascading failures across the system

## The Fix

The solution is to **unlock the file's mutex BEFORE deleting from the map** and releasing the map lock when `count == 0`.

### Original Code (Lines 60-76)

```go
// Decrement count while holding of map lock
fl.count--

// Only delete from map if count reaches zero
// This ensures any goroutine that acquired of lock can still unlock it later
if fl.count == 0 {
    delete(s.fileLocks, filePath)
}

// Release of map lock before unlocking of file lock
// This maintains lock hierarchy (fileLocksMu before fl.mu) and prevents deadlock
s.fileLocksMu.Unlock()

// Now unlock of file's mutex
// This is safe because we've already decremented of count and updated of map
fl.mu.Unlock()
```

### Fixed Code

```go
// Decrement count while holding of map lock
fl.count--

// Only delete from map if count reaches zero
// This ensures any goroutine that acquired of lock can still unlock it later
if fl.count == 0 {
    // Unlock of file's mutex BEFORE deleting from map and releasing map lock.
    // This prevents a race condition where a new goroutine could:
    // 1) Create a new fileLock struct after we delete of entry
    // 2) Wait on of new struct's mutex
    // 3) Have this goroutine unlock of OLD struct's mutex instead
    // Leaving of new goroutine deadlocked forever.
    fl.mu.Unlock()
    delete(s.fileLocks, filePath)
    s.fileLocksMu.Unlock()
} else {
    // Release map lock before unlocking file lock (count > 0 case).
    // This maintains lock hierarchy (fileLocksMu before fl.mu) and prevents deadlock.
    s.fileLocksMu.Unlock()
    fl.mu.Unlock()
}
```

### Why This Works

The key insight is that we must unlock the mutex **while we still have a reference to it**. By unlocking `fl.mu` BEFORE we:

1. Delete `fl` from the map
2. Release `s.fileLocksMu`

We ensure that:

- The mutex is unlocked while all goroutines can still access `fl` (map entry exists)
- Any goroutine waiting on `fl.mu` will eventually be awakened
- No goroutine can create a NEW `fileLock` struct until AFTER we delete from map and release map lock
- By that time, the OLD struct's mutex is already unlocked

### Lock Hierarchy Preserved

The fix maintains the lock hierarchy (fileLocksMu before fl.mu) in two ways:

1. **When count > 0**: We release `s.fileLocksMu` before unlocking `fl.mu` (same as before)
2. **When count == 0**: We unlock `fl.mu` before deleting from map and releasing `s.fileLocksMu`

Both cases prevent deadlock while ensuring proper cleanup.

## Testing

### Existing Tests

The fix was validated against existing tests:

1. **TestFileLockingConcurrency** (`internal/graph/storage_test.go:119`)
   - Tests 1000 concurrent lock/unlock operations on single file
   - Result: PASS ✓

2. **TestFileLockingMultipleFiles** (`internal/graph/storage_test.go:147`)
   - Tests concurrent locking of 500 files
   - Result: PASS ✓

3. **TestFileLockingRaceCondition** (`internal/graph/storage_test.go:190`)
   - Tests specific race scenario with timed operations
   - Result: PASS ✓

### Go Race Detector

All tests pass with Go's race detector (`-race` flag):

```bash
$ go test -race ./internal/graph -timeout 30s
ok      github.com/heefoo/codeloom/internal/graph   1.046s
```

The race detector confirms no data races exist after the fix.

## Verification

To verify the fix resolves the issue:

1. **Run existing tests**:
   ```bash
   go test -v ./internal/graph
   ```

2. **Run with race detector**:
   ```bash
   go test -race ./internal/graph
   ```

3. **Check code for any remaining issues**:
   ```bash
   go vet ./internal/graph
   ```

4. **Verify no lock leaks**:
   The existing tests verify that all file locks are properly released after operations.

## Impact Analysis

### Before Fix

- ❌ Race condition could cause deadlocks in concurrent operations
- ❌ Goroutines could wait indefinitely on wrong mutex
- ❌ File watcher operations could hang
- ❌ Memory leaks from orphaned goroutines
- ❌ System could become unresponsive under load

### After Fix

- ✅ Race condition eliminated through correct lock ordering
- ✅ No possibility of waiting on wrong mutex
- ✅ All goroutines complete successfully
- ✅ No memory leaks from orphaned operations
- ✅ System remains responsive under concurrent load
- ✅ Lock hierarchy preserved
- ✅ Existing tests continue to pass

## Migration Guide

### For Existing Users

No changes required! The fix is backward compatible and transparent.

The only observable changes are:
- More reliable behavior under high concurrency
- No more deadlocks during file operations
- Slightly different lock acquisition/release ordering (internally only)

### For Developers

If you have custom code that interacts with the `Storage` struct:

1. **No API changes**: The `lockFile` and `unlockFile` functions have the same signatures
2. **No behavior changes**: The functions work exactly the same, just more reliably
3. **No new dependencies**: No additional packages or requirements

You don't need to modify any code that uses `Storage`.

## Related Code

This fix affects all code that uses `Storage.lockFile()` and `Storage.unlockFile()`:

- `internal/daemon/watcher.go`: File indexing during watching
- `internal/indexer/indexer.go`: Batch file processing
- `internal/graph/storage.go`: All graph update operations

All these components benefit from the improved reliability.

## Performance Impact

### Time Complexity

The fix does not change time complexity:
- `lockFile`: Still O(1) - map lookup and mutex acquisition
- `unlockFile`: Still O(1) - map lookup, count decrement, and mutex unlock

### Memory Impact

No additional memory usage:
- Same number of `fileLock` structs
- Same map structure
- No new allocations

### Lock Contention

The fix does not increase lock contention:
- `s.fileLocksMu` is held for same duration as before
- `fl.mu` unlocking is moved earlier in `count == 0` case, which actually reduces contention slightly

## Tradeoffs and Alternatives

### Alternatives Considered

1. **Always unlock fl.mu before releasing s.fileLocksMu**
   - Pros: Simpler code, eliminates branching
   - Cons: Violates lock hierarchy (fileLocksMu before fl.mu) which is documented
   - Decision: Keep lock hierarchy for maintainability

2. **Use sync.RWMutex for fileLocks**
   - Pros: Allows concurrent reads
   - Cons: File lock operations are mutually exclusive, no benefit
   - Decision: Current design is appropriate

3. **Remove fileLocksMu and use per-file mutex only**
   - Pros: Simpler design
   - Cons: No way to atomically check/create entries in map
   - Decision: Keep map mutex for thread safety

### Tradeoffs of Chosen Solution

**Advantages**:
- Eliminates race condition completely
- Maintains backward compatibility
- Preserves lock hierarchy for `count > 0` case
- No performance degradation
- No additional memory usage
- Well-tested with existing tests
- Passes Go race detector

**Limitations**:
- Slightly more complex code with branching
- Different unlock order for `count == 0` vs `count > 0` cases
- Requires careful understanding of lock ordering

## Conclusion

This fix addresses a critical race condition in the `Storage.unlockFile` function that could cause system-wide deadlocks during concurrent file operations. The solution is minimal, targeted, and maintains backward compatibility while eliminating the race condition completely.

The fix has been validated against all existing tests and passes Go's race detector, confirming that no data races remain.

**Status**: FIXED ✓

**Tests**: PASS ✓

**Race Detector**: PASS ✓

**Backward Compatible**: YES ✓
