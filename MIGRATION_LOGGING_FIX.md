# Migration Error Logging Fix

## Issue

**Location**: `internal/graph/storage.go:793-850` (RunMigrations function)

**Problem**: All migration errors were silently suppressed. The comment suggested this handles "already exists" errors gracefully, but this also masked legitimate failures like:
- Connection issues to SurrealDB
- Permission problems
- Invalid schema definitions
- Network timeouts
- Missing required tables after expected to be created

## Example of the Problem

```go
// Before fix: All errors silently ignored
for _, m := range migrations {
    if _, err := surrealdb.Query[any](ctx, s.db, m, nil); err != nil {
        // Log migration errors for debugging, but continue
        // This handles "already exists" errors gracefully
        continue  // <-- ALL errors ignored!
    }
}
```

With this behavior:
- ✗ Permission errors are invisible
- ✗ Connection failures are hidden
- ✗ Schema definition bugs are masked
- ✗ Database misconfiguration goes undetected

## Solution

Added selective error logging to surface real issues while still allowing benign "already exists" errors to pass.

```go
// After fix: Selective logging based on error type
for _, m := range migrations {
    if _, err := surrealdb.Query[any](ctx, s.db, m, nil); err != nil {
        // Check if this is an "already exists" error, which is benign
        // Use lowercase matching for case-insensitivity across different SurrealDB versions
        errStr := strings.ToLower(err.Error())
        isAlreadyExists := strings.Contains(errStr, "already defined") ||
            strings.Contains(errStr, "already exists") ||
            strings.Contains(errStr, "duplicate index") ||
            strings.Contains(errStr, "duplicate field") ||
            (strings.Contains(errStr, "table name") && strings.Contains(errStr, "already exists"))

        if isAlreadyExists {
            // Benign error - table/index already exists, skip silently
            continue
        }

        // Real error that should be surfaced
        // Log as warning to make it visible without breaking startup
        // This helps diagnose configuration issues, permission problems, etc.
        log.Printf("Warning: migration failed for query '%s': %v\n"+
            "This may indicate a database configuration issue. Continuing anyway.", m, err)
        continue
    }
}
```

## Benefits

1. **Improved Observability**: Real database configuration issues are now logged as warnings
2. **Preserves Idempotency**: Multiple migrations still run cleanly (benign errors skipped)
3. **Case-Insensitive Matching**: Handles various SurrealDB error message formats
4. **Backward Compatible**: App still starts even with migration errors (logged but not fatal)
5. **Comprehensive Pattern Recognition**: Detects multiple "already exists" error patterns

## Error Patterns Recognized as Benign

| Pattern | Example | Action |
|----------|----------|---------|
| "already defined" | "table 'nodes' already defined" | Skip silently |
| "already exists" | "index 'idx_nodes_id' already exists" | Skip silently |
| "duplicate index" | "Duplicate index: idx_edges_id" | Skip silently |
| "duplicate field" | "duplicate field 'name'" | Skip silently |
| "table name" + "already exists" | "table name 'file_metadata' already exists" | Skip silently |

## Error Patterns That Are Now Logged

| Pattern | Example | Action |
|----------|----------|---------|
| Permission errors | "permission denied to create table" | Log warning |
| Connection errors | "connection to database failed: timeout" | Log warning |
| Syntax errors | "syntax error at position 10" | Log warning |
| Unknown errors | "failed to create table: internal error" | Log warning |

## Testing

Created comprehensive unit test (`internal/graph/storage_migration_test.go`) that verifies:
- ✓ Benign "already exists" errors are correctly identified and skipped
- ✓ Real error types (permission, connection, syntax) are detected
- ✓ Case-insensitive matching works correctly
- ✓ All error patterns are tested

Run the test:
```bash
go test ./internal/graph -run TestMigrationErrorLogging -v
```

## Verification

Created verification script (`verify_migration_logging.go`) that demonstrates the fix:

```bash
go run verify_migration_logging.go
```

This shows:
- ✓ Benign 'already exists' errors are silently skipped
- ✓ Real database errors are logged as warnings
- ✓ Case-insensitive matching handles variations
- ✓ Multiple error patterns are recognized

## Files Modified

1. `internal/graph/storage.go`:
   - Added `log` package import
   - Modified `RunMigrations` function with selective error logging
   - Uses case-insensitive error matching

2. `internal/graph/storage_migration_test.go` (new file):
   - Comprehensive unit tests for error handling logic
   - Tests all benign and severe error patterns

3. `verify_migration_logging.go` (new file):
   - Demonstration script showing fix behavior
   - Shows before/after comparison

## Impact

**Before Fix**:
- All migration errors were completely silent
- Database configuration issues could go undetected
- Developers had no visibility into migration failures
- Difficult to debug database-related problems

**After Fix**:
- Real migration errors are logged as warnings
- Benign "already exists" errors don't pollute logs
- Database configuration issues are immediately visible
- Easier to debug database-related problems
- Maintains backward compatibility (errors logged but don't fail startup)
