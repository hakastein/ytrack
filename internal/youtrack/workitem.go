package youtrack

import (
	"context"
	"fmt"
	"net/http"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// What a work item holds where the caller writes no expression of their own. The id comes first: it is what
// ytrack update and ytrack delete address a work item by. The issue it hangs from is left out — the caller
// named it in the argument — and so are creator, created and updated, and textPreview, which is the same text
// in HTML and would be paid for twice.
const WorkItemListFields = "id,duration,type(name),attributes,author(login),date,text"

// What the answer to a write holds where the caller writes no expression of their own: the record time list
// prints, and with it the issue as it stands afterwards. The time spent on an issue is a custom field YouTrack
// recomputes from its work items, so the write that moved it is where it is read — asking for it costs nothing
// here and a second request anywhere else.
const WorkItemWriteFields = "id,duration,type(name),attributes,author(login),date,issue(idReadable,customFields),text"

const (
	workItemSchema   = "IssueWorkItem"
	workItemsPlural  = "workItems"
	workItemsListing = "[]" + workItemSchema
	durationKey      = "duration"
	dateKey          = "date"
	typeKey          = "type"
	// What a refusal about the id of a work item calls the thing the command was given.
	workItemNoun = "work item"
	// Where that refusal sends the caller to read the id off: time is written against an issue and against
	// nothing else, so an article holds no work item to print one under.
	workItemHangsFrom = "the issue"
	// Where a project keeps the types of work its issues are written against. Neither key is declared by the
	// specification, and the server sends both all the same.
	pluginsKey              = "plugins"
	timeTrackingSettingsKey = "timeTrackingSettings"
	workItemTypesKey        = "workItemTypes"
)

// ListWorkItems is the call for one page of the work items of the issue of that readable id, with the fields of
// expression, or with them added to WorkItemListFields when it starts with +; nil is the caller leaning on the
// default whole.
func ListWorkItems(id string, expression *string, page Page) (Call, *diag.Fault) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	if fault := page.parse(); fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := workItemFields(spec, expression, WorkItemListFields)
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.listWorkItems(ctx, spec, id, requested, page)
	}, nil
}

// CreateWorkItem is the call that writes spent against the issue of that readable id, on day where the call
// names one, against the type of work workType names, with the attributes set as Name=value and carrying text
// where it does, and prints the work item as the server kept it, with the fields of expression, or with them
// added to WorkItemWriteFields when it starts with +; nil is the caller leaning on the default whole.
func CreateWorkItem(id, spent string, day, text, workType *string, attributes []string, expression *string) (Call, *diag.Fault) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	written, fault := filedWorkItem(spent, day, text, workType, attributes)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := workItemFields(spec, expression, WorkItemWriteFields)
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.workItemCreated(ctx, spec, id, written, workType, requested)
	}, nil
}

// A named type of work and named attributes are the one thing read before the write: YouTrack takes a type by id
// alone and refuses one that is no setting of the project, so the names are resolved against the settings of the
// project the issue is filed in and nothing of them reaches the server. Where the call names none, one POST
// is the whole command — an issue the instance has none of, and one the token may not see, are both answered 404 by the
// server itself with nothing written, and the answer carries the work item that was added and the issue it
// moved, so nothing is read back afterwards.
func (c *Client) workItemCreated(ctx context.Context, spec *schemas, id string, written writtenWorkItem, named *string, requested []requestedField) (*render.Node, *diag.Fault) {
	at, workType, attributes, fault := c.workItemSettled(ctx, spec, id, named, written.attributes, nil)
	if fault != nil {
		return nil, fault
	}
	filed := workItemFiledWithType{written: written, workType: workType, attributes: attributes}
	asked := asking(requested, filed.checked()...)
	fillInDurations(spec, workItemSchema, asked)
	issueBlocks(spec, composedWorkItem(), asked)
	body := filed.body()
	return c.write(ctx, spec, workItemSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.createIssueWorkItem(ctx, at, body, fields)
	}, filed.confirmedBy, writtenNode(requested))
}

// workItemSettled is the type of work and the attributes the call named, resolved against the settings of the
// project the issue is filed in, beside the issue the write is then addressed to: the readable id that read
// gave, and the argument the caller wrote where nothing was read at all.
func (c *Client) workItemSettled(ctx context.Context, spec *schemas, id string, named *string, set []namedValue, cleared []string) (string, *filedWorkItemType, []filedAttribute, *diag.Fault) {
	withAttributes := len(set) > 0 || len(cleared) > 0
	if named == nil && !withAttributes {
		return id, nil, nil, nil
	}
	readable, project, fault := c.readWorkItemTypes(ctx, spec, id, withAttributes)
	if fault != nil {
		return "", nil, nil, fault
	}
	var workType *filedWorkItemType
	if named != nil {
		resolved, fault := project.resolving(*named)
		if fault != nil {
			return "", nil, nil, fault
		}
		workType = &resolved
	}
	attributes, fault := project.resolvingAttributes(set, cleared)
	if fault != nil {
		return "", nil, nil, fault
	}
	return readable.String(), workType, attributes, nil
}

// UpdateWorkItem is the call that writes the parts given into the work item of that id on the issue of that
// readable id: how long it is where spent names a length, the type of work where workType names one, the day
// where day names one, the text where text names one, the attributes set as Name=value, and an empty value into
// each part or attribute cleared names. A part
// the call does not give is left as the work item holds it. It prints the work item as the server kept it, with
// the fields of expression, or with them added to WorkItemWriteFields when it starts with +; nil is the caller
// leaning on the default whole.
func UpdateWorkItem(id, item string, spent, day, text, workType *string, attributes, cleared []string, expression *string) (Call, *diag.Fault) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	at, fault := parseChildID(workItemNoun, workItemHangsFrom, item)
	if fault != nil {
		return nil, fault
	}
	written, fault := rewrittenWorkItem(spent, day, text, workType, attributes, cleared)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := workItemFields(spec, expression, WorkItemWriteFields)
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.workItemUpdated(ctx, spec, id, at, written, workType, requested)
	}, nil
}

// The pair is not read before the write: YouTrack checks itself that the work item hangs from the issue in the
// path, and answers 404 for a work item of somebody else's issue, for one the instance has none of and for an
// issue the token may not see alike, with nothing written. So the only read there is stands here for the
// same reason as in a creation — a type of work and an attribute are taken by id, and the id comes from the
// project.
func (c *Client) workItemUpdated(ctx context.Context, spec *schemas, id string, at childID, written changedWorkItem, named *string, requested []requestedField) (*render.Node, *diag.Fault) {
	issue, workType, attributes, fault := c.workItemSettled(ctx, spec, id, named, written.attributes, written.clearsAttributes)
	if fault != nil {
		return nil, fault
	}
	changed := workItemRewritten{written: written, issue: issue, at: at, workType: workType, attributes: attributes}
	asked := asking(requested, changed.checked()...)
	fillInDurations(spec, workItemSchema, asked)
	issueBlocks(spec, composedWorkItem(), asked)
	body := changed.body()
	return c.write(ctx, spec, workItemSchema, asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.updateIssueWorkItem(ctx, issue, at, body, fields)
	}, changed.confirmedBy, writtenNode(requested))
}

// DeleteWorkItem is the call that takes the work item of that id away from the issue of that readable id for
// good, and prints the pair it was known by.
func DeleteWorkItem(id, item string) (Call, *diag.Fault) {
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	at, fault := parseChildID(workItemNoun, workItemHangsFrom, item)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.workItemRemoved(ctx, spec, id, at)
	}, nil
}

// What the read before the removal asks for, and the whole of what a removal prints: a work item carries no
// readable id of its own, so the pair it is addressed by is its identity.
func removedWorkItemFields() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: issueOwner.String(), children: []requestedField{{name: idReadableKey}}},
	}
}

// The work item is read before it is destroyed, unlike a comment: the removal answers 200 with an empty
// body, so what is printed has to be read while the work item is still there, and that read is what turns a
// work item the issue has none of into a not_found before anything is destroyed. The server checks the pair
// itself, on the read as on the removal, so nothing here holds the work item against the issue.
func (c *Client) workItemRemoved(ctx context.Context, spec *schemas, id string, at childID) (*render.Node, *diag.Fault) {
	requested := removedWorkItemFields()
	a, fault := c.request(ctx, spec, workItemSchema, requested, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getIssueWorkItem(ctx, id, at, fields)
	})
	if fault != nil {
		return nil, fault
	}
	issue, fault := owningIssueAddressed(a)
	if fault != nil {
		return nil, fault
	}
	known, fault := workItemAddressed(a)
	if fault != nil {
		return nil, fault
	}
	if fault := writeEmpty(ctx, func(ctx context.Context) (*http.Response, error) {
		return c.deleteIssueWorkItem(ctx, issue, known)
	}); fault != nil {
		return nil, fault
	}
	return objectNode(a, requested, a.objects[0], nil)
}

// The issue the work item hangs from, as the read gave it and held to the form ytrack sends before the removal
// goes out: what arrived becomes a path segment, and "..", a slash or an empty string would reach an endpoint
// other than the work item that was read.
func owningIssueAddressed(a answer) (addressed, *diag.Fault) {
	issue, isObject := a.objects[0][issueOwner.String()].(map[string]any)
	if !isObject {
		return addressed{}, shapeFailure(a.response, a.body, "the issue the work item hangs from is not a JSON object")
	}
	return addressedIn(a, issue, issueOwner, "a removal")
}

// The id the work item goes by, held to the same form the argument was held to: it is the other path segment of
// the removal, and an empty one there would reach the work items of the issue whole.
func workItemAddressed(a answer) (childID, *diag.Fault) {
	id, isText := a.objects[0][idKey].(string)
	if !isText {
		return childID{}, shapeFailure(a.response, a.body, "the id of the work item arrived as something other than a string")
	}
	known, fault := parseChildID(workItemNoun, workItemHangsFrom, id)
	if fault != nil {
		message := fmt.Sprintf("the work item arrived with %s for an id, and a removal is addressed by the id the "+
			"server gave", render.Quote(id))
		return childID{}, shapeFailure(a.response, a.body, message)
	}
	return known, nil
}

// The types of work the project of an issue writes work items against, as the read before a write found them,
// beside the code of that project, which is what a refusal names the types were held against.
type projectWorkItemTypes struct {
	project    string
	types      []workItemType
	attributes []projectAttribute
	arrived    answer
}

// One type of work: the id the body of a write carries and the name a caller addresses it by.
type workItemType struct {
	id   string
	name string
}

// What the read before a write asks of the issue: the readable id the write is addressed by, and the types of
// work of the project it is filed in. The set comes from the project rather than from the global catalogue —
// the instance has seventeen types and DEV writes against fifteen of them — and reading it off the issue costs
// the one request that settles the id as well.
func workItemTypesFields(withAttributes bool) []requestedField {
	settings := []requestedField{{name: workItemTypesKey, children: []requestedField{{name: idKey}, {name: nameKey}}}}
	if withAttributes {
		settings = append(settings, requestedField{name: attributesKey, children: []requestedField{
			{name: idKey},
			{name: nameKey},
			{name: "values", children: []requestedField{{name: idKey}, {name: nameKey}}},
		}})
	}
	return []requestedField{
		{name: idReadableKey},
		{name: projectKey, children: []requestedField{
			{name: shortNameKey},
			{name: pluginsKey, children: []requestedField{
				{name: timeTrackingSettingsKey, children: settings},
			}},
		}},
	}
}

func (c *Client) readWorkItemTypes(ctx context.Context, spec *schemas, id string, withAttributes bool) (addressed, projectWorkItemTypes, *diag.Fault) {
	a, fault := c.request(ctx, spec, issueSchema, workItemTypesFields(withAttributes), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getIssue(ctx, id, fields, nil)
	})
	if fault != nil {
		return addressed{}, projectWorkItemTypes{}, fault
	}
	readable, fault := addressedBy(a, issueOwner, "a creation")
	if fault != nil {
		return addressed{}, projectWorkItemTypes{}, fault
	}
	found, fault := workItemTypesOf(a, withAttributes)
	if fault != nil {
		return addressed{}, projectWorkItemTypes{}, fault
	}
	return readable, found, nil
}

// The judgment of names says a member arrived, not what it holds, so everything the settings are read for is
// held to its shape here.
func workItemTypesOf(a answer, withAttributes bool) (projectWorkItemTypes, *diag.Fault) {
	project, isObject := a.objects[0][projectKey].(map[string]any)
	if !isObject {
		return projectWorkItemTypes{}, shapeFailure(a.response, a.body, "the project of the issue is not a JSON object")
	}
	code, isText := project[shortNameKey].(string)
	if !isText {
		return projectWorkItemTypes{}, shapeFailure(a.response, a.body, "the short name of the project is not text")
	}
	settings, fault := timeTrackingSettingsOf(a, project)
	if fault != nil {
		return projectWorkItemTypes{}, fault
	}
	items, isList := settings[workItemTypesKey].([]any)
	if !isList {
		return projectWorkItemTypes{}, shapeFailure(a.response, a.body, "the types of work of the project are not a JSON array")
	}
	types := make([]workItemType, 0, len(items))
	for _, item := range items {
		object, isObject := item.(map[string]any)
		if !isObject {
			return projectWorkItemTypes{}, shapeFailure(a.response, a.body, brokenWorkItemType)
		}
		id, isText := object[idKey].(string)
		name, isNamed := object[nameKey].(string)
		if !isText || !isNamed {
			return projectWorkItemTypes{}, shapeFailure(a.response, a.body, brokenWorkItemType)
		}
		types = append(types, workItemType{id: id, name: name})
	}
	found := projectWorkItemTypes{project: code, types: types, arrived: a}
	if withAttributes {
		if found.attributes, fault = attributesOf(a, settings); fault != nil {
			return projectWorkItemTypes{}, fault
		}
	}
	return found, nil
}

func timeTrackingSettingsOf(a answer, project map[string]any) (map[string]any, *diag.Fault) {
	plugins, isObject := project[pluginsKey].(map[string]any)
	if !isObject {
		return nil, shapeFailure(a.response, a.body, "the plugins of the project are not a JSON object")
	}
	settings, isObject := plugins[timeTrackingSettingsKey].(map[string]any)
	if !isObject {
		return nil, shapeFailure(a.response, a.body, "the time tracking settings of the project are not a JSON object")
	}
	return settings, nil
}

const brokenWorkItemType = "the id or the name of a type of work of the project is not text"

// resolving is the type of work a name answers to, by the rule every name a caller writes is resolved by:
// letter case aside, and, where several answer, the one whose name was written byte for byte, so the name a
// type is printed under stays the address.
func (p projectWorkItemTypes) resolving(name string) (filedWorkItemType, *diag.Fault) {
	catalogue := p.catalogue()
	at, found := resolvedAmong(name, catalogue)
	if !found {
		return filedWorkItemType{}, p.refusing(name, catalogue)
	}
	return filedWorkItemType{id: p.types[at].id, written: name}, nil
}

// A type of work is addressed by the one name it carries: a project translates none of them.
func (p projectWorkItemTypes) catalogue() []naming {
	catalogue := make([]naming, 0, len(p.types))
	for _, found := range p.types {
		catalogue = append(catalogue, naming{name: found.name})
	}
	return catalogue
}

// A name that answers to no one type is handed back with the names nearest it, which are every name the project
// has where none is near. The request the refusal names is the read of the issue: it is the only one that went
// out, and nothing is written.
func (p projectWorkItemTypes) refusing(name string, catalogue []naming) *diag.Fault {
	entry := render.NewMap(
		render.Pair{Key: typeKey, Value: render.NewString(name)},
		render.Pair{Key: "nearest", Value: render.NewList(names(nearestNamed(name, catalogue))...)})
	message := "the name under unknown is not one type of work the project writes work items against"
	return unknownNames(p.arrived.response, render.Pair{Key: projectKey, Value: render.NewString(p.project)},
		"unknown", message, []*render.Node{entry})
}

// The work items of an issue arrive under a path of their own rather than as a member of the issue, so $top
// works on them and the count is read off a second pass over ids alone: YouTrack keeps no counter of them, and
// the subresource cuts a page down to 42 where no $top goes out at all.
func (c *Client) listWorkItems(ctx context.Context, spec *schemas, id string, requested []requestedField, page Page) (*render.Node, *diag.Fault) {
	ask := func(ctx context.Context, fields string, w window) (*http.Response, error) {
		return c.getIssueWorkItems(ctx, id, fields, w)
	}
	selection := c.countedByIDs(spec, workItemsPlural, workItemsListing, requested, askedOfAWorkItem(spec, requested), page, ask)
	return selection.selected(ctx)
}

// askedOfAWorkItem is what goes out for a record: the caller's expression with the minutes filled in under
// every duration they asked for, since a bare duration answers with its type and nothing else, and with the
// blocks of any issue they reached through it composed the way a show of one composes them.
func askedOfAWorkItem(spec *schemas, requested []requestedField) []requestedField {
	asked := cloneFields(requested)
	fillInDurations(spec, workItemSchema, asked)
	issueBlocks(spec, composedWorkItem(), asked)
	return asked
}

// workItemFields is an expression of a command that prints work items, held to what one may be asked for. A nil
// expression is the caller leaning on the default whole, and then nothing in the tree is theirs to answer for.
func workItemFields(spec *schemas, expression *string, defaults string) ([]requestedField, *diag.Fault) {
	written := defaults
	requested, fault := theDefault(defaults, false)
	if expression != nil {
		written = *expression
		requested, fault = parseFields(written, defaults)
	}
	if fault != nil {
		return nil, fault
	}
	if fault := refuseDurationParts(spec, workItemSchema, written, requested); fault != nil {
		return nil, fault
	}
	return requested, refuseIssueBlocks(spec, composedWorkItem(), written, requested)
}
