package render

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type YAML struct{}

func (YAML) Render(w io.Writer, n *Node) error {
	if n == nil || n.kind != Map {
		return errors.New("render: the document is not a mapping")
	}
	var doc strings.Builder
	if err := writeMap(&doc, n.pairs, ""); err != nil {
		return err
	}
	_, err := io.WriteString(w, doc.String())
	return err
}

const (
	indentStep = "  "
	listDash   = "- "
)

func writeMap(doc *strings.Builder, pairs []Pair, indent string) error {
	return writePairs(doc, pairs, indent, indent)
}

func writePairs(doc *strings.Builder, pairs []Pair, firstLinePrefix, indent string) error {
	seen := make(map[string]bool, len(pairs))
	linePrefix := firstLinePrefix
	for _, pair := range pairs {
		if err := checkMappingKey(pair, seen); err != nil {
			return err
		}
		doc.WriteString(linePrefix)
		linePrefix = indent
		writeKey(doc, pair)
		doc.WriteByte(':')
		if err := writeValue(doc, pair.Key, pair.Value, indent+indentStep); err != nil {
			return err
		}
	}
	return nil
}

func writeValue(doc *strings.Builder, key string, n *Node, indent string) error {
	switch {
	case n != nil && n.kind == Text:
		writeTextBlock(doc, n.text, indent)
		return nil
	case n != nil && n.kind == Map && len(n.pairs) > 0:
		doc.WriteByte('\n')
		return writeMap(doc, n.pairs, indent)
	case n != nil && n.kind == List && len(n.items) > 0:
		doc.WriteByte('\n')
		for _, item := range n.items {
			if containsText(item) {
				if err := writeListItem(doc, key, item, indent+listDash, indent+indentStep); err != nil {
					return err
				}
				continue
			}
			doc.WriteString(indent + listDash)
			if err := writeFlow(doc, key, item); err != nil {
				return err
			}
			doc.WriteByte('\n')
		}
		return nil
	}
	doc.WriteByte(' ')
	err := writeFlow(doc, key, n)
	doc.WriteByte('\n')
	return err
}

func writeListItem(doc *strings.Builder, key string, n *Node, dashedPrefix, indent string) error {
	if n.kind != Map || len(n.pairs) == 0 {
		return fmt.Errorf("render: an item under %s is prose rather than a record holding it", Quote(key))
	}
	return writePairs(doc, n.pairs, dashedPrefix, indent)
}

func containsText(n *Node) bool {
	if n == nil {
		return false
	}
	if n.kind == Text {
		return true
	}
	for _, pair := range n.pairs {
		if containsText(pair.Value) {
			return true
		}
	}
	return slices.ContainsFunc(n.items, containsText)
}

func writeFlow(doc *strings.Builder, key string, n *Node) error {
	if n == nil {
		return fmt.Errorf("render: a value under %s is a nil node", Quote(key))
	}
	switch n.kind {
	case Null:
		doc.WriteString("null")
	case Scalar:
		if n.bare {
			doc.WriteString(n.text)
		} else {
			writeQuoted(doc, n.text)
		}
	case Map:
		seen := make(map[string]bool, len(n.pairs))
		doc.WriteByte('{')
		for i, pair := range n.pairs {
			if err := checkMappingKey(pair, seen); err != nil {
				return err
			}
			if i > 0 {
				doc.WriteString(", ")
			}
			writeKey(doc, pair)
			doc.WriteString(": ")
			if err := writeFlow(doc, pair.Key, pair.Value); err != nil {
				return err
			}
		}
		doc.WriteByte('}')
	case List:
		doc.WriteByte('[')
		for i, item := range n.items {
			if i > 0 {
				doc.WriteString(", ")
			}
			if err := writeFlow(doc, key, item); err != nil {
				return err
			}
		}
		doc.WriteByte(']')
	default:
		return fmt.Errorf("render: a value under %s is not a scalar, null, a list or a mapping", Quote(key))
	}
	return nil
}

func CheckKey(key string) error {
	for i, c := range []byte(key) {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', c == '_', c == '$':
		case '0' <= c && c <= '9' && i > 0:
		case '0' <= c && c <= '9':
			return fmt.Errorf("the key %s starts with a digit", Quote(key))
		default:
			return fmt.Errorf("the key %s is not one of ytrack's own names", Quote(key))
		}
	}
	if readsAsBoolOrNullInYAML11(key) {
		return fmt.Errorf("the key %s reads as a bool or null", Quote(key))
	}
	return nil
}

func readsAsBoolOrNullInYAML11(key string) bool {
	switch strings.ToLower(key) {
	case "", "null", "true", "false", "yes", "no", "on", "off", "y", "n":
		return true
	}
	return false
}

func writeKey(doc *strings.Builder, pair Pair) {
	if pair.FromData {
		writeQuoted(doc, pair.Key)
		return
	}
	doc.WriteString(pair.Key)
}

func checkMappingKey(pair Pair, seen map[string]bool) error {
	if !pair.FromData {
		if err := CheckKey(pair.Key); err != nil {
			return fmt.Errorf("render: %w", err)
		}
	}
	if seen[pair.Key] {
		return fmt.Errorf("render: the key %s appears twice in one mapping", Quote(pair.Key))
	}
	seen[pair.Key] = true
	return nil
}

func writeTextBlock(doc *strings.Builder, text, indent string) {
	if !canUseLiteralBlock(text) {
		doc.WriteByte(' ')
		writeQuoted(doc, text)
		doc.WriteByte('\n')
		return
	}
	doc.WriteString(" |" + indentIndicator(text) + chomping(text) + "\n")
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
		if strings.HasSuffix(text, "\n") {
			lines = lines[:len(lines)-1]
		}
	}
	for _, line := range lines {
		if line != "" {
			doc.WriteString(indent)
		}
		doc.WriteString(line + "\n")
	}
}

func indentIndicator(text string) string {
	for _, line := range strings.Split(text, "\n") {
		switch {
		case line == "":
		case line[0] == ' ' || line[0] == '\t':
			return strconv.Itoa(len(indentStep))
		default:
			return ""
		}
	}
	return ""
}

const (
	stripFinalBreaks = "-"
	keepFinalBreaks  = "+"
	clipToOneBreak   = ""
)

func chomping(text string) string {
	onlyLineBreaks := strings.Trim(text, "\n") == ""
	switch {
	case !strings.HasSuffix(text, "\n"):
		return stripFinalBreaks
	case strings.HasSuffix(text, "\n\n"), onlyLineBreaks:
		return keepFinalBreaks
	}
	return clipToOneBreak
}

const (
	nextLine           = 0x85
	lineSeparator      = 0x2028
	paragraphSeparator = 0x2029
	byteOrderMark      = 0xFEFF
)

func canUseLiteralBlock(text string) bool {
	for at := 0; at < len(text); {
		r, size := utf8.DecodeRuneInString(text[at:])
		at += size
		switch {
		case r == utf8.RuneError && size == 1:
			return false
		case r == nextLine, r == lineSeparator, r == paragraphSeparator, r == byteOrderMark:
			return false
		case r == '\t' || r == '\n':
		case 0x20 <= r && r <= 0x7E:
		case 0xA0 <= r && r <= 0xD7FF:
		case 0xE000 <= r && r <= 0xFFFD:
		case 0x10000 <= r:
		default:
			return false
		}
	}
	return true
}

func writeQuoted(doc *strings.Builder, s string) {
	doc.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			doc.WriteString(`\"`)
		case '\\':
			doc.WriteString(`\\`)
		case '\n':
			doc.WriteString(`\n`)
		case '\r':
			doc.WriteString(`\r`)
		case '\t':
			doc.WriteString(`\t`)
		case nextLine:
			doc.WriteString(`\N`)
		case lineSeparator:
			doc.WriteString(`\L`)
		case paragraphSeparator:
			doc.WriteString(`\P`)
		case byteOrderMark, 0xFFFE, 0xFFFF:
			fmt.Fprintf(doc, `\u%04X`, r)
		default:
			if unicode.IsControl(r) {
				fmt.Fprintf(doc, `\x%02X`, r)
			} else {
				doc.WriteRune(r)
			}
		}
	}
	doc.WriteByte('"')
}
