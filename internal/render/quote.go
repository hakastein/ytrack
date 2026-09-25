package render

import (
	"strconv"
	"strings"
)

func Quote(s string) string {
	goQuoted := strconv.Quote(s)
	escaped := strings.ReplaceAll(goQuoted[1:len(goQuoted)-1], `\"`, `"`)
	return "`" + strings.ReplaceAll(escaped, "`", "\\`") + "`"
}
