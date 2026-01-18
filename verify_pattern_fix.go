package main

import (
	"fmt"
	"path/filepath"
)

// This script demonstrates the difference between old and new behavior

func oldBehavior(pattern, name string) bool {
	// Old: silently ignores errors
	if matched, _ := filepath.Match(pattern, name); matched {
		return true
	}
	return false
}

func newBehavior(pattern, name string) bool {
	// New: logs errors properly
	matched, err := filepath.Match(pattern, name)
	if err != nil {
		fmt.Printf("Warning: invalid pattern '%s': %v. Pattern will not match any files.\n", pattern, err)
		return false
	}
	return matched
}

func main() {
	fmt.Println("Testing filepath.Match error handling:")
	fmt.Println()

	// Test case 1: Valid pattern
	validPattern := "*.go"
	testName := "test.go"

	fmt.Println("Test 1: Valid pattern")
	fmt.Printf("  Pattern: %s, Name: %s\n", validPattern, testName)
	fmt.Printf("  Old behavior (no error logged): %v\n", oldBehavior(validPattern, testName))
	fmt.Printf("  New behavior (no error logged): %v\n", newBehavior(validPattern, testName))
	fmt.Println()

	// Test case 2: Malformed pattern (unbalanced brackets)
	malformedPattern := "[[]"
	fmt.Println("Test 2: Malformed pattern '[[]'")
	fmt.Printf("  Pattern: %s, Name: %s\n", malformedPattern, testName)
	fmt.Println("  Old behavior (ERROR SILENTLY IGNORED):")
	fmt.Printf("    Result: %v (user has NO idea the pattern is broken!)\n", oldBehavior(malformedPattern, testName))
	fmt.Println("  New behavior (ERROR PROPERLY LOGGED):")
	fmt.Printf("    Result: %v (user sees warning about broken pattern)\n", newBehavior(malformedPattern, testName))
	fmt.Println()

	// Test case 3: Another malformed pattern
	malformedPattern2 := "*.["
	fmt.Println("Test 3: Malformed pattern '*.['")
	fmt.Printf("  Pattern: %s, Name: %s\n", malformedPattern2, testName)
	fmt.Println("  Old behavior (ERROR SILENTLY IGNORED):")
	fmt.Printf("    Result: %v (user has NO idea the pattern is broken!)\n", oldBehavior(malformedPattern2, testName))
	fmt.Println("  New behavior (ERROR PROPERLY LOGGED):")
	fmt.Printf("    Result: %v (user sees warning about broken pattern)\n", newBehavior(malformedPattern2, testName))
	fmt.Println()

	fmt.Println("Summary:")
	fmt.Println("  Old behavior: Silently ignores errors, leading to unexpected behavior")
	fmt.Println("  New behavior: Logs errors with clear messages, helping users fix issues")
}
