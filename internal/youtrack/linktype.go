package youtrack

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

const linkTypeCatalogue = "[]IssueLinkType"

// As many link types as the catalogue is read with. The specification declares no count of them and $top=-1
// comes back cut at a thousand elsewhere, so the number is written here and an answer that fills it is refused
// rather than taken for the whole.
const linkTypesAtMost = 1000

// linkPhrases is the phrases of the instance by the label each of them is written under, folded to lower case.
// A record of the journal names the end of a link by the translated phrase alone and by no id ytrack can read,
// so the label is the one thing the two have in common; YouTrack writes it capitalized on a record and in lower
// case on the type, and translates it whatever Accept-Language says.
type linkPhrases map[string][]string

func linkTypeFields() []requestedField {
	return []requestedField{
		{name: sourceToTarget},
		{name: targetToSource},
		{name: localizedSourceToTarget},
		{name: localizedTargetToSource},
	}
}

// linkPhrases is the catalogue read from the instance, which is asked for only where a journal may print a
// link: what it is read for is the untranslated phrase, and nothing else of the instance carries one.
func (c *Client) linkPhrases(ctx context.Context, spec *schemas) (linkPhrases, *diag.Fault) {
	a, fault := c.passing(ctx, spec, linkTypeCatalogue, linkTypeFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getIssueLinkTypes(ctx, fields, linkTypesAtMost)
	})
	if fault != nil {
		return nil, fault
	}
	if len(a.objects) >= linkTypesAtMost {
		message := fmt.Sprintf("the instance answered with as many link types as were asked for, %d, so the "+
			"phrases of any past them are missing", linkTypesAtMost)
		return nil, shapeFailure(a.response, a.body, message)
	}
	phrases := linkPhrases{}
	for _, kind := range a.objects {
		for _, end := range [][2]string{
			{sourceToTarget, localizedSourceToTarget},
			{targetToSource, localizedTargetToSource},
		} {
			label, phrase, held, reason := linkEnd(kind, end[0], end[1])
			if reason != "" {
				return nil, shapeFailure(a.response, a.body, reason)
			}
			if held {
				phrases.add(label, phrase)
			}
		}
	}
	return phrases, nil
}

// linkEnd is one end of a link type: the phrase ytrack prints for it and the label a record writes it under,
// which is the translated phrase where the type carries one. A type that is no direction has one end and an
// empty phrase at the other, and an empty phrase is no end rather than a phrase of no words.
func linkEnd(kind map[string]any, plain, translated string) (label, phrase string, held bool, reason string) {
	phrase, reason = linkText(kind, plain)
	if reason != "" || phrase == "" {
		return "", "", false, reason
	}
	label, reason = linkText(kind, translated)
	if reason != "" {
		return "", "", false, reason
	}
	if label == "" {
		label = phrase
	}
	return label, phrase, true, ""
}

// A phrase the instance keeps none of arrives as an empty string from one type and as null from the next, and
// both say the same thing.
func linkText(kind map[string]any, name string) (string, string) {
	switch value := kind[name].(type) {
	case nil:
		return "", ""
	case string:
		return value, ""
	}
	return "", fmt.Sprintf("the %s of a link type of the instance arrived as something other than text", name)
}

// Two ends of two types written alike stand under one label; the same phrase twice is one phrase, since which
// of the two a record meant is a question only where the answers differ.
func (p linkPhrases) add(label, phrase string) {
	folded := strings.ToLower(label)
	if !slices.Contains(p[folded], phrase) {
		p[folded] = append(p[folded], phrase)
	}
}

// phrase is what a label of a record stands for, or the reason it stands for nothing that can be printed.
func (p linkPhrases) phrase(label string) (string, string) {
	found := p[strings.ToLower(label)]
	switch len(found) {
	case 1:
		return found[0], ""
	case 0:
		return "", fmt.Sprintf("an activity arrived for a link the instance writes %s, and no link type of it "+
			"goes by that phrase", render.Quote(label))
	}
	return "", fmt.Sprintf("an activity arrived for a link the instance writes %s, and %d link types of it go "+
		"by that phrase", render.Quote(label), len(found))
}
