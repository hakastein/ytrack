package youtrack

import (
	"context"
	"net/http"
	"strings"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

const (
	suggestionsSchema = "SearchSuggestions"
	queryKey          = "query"
	styleRangesKey    = "styleRanges"
	startKey          = "start"
	lengthKey         = "length"
	styleKey          = "style"
	freeTextKey       = "free_text"
)

// The style of a stretch the server made no field name, value or operator of: words it looks for in the text of
// the issues instead. The other styles it gives are of a search it read, so none of them is warned about.
const textStyle = "text"

const freeTextMessage = "part of the search names no field of the instance and is looked for as text"

// A Warn is where a call says what it does not stop for. The command that warns is handed one, so nothing in
// this package knows where a warning is printed.
type Warn func(*diag.Warning)

// A stretch of the caller's search the server gave a style to. start and length count units of UTF-16, the way
// the server counts them, and neither runes nor bytes.
type styleRange struct {
	start  int
	length int
	style  string
}

// The names of the markup are ytrack's own: the specification declares neither styleRanges nor the members of
// a range, though the server answers both.
func markupFields() []requestedField {
	return []requestedField{
		{name: queryKey},
		{name: styleRangesKey, children: []requestedField{{name: startKey}, {name: lengthKey}, {name: styleKey}}},
	}
}

// markUp is the search as the server reads it: which stretch of the text is a field name, a value, an operator,
// free text or an error. It is asked for every search ytrack runs, before the search runs, so a selection is
// never printed over a query nothing was said about; ytrack reads no token of the query language itself.
func (c *Client) markUp(ctx context.Context, spec *schemas, query string) ([]styleRange, *diag.Fault) {
	body := searchBody(query)
	answer, fault := c.request(ctx, spec, suggestionsSchema, markupFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.assistSearch(ctx, body, fields)
	})
	if fault != nil {
		return nil, fault
	}
	marked := answer.objects[0]
	// The offsets point into the text the server read back, so a markup of any other text says nothing of this one.
	if echoed, isText := marked[queryKey].(string); !isText || echoed != query {
		message := "the search the markup came back with is not the one that was sent"
		return nil, shapeFailure(answer.response, answer.body, message)
	}
	arrived, isList := marked[styleRangesKey].([]any)
	if !isList {
		message := "the styled ranges of the search arrived as something other than an array"
		return nil, shapeFailure(answer.response, answer.body, message)
	}
	within := utf16Units(query)
	ranges := make([]styleRange, 0, len(arrived))
	for _, item := range arrived {
		styled, isObject := item.(map[string]any)
		if !isObject {
			message := "a styled range of the search arrived as something other than an object"
			return nil, shapeFailure(answer.response, answer.body, message)
		}
		start, startIsWhole := wholeNumber(styled[startKey])
		length, lengthIsWhole := wholeNumber(styled[lengthKey])
		style, styleIsText := styled[styleKey].(string)
		switch {
		case !startIsWhole || !lengthIsWhole:
			message := "where a styled range of the search begins, or how far it runs on, is no whole number"
			return nil, shapeFailure(answer.response, answer.body, message)
		case !styleIsText:
			message := "the style of a range of the search arrived as something other than text"
			return nil, shapeFailure(answer.response, answer.body, message)
		// The end is measured backwards from the text, since two counts the server chose could overflow added up.
		case start < 0 || length < 0 || start > int64(within)-length:
			message := "a styled range of the search lies outside the text that was sent"
			return nil, shapeFailure(answer.response, answer.body, message)
		}
		ranges = append(ranges, styleRange{start: int(start), length: int(length), style: style})
	}
	return ranges, nil
}

// freeTextWarning is one warning for the whole search, or none where the server read every word of it. What the
// markup says is the server's own way of saying it, so neither a style, an offset nor styleRanges is printed:
// the document carries the search and the parts of it the caller is to look at.
func freeTextWarning(query string, ranges []styleRange) *diag.Warning {
	parts := freeText(query, ranges)
	if len(parts) == 0 {
		return nil
	}
	written := make([]*render.Node, 0, len(parts))
	for _, part := range parts {
		written = append(written, render.NewString(part))
	}
	details := []render.Pair{
		{Key: queryKey, Value: render.NewString(query)},
		{Key: freeTextKey, Value: render.NewList(written...)},
	}
	return &diag.Warning{Code: diag.UnknownName, Message: freeTextMessage, Details: details}
}

// freeText is those stretches as the caller wrote them: ranges that run into one another are one part, since the
// server marks a name it does not know and the colon after it as two while the caller wrote one word. A range of
// no length marks nothing and gives no part of its own.
func freeText(query string, ranges []styleRange) []string {
	var parts []string
	after := -1
	for _, marked := range ranges {
		if marked.style != textStyle || marked.length == 0 {
			continue
		}
		part := utf16Substring(query, marked.start, marked.length)
		if marked.start == after {
			parts[len(parts)-1] += part
		} else {
			parts = append(parts, part)
		}
		after = marked.start + marked.length
	}
	return parts
}

// A stretch of the search cut where the markup counts, in units of UTF-16. A rune outside the basic plane is
// taken whole or left out: half a surrogate pair is no character the caller wrote.
func utf16Substring(text string, start, length int) string {
	var cut strings.Builder
	units := 0
	for _, r := range text {
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if units >= start && units+width <= start+length {
			cut.WriteRune(r)
		}
		units += width
	}
	return cut.String()
}

// The server counts the offsets of a markup in units of UTF-16: a rune outside the basic plane takes two of
// them, so counting runes instead leaves everything written after one an offset short.
func utf16Units(text string) int {
	units := 0
	for _, r := range text {
		units++
		if r > 0xFFFF {
			units++
		}
	}
	return units
}
