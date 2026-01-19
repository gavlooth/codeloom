# Fix: Validate Common Lisp radix number base range (2-36)

## Issue

The Common Lisp radix number grammar was not constraining the base number to valid range (2-36), allowing syntactically invalid forms like:

- `#0r0` - base 0 is invalid (minimum is 2)
- `#1r0` - base 1 is invalid (minimum is 2)
- `#37r10` - base 37 is invalid (maximum is 36)
- `#99r1` - base 99 is invalid (maximum is 36)

### Root Cause

The original `RADIX_NUMBER` grammar rule was:

```javascript
const RADIX_NUMBER =
    seq('#',
        repeat1(DIGIT),  // XXX: any digits allowed
        /[rR]/,
        repeat1(ALPHANUMERIC));  // XXX: no digit validation per base
```

This pattern used:
- `repeat1(DIGIT)` - matches one or more digits (0-9) without constraint
- `/#rR/` - matches 'r' or 'R' (radix indicator)
- `repeat1(ALPHANUMERIC)` - matches one or more alphanumeric characters

The `repeat1(DIGIT)` pattern allowed ANY sequence of digits, including bases outside the valid range of 2-36 specified by the Common Lisp ANSI standard.

## Solution

Modified the `RADIX_NUMBER` rule to explicitly constrain the base to valid range (2-36):

```javascript
// Radix numbers: #Nrdigits where N is base (2-36) and r/R is radix indicator
// Syntax validation only - semantic validation (e.g., digit validity for base) handled by compiler
const RADIX_NUMBER =
    seq('#',
        // Constrain base to 2-36:
        // Single digit: 2-9
        // Two digits: 10-36
        choice(
            /[2-9]/,           // 2-9
            /1\d/,              // 10-19
            /2\d/,              // 20-29
            /3[0-6]/),          // 30-36
        /[rR]/,
        repeat1(ALPHANUMERIC));
```

### Validation Logic

The `choice()` operator tries each pattern in order:
1. `[2-9]` - matches single digit 2-9 (bases 2-9)
2. `1\d` - matches '1' followed by digit 0-9 (bases 10-19)
3. `2\d` - matches '2' followed by digit 0-9 (bases 20-29)
4. `3[0-6]` - matches '3' followed by digit 0-6 (bases 30-36)

This rejects invalid bases because:
- `0`, `1` don't match `[2-9]` (single digit pattern)
- `37` doesn't match `3[0-6]` (second digit '7' is outside 0-6 range)
- `99`, `100` don't match any of the patterns

### Parsing Behavior

When an invalid base is used (e.g., `#37r10`), the parser behaves as follows:

**Before Fix:**
- `#37r10` would match `#` + `repeat1(DIGIT)` + `/[rR]/` + `repeat1(ALPHANUMERIC)`
- Parsed successfully as a single `num_lit` node
- INCORRECT - syntax accepted even though base is invalid

**After Fix:**
- `37` doesn't match any base pattern (not 2-9, not 10-19, not 20-29, not 30-36)
- Parser falls back to other patterns:
  - `#` is parsed as the dispatch character
  - `37r10` is left over and parsed as a `sym_lit` (symbol)
- Result: `(source (# + sym_lit))`
- CORRECT - syntax properly rejected, fallback to alternative parse

## Test Results

### Valid Radix Numbers (Base 2-36)

| Test | Input | Base | Status |
|------|--------|-------|--------|
| 1 | `#2r1010` | 2 | ✓ NUM_LIT |
| 2 | `#8r17` | 8 | ✓ NUM_LIT |
| 3 | `#10r256` | 10 | ✓ NUM_LIT |
| 4 | `#16rFF` | 16 | ✓ NUM_LIT |
| 5 | `#36rZ` | 36 | ✓ NUM_LIT |
| 6 | `#3r210` | 3 | ✓ NUM_LIT |
| 7 | `#7r16` | 7 | ✓ NUM_LIT |
| 8 | `#11rA` | 11 | ✓ NUM_LIT |
| 9 | `#20rJ` | 20 | ✓ NUM_LIT |
| 10 | `#30rN` | 30 | ✓ NUM_LIT |
| 11 | `#35rZ` | 35 | ✓ NUM_LIT |

### Invalid Radix Numbers (Base < 2 or > 36)

| Test | Input | Base | Status | Parse Tree |
|------|--------|-------|--------|-----------|
| 12 | `#37r10` | 37 | ✓ SPLIT | (# + sym_lit) |
| 13 | `#40r10` | 40 | ✓ SPLIT | (# + sym_lit) |
| 14 | `#99r1` | 99 | ✓ SPLIT | (# + sym_lit) |
| 15 | `#0r0` | 0 | ✓ SPLIT | (# + sym_lit) |
| 16 | `#1r0` | 1 | ✓ SPLIT | (# + sym_lit) |

**Note:** "SPLIT" status indicates that the input was parsed as multiple nodes (e.g., `#` dispatch character + `sym_lit` for the rest) rather than as a single radix number literal. This is the correct fallback behavior in tree-sitter when a specific syntax is rejected.

### Test Suites Verified

All existing tests continue to pass:
```bash
$ tree-sitter test
Total parses: 43; successful parses: 43; failed parses: 0; success percentage: 100.00%
```

Parser tests also pass:
```bash
$ go test ./internal/parser/...
ok  	github.com/heefoo/codeloom/internal/parser	0.004s
```

## Impact

### Before Fix
- ❌ Grammar accepted invalid radix bases (0, 1, 37, 40, 99, etc.)
- ❌ Forms like `#37r10` parsed successfully as `num_lit` despite violating spec
- ❌ No syntax-level validation of base range
- ❌ Violated Common Lisp ANSI specification

### After Fix
- ✅ Base range constrained to valid 2-36 range
- ✅ Invalid bases are rejected at syntax level
- ✅ Aligns grammar with Common Lisp ANSI specification
- ✅ Invalid forms parse as fallback patterns (# + sym_lit) rather than as radix numbers
- ✅ All existing tests continue to pass (100% success rate)
- ✅ Backward compatible for valid inputs

### Parsing Behavior Change

**Before:** `#37r10` → `(source (num_lit))` (incorrectly accepted as radix number)
**After:** `#37r10` → `(source (# + sym_lit))` (base rejected, parsed as dispatch char + symbol)

This is the correct tree-sitter behavior for syntax rejection - invalid forms are still parsed, just not as the intended token type.

## Documentation

Created comprehensive documentation:
- `COMMONLISP_RADIX_FIX.md` - This detailed analysis document

## Files Changed

| File | Lines Changed | Description |
|-------|---------------|-------------|
| `internal/parser/grammars/commonlisp/grammar.js` | +9, -3 | Added base validation patterns, removed XXX comments |
| `internal/parser/grammars/commonlisp/src/grammar.json` | auto-regenerated | Updated from grammar.js |
| `internal/parser/grammars/commonlisp/src/parser.c` | auto-regenerated | Updated from grammar.js |
| `internal/parser/grammars/commonlisp/src/node-types.json` | auto-regenerated | Updated from grammar.js |
| `internal/parser/grammars/commonlisp/libtree-sitter-commonlisp.a` | rebuilt | Updated C library |
| `internal/parser/grammars/commonlisp/libtree-sitter-commonlisp.so` | rebuilt | Updated C shared library |
| `cmd/test_commonlisp_radix/main.go` | +148 (new) | Comprehensive radix validation test |
| `COMMONLISP_RADIX_FIX.md` | +180 (new) | This detailed fix documentation |

## Verification Steps

To verify the fix works correctly:

1. **Regenerate grammar:**
   ```bash
   cd internal/parser/grammars/commonlisp
   tree-sitter generate
   ```

2. **Rebuild C library:**
   ```bash
   make
   ```

3. **Rebuild Go language binding:**
   ```bash
   cd ../commonlisp_lang
   go build -a
   ```

4. **Run tree-sitter test suite:**
   ```bash
   cd ../commonlisp
   tree-sitter test
   ```
   Expected: 43/43 tests pass (100%)

5. **Test valid radix numbers:**
   ```bash
   cd /home/heefoo/codeloom
   go run ./cmd/test_commonlisp_radix/
   ```
   Expected: Tests 1-11 (bases 2-36) show "NUM_LIT"

6. **Test invalid radix numbers:**
   ```bash
   go run ./cmd/test_commonlisp_radix/
   ```
   Expected: Tests 12-16 (bases < 2 or > 36) show "SPLIT"

7. **Run parser tests:**
   ```bash
   go test ./internal/parser/...
   ```
   Expected: All tests pass

## Related Issues

This fix is analogous to the Clojure radix number fix that was recently applied:
- Clojure fix: `CLOJURE_RADIX_FIX.md` and `BUG_FIX_SUMMARY.md`
- Common Lisp fix: `COMMONLISP_RADIX_FIX.md` (this document)

Both languages support radix notation with the same base range (2-36) as specified by their respective language standards.

## Notes

The comment "Syntax validation only - semantic validation (e.g., digit validity for base) handled by compiler" indicates that:
- This fix validates the **base range** at the syntax level
- Semantic validation (ensuring digits after 'r' are valid for the specified base) is left to the compiler/runtime
- For example, `#2r23` would parse successfully as `num_lit` (base 2 is valid), even though '2' and '3' are not valid binary digits - this is intentional permissiveness at the syntax level

This aligns with tree-sitter's design philosophy of being permissive at the syntax level while allowing tools to perform stricter validation when needed.

## Next Steps

The fix is complete and tested. Recommended actions:

1. Review the fix in `internal/parser/grammars/commonlisp/grammar.js`
2. Review documentation in `COMMONLISP_RADIX_FIX.md`
3. Run all tests to confirm no regressions:
   ```bash
   go test ./...
   cd internal/parser/grammars/commonlisp && tree-sitter test
   ```
4. Verify the test program works correctly:
   ```bash
   go run ./cmd/test_commonlisp_radix/
   ```
