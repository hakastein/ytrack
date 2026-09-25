package render

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type YAML struct{}

func (YAML) Render(w io.Writer, n *Node) error {
	if n == nil || n.kind != Map {
		return errors.New("render: the document is not a mapping")
	}
	// Built whole before the first write, so a node it cannot print leaves nothing
	// half-written.
	var doc strings.Builder
	if err := writeMap(&doc, n.pairs, ""); err != nil {
		return err
	}
	_, err := io.WriteString(w, doc.String())
	return err
}

func writeMap(doc *strings.Builder, pairs []Pair, indent string) error {
	return writePairs(doc, pairs, indent, true)
}

// writePairs is a block mapping, a key to a line. indentFirst is false for the one mapping whose first key
// stands on a line already begun: a record of a list, which the dash before it has indented.
func writePairs(doc *strings.Builder, pairs []Pair, indent string, indentFirst bool) error {
	seen := make(map[string]bool, len(pairs))
	for i, pair := range pairs {
		if err := checkMappingKey(pair, seen); err != nil {
			return err
		}
		if i > 0 || indentFirst {
			doc.WriteString(indent)
		}
		writeKey(doc, pair)
		doc.WriteByte(':')
		if err := writeValue(doc, pair.Key, pair.Value, indent+"  "); err != nil {
			return err
		}
	}
	return nil
}

// A mapping or a list with something in it goes on the lines under its key, a list one flow item a line, so that
// grep takes a record whole; anything else stays on the line of its key.
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
			doc.WriteString(indent + "- ")
			if containsText(item) {
				if err := writeListItem(doc, key, item, indent+"  "); err != nil {
					return err
				}
				continue
			}
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

func writeListItem(doc *strings.Builder, key string, n *Node, indent string) error {
	if n.kind != Map || len(n.pairs) == 0 {
		return fmt.Errorf("render: an item under %s is prose rather than a record holding it", Quote(key))
	}
	return writePairs(doc, n.pairs, indent, false)
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

// key names the pair the value stands under in an error.
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

// CheckKey refuses a key a reader would not read back as the name it is (ADR-0003): a bare key keeps to the
// characters of the specification's names, starts with no digit and is no word a reader takes for a bool or null.
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
	switch strings.ToLower(key) {
	case "", "null", "true", "false", "yes", "no", "on", "off", "y", "n":
		return fmt.Errorf("the key %s reads as a bool or null", Quote(key))
	}
	return nil
}

// A key from the data is written by the writer of double-quoted strings, the one that writes the values, so a
// name holding a colon, a quote or a word a reader takes for a bool comes back as the name it is.
func writeKey(doc *strings.Builder, pair Pair) {
	if pair.FromData {
		writeQuoted(doc, pair.Key)
		return
	}
	doc.WriteString(pair.Key)
}

// The grammar is what a key of ytrack's own is held to; a key from the data is held only to standing once,
// which is the last guard against a mapping a reader would read back with a key missing.
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
		// The last field of a text that ends in a line break is what follows that break: nothing.
		if strings.HasSuffix(text, "\n") {
			lines = lines[:len(lines)-1]
		}
	}
	for _, line := range lines {
		// An empty line carries no indentation: spaces on it would be text of the block.
		if line != "" {
			doc.WriteString(indent)
		}
		doc.WriteString(line + "\n")
	}
}

// A reader takes the indentation of a block from its first non-empty line — a line of spaces alone counts,
// an empty line does not — so such a line beginning with a space loses it and one beginning with a tab is
// refused outright. The indicator is always 2: nesting steps by two, and a reader counts it from the parent.
func indentIndicator(text string) string {
	for _, line := range strings.Split(text, "\n") {
		switch {
		case line == "":
		case line[0] == ' ' || line[0] == '\t':
			return "2"
		default:
			return ""
		}
	}
	return ""
}

// The chomping indicator settles the line breaks at the end of the block: - for a text that ends in none,
// + for a text of line breaks alone or one ending in two or more, and the default, which keeps one, for the
// rest. A text that is one line break is kept rather than clipped, since clipping would read it as "".
func chomping(text string) string {
	switch {
	case !strings.HasSuffix(text, "\n"):
		return "-"
	case strings.HasSuffix(text, "\n\n"), strings.Trim(text, "\n") == "":
		return "+"
	}
	return ""
}

// A literal block carries every rune raw, so it may hold only what a reader reads back as that rune: tab,
// line feed and the printable runes, less NEL, LS and PS, which a YAML 1.1 reader takes for line breaks,
// and less the BOM, which may not stand inside a document. A byte that is no UTF-8 is no rune at all.
func canUseLiteralBlock(text string) bool {
	for at := 0; at < len(text); {
		r, size := utf8.DecodeRuneInString(text[at:])
		at += size
		switch {
		case r == utf8.RuneError && size == 1:
			return false
		case r == '\t' || r == '\n':
		case 0x20 <= r && r <= 0x7E:
		case 0xA0 <= r && r <= 0xD7FF && r != 0x2028 && r != 0x2029:
		case 0xE000 <= r && r <= 0xFFFD && r != 0xFEFF:
		case 0x10000 <= r:
		default:
			return false
		}
	}
	return true
}

// NEL, LS and PS are line breaks to a YAML 1.1 reader, a BOM may not stand inside a
// document, and nothing outside c-printable may stand raw.
func writeQuoted(doc *strings.Builder, s string) {
	doc.WriteByte('"')
	// Ranging over a string yields U+FFFD for each byte that is not UTF-8: the same
	// replacement encoding/json makes in everything the server sends.
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
		case 0x85:
			doc.WriteString(`\N`)
		case 0x2028:
			doc.WriteString(`\L`)
		case 0x2029:
			doc.WriteString(`\P`)
		case 0xFEFF, 0xFFFE, 0xFFFF:
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
