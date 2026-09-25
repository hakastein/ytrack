package youtrack

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	yt "github.com/hakastein/youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

type fieldInfo struct {
	name          string
	localizedName string
	kind          yt.FieldType
}

func readLocalized(value any) (string, bool) {
	switch name := value.(type) {
	case string:
		return name, true
	case nil:
		return "", true
	}
	return "", false
}

func (n fieldInfo) translatedAs(name string) bool {
	return n.localizedName != "" && strings.EqualFold(name, n.localizedName)
}

func (n fieldInfo) translations() []string {
	if n.localizedName == "" {
		return nil
	}
	return []string{n.localizedName}
}

func findMatches(name string, catalogue []fieldInfo) []int {
	var byName, byTranslation []int
	for at, field := range catalogue {
		switch {
		case strings.EqualFold(name, field.name):
			byName = append(byName, at)
		case field.translatedAs(name):
			byTranslation = append(byTranslation, at)
		}
	}
	if len(byName) > 0 {
		return byName
	}
	return byTranslation
}

func pick(catalogue []fieldInfo, places []int) []fieldInfo {
	found := make([]fieldInfo, 0, len(places))
	for _, at := range places {
		found = append(found, catalogue[at])
	}
	return found
}

func nearestNamed(name string, catalogue []fieldInfo) []string {
	among := make([]suggestion, 0, len(catalogue))
	for _, field := range catalogue {
		among = append(among, suggestion{name: field.name, also: field.translations()})
	}
	return nearest(name, among, canonical(catalogue))
}

func canonical(catalogue []fieldInfo) []string {
	names := make([]string, 0, len(catalogue))
	for _, field := range catalogue {
		names = append(names, field.name)
	}
	slices.Sort(names)
	return names
}

type customField struct {
	id   string
	info fieldInfo
}

const (
	brokenField     = "a custom field of the project is not a JSON object"
	brokenFieldInfo = "the name or the type of a custom field is not of the shape the specification gives it"
)

func encodeValue(kind yt.FieldType, text string) (yt.Encoded, string) {
	if text == "" {
		return yt.Encoded{}, emptyValueReason(kind)
	}
	encoded, err := kind.Encode(text)
	var argument *yt.ArgumentError
	switch {
	case errors.As(err, &argument):
		return yt.Encoded{}, argument.Reason
	case err != nil:
		return yt.Encoded{}, err.Error()
	}
	return encoded, ""
}

func emptyValueReason(kind yt.FieldType) string {
	const leftAlone = "; a field is emptied by --clear Name and a field the call does not name is left as it stands"
	switch {
	case kind.ValueType == yt.StringType || kind.ValueType == yt.TextType:
		return fmt.Sprintf("YouTrack keeps a %s field it is given nothing for as holding nothing at all",
			kind.ValueType) + leftAlone
	case kind.Named():
		return fmt.Sprintf("a value of a %s field is a name, and no value is named by nothing", kind.ValueType) + leftAlone
	}
	return fmt.Sprintf("no value of a %s field is empty", kind.ValueType) + leftAlone
}

func (n converter) readValue(kind yt.FieldType, item any) (*render.Node, bool, error) {
	value, present, err := kind.ReadValue(item)
	if err != nil || !present {
		return nil, present, err
	}
	number, isNumber := item.(json.Number)
	switch {
	case isNumber && (kind.ValueType == yt.IntegerType || kind.ValueType == yt.FloatType):
		return render.NewNumber(number), true, nil
	case kind.ValueType == yt.TextType:
		return n.textNode(value.Text), true, nil
	}
	return render.NewString(value.Text), true, nil
}

func (n converter) valueKeys(f issueCustomField) ([]string, *diag.Fault) {
	values, fault := n.valuesOf(f)
	if fault != nil {
		return nil, fault
	}
	texts := make([]string, 0, len(values))
	for _, item := range values {
		value, present, err := f.kind.ReadValue(item)
		if err != nil {
			return nil, n.unreadableValue(f, err)
		}
		if present {
			texts = append(texts, value.Text)
		}
	}
	return texts, nil
}

const (
	customFieldsKey   = "customFields"
	customFieldSchema = "IssueCustomField"
)

func customFieldsAsked(translated bool) []requestedField {
	held := []requestedField{{name: fieldTypeKey, children: []requestedField{{name: valueTypeKey}, {name: "isMultiValue"}}}}
	if translated {
		held = append(held, requestedField{name: "localizedName"})
	}
	return []requestedField{
		{name: nameKey},
		{name: "value", children: valueKeyFields()},
		{name: "projectCustomField", children: []requestedField{
			{name: idKey},
			{name: "ordinal"},
			{name: "field", children: held},
		}},
	}
}

func valueKeyFields() []requestedField {
	var members []requestedField
	for _, key := range yt.ValueKeys() {
		members = append(members, requestedField{name: key})
	}
	return members
}

type issueCustomField struct {
	name          string
	value         any
	kind          yt.FieldType
	ordinal       int64
	binding       string
	localizedName string
}

func (n converter) customFields(asked requestedField, value any) (*render.Node, *diag.Fault) {
	fields, fault := n.readCustomFields(value)
	if fault != nil {
		return nil, fault
	}
	if asked.children == nil {
		return n.allFieldsNode(fields)
	}
	return n.selectedFieldsNode(asked.children, fields)
}

func (n converter) readCustomFields(value any) ([]issueCustomField, *diag.Fault) {
	received, isList := value.([]any)
	if !isList {
		return nil, n.malformed("the custom fields of the issue arrived as something other than an array")
	}
	fields := make([]issueCustomField, 0, len(received))
	named := make(map[string]bool, len(received))
	for _, item := range received {
		field, fault := n.readCustomField(item)
		if fault != nil {
			return nil, fault
		}
		if named[field.name] {
			return nil, n.malformed(fmt.Sprintf("two custom fields of the issue are named %s", render.Quote(field.name)))
		}
		named[field.name] = true
		fields = append(fields, field)
	}
	return fields, nil
}

func (n converter) allFieldsNode(fields []issueCustomField) (*render.Node, *diag.Fault) {
	slices.SortStableFunc(fields, inProjectOrder)
	pairs := make([]render.Pair, 0, len(fields))
	for _, field := range fields {
		printed, present, fault := n.valueNode(field)
		if fault != nil {
			return nil, fault
		}
		if present {
			pairs = append(pairs, render.FromData(field.name, printed))
		}
	}
	return render.NewMap(pairs...), nil
}

func (n converter) selectedFieldsNode(asked []requestedField, fields []issueCustomField) (*render.Node, *diag.Fault) {
	onIssue := make([]fieldInfo, 0, len(fields))
	for _, field := range fields {
		onIssue = append(onIssue, fieldInfo{name: field.name, localizedName: field.localizedName})
	}
	pairs := make([]render.Pair, 0, len(asked))
	for _, name := range asked {
		matched := findMatches(name.name, onIssue)
		if len(matched) == 0 {
			continue
		}
		field := fields[matched[0]]
		printed, present, fault := n.valueNode(field)
		if fault != nil {
			return nil, fault
		}
		if !present {
			printed = emptyValue(field.kind)
		}
		pairs = append(pairs, render.FromData(field.name, printed))
	}
	return render.NewMap(pairs...), nil
}

func emptyValue(kind yt.FieldType) *render.Node {
	if kind.Multi {
		return render.NewList()
	}
	return render.NewNull()
}

func inProjectOrder(a, b issueCustomField) int {
	return cmp.Or(cmp.Compare(a.ordinal, b.ordinal), compareBindings(a.binding, b.binding))
}

func compareBindings(a, b string) int {
	first, isNumbered := bindingNumbers(a)
	second, alsoNumbered := bindingNumbers(b)
	if !isNumbered || !alsoNumbered {
		return strings.Compare(a, b)
	}
	return cmp.Or(cmp.Compare(first[0], second[0]), cmp.Compare(first[1], second[1]))
}

func bindingNumbers(id string) ([2]int, bool) {
	before, after, dashed := strings.Cut(id, "-")
	if !dashed {
		return [2]int{}, false
	}
	first, firstErr := strconv.Atoi(before)
	second, secondErr := strconv.Atoi(after)
	if firstErr != nil || secondErr != nil {
		return [2]int{}, false
	}
	return [2]int{first, second}, true
}

func (n converter) readCustomField(item any) (issueCustomField, *diag.Fault) {
	object, isObject := item.(map[string]any)
	if !isObject {
		return issueCustomField{}, n.malformed("a custom field of the issue is not a JSON object")
	}
	name, isText := object[nameKey].(string)
	if !isText {
		return issueCustomField{}, n.malformed("the name of a custom field of the issue is not text")
	}
	place, _ := object["projectCustomField"].(map[string]any)
	binding, named, whole := readBinding(place)
	if !whole {
		return issueCustomField{}, n.malformed(brokenBinding(name))
	}
	ordinal, isWhole := parseInt64(place["ordinal"])
	if !isWhole {
		message := fmt.Sprintf("the place of the custom field %s among the fields of the project is no whole number", render.Quote(name))
		return issueCustomField{}, n.malformed(message)
	}
	if !named.kind.Known() {
		return issueCustomField{}, n.malformed(unmodelledType(named))
	}
	return issueCustomField{name: name, value: object["value"], kind: named.kind, ordinal: ordinal,
		binding: binding, localizedName: named.localizedName}, nil
}

func brokenBinding(name string) string {
	return fmt.Sprintf("the project's field the custom field %s stands for is not of the shape the "+
		"specification gives it", render.Quote(name))
}

func readBinding(place map[string]any) (binding string, named fieldInfo, ok bool) {
	binding, isText := place[idKey].(string)
	if !isText {
		return "", fieldInfo{}, false
	}
	field, isObject := place["field"].(map[string]any)
	if !isObject {
		return "", fieldInfo{}, false
	}
	kind, isObject := field[fieldTypeKey].(map[string]any)
	if !isObject {
		return "", fieldInfo{}, false
	}
	valueType, isText := kind[valueTypeKey].(string)
	isMultiValue, isFlag := kind["isMultiValue"].(bool)
	if !isText || !isFlag {
		return "", fieldInfo{}, false
	}
	translated, isName := readLocalized(field["localizedName"])
	if !isName {
		return "", fieldInfo{}, false
	}
	fieldType := yt.FieldType{ValueType: yt.ValueType(valueType), Multi: isMultiValue}
	return binding, fieldInfo{localizedName: translated, kind: fieldType}, true
}

func (n converter) valuesOf(f issueCustomField) ([]any, *diag.Fault) {
	values, isList := f.value.([]any)
	switch {
	case f.value == nil:
		return nil, nil
	case isList && !f.kind.Multi:
		return nil, n.malformed(fmt.Sprintf("the custom field %s holds one value by its type and arrived as a list", render.Quote(f.name)))
	case !isList && f.kind.Multi:
		message := fmt.Sprintf("the custom field %s holds more than one value by its type and arrived as "+
			"something other than a list", render.Quote(f.name))
		return nil, n.malformed(message)
	case !isList:
		return []any{f.value}, nil
	}
	return values, nil
}

func (n converter) valueNode(f issueCustomField) (*render.Node, bool, *diag.Fault) {
	values, fault := n.valuesOf(f)
	if fault != nil {
		return nil, false, fault
	}
	items := make([]*render.Node, 0, len(values))
	for _, value := range values {
		node, present, fault := n.valueKeyNode(f, value)
		if fault != nil {
			return nil, false, fault
		}
		if present {
			items = append(items, node)
		}
	}
	switch {
	case len(items) == 0:
		return nil, false, nil
	case !f.kind.Multi:
		return items[0], true, nil
	}
	return render.NewList(items...), true, nil
}

func (n converter) valueKeyNode(f issueCustomField, item any) (*render.Node, bool, *diag.Fault) {
	node, present, err := n.readValue(f.kind, item)
	if err != nil {
		return nil, false, n.unreadableValue(f, err)
	}
	return node, present, nil
}

func (n converter) unreadableValue(f issueCustomField, err error) *diag.Fault {
	return n.malformed(fmt.Sprintf("custom field %s: %v", render.Quote(f.name), err))
}

const customFieldCatalogue = "[]CustomField"

const brokenCatalogue = "a custom field of the instance is named in some shape other than text"

func catalogueFields() []requestedField {
	return []requestedField{{name: nameKey}, {name: "localizedName"}}
}

func (c *Client) customFieldCatalogue(ctx context.Context, spec *schemas) (decodedResponse, []fieldInfo, *diag.Fault) {
	a, fault := c.request(ctx, spec, customFieldCatalogue, catalogueFields(), func(ctx context.Context, fields string) (*http.Response, error) {
		return c.apiGetCustomFields(ctx, fields, topAll)
	})
	if fault != nil {
		return decodedResponse{}, nil, fault
	}
	catalogue := make([]fieldInfo, 0, len(a.objects))
	for _, object := range a.objects {
		found, ok := readCatalogueEntry(object)
		if !ok {
			return decodedResponse{}, nil, shapeFailure(a.httpResponse, a.body, brokenCatalogue)
		}
		catalogue = append(catalogue, found)
	}
	return a, catalogue, nil
}

func readCatalogueEntry(object map[string]any) (fieldInfo, bool) {
	name, isText := object[nameKey].(string)
	if !isText {
		return fieldInfo{}, false
	}
	translated, isName := readLocalized(object["localizedName"])
	if !isName {
		return fieldInfo{}, false
	}
	return fieldInfo{name: name, localizedName: translated}, true
}

func (c *Client) resolveCustomFields(ctx context.Context, spec *schemas, requested []requestedField) *diag.Fault {
	named := namedCustomFields(spec, requested)
	if named == nil || !slices.ContainsFunc(named.children, fromCaller) {
		return nil
	}
	a, catalogue, fault := c.customFieldCatalogue(ctx, spec)
	if fault != nil {
		return fault
	}
	resolved, fault := resolveNames(a, requested, named.children, catalogue)
	if fault != nil {
		return fault
	}
	named.children = resolved
	return nil
}

func fromCaller(name requestedField) bool {
	return name.fromCaller
}

func fromDefault(name requestedField) bool {
	return !name.fromCaller
}

func hasDefaultNames(spec *schemas, requested []requestedField) bool {
	named := namedCustomFields(spec, requested)
	return named != nil && slices.ContainsFunc(named.children, fromDefault)
}

func resolveNames(a decodedResponse, requested, asked []requestedField, catalogue []fieldInfo) ([]requestedField, *diag.Fault) {
	var resolved []requestedField
	var unknown, ambiguous []*render.Node
	for _, name := range asked {
		if !fromCaller(name) {
			resolved = merge(resolved, name)
			continue
		}
		places := findMatches(name.name, catalogue)
		written := fieldPath([]string{customFieldsKey}, formatName(name))
		switch {
		case len(places) == 0:
			unknown = append(unknown, unknownEntry(written, nearestNamed(name.name, catalogue)))
		case len(places) > 1:
			ambiguous = append(ambiguous, ambiguousEntry(written, canonical(pick(catalogue, places))))
		default:
			resolved = merge(resolved, requestedField{name: catalogue[places[0]].name, fromCaller: true})
		}
	}
	switch {
	case len(unknown) > 0:
		message := "the names under unknown are not custom fields of the instance"
		return nil, unresolvedNames(a, requested, "unknown", message, unknown)
	case len(ambiguous) > 0:
		message := "the names under ambiguous are the names of more than one custom field of the instance each"
		return nil, unresolvedNames(a, requested, "ambiguous", message, ambiguous)
	}
	return resolved, nil
}

func unresolvedNames(a decodedResponse, requested []requestedField, key, message string, entries []*render.Node) *diag.Fault {
	against := render.Pair{Key: "fields", Value: render.NewString(formatFields(requested))}
	sent := requestDetail(a.httpResponse.Request.Method, a.httpResponse.Request.URL.Redacted())
	return unknownNames(sent, against, key, message, entries)
}

func unknownNames(sent, against render.Pair, key, message string, entries []*render.Node) *diag.Fault {
	details := []render.Pair{
		sent,
		against,
		{Key: key, Value: render.NewList(entries...)},
	}
	return &diag.Fault{Code: diag.UnknownName, Message: message, Details: details}
}

func ambiguousEntry(field string, candidates []string) *render.Node {
	names := make([]*render.Node, 0, len(candidates))
	for _, name := range candidates {
		names = append(names, render.NewString(name))
	}
	return render.NewMap(
		render.Pair{Key: "field", Value: render.NewString(field)},
		render.Pair{Key: "candidates", Value: render.NewList(names...)})
}

func defaultFields(n fieldInfo) (string, bool) {
	switch {
	case !n.kind.Known():
		return "", false
	case n.kind.BundleFields() == "":
		return FieldListFields, true
	}
	return FieldListFields + "," + n.kind.BundleFields(), true
}

func fieldsToPrint(expression *string, n fieldInfo) (requested []requestedField, modelled bool, fault *diag.Fault) {
	defaults := ""
	if expression == nil || extendsDefault(*expression) {
		if defaults, modelled = defaultFields(n); !modelled {
			return nil, false, nil
		}
	}
	_, requested, fault = fieldsOrDefault(expression, defaults, false)
	return requested, true, fault
}

func unmodelledType(n fieldInfo) string {
	return fmt.Sprintf("valueType %s with isMultiValue %t is not one of the twenty custom-field types ytrack models",
		render.Quote(string(n.kind.ValueType)), n.kind.Multi)
}

func fieldInfoFields() requestedField {
	return requestedField{name: "field", children: []requestedField{
		{name: nameKey},
		{name: "localizedName"},
		{name: fieldTypeKey, children: []requestedField{{name: valueTypeKey}, {name: "isMultiValue"}}},
	}}
}

func readFieldInfo(object map[string]any) (fieldInfo, bool) {
	field, isObject := object["field"].(map[string]any)
	if !isObject {
		return fieldInfo{}, false
	}
	name, isText := field[nameKey].(string)
	if !isText {
		return fieldInfo{}, false
	}
	kind, isObject := field[fieldTypeKey].(map[string]any)
	if !isObject {
		return fieldInfo{}, false
	}
	valueType, isText := kind[valueTypeKey].(string)
	if !isText {
		return fieldInfo{}, false
	}
	isMultiValue, isBool := kind["isMultiValue"].(bool)
	if !isBool {
		return fieldInfo{}, false
	}
	translated, isName := readLocalized(field["localizedName"])
	if !isName {
		return fieldInfo{}, false
	}
	fieldType := yt.FieldType{ValueType: yt.ValueType(valueType), Multi: isMultiValue}
	return fieldInfo{name: name, localizedName: translated, kind: fieldType}, true
}

func lookUp(name string, fields []customField) (customField, bool) {
	places := findMatches(name, fieldInfos(fields))
	if len(places) != 1 {
		return customField{}, false
	}
	return fields[places[0]], true
}

func unresolved(sent yt.Request, code, name string, fields []customField) *diag.Fault {
	catalogue := fieldInfos(fields)
	if places := findMatches(name, catalogue); len(places) > 0 {
		message := "the name under unknown belongs to more than one custom field of the project"
		return unknownField(sent, code, name, canonical(pick(catalogue, places)), message)
	}
	message := "the name under unknown is not a custom field of the project"
	return unknownField(sent, code, name, nearestNamed(name, catalogue), message)
}

func unknownField(sent yt.Request, code, name string, nearest []string, message string) *diag.Fault {
	against := render.Pair{Key: "project", Value: render.NewString(code)}
	return unknownNames(sentRequest(sent), against, "unknown", message, []*render.Node{unknownEntry(name, nearest)})
}

func fieldInfos(fields []customField) []fieldInfo {
	catalogue := make([]fieldInfo, 0, len(fields))
	for _, field := range fields {
		catalogue = append(catalogue, field.info)
	}
	return catalogue
}

func (f customField) hasValidID() bool {
	return isInternalID(f.id)
}

func invalidFieldID(id string) string {
	return fmt.Sprintf("the id %s of a custom field is not two numbers with a dash between them", render.Quote(id))
}

func (n fieldInfo) verifyUnchanged(a decodedResponse, code string) *diag.Fault {
	answered, ok := readFieldInfo(a.objects[0])
	if !ok {
		return shapeFailure(a.httpResponse, a.body, brokenFieldInfo)
	}
	if answered == n {
		return nil
	}
	details := append(responseDetails(a.httpResponse),
		render.Pair{Key: "project", Value: render.NewString(code)},
		render.Pair{Key: "field", Value: render.NewString(n.name)},
		bodyDetail(a.body))
	message := "the custom field the id addresses is no longer the one the name resolved to"
	return &diag.Fault{Code: diag.UpstreamFailed, Message: message, Details: details}
}
