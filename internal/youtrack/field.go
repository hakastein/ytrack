package youtrack

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// What a field is called, what kind of value it holds and whether it may be left empty. The values it allows
// belong to one field, not to a page of them, so no bundle is asked for here.
const FieldListFields = "field(name,localizedName,fieldType(valueType,isMultiValue)),canBeEmpty"

// The right a token needs on a project to be sent its custom fields at all.
const projectRead = "jetbrains.jetpass.project-read"

// ordinal is where the project was told to put the field. The array itself comes in the order of the ids the
// attachments were given, which groups the fields by class and is nobody's configured order.
const ordinal = "ordinal"

// ListFields is the call for the custom fields of a project with the fields of expression, or with them added to
// FieldListFields when it starts with +.
func ListFields(code, expression string) (Call, *diag.Fault) {
	code, fault := parseProjectCode(code)
	if fault != nil {
		return nil, fault
	}
	requested, fault := parseFields(expression, FieldListFields)
	if fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.listFields(ctx, spec, code, requested)
	}, nil
}

// ShowField is the call for the one custom field of a project the caller named, with the fields of expression,
// or with them added to the default of the field's own type when it starts with +. A nil expression is the
// caller who wrote no --fields at all, and what they asked for is that default and nothing besides.
func ShowField(code, name string, expression *string) (Call, *diag.Fault) {
	code, fault := parseProjectCode(code)
	if fault != nil {
		return nil, fault
	}
	if name == "" {
		return nil, &diag.Fault{Code: diag.BadUsage, Message: "the name of a custom field is empty"}
	}
	// Which default the expression is read against is settled only once the field is resolved, so here it is
	// held to the grammar alone and a call that cannot be sent is still refused before any request.
	if expression != nil {
		if _, fault := parseFields(*expression, FieldListFields); fault != nil {
			return nil, fault
		}
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.showField(ctx, spec, code, name, expression)
	}, nil
}

// The metadata kept on disk may be behind the server, so it is asked first and refuses nothing: where it turns
// out to be behind, the metadata is read again and the name resolved against that (ADR-0002).
func (c *Client) showField(ctx context.Context, spec *schemas, code, name string, expression *string) (*render.Node, *diag.Fault) {
	target := metadataTarget(code)
	if cached, hit := c.cache.load(target); hit {
		node, fault, behind := c.showFieldFrom(ctx, spec, code, name, expression, fromDisk(cached))
		if !behind {
			return node, fault
		}
	}
	metadata, fault := c.passing(ctx, spec, "Project", metadataFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getProject(ctx, code, fields)
	})
	if fault != nil {
		return nil, fault
	}
	fields, fault := readMetadata(metadata)
	if fault != nil {
		return nil, fault
	}
	if len(fields) == 0 {
		return nil, noFields(metadata.response, code)
	}
	c.cache.store(target, fields)
	node, fault, _ := c.showFieldFrom(ctx, spec, code, name, expression, fromServer(metadata, fields))
	return node, fault
}

// A source is the metadata one showing of a field is built from, and what a step that does not carry through
// means for it. Metadata off the disk may be older than the server, so such a step prints nothing and sends the
// call back for the metadata; metadata the server has just sent is the last word, so the same step refuses over
// the answer it arrived in (ADR-0002).
type source struct {
	fields []customField
	// The answer the metadata arrived in, and nil for the disk, which has no request a refusal could name.
	arrived *answer
}

func fromServer(arrived answer, fields []customField) source {
	return source{fields: fields, arrived: &arrived}
}

func fromDisk(fields []customField) source {
	return source{fields: fields}
}

func (s source) mayBeBehind() bool {
	return s.arrived == nil
}

// stale disposes of a step that shows the metadata no longer describes the server, and asks refuse for the
// refusal only where there is an answer to build one over.
func (s source) stale(refuse func(answer) *diag.Fault) (*render.Node, *diag.Fault, bool) {
	if s.mayBeBehind() {
		return nil, nil, true
	}
	return nil, refuse(*s.arrived), false
}

// showFieldFrom is the whole of field show over one set of metadata: the name resolved, the id held to the form
// a path takes, what to print settled against what the field holds, the field asked for by that id and the
// answer confirmed against the naming the name resolved to. The steps are the same whichever metadata they run
// over, and the source alone says what a step that does not carry through comes to.
func (c *Client) showFieldFrom(ctx context.Context, spec *schemas, code, name string, expression *string, from source) (*render.Node, *diag.Fault, bool) {
	found, ok := lookUp(name, from.fields)
	if !ok {
		return from.stale(func(a answer) *diag.Fault { return unresolved(a, code, name, from.fields) })
	}
	if !found.addressable() {
		return from.stale(func(a answer) *diag.Fault { return unaddressableID(found.id, a) })
	}
	requested, modelled, fault := fieldsToPrint(expression, found.naming)
	if fault != nil {
		// The grammar of the expression is the caller's own, and reading the metadata again would not mend it.
		return nil, fault, false
	}
	if !modelled {
		return from.stale(func(a answer) *diag.Fault { return unmodelledType(found.naming, a) })
	}
	arrived, fault := c.askForField(ctx, spec, code, found.id, requested)
	if fault != nil {
		return nil, fault, from.mayBeBehind() && outdated(fault)
	}
	if fault := found.naming.confirmed(arrived, code); fault != nil {
		return nil, fault, from.mayBeBehind()
	}
	node, fault := objectNode(arrived, requested, arrived.objects[0], nil)
	return node, fault, false
}

// outdated is whether what the server said about one field says the metadata the request was built from is
// older than the server: the id addresses nothing any more, or what arrived is not shaped as the type kept on
// disk said it would be. Anything else stands, since reading the metadata again would not change it.
func outdated(fault *diag.Fault) bool {
	return fault.Code == diag.NotFound || fault.Code == diag.UpstreamLied
}

func (c *Client) askForField(ctx context.Context, spec *schemas, code, id string, requested []requestedField) (answer, *diag.Fault) {
	// The naming goes out beside what the caller asked for, and only what the caller asked for is printed.
	asked := asking(requested, namingFields())
	return c.passing(ctx, spec, "ProjectCustomField", asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getProjectCustomField(ctx, code, id, fields)
	})
}

// The whole project is read and ordered, and printed whole: $top and $skip count the fields of the array, which
// is not the order of the project, so no page of it is a page of the list.
func (c *Client) listFields(ctx context.Context, spec *schemas, code string, requested []requestedField) (*render.Node, *diag.Fault) {
	asked := asking(requested, requestedField{name: ordinal})
	answer, fault := c.passing(ctx, spec, "[]ProjectCustomField", asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.getProjectCustomFields(ctx, code, fields, everything)
	})
	if fault != nil {
		return nil, fault
	}
	if len(answer.objects) == 0 {
		return nil, noFields(answer.response, code)
	}
	ordered, fault := inOrder(answer)
	if fault != nil {
		return nil, fault
	}
	records, fault := printing(answer, onLinesOfItsOwn).objectsAt(answer.schema, requested, ordered)
	if fault != nil {
		return nil, fault
	}
	return listing("fields", counted(len(ordered)), records), nil
}

type placedField struct {
	place int64
	field map[string]any
}

func inOrder(answer answer) ([]map[string]any, *diag.Fault) {
	placed := make([]placedField, 0, len(answer.objects))
	for _, field := range answer.objects {
		number, isNumber := field[ordinal].(json.Number)
		if !isNumber {
			return nil, shapeFailure(answer.response, answer.body, "the ordinal of a custom field is not a number")
		}
		place, err := number.Int64()
		if err != nil {
			return nil, shapeFailure(answer.response, answer.body, "the ordinal of a custom field is not a whole number")
		}
		placed = append(placed, placedField{place: place, field: field})
	}
	// Fields of one ordinal keep the order the server sent them in, which is the order of their attachment ids.
	slices.SortStableFunc(placed, func(a, b placedField) int { return cmp.Compare(a.place, b.place) })
	ordered := make([]map[string]any, 0, len(placed))
	for _, p := range placed {
		ordered = append(ordered, p.field)
	}
	return ordered, nil
}

// A token without projectRead on the project is sent an empty list rather than a refusal, so nothing arriving
// says the caller cannot read the project, not that the project has no fields.
func noFields(response *http.Response, code string) *diag.Fault {
	details := []render.Pair{
		requestDetail(response.Request.Method, response.Request.URL.Redacted()),
		{Key: "project", Value: render.NewString(code)},
		{Key: "permission", Value: render.NewString(projectRead)},
	}
	message := "not one custom field of the project arrived, and a token without the right under permission is sent an empty list"
	return &diag.Fault{Code: diag.Denied, Message: message, Details: details}
}
