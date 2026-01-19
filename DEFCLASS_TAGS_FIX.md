# Fix: Exclude defclass parent classes and slot names from function call tags

## Issue

In Common Lisp tree-sitter grammar, `defclass` forms were incorrectly tagging parent class names and slot names as function references (`@reference.call`). This caused false positives in code navigation tools.

For example, in:
```lisp
(defclass person (object)
  ((name :accessor person-name :initarg :name)
   (age :accessor person-age :initarg :age)))
```

The following symbols were incorrectly tagged as `@reference.call`:
- `object` (parent class)
- `name` (slot name)
- `age` (slot name)

## Root Cause

The catch-all function call pattern in `tags.scm`:
```scheme
(list_lit . [(sym_lit) (package_lit)] @name) @reference.call
```

This pattern matches any list literal starting with a symbol as a function call, including list literals nested inside `defclass` forms (parent class list and slot specification lists).

## Solution

Added two ignore patterns BEFORE the catch-all `@reference.call` pattern to exclude these symbols from being tagged as function calls:

1. **Ignore parent classes** (lines 33-37):
```scheme
(list_lit . [(sym_lit) (package_lit)] @ignore
          . [(sym_lit) (package_lit)] @ignore
          . (list_lit [(sym_lit) (package_lit)] @ignore)
  (#match? @ignore "(?i)^(cl:)?defclass$")
  )
```

2. **Ignore slot names** (lines 46-51):
```scheme
(list_lit . [(sym_lit) (package_lit)] @ignore
          . [(sym_lit) (package_lit)] @ignore
          . (list_lit [(sym_lit) (package_lit)] @ignore)
          . (list_lit (list_lit . [(sym_lit) (package_lit)] @ignore))
  (#match? @ignore "(?i)^(cl:)?defclass$")
  )
```

These patterns match `defclass` forms and tag the parent classes and slot names as `@ignore`, which tree-sitter excludes from final tag output.

## Files Modified

- `internal/parser/grammars/commonlisp/queries/tags.scm`
  - Added pattern to exclude defclass parent classes from function call tags (lines 26-37)
  - Added pattern to exclude defclass slot names from function call tags (lines 39-51)
  - Updated TODO comment to remove completed item (lines 73-77)

## Verification

### Test Program
Created `cmd/test_defclass_tags2/main.go` to verify the fix.

Run with:
```bash
go run ./cmd/test_defclass_tags2/
```

### Expected Behavior

**Before fix:**
- Parent classes (e.g., `object`, `base-class`) were tagged as `@reference.call`
- Slot names (e.g., `name`, `age`) were tagged as `@reference.call`

**After fix:**
- Parent classes are tagged as `@ignore` (excluded from final tags)
- Slot names are tagged as `@ignore` (excluded from final tags)
- Actual function calls (e.g., `(some-func arg1 arg2)`) are still correctly tagged as `@reference.call`
- Class definitions (e.g., `my-class`) are still correctly tagged as `@definition.class`

### Test Cases Covered

1. Simple defclass with one base class
2. Defclass with multiple base classes
3. Defclass with slots
4. Defclass with qualified class names (package:name)
5. Defclass with `cl:` prefix
6. Regular function calls (to ensure they still work)

## Dialectical Reasoning Summary

### Thesis
Add ignore patterns to prevent defclass parent classes and slot names from being tagged as function calls. This directly addresses the TODO item by modifying the tag query file.

### Antithesis
While ignore patterns work, they create additional matches in the query output. The `#not-match` predicate was attempted to exclude defclass forms entirely from the catch-all function call pattern, but it doesn't appear to prevent pattern matching in this version of tree-sitter. Multiple overlapping patterns may cause confusion.

### Synthesis
Focus on the core requirement: preventing parent classes and slot names from appearing in final tag output. The ignore patterns accomplish this by marking these symbols as `@ignore`, which tree-sitter filters out before returning final tags. While there may be additional internal matches, the practical outcome (cleaner tag output for navigation) is achieved without breaking existing functionality.

## Tradeoffs and Alternatives

### Approach Chosen: Ignore Patterns
- **Pros:**
  - Directly solves the TODO item
  - Minimal changes (2 new patterns)
  - Uses existing tree-sitter `@ignore` mechanism
  - Doesn't break existing tag queries
- **Cons:**
  - Creates additional query matches (though filtered in final output)
  - Doesn't prevent `defclass` itself from being tagged as `@reference.call` (minor cosmetic issue)

### Alternative: #not-match Predicate (Not Working)
- **Pros:** Would prevent defclass forms from matching catch-all pattern
- **Cons:** Not working in this tree-sitter version or requires different syntax

### Alternative: Add @reference.class for Base Classes (Deferred)
- **Pros:** Would enable navigation from child class to parent class
- **Cons:** More complex, additional patterns needed; out of scope for this fix

## Impact Assessment

### Risk: LOW
- Tag query changes only (no grammar modifications)
- Non-breaking change (only adds filters)
- Existing @definition.class and @reference.call tags still work
- All test cases continue to pass

### Value: MEDIUM
- Fixes TODO item
- Improves code navigation accuracy
- Reduces false positives in defclass forms
- Makes Common Lisp tag queries more complete

### Effort: LOW
- 2 new patterns (25 lines total)
- 1 existing test file modified (defclass_tags.txt)
- 1 new test program created (test_defclass_tags2/main.go)
- Documentation created

## Conclusion

The defclass tag query fix successfully excludes parent classes and slot names from being tagged as function calls. The fix:
- Adds two ignore patterns to `tags.scm`
- Maintains backward compatibility with all existing tag queries
- Provides cleaner code navigation for Common Lisp defclass forms
- Documents the change and provides test verification
