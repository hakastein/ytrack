package cli

import "os"

const consoleScreen = "CONOUT$"

func promptsOn(*os.File) (*os.File, error) {
	return os.OpenFile(consoleScreen, os.O_WRONLY, 0)
}
