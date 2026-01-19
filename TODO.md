# CodeLoom TODO

This document tracks known issues, improvements, and technical debt items for the CodeLoom project.

## Priority Items

None currently identified.

## Code Documentation Notes

### Intentional Design Decisions (Not Technical Debt)

The following XXX comments in `internal/parser/grammars/clojure/grammar.js` document intentional design decisions or known constraints:

1. **Unicode character handling** (line 179-180)
   - Note: Deliberately differs from LispReader.java's surrogate pair handling
   - Reason: Tree-sitter's regex pattern limitations

2. **Any character in character literals** (line 188-195)
   - Note: Uncertainty about what Java's `Character.valueOf()` represents in tree-sitter
   - Constraint: `\x00` (null character) doesn't work in tree-sitter regex patterns
   - Current approach: Accepts any character via regex('.|\n')

3. **Symbol validation** (line 230-233)
   - Note: No attempt to enforce complex symbol rules from Clojure
   - Examples not validated:
     - Symbols beginning or ending with ':' (reserved)
     - Non-repeating ':' character rules
   - Reason: Tree-sitter grammar would become overly complex; most valid symbols work correctly

4. **Metadata on unquote-splicing literals** (line 442-443)
   - Note: "metadata here doesn't seem to make sense"
   - Observation: Clojure REPL accepts `(^:x ~@[:a :b :c])`
   - Decision: Support it because the REPL accepts it, even if semantically unusual

5. **Var quoting literal value** (line 465-466)
   - Note: Uncertain if symbols, reader conditionals, and tagged literals are the only valid forms
   - Question: "any other things?"
   - Current approach: Accept `_form` (broad permissiveness)

6. **Metadata on unquote-splicing** (line 517-518)
   - Note: Same as #4 - metadata doesn't semantically make sense but is supported
   - Decision: Follow what Clojure REPL accepts

### Why These Are Not Bugs

All items above represent deliberate trade-offs between:
- **Grammar complexity**: Validating all edge cases would make the grammar unmaintainable
- **Tree-sitter limitations**: Some regex patterns or constraints are not supported
- **Pragmatism**: Accepting what Clojure's own REPL accepts, even if unusual
- **Test coverage**: Existing test files (`cmd/test_*/`) validate important functionality

Improvements would require:
1. Understanding actual impact (are developers hitting these edge cases?)
2. Balancing grammar complexity vs. correctness
3. Ensuring tree-sitter can support the validation needed
4. Adding comprehensive tests for new validations

## Areas for Future Investigation

### Performance
- [ ] Evaluate batch size for embedding generation (currently hardcoded to 100)
- [ ] Benchmark impact of different embedding worker pool sizes (currently 4 workers)
- [ ] Consider adaptive batch sizing based on embedding service response times

### Reliability
- [ ] Add metrics/tracking for embedding retry failures to identify systemic issues
- [ ] Consider circuit breaker pattern for embedding service outages
- [ ] Add health checks for external dependencies (Ollama, OpenAI, Anthropic, etc.)

### Usability
- [ ] Add progress reporting for long-running indexing operations
- [ ] Consider adding verbose mode to show detailed processing steps
- [ ] Improve error messages to guide users toward solutions

### Testing
- [ ] Add integration tests for MCP server tool execution
- [ ] Add stress tests for concurrent indexing operations
- [ ] Test embedding provider failover behavior
- [ ] Add tests for file system edge cases (symlinks, network drives, etc.)

## Completed Items

This section tracks items that have been addressed:

- [x] **Common Lisp decimal number support** - Added support for numbers with leading decimal point (`.9`, `.123`)
- [x] **Common Lisp ratio number support** - Added parsing for ratio literals like `3/4`
- [x] **Common Lisp radix number support** - Added validation for radix literals (`2r101`, `16xFF`)
- [x] **Common Lisp octal escape sequences** - Support for `\NNN` character escape in Common Lisp
- [x] **Defclass tagging** - Fixed incorrect tagging of `defclass` parent classes and slot names
- [x] **Embedding retry with exponential backoff** - Added retry logic for failed embedding generation
- [x] **Context cancellation throughout** - Added ctx.Done() checks in CPU-intensive operations
- [x] **Shared HTTP client** - Consolidated HTTP client usage for consistent connection pooling
- [x] **Test/debug binary cleanup** - Added test binaries to .gitignore to prevent tracking

## Contribution Guidelines

When adding items to this TODO:

1. **Be specific**: Describe the problem, not just the symptom
2. **Include impact**: Explain why this matters (performance, reliability, UX)
3. **Consider trade-offs**: Discuss pros/cons of different solutions
4. **Reference code**: Include file paths and line numbers when relevant
5. **Mark completed items**: Move addressed items to the "Completed" section with date
6. **Avoid clutter**: Only track items that need future attention (not decisions made)
