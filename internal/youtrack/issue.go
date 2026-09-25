package youtrack

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"unicode/utf8"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

const IssueShowFields = "idReadable,summary,reporter(login),created,updated,resolved,tags(name),customFields," +
	"links(issues(idReadable,summary)),description"

const IssueListFields = "idReadable,summary,customFields(State,Type),created"

const (
	issueSchema   = "Issue"
	idReadableKey = "idReadable"
	issuesPlural  = "issues"
	countSchema   = "IssueCountResponse"
	countKey      = "count"
)

const stillCounting = -1

func ListIssues(query string, expression *string, page Page, warn WarnFunc) (Call, *diag.Fault) {
	if fault := rejectUnreadableQuery(query); fault != nil {
		return nil, fault
	}
	if fault := page.parse(); fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := issueFields(spec, expression, IssueListFields, issueCommentTarget().commentsOfAList())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.listIssues(ctx, spec, query, requested, page, warn)
	}, nil
}

func issueFields(spec *schemas, expression *string, defaults, comments string) ([]requestedField, *diag.Fault) {
	written, requested, fault := fieldsOrDefault(expression, defaults, true)
	if fault != nil {
		return nil, fault
	}
	expandBareCustomFields(spec, requested)
	if fault := issueCommentTarget().reject(spec, written, requested, comments); fault != nil {
		return nil, fault
	}
	return requested, rejectIssueBlocks(spec, composedIssue(), written, requested)
}

func rejectUnreadableQuery(query string) *diag.Fault {
	if utf8.ValidString(query) {
		return nil
	}
	message := "the query is no valid UTF-8, and YouTrack reads a search as text"
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

func (c *Client) listIssues(ctx context.Context, spec *schemas, query string, requested []requestedField, page Page, warn WarnFunc) (*render.Node, *diag.Fault) {
	marked, fault := c.searchMarkup(ctx, spec, query)
	if fault != nil {
		return nil, fault
	}
	if warning := freeTextWarning(query, marked); warning != nil {
		warn(warning)
	}
	asked, named, fault := c.issueRequest(ctx, spec, requested)
	if fault != nil {
		return nil, fault
	}
	selection := list{
		client:    c,
		spec:      spec,
		plural:    issuesPlural,
		schema:    "[]" + issueSchema,
		requested: requested,
		page:      page,
		fetchPage: func(ctx context.Context, fields string, w window) (*http.Response, error) {
			return c.apiGetIssues(ctx, query, fields, named, w)
		},
		sentFields: asked,
		countTotal: func(ctx context.Context) (count, *diag.Fault) { return c.countIssuesWithRetry(ctx, spec, query) },
	}
	return selection.fetch(ctx)
}

func (c *Client) countIssuesWithRetry(ctx context.Context, spec *schemas, query string) (count, *diag.Fault) {
	found, fault := c.countIssues(ctx, spec, query)
	if fault != nil || found.known {
		return found, fault
	}
	return c.countIssues(ctx, spec, query)
}

func (c *Client) countIssues(ctx context.Context, spec *schemas, query string) (count, *diag.Fault) {
	body := searchBody(query)
	decoded, fault := c.request(ctx, spec, countSchema, []requestedField{{name: countKey}}, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiCountIssues(ctx, body, fields)
	})
	if fault != nil {
		return count{}, fault
	}
	found, whole := parseInt64(decoded.objects[0][countKey])
	switch {
	case !whole || found < stillCounting:
		message := "the count of the issues the search finds is neither a whole number of them nor -1 for a " +
			"count that is not ready"
		return count{}, shapeFailure(decoded.httpResponse, decoded.body, message)
	case found == stillCounting:
		return count{}, nil
	}
	return counted(int(found)), nil
}

func ShowIssue(id string, expression *string, comments Comments) (Call, *diag.Fault) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := issueFields(spec, expression, IssueShowFields, commentsOfAShow)
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.showIssue(ctx, spec, id, requested, comments)
	}, nil
}

func DeleteIssue(id string) (Call, *diag.Fault) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.deleteOwner(ctx, spec, issueOwner, issueSchema, func(ctx context.Context, fields string) (*http.Response, error) {
			return c.apiGetIssue(ctx, id, fields, nil)
		}, c.apiDeleteIssue)
	}, nil
}

func rejectCustomFieldNames(spec *schemas, at, expression string, requested []requestedField) *diag.Fault {
	var fault *diag.Fault
	eachCustomFields(spec, at, requested, func(parents []string, field *requestedField) {
		if field.children == nil || fault != nil {
			return
		}
		if at != issueSchema || len(parents) > 0 {
			fault = rejectBareCustomFields(at, expression, fieldPath(parents, field.name))
			return
		}
		for _, name := range field.children {
			if name.children == nil {
				continue
			}
			message := fmt.Sprintf("fields %s: %s names a custom field of the issue, which is printed as the "+
				"one value it holds, so no name stands under it",
				render.Quote(expression), fieldPath([]string{field.name}, formatName(name)))
			fault = &diag.Fault{Code: diag.BadUsage, Message: message}
			return
		}
	})
	return fault
}

func rejectBareCustomFields(at, expression, path string) *diag.Fault {
	message := fmt.Sprintf("fields %s: %s holds the custom fields of another issue, which are printed whole, so "+
		"no name stands under it; only the custom fields of the issue asked for are named one by one",
		render.Quote(expression), path)
	if at != issueSchema {
		message = fmt.Sprintf("fields %s: %s holds the custom fields of the issue, which are printed whole, so "+
			"no name stands under it; a custom field is named one by one in ytrack issue show", render.Quote(expression), path)
	}
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

func rejectQuotedNames(spec *schemas, at, expression string, requested []requestedField) *diag.Fault {
	own := ownCustomFields(spec, at, requested)
	var reject func(fields []requestedField, parents []string, named bool) *diag.Fault
	reject = func(fields []requestedField, parents []string, named bool) *diag.Fault {
		for i := range fields {
			field := &fields[i]
			written := formatName(*field)
			if field.quoted && !named {
				message := fmt.Sprintf("fields %s: %s is a name in double quotes, which is how a custom field "+
					"of the issue is named, and such a name stands only under customFields of the issue asked for",
					render.Quote(expression), fieldPath(parents, written))
				return &diag.Fault{Code: diag.BadUsage, Message: message}
			}
			if fault := reject(field.children, append(slices.Clip(parents), written), field == own); fault != nil {
				return fault
			}
		}
		return nil
	}
	return reject(requested, nil, false)
}

func expandBareCustomFields(spec *schemas, requested []requestedField) {
	eachCustomFields(spec, issueSchema, requested, func(_ []string, field *requestedField) {
		if field.bare {
			field.children = nil
		}
	})
}

type blockSchema struct{ schema string }

func composedIssue() blockSchema    { return blockSchema{issueSchema} }
func composedWorkItem() blockSchema { return blockSchema{workItemSchema} }

func composedSchemas() []blockSchema {
	return []blockSchema{composedIssue(), composedWorkItem()}
}

func hasIssueBlocks(schema string) bool {
	return slices.ContainsFunc(composedSchemas(), func(at blockSchema) bool { return at.schema == schema })
}

func issueBlocks(spec *schemas, at blockSchema, asked []requestedField) {
	matchTranslatedNames := hasDefaultNames(spec, asked)
	eachCustomFields(spec, at.schema, asked, func(parents []string, field *requestedField) {
		field.children = customFieldsAsked(matchTranslatedNames && len(parents) == 0)
	})
	eachIssueLink(spec, at.schema, asked, func(_ []string, field *requestedField) { field.children = linkRequestFields(field.children) })
	eachAttributes(spec, at.schema, asked, func(_ []string, field *requestedField) { field.children = attributesAsked() })
}

func rejectIssueBlocks(spec *schemas, at blockSchema, expression string, requested []requestedField) *diag.Fault {
	if fault := rejectQuotedNames(spec, at.schema, expression, requested); fault != nil {
		return fault
	}
	if fault := rejectCustomFieldNames(spec, at.schema, expression, requested); fault != nil {
		return fault
	}
	if fault := rejectLinkParts(spec, at.schema, expression, requested); fault != nil {
		return fault
	}
	return rejectAttributeNames(spec, at.schema, expression, requested)
}

func eachCustomFields(spec *schemas, at string, requested []requestedField, visit func(parents []string, field *requestedField)) {
	fieldsNamed(spec, at, issueSchema, customFieldsKey, requested, nil, visit)
}

func ownCustomFields(spec *schemas, at string, requested []requestedField) *requestedField {
	var own *requestedField
	eachCustomFields(spec, at, requested, func(parents []string, field *requestedField) {
		if len(parents) == 0 {
			own = field
		}
	})
	return own
}

func namedCustomFields(spec *schemas, requested []requestedField) *requestedField {
	own := ownCustomFields(spec, issueSchema, requested)
	if own == nil || own.children == nil {
		return nil
	}
	return own
}

func (c *Client) showIssue(ctx context.Context, spec *schemas, id string, requested []requestedField, comments Comments) (*render.Node, *diag.Fault) {
	asked, named, fault := c.issueRequest(ctx, spec, requested)
	if fault != nil {
		return nil, fault
	}
	held := issueCommentTarget()
	decoded, fault := c.request(ctx, spec, issueSchema, comments.merged(held, asked), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetIssue(ctx, id, fields, named)
	})
	if fault != nil {
		return nil, fault
	}
	issue := decoded.objects[0]
	own, fault := comments.pair(held, decoded, issue)
	if fault != nil {
		return nil, fault
	}
	return objectNode(decoded, requested, issue, own)
}

func (c *Client) issueRequest(ctx context.Context, spec *schemas, requested []requestedField) ([]requestedField, []string, *diag.Fault) {
	if fault := c.resolveCustomFields(ctx, spec, requested); fault != nil {
		return nil, nil, fault
	}
	asked := cloneFields(requested)
	issueBlocks(spec, composedIssue(), asked)
	return asked, customFieldsFilter(spec, requested), nil
}

func customFieldsFilter(spec *schemas, requested []requestedField) []string {
	named := namedCustomFields(spec, requested)
	if named == nil || filterReachesOtherBlocks(spec, requested) {
		return nil
	}
	names := make([]string, 0, len(named.children))
	for _, name := range named.children {
		names = append(names, name.name)
	}
	return names
}

func filterReachesOtherBlocks(spec *schemas, requested []requestedField) bool {
	blocks := 0
	eachCustomFields(spec, issueSchema, requested, func(_ []string, _ *requestedField) { blocks++ })
	return blocks > 1
}
