package cli

import "os"

// A console on stdin is its input buffer, which takes no writes; the screen the answers are echoed on is CONOUT$.
func promptsOn(*os.File) (*os.File, error) {
	return os.OpenFile("CONOUT$", os.O_WRONLY, 0)
}
