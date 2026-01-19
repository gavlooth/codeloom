# CodeLoom Bug Fix Summary

## Overview

Fixed a correctness issue in the Clojure tree-sitter grammar that was allowing radix numbers with invalid base values (e.g., `37r10`, `999r1`, `0r0`, `1r0`) to be accepted as valid number literals.

## Primary Fix: Clojure Radix Number Base Validation

### File Modified
- `internal/parser/grammars/clojure/grammar.js` (lines 75-88, `RADIX_NUMBER` rule)

### Issue
The `RADIX_NUMBER` grammar rule was not constraining the base number to the valid range specified by Clojure (2-36), allowing syntactically invalid forms that violated the language specification.

**Before Fix:**
```javascript
const RADIX_NUMBER =
      seq(repeat1(DIGIT),  // XXX: not constraining number before r/R
          regex('[rR]'),
          repeat1(ALPHANUMERIC)); // XXX: not constraining portion after r/R
```

**Problem:**
- `repeat1(DIGIT)` matches ANY sequence of digits (0-9), allowing bases like `37`, `99`, `999`, `0`, `1`
- According to [Clojure Reader Reference](https://clojure.org/reference/reader#_numbers), valid radix numbers must use bases 2-36
- Invalid forms like `37r10` would parse successfully as a `num_lit` node despite violating specification

### Fix Details

**After Fix:**
```javascript
const RADIX_NUMBER =
      seq(
          // Constrain base to 2-36:
          // Single digit: 2-9
          // Two digits: 10-36
          choice(
              /[2-9]/,           // 2-9 (single digit bases)
              /1\d/,              // 10-19 (two digits starting with 1)
              /2\d/,              // 20-29 (two digits starting with 2)
              /3[0-6]/),          // 30-36 (two digits starting with 3, second digit 0-6)
          regex('[rR]'),         // radix indicator
          repeat1(ALPHANUMERIC));  // digits for the number
```

**Pattern Logic:**
- `[2-9]`: Matches single-digit bases 2-9
- `1\d`: Matches bases 10-19 (digit '1' followed by any digit 0-9)
- `2\d`: Matches bases 20-29 (digit '2' followed by any digit 0-9)
- `3[0-6]`: Matches bases 30-36 (digit '3' followed by digit 0-6)

This explicitly rejects:
- `0r0` (base 0 is invalid - < 2)
- `1r0` (base 1 is invalid - < 2)
- `37r10` (base 37 is invalid - > 36)
- `40r10` (base 40 is invalid - > 36)
- `99r10` (base 99 is invalid - > 36)

## Testing

### Test Results

#### Valid Radix Numbers (Base 2-36)

| Test | Input | Base | Status |
|------|--------|-------|--------|
| 1 | `2r1010` | 2 | ✓ NUM_LIT |
| 2 | `8r17` | 8 | ✓ NUM_LIT |
| 3 | `10r256` | 10 | ✓ NUM_LIT |
| 4 | `16rFF` | 16 | ✓ NUM_LIT |
| 5 | `36rZ` | 36 | ✓ NUM_LIT |
| 6 | `3r210` | 3 | ✓ NUM_LIT |
| 7 | `7r16` | 7 | ✓ NUM_LIT |
| 8 | `11rA` | 11 | ✓ NUM_LIT |
| 9 | `20rJ` | 20 | ✓ NUM_LIT |
| 10 | `30rN` | 30 | ✓ NUM_LIT |
| 11 | `35rZ` | 35 | ✓ NUM_LIT |

#### Invalid Radix Numbers (Base < 2 or > 36)

| Test | Input | Base | Status | Parse Tree |
|------|--------|-------|--------|-----------|
| 12 | `37r10` | 37 | ✓ SPLIT (INTEGER + SYMBOL) |
| 13 | `40r10` | 40 | ✓ SPLIT (INTEGER + SYMBOL) |
| 14 | `99r10` | 99 | ✓ SPLIT (INTEGER + SYMBOL) |
| 15 | `0r0` | 0 | ✓ SPLIT (INTEGER + SYMBOL) |
| 16 | `1r0` | 1 | ✓ SPLIT (INTEGER + SYMBOL) |

**Note:** "SPLIT" status indicates that the input was parsed as multiple nodes (e.g., `num_lit` for the integer part and `sym_lit` for the `r...` part) rather than as a single radix number literal. This is the correct fallback behavior in tree-sitter when a specific syntax is rejected.

### Test Suites Verified

All existing tests continue to pass:
```bash
$ tree-sitter test
Total parses: 115; successful parses: 115; failed parses: 0; success percentage: 100.00%
```

Parser tests also pass:
```bash
$ go test ./internal/parser/...
ok  	github.com/heefoo/codeloom/internal/parser	0.002s
```

## Impact

### Before Fix
- ❌ Grammar accepted invalid radix bases (0, 1, 37, 40, 99, etc.)
- ❌ Forms like `37r10` parsed successfully as `num_lit` despite violating spec
- ❌ No syntax-level validation of base range
- ❌ Violated Clojure language specification

### After Fix
- ✅ Base range constrained to valid 2-36 range
- ✅ Invalid bases are rejected at syntax level
- ✅ Aligns grammar with Clojure specification
- ✅ Invalid forms parse as fallback patterns (INTEGER + SYMBOL) rather than as radix numbers
- ✅ All existing tests continue to pass (100% success rate)
- ✅ Backward compatible for valid inputs

### Parsing Behavior Change

**Before:** `37r10` → `(source (num_lit))` (incorrectly accepted as radix number)
**After:** `37r10` → `(source (num_lit) (sym_lit name: (sym_name)))` (base rejected, parsed as integer + symbol)

This is the correct tree-sitter behavior for syntax rejection - invalid forms are still parsed, just not as the intended token type.

## Documentation

Created comprehensive documentation:
- `CLOJURE_RADIX_FIX.md` - Detailed analysis of issue and fix with test results
- `BUG_FIX_SUMMARY.md` - This summary document

## Files Changed

| File | Lines Changed | Description |
|-------|---------------|-------------|
| `internal/parser/grammars/clojure/grammar.js` | +20, -4 | Added base validation patterns, removed XXX comments |
| `internal/parser/grammars/clojure/src/grammar.json` | auto-regenerated | Updated from grammar.js |
| `internal/parser/grammars/clojure/src/parser.c` | auto-regenerated | Updated from grammar.js |
| `internal/parser/grammars/clojure/src/node-types.json` | auto-regenerated | Updated from grammar.js |
| `CLOJURE_RADIX_FIX.md` | +260 (new) | Detailed fix documentation |
| `BUG_FIX_SUMMARY.md` | +150 (updated) | This summary document |

## Verification Steps

To verify the fix works correctly:

1. **Regenerate grammar:**
   ```bash
   cd internal/parser/grammars/clojure
   tree-sitter generate
   ```

2. **Rebuild Go language binding:**
   ```bash
   cd ../clojure_lang
   go build -a
   ```

3. **Run tree-sitter test suite:**
   ```bash
   cd ../clojure
   tree-sitter test
   ```
   Expected: 115/115 tests pass (100%)

4. **Test valid radix numbers:**
   ```bash
   cd /home/heefoo/codeloom
   go run ./cmd/test_simple_radix/
   ```
   Expected: Tests 1-11 (bases 2-36) show "NUM_LIT"

5. **Test invalid radix numbers:**
   ```bash
   go run ./cmd/test_simple_radix/
   ```
   Expected: Tests 12-16 (bases < 2 or > 36) show "SPLIT"

6. **Run parser tests:**
   ```bash
   go test ./internal/parser/...
   ```
   Expected: All tests pass

## Related Issues

This fix resolves XXX comments in `grammar.js`:
- ✓ Fixed: "not constraining number before r/R" (base range now validated)
- ℹ️ Not addressed: "not constraining portion after r/R" (digit validity per base - left for semantic validation)

The second comment ("not constraining portion after r/R") refers to validating that digits after the 'r' indicator are valid for the specified base (e.g., base 2 should only allow 0-1). This is a **semantic validation** concern rather than a syntax-level concern. Semantic validation is better handled by:
- Clojure compiler/reader at runtime
- Linter/static analysis tools
- Future grammar enhancements (though significantly more complex)

## Next Steps

The fix is complete and tested. Recommended actions:

1. Review the fix in `internal/parser/grammars/clojure/grammar.js`
2. Review documentation in `CLOJURE_RADIX_FIX.md`
3. Run all tests to confirm no regressions:
   ```bash
   go test ./...
   cd internal/parser/grammars/clojure && tree-sitter test
   ```
4. Consider addressing the remaining XXX comment ("not constraining portion after r/R") in future work
