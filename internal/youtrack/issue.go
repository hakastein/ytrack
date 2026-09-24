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

// The identity of an issue, what it is about, who filed it and how it is marked, and last the prose. A user
// with no role on the project is sent each of these, so no key of the default costs a reader their document.
// A name that reaches the expression through the default alone is resolved against nothing, so this costs no
// reading of the instance's catalogue whatever custom field it comes to name.
const IssueShowFields = "idReadable,summary,reporter(login),created,updated,resolved,tags(name),customFields," +
	"links(issues(idReadable,summary)),description"

// A record of a selection is one line, and these answer what every reader of a search asks first: which issue it
// is, what it is about, where it stands and when it was filed. The two custom fields are named rather than taken
// with the block whole: the block holds up to thirty fields and costs six times the wire, while a name costs none
// of it and no request either, since a name of the default is resolved against nothing.
const IssueListFields = "idReadable,summary,customFields(State,Type),created"

const (
	issueSchema   = "Issue"
	idReadableKey = "idReadable"
	issuesPlural  = "issues"
	countSchema   = "IssueCountResponse"
	countKey      = "count"
)

// What the counter answers while it is still counting: the count is running and has no number yet.
const stillCounting = -1

// ListIssues is the call for one page of the issues the search query finds, with the fields of expression, or
// with them added to IssueListFields when it starts with +; nil is the caller leaning on the default whole.
// What the server made text of the search is said through warn.
func ListIssues(query string, expression *string, page Page, warn Warn) (Call, *diag.Fault) {
	if fault := refuseUnreadableQuery(query); fault != nil {
		return nil, fault
	}
	if fault := page.parse(); fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := issueFields(spec, expression, IssueListFields, issueComments().commentsOfAList())
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.listIssues(ctx, spec, query, requested, page, warn)
	}, nil
}

// issueFields is an expression of a command that prints issues, read and held to what an issue may be asked
// for, with every name of it marked by the hand that wrote it. A nil expression is the caller leaning on the
// default whole, and a name that reached the tree through the default alone is resolved against nothing, so a
// default costs no request of its own and fails no token with no role on a project.
func issueFields(spec *schemas, expression *string, defaults, comments string) ([]requestedField, *diag.Fault) {
	written, requested, fault := theExpression(expression, defaults, true)
	if fault != nil {
		return nil, fault
	}
	takeCustomFieldsWhole(spec, requested)
	if fault := issueComments().refuse(spec, written, requested, comments); fault != nil {
		return nil, fault
	}
	return requested, refuseIssueBlocks(spec, composedIssue(), written, requested)
}

// The search is the caller's and nothing of it is read here, but it has to be text: the server answers bytes
// that are no UTF-8 in a URL with a 500 naming a 400, and the body of the counter would carry them as U+FFFD,
// so the two requests would ask different things.
func refuseUnreadableQuery(query string) *diag.Fault {
	if utf8.ValidString(query) {
		return nil
	}
	message := "the query is no valid UTF-8, and YouTrack reads a search as text"
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

func (c *Client) listIssues(ctx context.Context, spec *schemas, query string, requested []requestedField, page Page, warn Warn) (*render.Node, *diag.Fault) {
	marked, fault := c.markUp(ctx, spec, query)
	if fault != nil {
		return nil, fault
	}
	// Said before the search goes out, so a caller whose search the server then refuses is told what their words
	// were taken for as well.
	if warning := freeTextWarning(query, marked); warning != nil {
		warn(warning)
	}
	asked, named, fault := c.askOfAnIssue(ctx, spec, requested)
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
		ask: func(ctx context.Context, fields string, w window) (*http.Response, error) {
			return c.getIssues(ctx, query, fields, named, w)
		},
		filledIn: asked,
		counting: func(ctx context.Context) (count, *diag.Fault) { return c.issuesCounted(ctx, spec, query) },
	}
	return selection.selected(ctx)
}

// issuesCounted asks the counter once more, straight away, where it answered -1, and once is all: an instance
// that answers -1 answers a number milliseconds later, so a pace of ytrack's own would be an invention. The -1
// arrives under a 200 of the shape the counter promises, so the repeat follows its protocol rather than retrying
// a request that failed; a failure is never sent twice, on either call. A second -1 is a total that is not known.
func (c *Client) issuesCounted(ctx context.Context, spec *schemas, query string) (count, *diag.Fault) {
	found, fault := c.issuesFound(ctx, spec, query)
	if fault != nil || found.known {
		return found, fault
	}
	return c.issuesFound(ctx, spec, query)
}

// issuesFound is how many issues the search finds, which the server counts itself: a second page over ids alone
// costs hundreds of kilobytes of a live selection where this answer costs forty bytes.
//
// A count of -1 is the counter saying it has started and has no number yet.
func (c *Client) issuesFound(ctx context.Context, spec *schemas, query string) (count, *diag.Fault) {
	body := searchBody(query)
	answer, fault := c.request(ctx, spec, countSchema, []requestedField{{name: countKey}}, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.countIssues(ctx, body, fields)
	})
	if fault != nil {
		return count{}, fault
	}
	found, whole := wholeNumber(answer.objects[0][countKey])
	switch {
	case !whole || found < stillCounting:
		message := "the count of the issues the search finds is neither a whole number of them nor -1 for a " +
			"count that is not ready"
		return count{}, shapeFailure(answer.response, answer.body, message)
	case found == stillCounting:
		return count{}, nil
	}
	return counted(int(found)), nil
}

// ShowIssue is the call for the issue of that id with the fields of expression, or with them added to
// IssueShowFields when it starts with +; nil is the caller leaning on the default whole. As many of its
// comments are printed as comments asks for.
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

// DeleteIssue is the call that deletes the issue of that id and prints the id the server knows it by.
func DeleteIssue(id string) (Call, *diag.Fault) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.deletion(ctx, spec, issueOwner, issueSchema, func(ctx context.Context, fields string) (*http.Response, error) {
			// No custom field is named: the id is the whole of what a deletion prints.
			return c.getIssue(ctx, id, fields, nil)
		}, c.deleteIssue)
	}, nil
}

// Under customFields of the issue asked for stand the names the project gave its custom fields, one key each.
// A name has nothing below it — a custom field is printed as the one value it holds — and the custom fields
// of any other issue are printed whole, since a name there would pick fields out of a different project.
func refuseCustomFieldNames(spec *schemas, at, expression string, requested []requestedField) *diag.Fault {
	var fault *diag.Fault
	eachCustomFields(spec, at, requested, func(parents []string, field *requestedField) {
		if field.children == nil || fault != nil {
			return
		}
		if at != issueSchema || len(parents) > 0 {
			fault = refuseWholeCustomFields(at, expression, fieldPath(parents, field.name))
			return
		}
		for _, name := range field.children {
			if name.children == nil {
				continue
			}
			message := fmt.Sprintf("fields %s: %s names a custom field of the issue, which is printed as the "+
				"one value it holds, so no name stands under it",
				render.Quote(expression), fieldPath([]string{field.name}, writtenName(name)))
			fault = &diag.Fault{Code: diag.BadUsage, Message: message}
			return
		}
	})
	return fault
}

// A block of custom fields that is no block of the issue asked for is printed whole all the same, and the two
// refusals differ only in which issue that block belongs to: below a link of the issue it is another issue,
// while a command that prints work items has no issue asked for at all.
func refuseWholeCustomFields(at, expression, path string) *diag.Fault {
	message := fmt.Sprintf("fields %s: %s holds the custom fields of another issue, which are printed whole, so "+
		"no name stands under it; only the custom fields of the issue asked for are named one by one",
		render.Quote(expression), path)
	if at != issueSchema {
		message = fmt.Sprintf("fields %s: %s holds the custom fields of the issue, which are printed whole, so "+
			"no name stands under it; a custom field is named one by one in ytrack issue show", render.Quote(expression), path)
	}
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

// A name in double quotes is how a custom field of the issue is named, so it stands nowhere else: every other
// name of an expression is the specification's own and is written bare.
func refuseQuotedNames(spec *schemas, at, expression string, requested []requestedField) *diag.Fault {
	own := ownCustomFields(spec, at, requested)
	var refuse func(fields []requestedField, parents []string, named bool) *diag.Fault
	refuse = func(fields []requestedField, parents []string, named bool) *diag.Fault {
		for i := range fields {
			field := &fields[i]
			written := writtenName(*field)
			if field.quoted && !named {
				message := fmt.Sprintf("fields %s: %s is a name in double quotes, which is how a custom field "+
					"of the issue is named, and such a name stands only under customFields of the issue asked for",
					render.Quote(expression), fieldPath(parents, written))
				return &diag.Fault{Code: diag.BadUsage, Message: message}
			}
			if fault := refuse(field.children, append(slices.Clip(parents), written), field == own); fault != nil {
				return fault
			}
		}
		return nil
	}
	return refuse(requested, nil, false)
}

// A customFields written bare asks for the whole block, whatever names an earlier spelling of it put under
// the same key: the last word of the expression is the one that counts.
func takeCustomFieldsWhole(spec *schemas, requested []requestedField) {
	eachCustomFields(spec, issueSchema, requested, func(_ []string, field *requestedField) {
		if field.whole {
			field.children = nil
		}
	})
}

// The schema an answer stands at where the blocks of an issue in it are composed by ytrack: one issue, a record
// of a selection, the answer to a write, the issue a work item hangs from. issueBlocks, refuseIssueBlocks and
// the printing of a block all take the place through this type, so a fifth one is written into the list below
// and is composed, refused and printed as a block by that alone.
type composedSchema struct{ schema string }

func composedIssue() composedSchema    { return composedSchema{issueSchema} }
func composedWorkItem() composedSchema { return composedSchema{workItemSchema} }

func composedSchemas() []composedSchema {
	return []composedSchema{composedIssue(), composedWorkItem()}
}

// composesBlocks is whether an answer of that schema was built by one of those places.
func composesBlocks(schema string) bool {
	return slices.ContainsFunc(composedSchemas(), func(at composedSchema) bool { return at.schema == schema })
}

// issueBlocks fills the tree that goes out with what the custom fields and the links of an issue are read
// through: the composition is the tool's own and the caller's expression says only which blocks to print. They
// are filled in wherever an issue stands below the schema of at, since each of those places is declared Issue.
func issueBlocks(spec *schemas, at composedSchema, asked []requestedField) {
	// A block carries the translated name only where a name of the default stands among the names, which is
	// how the answer is held against a name nobody resolved.
	translated := namesOfADefault(spec, asked)
	eachCustomFields(spec, at.schema, asked, func(parents []string, field *requestedField) {
		field.children = customFieldsAsked(translated && len(parents) == 0)
	})
	eachLinkSlot(spec, at.schema, asked, func(_ []string, field *requestedField) { field.children = askedOfLink(field.children) })
	eachAttributes(spec, at.schema, asked, func(_ []string, field *requestedField) { field.children = attributesAsked() })
}

// refuseIssueBlocks is what an expression may not write where issueBlocks composes: a name the caller puts under
// a block would be overwritten rather than answered.
func refuseIssueBlocks(spec *schemas, at composedSchema, expression string, requested []requestedField) *diag.Fault {
	if fault := refuseQuotedNames(spec, at.schema, expression, requested); fault != nil {
		return fault
	}
	if fault := refuseCustomFieldNames(spec, at.schema, expression, requested); fault != nil {
		return fault
	}
	if fault := refuseLinkParts(spec, at.schema, expression, requested); fault != nil {
		return fault
	}
	return refuseAttributeNames(spec, at.schema, expression, requested)
}

func eachCustomFields(spec *schemas, at string, requested []requestedField, visit func(parents []string, field *requestedField)) {
	namesAt(spec, at, issueSchema, customFieldsKey, requested, nil, visit)
}

// ownCustomFields is the customFields of the issue asked for, and nil where the expression holds none. It is
// the one place the names of a project's fields stand, and which place that is comes from the schema of
// every name above it — the rule the whole block is normalized by — so the custom fields of an issue at the
// other end of a link are not it.
func ownCustomFields(spec *schemas, at string, requested []requestedField) *requestedField {
	var own *requestedField
	eachCustomFields(spec, at, requested, func(parents []string, field *requestedField) {
		if len(parents) == 0 {
			own = field
		}
	})
	return own
}

// namedCustomFields is the custom fields of the issue itself the caller named, and nil where they took the
// block whole.
func namedCustomFields(spec *schemas, requested []requestedField) *requestedField {
	own := ownCustomFields(spec, issueSchema, requested)
	if own == nil || own.children == nil {
		return nil
	}
	return own
}

func (c *Client) showIssue(ctx context.Context, spec *schemas, id string, requested []requestedField, comments Comments) (*render.Node, *diag.Fault) {
	asked, named, fault := c.askOfAnIssue(ctx, spec, requested)
	if fault != nil {
		return nil, fault
	}
	held := issueComments()
	answer, fault := c.request(ctx, spec, issueSchema, comments.merged(held, asked), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getIssue(ctx, id, fields, named)
	})
	if fault != nil {
		return nil, fault
	}
	issue := answer.objects[0]
	own, fault := comments.pair(held, answer, issue)
	if fault != nil {
		return nil, fault
	}
	return objectNode(answer, requested, issue, own)
}

// askOfAnIssue is one request for issues, whatever command asks it: the expression that goes out and the names
// the request itself is narrowed by. The blocks an issue holds are filled in wherever an issue stands, and in a
// selection that is every record — the tree of a record stands at Issue just as the tree of one issue does.
// The resolving is done here because both of those are the names the instance keeps its fields
// under, which the caller's are only after it.
func (c *Client) askOfAnIssue(ctx context.Context, spec *schemas, requested []requestedField) ([]requestedField, []string, *diag.Fault) {
	if fault := c.resolveCustomFields(ctx, spec, requested); fault != nil {
		return nil, nil, fault
	}
	asked := cloneFields(requested)
	issueBlocks(spec, composedIssue(), asked)
	return asked, cutDownTo(spec, requested), nil
}

// cutDownTo is the names the request itself is narrowed by, one customFields= parameter each. The server
// applies it to every block of custom fields the answer holds, not to the issue's own alone, so it goes out
// only where the issue's own is the only block asked for; otherwise each block arrives whole and the names
// are picked out where they are printed. All that costs is the size of the answer.
func cutDownTo(spec *schemas, requested []requestedField) []string {
	named := namedCustomFields(spec, requested)
	if named == nil || customFieldBlocks(spec, requested) > 1 {
		return nil
	}
	names := make([]string, 0, len(named.children))
	for _, name := range named.children {
		names = append(names, name.name)
	}
	return names
}

func customFieldBlocks(spec *schemas, requested []requestedField) int {
	blocks := 0
	eachCustomFields(spec, issueSchema, requested, func(_ []string, _ *requestedField) { blocks++ })
	return blocks
}
