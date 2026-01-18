#!/usr/bin/env python3
"""
Test script to verify that the line_comment fix works correctly.

This script tests that:
1. Line comments (# comment) are properly parsed
2. Block comments (#= comment =#) are properly parsed
3. There's no ambiguity between the two
"""

import subprocess
import sys
import tempfile
import os

def test_parse(code, expected_types):
    """Test that code parses with expected node types."""
    with tempfile.NamedTemporaryFile(mode='w', suffix='.jl', delete=False) as f:
        f.write(code)
        f.flush()
        temp_file = f.name

    try:
        # Use tree-sitter to parse the file
        result = subprocess.run(
            ['tree-sitter', 'parse', temp_file],
            capture_output=True,
            text=True,
            cwd='/home/heefoo/codeloom/internal/parser/grammars/julia'
        )

        if result.returncode != 0:
            print(f"FAILED: Parse error for code: {code}")
            print(f"Error: {result.stderr}")
            return False

        # Check for expected types in the output
        output = result.stdout
        for expected_type in expected_types:
            if expected_type not in output:
                print(f"FAILED: Expected '{expected_type}' not found in parse output for: {code}")
                print(f"Output: {output}")
                return False

        return True

    finally:
        os.unlink(temp_file)

def main():
    """Run all tests."""
    tests = [
        # Line comment tests
        ("# This is a line comment\n", ["line_comment"]),
        ("x = 1 # inline comment\n", ["line_comment"]),

        # Block comment tests
        ("#= This is a block comment =#\n", ["block_comment"]),
        ("x = #= inline block comment =# 1\n", ["block_comment"]),

        # Mixed comment tests
        ("# Line comment\n#= Block comment =#\n", ["line_comment", "block_comment"]),
        ("x = #= block =# y # line\n", ["block_comment", "line_comment"]),
    ]

    passed = 0
    failed = 0

    for code, expected_types in tests:
        print(f"Testing: {repr(code)}")
        if test_parse(code, expected_types):
            print(f"  ✓ PASSED")
            passed += 1
        else:
            print(f"  ✗ FAILED")
            failed += 1
        print()

    print(f"Results: {passed} passed, {failed} failed out of {len(tests)} tests")

    return 0 if failed == 0 else 1

if __name__ == '__main__':
    sys.exit(main())
