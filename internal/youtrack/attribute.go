package youtrack

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

const (
	attributesKey           = "attributes"
	workItemAttributeSchema = "WorkItemAttribute"
	attributeFlag           = "--attribute"
	valueKey                = "value"
)

// A work item carries every attribute of its project, with a null value where it was given none; the ids are
// what the check of a write holds the answer to.
func attributesAsked() []requestedField {
	return []requestedField{
		{name: idKey},
		{name: nameKey},
		{name: valueKey, children: []requestedField{{name: idKey}, {name: nameKey}}},
	}
}

func eachAttributes(spec *schemas, at string, requested []requestedField, visit func(parents []string, field *requestedField)) {
	positionsOf(spec, at, workItemAttributeSchema, requested, nil, visit)
}

func refuseAttributeNames(spec *schemas, at, expression string, requested []requestedField) *diag.Fault {
	var fault *diag.Fault
	eachAttributes(spec, at, requested, func(parents []string, field *requestedField) {
		if field.children == nil || fault != nil {
			return
		}
		message := fmt.Sprintf("fields %s: %s holds the attributes of a work item, which are printed as the value "+
			"each holds under its name, so no name stands under it", render.Quote(expression), fieldPath(parents, field.name))
		fault = &diag.Fault{Code: diag.BadUsage, Message: message}
	})
	return fault
}

// attributes is the block the attributes of a work item are printed as: the name each goes by against the name
// of the value it holds, in the order the server sent them.
func (n nodes) attributes(value any) (*render.Node, *diag.Fault) {
	arrived, isList := value.([]any)
	if !isList {
		return nil, n.lied("the attributes of the work item arrived as something other than an array")
	}
	pairs := make([]render.Pair, 0, len(arrived))
	named := make(map[string]bool, len(arrived))
	for _, item := range arrived {
		name, isText := memberOf(item, nameKey).(string)
		if !isText {
			return nil, n.lied("an attribute of the work item arrived without a name")
		}
		if named[name] {
			return nil, n.lied(fmt.Sprintf("two attributes of the work item are named %s", render.Quote(name)))
		}
		named[name] = true
		held := render.NewNull()
		if value := memberOf(item, valueKey); value != nil {
			valueName, isText := memberOf(value, nameKey).(string)
			if !isText {
				return nil, n.lied(fmt.Sprintf("the value of the attribute %s arrived without a name", render.Quote(name)))
			}
			held = render.NewString(valueName)
		}
		pairs = append(pairs, render.FromData(name, held))
	}
	return render.NewMap(pairs...), nil
}

// attributeValues reads --attribute. The split is at the first =, as it is for --field, and an attribute
// named twice is refused: which of the two values the server kept would be its choice, not the caller's.
func attributeValues(filled []string) ([]namedValue, *diag.Fault) {
	named := make([]namedValue, 0, len(filled))
	for _, flag := range filled {
		name, value, split := strings.Cut(flag, "=")
		switch {
		case !split:
			message := fmt.Sprintf("%s %s holds no =: an attribute is set by writing its name, an = and the value, "+
				"as in %s 'Формат работы=ИИагент'", attributeFlag, render.Quote(flag), attributeFlag)
			return nil, &diag.Fault{Code: diag.BadUsage, Message: message}
		case name == "":
			message := fmt.Sprintf("%s %s names no attribute: the name stands before the =", attributeFlag, render.Quote(flag))
			return nil, &diag.Fault{Code: diag.BadUsage, Message: message}
		case value == "":
			message := fmt.Sprintf("%s %s names no value: --clear takes an attribute away", attributeFlag, render.Quote(flag))
			return nil, &diag.Fault{Code: diag.BadUsage, Message: message}
		}
		if slices.ContainsFunc(named, func(earlier namedValue) bool { return strings.EqualFold(earlier.name, name) }) {
			message := fmt.Sprintf("%s names the attribute %s twice", attributeFlag, render.Quote(name))
			return nil, &diag.Fault{Code: diag.BadUsage, Message: message}
		}
		named = append(named, namedValue{name: name, value: value})
	}
	return named, nil
}

// One attribute of the project a work item may be written against, with the values it takes.
type projectAttribute struct {
	id     string
	name   string
	values []workItemType
}

// An attribute as the read before the write resolved it: the id the body addresses it by, the name the project
// gives it, which a disagreement is shown under, and the value, nil where the call takes the attribute away.
type filedAttribute struct {
	id    string
	name  string
	value *filedWorkItemType
}

// What goes out for one attribute: its id and the id of its value, or an explicit null that takes it away.
type attributeWritten struct {
	ID    string             `json:"id"`
	Value *addressedWorkItem `json:"value"`
}

func attributesWritten(filed []filedAttribute) []attributeWritten {
	written := make([]attributeWritten, 0, len(filed))
	for _, attribute := range filed {
		sent := attributeWritten{ID: attribute.id}
		if attribute.value != nil {
			sent.Value = &addressedWorkItem{ID: attribute.value.id}
		}
		written = append(written, sent)
	}
	return written
}

// resolvingAttributes resolves by the rule a type of work is resolved by, and refuses every name that answers to
// no one attribute or value at once, before anything is written.
func (p projectWorkItemTypes) resolvingAttributes(set []namedValue, cleared []string) ([]filedAttribute, *diag.Fault) {
	catalogue := make([]naming, 0, len(p.attributes))
	for _, attribute := range p.attributes {
		catalogue = append(catalogue, naming{name: attribute.name})
	}
	var filed []filedAttribute
	var unknown []*render.Node
	for _, written := range set {
		at, found := resolvedAmong(written.name, catalogue)
		if !found {
			unknown = append(unknown, unknownAttribute(written.name, catalogue))
			continue
		}
		attribute := p.attributes[at]
		values := make([]naming, 0, len(attribute.values))
		for _, value := range attribute.values {
			values = append(values, naming{name: value.name})
		}
		place, found := resolvedAmong(written.value, values)
		if !found {
			unknown = append(unknown, render.NewMap(
				render.Pair{Key: "attribute", Value: render.NewString(attribute.name)},
				render.Pair{Key: valueKey, Value: render.NewString(written.value)},
				render.Pair{Key: "nearest", Value: render.NewList(names(nearestNamed(written.value, values))...)}))
			continue
		}
		value := filedWorkItemType{id: attribute.values[place].id, written: written.value}
		filed = append(filed, filedAttribute{id: attribute.id, name: attribute.name, value: &value})
	}
	for _, name := range cleared {
		at, found := resolvedAmong(name, catalogue)
		if !found {
			unknown = append(unknown, unknownAttribute(name, catalogue))
			continue
		}
		if slices.ContainsFunc(filed, func(f filedAttribute) bool { return f.id == p.attributes[at].id }) {
			return nil, attributeBothWays(p.attributes[at].name)
		}
		filed = append(filed, filedAttribute{id: p.attributes[at].id, name: p.attributes[at].name})
	}
	if len(unknown) > 0 {
		message := "the names under unknown are not attributes of the work items of the project, or values they take"
		return nil, unknownNames(p.arrived.response, render.Pair{Key: projectKey, Value: render.NewString(p.project)},
			"unknown", message, unknown)
	}
	return filed, nil
}

func attributeBothWays(name string) *diag.Fault {
	message := fmt.Sprintf("%s sets the attribute %s and --clear takes it away, and the call gives both",
		attributeFlag, render.Quote(name))
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

func unknownAttribute(name string, catalogue []naming) *render.Node {
	return render.NewMap(
		render.Pair{Key: "attribute", Value: render.NewString(name)},
		render.Pair{Key: "nearest", Value: render.NewList(names(nearestNamed(name, catalogue))...)})
}

// resolvedAmong is the one entry of the catalogue a name answers to: letter case aside, and, where several
// answer, the one written byte for byte, so the name an entry is printed under stays its address.
func resolvedAmong(name string, catalogue []naming) (int, bool) {
	places := answering(name, catalogue)
	if len(places) > 1 {
		places = slices.DeleteFunc(places, func(at int) bool { return catalogue[at].name != name })
	}
	if len(places) != 1 {
		return 0, false
	}
	return places[0], true
}

// The attributes of the project, read off the same settings as its types of work.
func attributesOf(a answer, settings map[string]any) ([]projectAttribute, *diag.Fault) {
	items, isList := settings[attributesKey].([]any)
	if !isList {
		return nil, shapeFailure(a.response, a.body, "the attributes of work items of the project are not a JSON array")
	}
	attributes := make([]projectAttribute, 0, len(items))
	for _, item := range items {
		id, isText := memberOf(item, idKey).(string)
		name, isNamed := memberOf(item, nameKey).(string)
		values, isList := memberOf(item, "values").([]any)
		if !isText || !isNamed || !isList {
			return nil, shapeFailure(a.response, a.body, brokenAttribute)
		}
		attribute := projectAttribute{id: id, name: name}
		for _, value := range values {
			id, isText := memberOf(value, idKey).(string)
			name, isNamed := memberOf(value, nameKey).(string)
			if !isText || !isNamed {
				return nil, shapeFailure(a.response, a.body, brokenAttribute)
			}
			attribute.values = append(attribute.values, workItemType{id: id, name: name})
		}
		attributes = append(attributes, attribute)
	}
	return attributes, nil
}

const brokenAttribute = "an attribute of work items of the project, or a value of one, arrived without its id or its name"

// attributeMismatches holds the attributes that went out against the ones that came back, by the id each went out
// under; a value is shown by name, since a caller who wrote one has no id of theirs to read.
func attributeMismatches(wrong []mismatch, filed []filedAttribute, value any) []mismatch {
	arrived, _ := value.([]any)
	for _, attribute := range filed {
		at := slices.IndexFunc(arrived, func(item any) bool { return memberOf(item, idKey) == attribute.id })
		var kept any
		if at >= 0 {
			kept = memberOf(arrived[at], valueKey)
		}
		if attribute.value == nil {
			if kept != nil {
				wrong = append(wrong, mismatch{field: attribute.name, written: render.NewNull(), arrived: asArrived(memberOf(kept, nameKey))})
			}
			continue
		}
		if at >= 0 && memberOf(kept, idKey) == attribute.value.id {
			continue
		}
		wrong = append(wrong, mismatch{
			field:   attribute.name,
			written: render.NewString(attribute.value.written),
			arrived: asArrived(memberOf(kept, nameKey)),
		})
	}
	return wrong
}
