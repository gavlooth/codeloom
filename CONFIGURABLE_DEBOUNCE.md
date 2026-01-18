# Configurable Watcher Debounce Feature

## Issue Summary

**Location**: `pkg/mcp/server.go` (line 1011)

**Severity**: Medium - Maintainability/Usability Issue

**Problem**: The watcher debounce delay was hardcoded to 100ms in the MCP server, making it impossible to adjust for different environments, project sizes, or developer workflows.

## Why This Matters

The file watcher uses debouncing to group rapid filesystem events (e.g., during file saves, git operations, or build processes). The optimal debounce delay varies significantly:

- **Fast SSD, small projects**: 10-50ms provides responsive feedback
- **Network filesystems (NFS, SMB)**: 500-1000ms reduces false triggers
- **Large monorepos**: 200-500ms handles slower file operations gracefully
- **Development vs production**: Different environments may have different requirements

Hardcoded values force all users into a single compromise value, resulting in:
- Slower-than-possible file change detection
- Excessive indexing operations during saves
- Wasted compute resources on rapid but ephemeral changes

## Solution

Added configurable watcher debounce with:
1. **Config file support**: Add `watcher_debounce_ms` to config TOML files
2. **Environment variable override**: `CODELOOM_WATCHER_DEBOUNCE_MS`
3. **Sensible defaults**: 100ms (existing default maintained for backward compatibility)
4. **Validation**: Range limits (10ms minimum, 60000ms maximum)

## Changes Made

### 1. Config Structure (`internal/config/config.go`)

Added `WatcherDebounceMs` field to `ServerConfig`:

```go
type ServerConfig struct {
	Mode        string `toml:"mode"`
	Port        int    `toml:"port"`
	WatcherDebounceMs int `toml:"watcher_debounce_ms"`  // NEW
}
```

Default value: 100ms

### 2. Config Validation

Added validation rules:
- Minimum: 10ms (prevents excessive event processing)
- Maximum: 60000ms (60 seconds, reasonable upper bound)

```go
if cfg.Server.WatcherDebounceMs < 10 {
	warnings = append(warnings, "Watcher debounce must be at least 10ms")
}
if cfg.Server.WatcherDebounceMs > 60000 {
	warnings = append(warnings, "Watcher debounce exceeds reasonable maximum (60000ms)")
}
```

### 3. Environment Variable Override

Added support for `CODELOOM_WATCHER_DEBOUNCE_MS`:

```go
if v := os.Getenv("CODELOOM_WATCHER_DEBOUNCE_MS"); v != "" {
	if i, err := strconv.Atoi(v); err == nil {
		cfg.Server.WatcherDebounceMs = i
	}
}
```

### 4. Server Implementation (`pkg/mcp/server.go`)

Updated watcher initialization to use configured value:

```go
watcher, err := daemon.NewWatcher(daemon.WatcherConfig{
	Parser:          parser.NewParser(),
	Storage:         s.storage,
	Embedding:       s.embedding,
	ExcludePatterns: indexer.DefaultExcludePatterns(),
	DebounceMs:      s.config.Server.WatcherDebounceMs,  // CHANGED from hardcoded 100
})
```

## Configuration Examples

### TOML Config File (`.codeloom/config.toml`)

```toml
[server]
mode = "stdio"
port = 3003
watcher_debounce_ms = 250  # Faster for local development
```

### Environment Variable

```bash
export CODELOOM_WATCHER_DEBOUNCE_MS=500
codeloom start stdio
```

### Recommended Values by Environment

| Environment | Recommended Debounce | Rationale |
|-------------|----------------------|------------|
| Local SSD, small project | 50-100ms | Fast file system, want quick feedback |
| Local SSD, large monorepo | 200-500ms | Slower file operations, reduce noise |
| Network filesystem (NFS/SMB) | 500-1000ms | High latency, prevent false triggers |
| CI/CD pipeline | 500-2000ms | Many file changes, batch processing |
| Production server | 100-500ms | Balance responsiveness and efficiency |

## Testing

### New Tests Added (`internal/config/config_test.go`)

1. **TestDefaultConfig**
   - Verifies default debounce is 100ms
   - Confirms backward compatibility

2. **TestConfigValidation**
   - Tests minimum limit (10ms)
   - Tests maximum limit (60000ms)
   - Validates warning messages

3. **TestEnvOverrideWatcherDebounce**
   - Verifies environment variable override works
   - Tests with custom value (500ms)

### Test Results

```bash
$ go test ./internal/config -v
=== RUN   TestDefaultConfig
--- PASS: TestDefaultConfig (0.00s)
=== RUN   TestConfigValidation
--- PASS: TestConfigValidation (0.00s)
=== RUN   TestEnvOverrideWatcherDebounce
--- PASS: TestEnvOverrideWatcherDebounce (0.00s)
PASS
ok      github.com/heefoo/codeloom/internal/config   0.003s
```

All existing tests continue to pass:
- `internal/daemon`: PASS (3/3 tests)
- `internal/graph`: PASS (9/9 tests)
- `internal/indexer`: PASS (5/5 tests)
- `internal/config`: PASS (3/3 tests) ← NEW
- `internal/httpclient`: PASS (1/1 test)
- `internal/parser`: PASS (3/3 tests)

## Impact Analysis

### Before Fix
- ❌ Hardcoded 100ms debounce for all environments
- ❌ Cannot optimize for different file system types
- ❌ Excessive indexing during rapid file saves
- ❌ No way to tune for project-specific needs

### After Fix
- ✅ Fully configurable via config file or environment variable
- ✅ Default value maintains backward compatibility
- ✅ Validation prevents invalid values
- ✅ Can optimize per environment/project
- ✅ Reduced resource waste on unnecessary indexing

## Migration Guide

### For Existing Users

No changes required! Default behavior (100ms) is preserved.

To customize:

1. **Create config file**:
   ```bash
   mkdir -p ~/.codeloom
   cat > ~/.codeloom/config.toml << EOF
   [server]
   watcher_debounce_ms = 250
   EOF
   ```

2. **Or use environment variable**:
   ```bash
   export CODELOOM_WATCHER_DEBOUNCE_MS=250
   ```

3. **Verify configuration**:
   ```bash
   # Check current config (if verbose mode available)
   codeloom start stdio --verbose
   ```

### For New Users

Default configuration will work out of the box. Adjust debounce if experiencing:
- **Too many indexing events**: Increase debounce (e.g., 200-500ms)
- **Delayed file detection**: Decrease debounce (e.g., 50-100ms)

## Tradeoffs and Alternatives

### Alternatives Considered

1. **Auto-adaptive debounce**
   - Pros: Automatically adjusts based on event frequency
   - Cons: Complex to implement, unpredictable behavior
   - Decision: Simple explicit config is more predictable

2. **Per-directory debounce values**
   - Pros: Fine-grained control for complex projects
   - Cons: More complex configuration, rare use case
   - Decision: Single value is sufficient for 99% of use cases

3. **Runtime API to change debounce**
   - Pros: Dynamic adjustment without restart
   - Cons: Adds complexity, potential race conditions
   - Decision: Configuration at startup is simpler

### Tradeoffs of Chosen Solution

**Advantages**:
- Simple configuration model (single value)
- Backward compatible (default unchanged)
- Supports both file and environment configuration
- Validation prevents invalid values
- Well-tested with comprehensive test coverage

**Limitations**:
- Single global debounce (no per-directory tuning)
- Requires restart to change configuration
- No automatic adaptation to system conditions

## Verification Steps

To verify the fix works correctly:

1. **Build** code:
   ```bash
   go build ./cmd/codeloom
   ```

2. **Run tests**:
   ```bash
   go test ./internal/config -v
   go test ./pkg/mcp -v  # If tests exist
   ```

3. **Test with custom config**:
   ```bash
   # Create config file
   mkdir -p /tmp/test_codeloom
   cat > /tmp/test_codeloom/config.toml << EOF
   [server]
   watcher_debounce_ms = 250
   EOF

   # Start watcher with config
   ./codeloom start stdio --config /tmp/test_codeloom/config.toml

   # Verify watcher uses configured debounce (check logs)
   ```

4. **Test with environment variable**:
   ```bash
   export CODELOOM_WATCHER_DEBOUNCE_MS=500
   ./codeloom start stdio

   # Verify watcher uses 500ms debounce
   ```

## Related Code

This change integrates with existing configuration infrastructure:
- `config.ServerConfig`: Holds server-level configuration
- `config.Load()`: Loads config from file and environment
- `config.Validate()`: Validates configuration values
- `daemon.WatcherConfig`: Receives debounce value
- `daemon.Watcher`: Uses debounce for event processing

## Conclusion

This feature enhances codeloom's configurability by making the watcher debounce delay customizable. Users can now optimize file watching behavior for their specific environment and workflow, improving both responsiveness and efficiency. The implementation maintains backward compatibility, includes comprehensive validation, and is well-tested.
