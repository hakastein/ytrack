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
	metadata, fault := c.request(ctx, spec, "Project", metadataFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProject(ctx, code, fields)
	})
	if fault != nil {
		return nil, fault
	}
	fields, fault := readMetadata(metadata)
	if fault != nil {
		return nil, fault
	}
	if len(fields) == 0 {
		return nil, noFields(metadata.httpResponse, code)
	}
	c.cache.store(target, fields)
	node, fault, _ := c.showFieldFrom(ctx, spec, code, name, expression, fromServer(metadata, fields))
	return node, fault
}

// A metadataSource is the metadata one showing of a field is built from, and what a step that does not carry through
// means for it. Metadata off the disk may be older than the server, so such a step prints nothing and sends the
// call back for the metadata; metadata the server has just sent is the last word, so the same step refuses over
// the answer it arrived in (ADR-0002).
type metadataSource struct {
	fields   []customField
	response *decodedResponse
}

func fromServer(decoded decodedResponse, fields []customField) metadataSource {
	return metadataSource{fields: fields, response: &decoded}
}

func fromDisk(fields []customField) metadataSource {
	return metadataSource{fields: fields}
}

func (s metadataSource) fromCache() bool {
	return s.response == nil
}

func (s metadataSource) handleStale(reject func(decodedResponse) *diag.Fault) (*render.Node, *diag.Fault, bool) {
	if s.fromCache() {
		return nil, nil, true
	}
	return nil, reject(*s.response), false
}

// showFieldFrom is the whole of field show over one set of metadata: the name resolved, the id held to the form
// a path takes, what to print settled against what the field holds, the field asked for by that id and the
// answer confirmed against the naming the name resolved to. The steps are the same whichever metadata they run
// over, and the source alone says what a step that does not carry through comes to.
func (c *Client) showFieldFrom(ctx context.Context, spec *schemas, code, name string, expression *string, from metadataSource) (*render.Node, *diag.Fault, bool) {
	found, ok := lookUp(name, from.fields)
	if !ok {
		return from.handleStale(func(a decodedResponse) *diag.Fault { return unresolved(a, code, name, from.fields) })
	}
	if !found.hasValidID() {
		return from.handleStale(func(a decodedResponse) *diag.Fault { return invalidFieldIDFault(found.id, a) })
	}
	requested, modelled, fault := fieldsToPrint(expression, found.info)
	if fault != nil {
		// The grammar of the expression is the caller's own, and reading the metadata again would not mend it.
		return nil, fault, false
	}
	if !modelled {
		return from.handleStale(func(a decodedResponse) *diag.Fault { return unmodelledType(found.info, a) })
	}
	decoded, fault := c.getField(ctx, spec, code, found.id, requested)
	if fault != nil {
		return nil, fault, from.fromCache() && isStale(fault)
	}
	if fault := found.info.verifyUnchanged(decoded, code); fault != nil {
		return nil, fault, from.fromCache()
	}
	node, fault := objectNode(decoded, requested, decoded.objects[0], nil)
	return node, fault, false
}

// isStale is whether what the server said about one field says the metadata the request was built from is
// older than the server: the id addresses nothing any more, or what arrived is not shaped as the type kept on
// disk said it would be. Anything else stands, since reading the metadata again would not change it.
func isStale(fault *diag.Fault) bool {
	return fault.Code == diag.NotFound || fault.Code == diag.UpstreamInvalid
}

func (c *Client) getField(ctx context.Context, spec *schemas, code, id string, requested []requestedField) (decodedResponse, *diag.Fault) {
	// The naming goes out beside what the caller asked for, and only what the caller asked for is printed.
	asked := withFields(requested, fieldInfoFields())
	return c.request(ctx, spec, "ProjectCustomField", asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProjectCustomField(ctx, code, id, fields)
	})
}

// The whole project is read and ordered, and printed whole: $top and $skip count the fields of the array, which
// is not the order of the project, so no page of it is a page of the list.
func (c *Client) listFields(ctx context.Context, spec *schemas, code string, requested []requestedField) (*render.Node, *diag.Fault) {
	asked := withFields(requested, requestedField{name: ordinal})
	decoded, fault := c.request(ctx, spec, "[]ProjectCustomField", asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProjectCustomFields(ctx, code, fields, topAll)
	})
	if fault != nil {
		return nil, fault
	}
	if len(decoded.objects) == 0 {
		return nil, noFields(decoded.httpResponse, code)
	}
	ordered, fault := inOrder(decoded)
	if fault != nil {
		return nil, fault
	}
	records, fault := newConverter(decoded, blockLayout).objectsAt(decoded.schema, requested, ordered)
	if fault != nil {
		return nil, fault
	}
	return countedListDocument("fields", counted(len(ordered)), records), nil
}

type orderedField struct {
	position int64
	field    map[string]any
}

func inOrder(decoded decodedResponse) ([]map[string]any, *diag.Fault) {
	placed := make([]orderedField, 0, len(decoded.objects))
	for _, field := range decoded.objects {
		number, isNumber := field[ordinal].(json.Number)
		if !isNumber {
			return nil, shapeFailure(decoded.httpResponse, decoded.body, "the ordinal of a custom field is not a number")
		}
		position, err := number.Int64()
		if err != nil {
			return nil, shapeFailure(decoded.httpResponse, decoded.body, "the ordinal of a custom field is not a whole number")
		}
		placed = append(placed, orderedField{position: position, field: field})
	}
	// Fields of one ordinal keep the order the server sent them in, which is the order of their attachment ids.
	slices.SortStableFunc(placed, func(a, b orderedField) int { return cmp.Compare(a.position, b.position) })
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
