package render

import (
	"strconv"
	"strings"
)

// Quote is how a message names a word it quotes — a command, a flag, an id, a value the caller gave or the
// server sent: in backticks, which the double-quoted string a message is printed as carries without escapes.
// Everything else is escaped as Go escapes it, so a line break or a byte that is no UTF-8 stays visible, and a
// backtick inside the word is written \`.
func Quote(s string) string {
	escaped := strconv.Quote(s)
	escaped = strings.ReplaceAll(escaped[1:len(escaped)-1], `\"`, `"`)
	return "`" + strings.ReplaceAll(escaped, "`", "\\`") + "`"
}
