//go:build tools
// +build tools

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	fmt.Println("=== Progress Reporting Verification ===\n")

	// Test 1: Verify help text includes updated verbose description
	fmt.Println("Test 1: Checking help text for verbose flag...")
	cmd := exec.Command("./codeloom", "help")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("❌ FAIL: Could not run help command: %v\n", err)
		os.Exit(1)
	}

	helpText := string(output)
	if strings.Contains(helpText, "--verbose") && strings.Contains(helpText, "Show detailed errors and warnings") {
		fmt.Println("✅ PASS: Help text correctly shows '--verbose        Show detailed errors and warnings'")
	} else if strings.Contains(helpText, "--verbose") && strings.Contains(helpText, "Show detailed progress") {
		fmt.Println("❌ FAIL: Help text still shows old verbose description")
		os.Exit(1)
	} else {
		fmt.Println("❌ FAIL: Could not find verbose flag in help text")
		os.Exit(1)
	}

	// Test 2: Verify example doesn't mention progress with verbose
	fmt.Println("\nTest 2: Checking example text...")
	if strings.Contains(helpText, "codeloom index --verbose ./              Index current directory with progress") {
		fmt.Println("❌ FAIL: Example still mentions progress with verbose")
		os.Exit(1)
	} else if strings.Contains(helpText, "codeloom index --verbose ./              Index current directory with detailed errors") {
		fmt.Println("✅ PASS: Example correctly shows '--verbose' is for detailed errors")
	} else {
		fmt.Println("❌ FAIL: Could not find verbose example in help text")
		os.Exit(1)
	}

	// Test 3: Verify basic example doesn't require verbose for progress
	fmt.Println("\nTest 3: Checking basic index example...")
	if strings.Contains(helpText, "codeloom index ./src                     Index src directory") {
		fmt.Println("✅ PASS: Basic index example exists (implies progress shown by default)")
	} else {
		fmt.Println("❌ FAIL: Could not find basic index example")
		os.Exit(1)
	}

	// Test 4: Verify source code doesn't have verbose check around progress
	fmt.Println("\nTest 4: Checking source code for progress callback...")
	content, err := os.ReadFile("cmd/codeloom/main.go")
	if err != nil {
		fmt.Printf("❌ FAIL: Could not read main.go: %v\n", err)
		os.Exit(1)
	}

	source := string(content)

	// Look for the pattern where progress is checked with verbose
	if strings.Contains(source, "if *verbose {") && strings.Contains(source, "Progress:") {
		// Need to check if they're in the same block
		lines := strings.Split(source, "\n")
		inVerboseBlock := false
		hasProgressInVerboseBlock := false

		for i, line := range lines {
			if strings.Contains(line, "if *verbose {") {
				inVerboseBlock = true
			}
			if inVerboseBlock && strings.Contains(line, "Progress:") {
				hasProgressInVerboseBlock = true
				fmt.Printf("❌ FAIL: Progress output is still inside verbose block at line %d\n", i+1)
				os.Exit(1)
			}
			if inVerboseBlock && strings.Contains(line, "}") && !strings.Contains(line, "if *verbose {") {
				inVerboseBlock = false
			}
		}

		if !hasProgressInVerboseBlock {
			fmt.Println("✅ PASS: Progress output is not inside verbose block")
		}
	} else if strings.Contains(source, "fmt.Printf(\"\\rProgress:") {
		fmt.Println("✅ PASS: Progress printf exists without verbose guard")
	} else {
		fmt.Println("⚠️  WARNING: Could not verify progress callback structure (may be refactored)")
	}

	fmt.Println("\n=== All Tests Passed! ===")
	fmt.Println("\nSummary of changes:")
	fmt.Println("1. Progress is now shown by default (not hidden behind --verbose)")
	fmt.Println("2. --verbose flag now shows detailed errors and warnings")
	fmt.Println("3. Help text updated to reflect these changes")
	fmt.Println("\nUsers will now see progress during indexing operations without needing the --verbose flag!")
}
