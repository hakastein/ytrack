//go:build !windows

package cli

import "os"

// A terminal on stdin is opened for reading and writing both, so the prompts go back on the file that is read.
func promptsOn(stdin *os.File) (*os.File, error) {
	return stdin, nil
}
