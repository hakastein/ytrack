//go:build !windows

package cli

import "os"

func promptsOn(readWriteTerminal *os.File) (*os.File, error) {
	return readWriteTerminal, nil
}
