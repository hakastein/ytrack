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

	"github.com/hakastein/go-youtrack"
)

type YAML struct{}

func (YAML) Render(w io.Writer, n *youtrack.Node) error {
	if n == nil || n.Kind() != youtrack.MapNode {
		return errors.New("render: the document is not a mapping")
	}
	var doc strings.Builder
	if err := writeMap(&doc, n.Pairs(), ""); err != nil {
		return err
	}
	_, err := io.WriteString(w, doc.String())
	return err
}

const (
	indentStep = "  "
	listDash   = "- "
)

func writeMap(doc *strings.Builder, pairs []youtrack.Pair, indent string) error {
	return writePairs(doc, pairs, indent, indent)
}

func writePairs(doc *strings.Builder, pairs []youtrack.Pair, firstLinePrefix, indent string) error {
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

func writeValue(doc *strings.Builder, key string, n *youtrack.Node, indent string) error {
	switch {
	case n != nil && n.Kind() == youtrack.TextNode:
		writeTextBlock(doc, n.Value(), indent)
		return nil
	case n != nil && n.Kind() == youtrack.MapNode && len(n.Pairs()) > 0:
		doc.WriteByte('\n')
		return writeMap(doc, n.Pairs(), indent)
	case n != nil && n.Kind() == youtrack.ListNode && len(n.Items()) > 0:
		doc.WriteByte('\n')
		for _, item := range n.Items() {
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

func writeListItem(doc *strings.Builder, key string, n *youtrack.Node, dashedPrefix, indent string) error {
	if n.Kind() != youtrack.MapNode || len(n.Pairs()) == 0 {
		return fmt.Errorf("render: an item under %s is prose rather than a record holding it", Quote(key))
	}
	return writePairs(doc, n.Pairs(), dashedPrefix, indent)
}

func containsText(n *youtrack.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind() == youtrack.TextNode {
		return true
	}
	for _, pair := range n.Pairs() {
		if containsText(pair.Value) {
			return true
		}
	}
	return slices.ContainsFunc(n.Items(), containsText)
}

func writeFlow(doc *strings.Builder, key string, n *youtrack.Node) error {
	if n == nil {
		return fmt.Errorf("render: a value under %s is a nil node", Quote(key))
	}
	switch n.Kind() {
	case youtrack.NullNode:
		doc.WriteString("null")
	case youtrack.NumberNode, youtrack.BoolNode:
		doc.WriteString(n.Value())
	case youtrack.StringNode:
		writeQuoted(doc, n.Value())
	case youtrack.MapNode:
		seen := make(map[string]bool, len(n.Pairs()))
		doc.WriteByte('{')
		for i, pair := range n.Pairs() {
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
	case youtrack.ListNode:
		doc.WriteByte('[')
		for i, item := range n.Items() {
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

func writeKey(doc *strings.Builder, pair youtrack.Pair) {
	if pair.FromData {
		writeQuoted(doc, pair.Key)
		return
	}
	doc.WriteString(pair.Key)
}

func checkMappingKey(pair youtrack.Pair, seen map[string]bool) error {
	if !pair.FromData {
		if err := youtrack.CheckKey(pair.Key); err != nil {
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
