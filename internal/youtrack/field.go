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

const FieldListFields = "field(name,localizedName,fieldType(valueType,isMultiValue)),canBeEmpty"

const projectRead = "jetbrains.jetpass.project-read"

const ordinalKey = "ordinal"

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

func ShowField(code, name string, expression *string) (Call, *diag.Fault) {
	code, fault := parseProjectCode(code)
	if fault != nil {
		return nil, fault
	}
	if name == "" {
		return nil, &diag.Fault{Code: diag.BadUsage, Message: "the name of a custom field is empty"}
	}
	if fault := checkFieldsSyntax(expression); fault != nil {
		return nil, fault
	}
	spec := loadSchemas()
	return func(ctx context.Context, c *Client) (*render.Node, *diag.Fault) {
		return c.showField(ctx, spec, code, name, expression)
	}, nil
}

func checkFieldsSyntax(expression *string) *diag.Fault {
	if expression == nil {
		return nil
	}
	_, fault := parseFields(*expression, FieldListFields)
	return fault
}

func (c *Client) showField(ctx context.Context, spec *schemas, code, name string, expression *string) (*render.Node, *diag.Fault) {
	target := metadataTarget(code)
	if cached, hit := c.cache.load(target); hit {
		node, fault, cacheStale := c.showFieldFrom(ctx, spec, code, name, expression, fromDisk(cached))
		if !cacheStale {
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

func isStale(fault *diag.Fault) bool {
	return fault.Code == diag.NotFound || fault.Code == diag.UpstreamInvalid
}

func (c *Client) getField(ctx context.Context, spec *schemas, code, id string, requested []requestedField) (decodedResponse, *diag.Fault) {
	asked := withFields(requested, fieldInfoFields())
	return c.request(ctx, spec, "ProjectCustomField", asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProjectCustomField(ctx, code, id, fields)
	})
}

func (c *Client) listFields(ctx context.Context, spec *schemas, code string, requested []requestedField) (*render.Node, *diag.Fault) {
	asked := withFields(requested, requestedField{name: ordinalKey})
	decoded, fault := c.request(ctx, spec, "[]ProjectCustomField", asked, func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetProjectCustomFields(ctx, code, fields, topAll)
	})
	if fault != nil {
		return nil, fault
	}
	if len(decoded.objects) == 0 {
		return nil, noFields(decoded.httpResponse, code)
	}
	ordered, fault := sortedByOrdinal(decoded)
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

func sortedByOrdinal(decoded decodedResponse) ([]map[string]any, *diag.Fault) {
	placed := make([]orderedField, 0, len(decoded.objects))
	for _, field := range decoded.objects {
		number, isNumber := field[ordinalKey].(json.Number)
		if !isNumber {
			return nil, shapeFailure(decoded.httpResponse, decoded.body, "the ordinal of a custom field is not a number")
		}
		position, err := number.Int64()
		if err != nil {
			return nil, shapeFailure(decoded.httpResponse, decoded.body, "the ordinal of a custom field is not a whole number")
		}
		placed = append(placed, orderedField{position: position, field: field})
	}
	slices.SortStableFunc(placed, func(a, b orderedField) int { return cmp.Compare(a.position, b.position) })
	ordered := make([]map[string]any, 0, len(placed))
	for _, p := range placed {
		ordered = append(ordered, p.field)
	}
	return ordered, nil
}

func noFields(response *http.Response, code string) *diag.Fault {
	details := []render.Pair{
		requestDetail(response.Request.Method, response.Request.URL.Redacted()),
		{Key: "project", Value: render.NewString(code)},
		{Key: "permission", Value: render.NewString(projectRead)},
	}
	message := "not one custom field of the project arrived, and a token without the right under permission is sent an empty list"
	return &diag.Fault{Code: diag.Denied, Message: message, Details: details}
}
