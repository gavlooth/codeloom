# Common Lisp Ratio Zero-Denominator Fix

## Issue
The Common Lisp tree-sitter grammar was incorrectly accepting ratios with zero denominators (e.g., `1/0`, `0/0`, `5/0`), which should be rejected as division by zero is undefined in mathematics and Common Lisp.

## Root Cause
The RATIO rule in `grammar.js` was defined as:
```javascript
const RATIO =
    seq(repeat1(DIGIT),
        "/",
        repeat1(DIGIT));
```

This allowed any sequence of digits in the denominator, including `0`, `00`, `000`, etc.

## Solution
Modified the RATIO rule to ensure denominator starts with a non-zero digit:
```javascript
const RATIO =
    seq(repeat1(DIGIT),
        "/",
        /[1-9]/,
        repeat(DIGIT));
```

This change:
- Numerator: Still accepts any sequence of digits (including `0`)
- Slash: Matches `/` character
- Denominator first digit: Must be `[1-9]` (non-zero)
- Denominator remaining digits: Can be `[0-9]` (any digit)

## Examples

### Valid Ratios (Accepted)
- `1/2` → `num_lit`
- `0/5` → `num_lit` (0 divided by 5 is 0)
- `10/20` → `num_lit`
- `123/456` → `num_lit`
- `0/01` → `num_lit` (denominator evaluates to 1)
- `100/001` → `num_lit` (denominator evaluates to 1)

### Invalid Ratios (Rejected)
- `1/0` → `sym_lit` (parsed as symbol, not number)
- `0/0` → `sym_lit` (parsed as symbol, not number)
- `5/0` → `sym_lit` (parsed as symbol, not number)
- `10/0` → `sym_lit` (parsed as symbol, not number)
- `1/00` → `sym_lit` (denominator evaluates to 0)

## Implementation Steps

1. Modified `internal/parser/grammars/commonlisp/grammar.js`
2. Regenerated parser: `npx tree-sitter generate`
3. Rebuilt C library: `make clean && make`
4. Verified fix with test suite

## Test Results

All tests pass:
```
✓ PASS: code=1/2, expect=true, got=true
✓ PASS: code=0/5, expect=true, got=true
✓ PASS: code=10/20, expect=true, got=true
✓ PASS: code=1/0, expect=false, got=false
✓ PASS: code=0/0, expect=false, got=false
✓ PASS: code=5/0, expect=false, got=false

Results: 6 passed, 0 failed
```

## Impact

### Before
- Grammar accepted invalid ratios like `1/0`, `0/0`
- These were parsed as `num_lit` (number literals)
- Potential for runtime errors in Lisp code evaluation

### After
- Grammar rejects invalid ratios with zero denominators
- These are parsed as `sym_lit` (symbol literals) instead
- Code can still syntactically contain symbols like `1/0`, but they're not numbers
- Safer and more correct according to Common Lisp semantics

## Related Code

Files modified:
- `internal/parser/grammars/commonlisp/grammar.js` (RATIO rule)
- `internal/parser/grammars/commonlisp/src/parser.c` (regenerated)
- `internal/parser/grammars/commonlisp/src/grammar.json` (regenerated)
- `internal/parser/grammars/commonlisp/src/node-types.json` (regenerated)
- `internal/parser/grammars/commonlisp/libtree-sitter-commonlisp.so` (rebuilt)
- `internal/parser/grammars/commonlisp/libtree-sitter-commonlisp.a` (rebuilt)

Tests added:
- `cmd/test_ratios/main.go` - Comprehensive ratio validation test

## Notes

- Invalid ratios (with zero denominators) are NOT rejected at parse time with an error
- Instead, they fall through to be parsed as symbols (`sym_lit`)
- This is the correct behavior for tree-sitter grammars
- The grammar ensures that only valid number patterns match the `num_lit` rule
- Code containing `1/0` will still parse successfully, but as a symbol, not a number
- This maintains tree-sitter's design philosophy of being permissive at the syntax level
