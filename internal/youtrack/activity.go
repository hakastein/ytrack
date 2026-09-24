package youtrack

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// When an activity happened, who did it, what kind of change it was, what of the issue it changed and what the
// change put there and took away. The issue itself is not asked for: the caller named it. A value under added
// and removed is printed by the id and by whichever of the names its type has — idReadable of an issue, login
// of a user, name of a value of a bundle, a tag or an attachment, urls of a commit — so the default names a
// value of any type without the heavy text of a comment or a commit (ADR-0011).
const ActivityListFields = "timestamp,author(login),category,field," +
	"added(id,idReadable,login,name,urls),removed(id,idReadable,login,name,urls)"

const (
	activitySchema    = "ActivityItem"
	categorySchema    = "ActivityCategory"
	customFilterField = "CustomFilterField"
	activitiesPlural  = "activities"
	categoryKey       = "category"
	addedKey          = "added"
	removedKey        = "removed"
	fieldKey          = "field"
	customFieldKey    = "customField"
	timestampKey      = "timestamp"
)

// The one category whose records say nothing themselves about the shape of their values: what a change of a
// custom field put there is the field's to settle, and the field is named by the record.
const customFieldCategory = "CustomFieldCategory"

// The largest --limit: the request asks for one record past it and $top is an int32.
const activityLimit = math.MaxInt32 - 1

// A row of the table: the category as YouTrack keeps it and what of the issue a change of it was of.
type activityCategory struct {
	id    string
	field fieldOf
	// How the values of the one record the row was copied for arrive, where the field the record names says so.
	values fieldValues
}

// What the field of a record names. YouTrack writes a filter on every record whatever its category, and for
// most categories that filter names the category over again — a filing stands for "создана" — so the field is
// printed only where the record stands for one field of the issue among others.
type fieldOf int

const (
	noFieldOfItsOwn fieldOf = iota
	theCustomField
	theLinkPhrase
)

// How the values of a change of a custom field arrive, read off the type of the field the record names: a value
// that carries a name of its own comes as an object and is printed as the tree asked of it, while a value ytrack
// reads itself — a duration, a day, a moment, a number, a text — comes bare, as a number of minutes or of
// milliseconds that reads nothing like what the field holds until it is written by the identity of its type.
type fieldValues struct {
	// The form was read at all, which is only where the record is a change of a custom field whose values are
	// going to be printed.
	read  bool
	named bool
	bare  identity
}

// activityTable is the categories a journal covers, in the order they go out. The server lists none of them and
// neither does the specification, so the table is written here, and a category is in it only once an activity
// of it has been seen: without one, a category that exists is indistinguishable from a misspelling. Every
// row but one is held to the polygon; VcsChangeCategory is held to records of a live instance, since the polygon has
// no VCS integration and no API files a commit without one.
func activityTable() []activityCategory {
	return []activityCategory{
		{id: "AttachmentsCategory"},
		{id: "CommentTextCategory"},
		{id: "CommentsCategory"},
		{id: customFieldCategory, field: theCustomField},
		{id: "DescriptionCategory"},
		{id: "IssueCreatedCategory"},
		{id: "IssueResolvedCategory"},
		{id: "LinksCategory", field: theLinkPhrase},
		{id: "SummaryCategory"},
		{id: "TagsCategory"},
		{id: "VcsChangeCategory"},
		{id: "WorkItemCategory"},
	}
}

// ActivityCategories is the identifiers of the table, which is what --category is written with and what the help
// names.
func ActivityCategories() []string {
	return categoryIDs(activityTable())
}

func categoryIDs(rows []activityCategory) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.id)
	}
	return ids
}

// ListActivities is the call for one page of the activities of the issue of that readable id, newest first, of the
// categories asked for, or of every category of the table where none was, and with the fields of expression, or
// with them added to ActivityListFields when it starts with +; nil is the caller leaning on the default whole.
func ListActivities(id string, expression *string, page Page, asked []string) (Call, *diag.Fault) {
	// An article keeps no journal: the API has no activities under /articles, so its id is refused as it is
	// wherever an issue alone is named.
	id, fault := parseIssueID(id)
	if fault != nil {
		return nil, fault
	}
	if fault := page.within(activityLimit); fault != nil {
		return nil, fault
	}
	categories, fault := resolveCategories(asked)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	requested, fault := activityFields(spec, expression)
	if fault != nil {
		return nil, fault
	}
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.listActivities(ctx, spec, id, requested, categories, page)
	}, nil
}

// resolveCategories is the rows that go out for the names --category was written with, each once and in the
// order of the table; every row of it where no name was written. YouTrack matches a category letter for letter
// and answers a name it does not know with an empty journal rather than with a refusal, so a misspelling left to
// the server would read as an issue with no journal at all.
func resolveCategories(asked []string) ([]activityCategory, *diag.Fault) {
	table := activityTable()
	if len(asked) == 0 {
		return table, nil
	}
	chosen := make([]bool, len(table))
	var unresolved []string
	for _, name := range asked {
		if name == "" {
			return nil, &diag.Fault{Code: diag.BadUsage, Message: "the name of a category is empty"}
		}
		at := slices.IndexFunc(table, func(row activityCategory) bool { return strings.EqualFold(row.id, name) })
		switch {
		case at >= 0:
			chosen[at] = true
		// Told apart by the rule that resolves them, so a name written two ways is one name to the tool and
		// stands in the refusal once, in the letter case it was written in first.
		case !slices.ContainsFunc(unresolved, func(seen string) bool { return strings.EqualFold(seen, name) }):
			unresolved = append(unresolved, name)
		}
	}
	if len(unresolved) > 0 {
		unknown := make([]*render.Node, 0, len(unresolved))
		for _, name := range unresolved {
			unknown = append(unknown, nearestEntry(categoryKey, name, nearestNames(name, categoryIDs(table))))
		}
		message := "the names under unknown are not categories of the journal ytrack asks for"
		details := []render.Pair{{Key: "unknown", Value: render.NewList(unknown...)}}
		return nil, &diag.Fault{Code: diag.UnknownName, Message: message, Details: details}
	}
	categories := make([]activityCategory, 0, len(table))
	for at, row := range table {
		if chosen[at] {
			categories = append(categories, row)
		}
	}
	return categories, nil
}

func activityFields(spec *schemas, expression *string) ([]requestedField, *diag.Fault) {
	written, requested, fault := theExpression(expression, ActivityListFields, false)
	if fault != nil {
		return nil, fault
	}
	if fault := refuseBlockParts(spec, written, requested); fault != nil {
		return nil, fault
	}
	if fault := refuseUnknownValueNames(spec, written, requested); fault != nil {
		return nil, fault
	}
	return requested, nil
}

// valuesStanding is what may stand under added and removed beside what the subtypes of an activity declare
// there: a change of a custom field holds values of a bundle, and the specification declares its values an
// object of no schema.
func valuesStanding() []string {
	return []string{"BundleElement"}
}

// refuseUnknownValueNames refuses, before any request, a name written under added or removed that no type of a
// value of a change declares. After the answer the judgment would refuse it only where a value arrived, and a
// misspelling over a journal that holds no value would read as values that carry nothing.
func refuseUnknownValueNames(spec *schemas, expression string, requested []requestedField) *diag.Fault {
	j := judgment{schemas: spec}
	activity := family{schemas: spec.subtree(activitySchema)}
	var unknown []*render.Node
	for _, field := range requested {
		if field.name != addedKey && field.name != removedKey {
			continue
		}
		field.standing = valuesStanding()
		names := j.familyBelow(activity, &place{field: field}).names
		for _, child := range field.children {
			if !slices.Contains(names, child.name) {
				unknown = append(unknown, unknownEntry(fieldPath([]string{field.name}, child.name), nearestNames(child.name, names)))
			}
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	details := []render.Pair{
		{Key: "fields", Value: render.NewString(expression)},
		{Key: "unknown", Value: render.NewList(unknown...)},
	}
	message := "the names under unknown are declared by no value a change of the journal holds"
	return &diag.Fault{Code: diag.UnknownName, Message: message, Details: details}
}

// Neither is printed as the object it arrives as: the category stands as the identifier --category names it
// with and the field as the name of what was changed, so a name written under either names nothing that reaches
// the document. added and removed are not among them: a value is printed as the tree asked of it.
func refuseBlockParts(spec *schemas, expression string, requested []requestedField) *diag.Fault {
	blocks := []struct{ name, printedAs string }{
		{categoryKey, "the identifier YouTrack keeps the category under"},
		{fieldKey, "the name of what the change was of"},
	}
	var fault *diag.Fault
	for _, block := range blocks {
		namesAt(spec, activitySchema, activitySchema, block.name, requested, nil, func(parents []string, field *requestedField) {
			if field.children == nil || fault != nil {
				return
			}
			message := fmt.Sprintf("fields %s: %s is printed as %s, so no name stands under it",
				render.Quote(expression), fieldPath(parents, field.name), block.printedAs)
			fault = &diag.Fault{Code: diag.BadUsage, Message: message}
		})
	}
	return fault
}

// carries is whether the document asked for carries a name, which is what settles the names ytrack merges into
// the request beside it.
func carries(requested []requestedField, name string) bool {
	return slices.ContainsFunc(requested, func(field requestedField) bool { return field.name == name })
}

// fieldAsked is what goes out under the field of a record, and each of the three names is asked for by the one
// thing that reads it: the label of a link and the name the project gave a custom field are what a printed
// field stands for, and the type of that field is what says how a bare value of the change reads.
// $type is not asked for at all: the server names the subtype of every filter it sends, asked or not.
func fieldAsked(named, values bool) []requestedField {
	asked := make([]requestedField, 0, 2)
	var held []requestedField
	if named {
		asked = append(asked, requestedField{name: nameKey})
		held = append(held, requestedField{name: nameKey})
	}
	if values {
		held = append(held, requestedField{name: fieldTypeKey, children: []requestedField{{name: valueTypeKey}}})
	}
	return append(asked, requestedField{name: customFieldKey, children: held})
}

// journalCounted is the whole of a journal whose page came short of the record past the limit: the records passed
// over and the records that arrived, unless nothing arrived after a skip, which may have passed the end.
func journalCounted(page Page, arrived int) count {
	if arrived == 0 && page.Skip > 0 {
		return count{}
	}
	return counted(page.Skip + arrived)
}

// The journal is asked for one record past the limit, and that record is what says the rest were cut off: the
// server counts activities nowhere, and a second pass for the count would read past a hundred thousand records
// of a live issue, while $top=-1 comes back silently cut to a thousand.
func (c *Client) listActivities(ctx context.Context, spec *schemas, id string, requested []requestedField, categories []activityCategory, page Page) (*render.Node, *diag.Fault) {
	own := []requestedField{
		{name: timestampKey},
		{name: categoryKey, children: []requestedField{{name: idKey}}},
	}
	values := carries(requested, addedKey) || carries(requested, removedKey)
	named := carries(requested, fieldKey)
	if values || named {
		own = append(own, requestedField{name: fieldKey, normalized: true, children: fieldAsked(named, values)})
	}
	// A change of the duration of a work item holds a DurationValue, which is printed out of its minutes and
	// arrives without them unless they are asked for; on a value of any other type the name is left out.
	for _, name := range []string{addedKey, removedKey} {
		if carries(requested, name) {
			own = append(own, requestedField{name: name, standing: valuesStanding(),
				children: []requestedField{{name: minutesKey}}})
		}
	}
	// Read before the journal and only where a record of a link may be printed: how many requests a call makes
	// follows what was asked of it rather than what comes back.
	var phrases linkPhrases
	if named && slices.ContainsFunc(categories, func(row activityCategory) bool { return row.field == theLinkPhrase }) {
		var fault *diag.Fault
		phrases, fault = c.linkPhrases(ctx, spec)
		if fault != nil {
			return nil, fault
		}
	}
	sent := asking(requested, own...)
	pastThePage := page.window()
	pastThePage.top++
	answer, fault := c.passing(ctx, spec, "[]"+activitySchema, sent, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getIssueActivities(ctx, id, strings.Join(categoryIDs(categories), ","), fields, pastThePage)
	})
	if fault != nil {
		return nil, fault
	}
	arrived := answer.objects
	if len(arrived) > page.Limit+1 {
		details := []render.Pair{{Key: "limit", Value: intNode(page.Limit)}, {Key: "returned", Value: intNode(len(arrived))}}
		message := "more activities arrived than the limit and the one record asked for past it"
		return nil, &diag.Fault{Code: diag.UpstreamLied, Message: message, Details: details}
	}
	// The record past the limit is judged with the rest: it came from the same request and says as much about
	// the answer as any of them, and it is thrown away only afterwards.
	rows, fault := activityRows(answer, categories, values)
	if fault != nil {
		return nil, fault
	}
	found, left := journalCounted(page, len(arrived)), false
	if len(arrived) > page.Limit {
		arrived, rows, found, left = arrived[:page.Limit], rows[:page.Limit], count{}, true
	}
	printer := printingActivities(answer, phrases)
	printed := make([]*render.Node, 0, len(arrived))
	for at, activity := range arrived {
		node, fault := printer.record(rows[at], requested, activity)
		if fault != nil {
			return nil, fault
		}
		printed = append(printed, node)
	}
	return listingCut(activitiesPlural, found, left, printed), nil
}

// activityRows is the row of the table each activity stands under, and it is where the answer is held to the
// two things the request asked of it rather than to what it hoped for: reverse=true, a parameter that would
// hand back the oldest activities were it to stop being understood, and categories, which the server answers an
// unknown name in with an empty journal rather than a refusal. Equal timestamps are lawful — activities of a
// live instance can share one. printingValues is whether the document carries added or removed, which is the
// one thing that makes the form of the values of a custom field be read at all.
func activityRows(a answer, sent []activityCategory, printingValues bool) ([]activityCategory, *diag.Fault) {
	rows := make([]activityCategory, 0, len(a.objects))
	previous := int64(math.MaxInt64)
	for _, activity := range a.objects {
		moment, isInstant := wholeNumber(activity[timestampKey])
		if !isInstant {
			return nil, shapeFailure(a.response, a.body, notAnInstant(timestampKey))
		}
		if moment > previous {
			message := "an activity arrived newer than the one before it, and the newest were asked for first"
			return nil, shapeFailure(a.response, a.body, message)
		}
		previous = moment
		named, reason := categoryID(activity[categoryKey])
		if reason != "" {
			return nil, shapeFailure(a.response, a.body, reason)
		}
		at := slices.IndexFunc(sent, func(row activityCategory) bool { return row.id == named })
		if at < 0 {
			message := fmt.Sprintf("an activity arrived of the category %s, which was not among the categories "+
				"the request asked for", render.Quote(named))
			return nil, shapeFailure(a.response, a.body, message)
		}
		row := sent[at]
		if printingValues && row.field == theCustomField {
			form, reason := fieldForm(activity[fieldKey])
			if reason != "" {
				return nil, shapeFailure(a.response, a.body, reason)
			}
			row.values = form
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// activities is the writer of the records of one journal. What a record is read by besides the answer itself —
// the phrases of the link types of the instance and the row of the table the record stands under — reaches the
// writer here and nowhere else, so a record cannot be written with either of the two unfilled.
type activities struct{ writer nodes }

// A record of a list is one line, and the phrases are read once for the whole page rather than for each record.
func printingActivities(a answer, phrases linkPhrases) activities {
	return activities{writer: nodes{answer: a, layout: onOneLine, phrases: phrases}}
}

// record is the document of one activity under the row it stands on. The rules of the row hold over the record
// and nowhere below it.
func (p activities) record(row activityCategory, requested []requestedField, object map[string]any) (*render.Node, *diag.Fault) {
	n := p.writer
	n.row = row
	n.ofTheRecord = true
	return n.object(activitySchema, requested, object)
}

// customFilter is the filter a record of a change of a custom field stands for, or the reason what arrived is
// none. It is read wherever such a record is, so that one lie of the server has one diagnosis whether the
// journal prints the field, the values or both.
func customFilter(value any) (map[string]any, string) {
	field, isObject := value.(map[string]any)
	if !isObject {
		return nil, fmt.Sprintf("an activity of %s arrived standing for no field of the issue, and a change of "+
			"that category is a change of one", customFieldCategory)
	}
	if named, _ := field["$type"].(string); named != customFilterField {
		return nil, fmt.Sprintf("an activity of %s arrived for a field of %s, and a change of that category "+
			"stands for a %s", customFieldCategory, render.Quote(named), customFilterField)
	}
	return field, ""
}

// fieldForm is how the values of a record of a custom field arrive, read off the field the record names: the
// category says a custom field changed, and the type of that field says what one value of it looks like.
// isMultiValue says nothing of it, so the shape is taken from the type alone.
func fieldForm(value any) (fieldValues, string) {
	field, reason := customFilter(value)
	if reason != "" {
		return fieldValues{}, reason
	}
	named, read := valueTypeOf(field)
	if !read {
		return fieldValues{}, fmt.Sprintf("the custom field an activity of %s stands for arrived with no type of "+
			"value, which is what says how the change reads", customFieldCategory)
	}
	kind, modelled := typeNamed(named)
	if !modelled {
		return fieldValues{}, fmt.Sprintf("an activity of %s arrived for a field holding values of the type %s, "+
			"which is none of the custom-field types ytrack models", customFieldCategory, render.Quote(named))
	}
	if kind.namedByAName() {
		return fieldValues{read: true, named: true}, ""
	}
	return fieldValues{read: true, bare: identity{form: kind.identity.form}}, ""
}

func valueTypeOf(field map[string]any) (string, bool) {
	held, isObject := field[customFieldKey].(map[string]any)
	if !isObject {
		return "", false
	}
	kind, isObject := held[fieldTypeKey].(map[string]any)
	if !isObject {
		return "", false
	}
	named, isText := kind[valueTypeKey].(string)
	return named, isText
}

// categoryID is the identifier of the category an activity stands under, or the reason what arrived is none.
func categoryID(value any) (id, reason string) {
	category, isObject := value.(map[string]any)
	if !isObject {
		return "", "the category of an activity arrived as something other than a JSON object"
	}
	id, isText := category[idKey].(string)
	if !isText {
		return "", "the id of the category of an activity arrived as something other than text"
	}
	return id, ""
}

// category is the block the category of an activity prints as: the identifier alone, since the object holds
// nothing else and $type is the same one for every category. It is read off the row rather than off the object
// standing there: activityRows read that very object before anything was printed, and a record whose category it
// could not read never reaches a document.
func (n nodes) category() *render.Node {
	return render.NewString(n.row.id)
}

// changedField is the block the field of an activity prints as: the name of what the change was of. A
// change of a custom field is printed by the name the project gave that field rather than by the label on the
// record, which arrives translated; a change of a link, by the untranslated phrase of the end of the link type
// that label stands for.
func (n nodes) changedField(value any) (*render.Node, *diag.Fault) {
	if n.row.field == noFieldOfItsOwn {
		return render.NewNull(), nil
	}
	if n.row.field == theCustomField {
		filter, reason := customFilter(value)
		if reason != "" {
			return nil, n.lied(reason)
		}
		held, _ := filter[customFieldKey].(map[string]any)
		name, isText := held[nameKey].(string)
		if !isText {
			return nil, n.lied(fmt.Sprintf("the custom field an activity of %s stands for arrived with no name "+
				"of the project's own", n.row.id))
		}
		return render.NewString(name), nil
	}
	filter, isObject := value.(map[string]any)
	if !isObject {
		return nil, n.lied(fmt.Sprintf("an activity of %s arrived standing for no field of the issue, and a "+
			"change of that category is a change of one", n.row.id))
	}
	label, isText := filter[nameKey].(string)
	if !isText {
		return nil, n.lied(fmt.Sprintf("an activity of %s arrived with no phrase of the link it stands for", n.row.id))
	}
	phrase, reason := n.phrases.phrase(label)
	if reason != "" {
		return nil, n.lied(reason)
	}
	return render.NewString(phrase), nil
}

// values is the block added and removed print as, and it is always a list: the server sends null for a change
// that put nothing there, a bare value where one was put and a list where several could be, and one reader of
// the journal reads every record alike only if all three stand as a list (ADR-0011). held is what the subtype of
// the record declares there, so a moment, a duration and an entity are each written as they are anywhere else.
func (n nodes) values(held element, field requestedField, value any) (*render.Node, *diag.Fault) {
	var items []any
	switch value := value.(type) {
	case nil:
	case []any:
		items = value
	default:
		items = []any{value}
	}
	n.ofTheRecord = false
	held.list = false
	printed := make([]*render.Node, 0, len(items))
	for _, item := range items {
		node, fault := n.oneValue(held, field, item)
		if fault != nil {
			return nil, fault
		}
		printed = append(printed, node)
	}
	return render.NewList(printed...), nil
}

// oneValue is one value of a change. A change of a custom field is the one record the specification declares
// nothing under — an object of no schema — so its bare value is read by the type of the field: 90 of a period
// field is PT1H30M, and a date arrives as the same kind of number a moment does.
func (n nodes) oneValue(held element, field requestedField, value any) (*render.Node, *diag.Fault) {
	form := n.row.values
	if !form.read {
		return n.value(held, field, value)
	}
	_, isObject := value.(map[string]any)
	switch {
	case form.named && !isObject:
		return nil, n.lied(fmt.Sprintf("a value under the %s of an activity of %s arrived as something other "+
			"than a JSON object, and the field it stands for holds values that carry names of their own",
			field.name, n.row.id))
	case !form.named && isObject:
		return nil, n.lied(fmt.Sprintf("a value under the %s of an activity of %s arrived as a JSON object, and "+
			"the field it stands for holds values ytrack reads itself", field.name, n.row.id))
	case isObject:
		return n.value(held, field, value)
	}
	node, read := n.printed(form.bare, value)
	if !read {
		return nil, n.lied(fmt.Sprintf("a value under the %s of an activity of %s is not %s",
			field.name, n.row.id, form.bare.shape()))
	}
	return node, nil
}
