package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Run looks for the scripts of a project upward from the working directory, so the package runs in a directory
// whose own empty root ends that search before the directories above the checkout.
func TestMain(m *testing.M) {
	os.Exit(runIsolated(m))
}

func runIsolated(m *testing.M) int {
	dir, err := os.MkdirTemp("", "ytrack-cli-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(dir)
	if err := os.MkdirAll(filepath.Join(dir, ".ytrack", "scripts"), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.Chdir(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}
