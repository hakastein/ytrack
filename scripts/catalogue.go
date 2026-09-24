//go:build ignore

// Command catalogue writes the schemas of the specification as the Go source the passage judges
// names by: go run scripts/catalogue.go <openapi.json> <output.go>.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"maps"
	"os"
	"slices"
	"strings"
)

const refPrefix = "#/components/schemas/"

// An int64 and a string property are written apart until classify settles their class; timeElement and
// proseElement are what an instant and prose end up written as, and internal/youtrack/catalogue.go reads
// those two back.
const (
	int64Element  = "!int64"
	stringElement = "!string"
	timeElement   = "!time"
	proseElement  = "!prose"
)

// Every key the specification uses is named, those the catalogue ignores included, so that a key of a
// shape it cannot read fails the generation instead of passing unread.
type schemaObject struct {
	Type          string                     `json:"type"`
	Description   json.RawMessage            `json:"description"`
	Properties    map[string]json.RawMessage `json:"properties"`
	Discriminator *discriminator             `json:"discriminator"`
	AllOf         []json.RawMessage          `json:"allOf"`
}

type allOfMember struct {
	Ref        string                     `json:"$ref"`
	Type       string                     `json:"type"`
	Properties map[string]json.RawMessage `json:"properties"`
}

type discriminator struct {
	PropertyName string            `json:"propertyName"`
	Mapping      map[string]string `json:"mapping"`
}

type property struct {
	Ref      string          `json:"$ref"`
	Type     string          `json:"type"`
	Items    *items          `json:"items"`
	Format   json.RawMessage `json:"format"`
	Enum     json.RawMessage `json:"enum"`
	Nullable json.RawMessage `json:"nullable"`
	ReadOnly json.RawMessage `json:"readOnly"`
}

type items struct {
	Ref    string          `json:"$ref"`
	Type   string          `json:"type"`
	Format json.RawMessage `json:"format"`
}

type schema struct {
	parent     string
	properties map[string]string
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/catalogue.go <openapi.json> <output.go>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "catalogue:", err)
		os.Exit(1)
	}
}

func run(specPath, outPath string) error {
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	var spec struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return fmt.Errorf("%s: %w", specPath, err)
	}
	if len(spec.Components.Schemas) == 0 {
		return fmt.Errorf("%s declares no schemas", specPath)
	}
	objects := map[string]schemaObject{}
	for name, raw := range spec.Components.Schemas {
		var object schemaObject
		if err := decodeStrictly(raw, &object); err != nil {
			return fmt.Errorf("schema %s: %w", name, err)
		}
		objects[name] = object
	}
	names, err := typeNames(objects)
	if err != nil {
		return err
	}
	catalogue := map[string]schema{}
	for _, name := range slices.Sorted(maps.Keys(objects)) {
		s, err := read(objects[name], names, objects)
		if err != nil {
			return fmt.Errorf("schema %s: %w", name, err)
		}
		if err := classify(name, s.properties); err != nil {
			return err
		}
		if _, taken := catalogue[names[name]]; taken {
			return fmt.Errorf("schema %s: $type %s names another schema too", name, names[name])
		}
		catalogue[names[name]] = s
	}
	// The passage walks up from a schema to the schemas it extends until there are none.
	for _, name := range slices.Sorted(maps.Keys(catalogue)) {
		steps := 0
		for s := catalogue[name]; s.parent != ""; s = catalogue[s.parent] {
			if steps++; steps > len(catalogue) {
				return fmt.Errorf("schema %s extends itself", name)
			}
		}
	}
	source, err := emit(catalogue)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, source, 0o644)
}

// A schema goes into the catalogue under what the server writes in $type, which a discriminator maps to
// the schema: $type SingleValueIssueCustomField is the schema DatabaseSingleValueIssueCustomField.
func typeNames(objects map[string]schemaObject) (map[string]string, error) {
	names := map[string]string{}
	for name := range objects {
		names[name] = name
	}
	for _, owner := range slices.Sorted(maps.Keys(objects)) {
		d := objects[owner].Discriminator
		if d == nil {
			continue
		}
		if d.PropertyName != "$type" {
			return nil, fmt.Errorf("schema %s: the discriminator is %q, not $type", owner, d.PropertyName)
		}
		for _, typeName := range slices.Sorted(maps.Keys(d.Mapping)) {
			target, err := resolve(d.Mapping[typeName], objects)
			if err != nil {
				return nil, fmt.Errorf("schema %s: discriminator %s: %w", owner, typeName, err)
			}
			if typeName == target {
				continue
			}
			if _, clash := objects[typeName]; clash {
				return nil, fmt.Errorf("schema %s: discriminator %s: names schema %s, and schema %s has that name of its own", owner, typeName, target, typeName)
			}
			if names[target] != target && names[target] != typeName {
				return nil, fmt.Errorf("schema %s: discriminator %s: names schema %s, which is named %s already", owner, typeName, target, names[target])
			}
			names[target] = typeName
		}
	}
	return names, nil
}

func read(object schemaObject, names map[string]string, objects map[string]schemaObject) (schema, error) {
	var s schema
	properties := object.Properties
	if object.AllOf != nil {
		if object.Type != "" || object.Properties != nil {
			return s, errors.New("allOf beside a type or properties of its own")
		}
		extended, own := false, false
		for _, raw := range object.AllOf {
			var member allOfMember
			if err := decodeStrictly(raw, &member); err != nil {
				return s, fmt.Errorf("allOf: %w", err)
			}
			switch {
			case member.Ref != "" && member.Type == "" && member.Properties == nil && !extended:
				parent, err := resolve(member.Ref, objects)
				if err != nil {
					return s, fmt.Errorf("allOf: %w", err)
				}
				s.parent, extended = names[parent], true
			case member.Ref == "" && member.Type == "object" && !own:
				properties, own = member.Properties, true
			default:
				return s, errors.New("allOf holds more than a schema to extend and an object")
			}
		}
		if !extended {
			return s, errors.New("allOf extends no schema")
		}
	} else if object.Type != "object" {
		return s, fmt.Errorf("type %q, not object", object.Type)
	}
	for _, name := range slices.Sorted(maps.Keys(properties)) {
		element, err := encode(properties[name], names, objects)
		if err != nil {
			return s, fmt.Errorf("property %s: %w", name, err)
		}
		if s.properties == nil {
			s.properties = map[string]string{}
		}
		s.properties[name] = element
	}
	return s, nil
}

// The specification gives an instant and a count the same type and format, so neither the class of a name nor
// the completeness of the two classes can be read off it: every int64 is named here and nowhere else, and one
// that is in neither class stops the generation. The cost of a name nobody classified is then a failed make
// ytapi, not a size printed as a date.
//
// Prose is named the same way and needs no completeness of its own: a string outside the class is quoted,
// which loses nothing, so a name nobody classified costs the layout of a value rather than the value.
func classify(owner string, properties map[string]string) error {
	instants := []string{
		"added", "assembleDate", "created", "creationDate", "date", "fetched", "finish", "releaseDate",
		"removed", "resolved", "start", "startDate", "startedTime", "timestamp", "updated",
	}
	numbers := []string{
		"availableDiskSpace", "count", "maxUploadFileSize", "numberInProject", "ordinal", "size",
		"startingNumber", "totalTransactions", "usersCount",
	}
	prose := []string{"content", "description", "text"}
	for _, name := range slices.Sorted(maps.Keys(properties)) {
		if written, isString := strings.CutSuffix(properties[name], stringElement); isString {
			if slices.Contains(prose, name) {
				written += proseElement
			}
			properties[name] = written
			continue
		}
		written, isInt64 := strings.CutSuffix(properties[name], int64Element)
		switch {
		case !isInt64:
		case slices.Contains(instants, name):
			properties[name] = written + timeElement
		case slices.Contains(numbers, name):
			properties[name] = written
		default:
			return fmt.Errorf("the int64 property %s.%s is in neither the class of instants nor the class of numbers of scripts/catalogue.go", owner, name)
		}
	}
	return nil
}

// "" is a scalar, "X" an object of schema X, "{}" an object of no schema, and "[]" before "" or "X" a list.
func encode(raw json.RawMessage, names map[string]string, objects map[string]schemaObject) (string, error) {
	var p property
	if err := decodeStrictly(raw, &p); err != nil {
		return "", err
	}
	switch {
	case p.Ref != "" && p.Type == "" && p.Items == nil:
		return reference(p.Ref, names, objects)
	case p.Ref == "" && p.Type == "array" && p.Items != nil:
		switch {
		case p.Items.Ref != "" && p.Items.Type == "":
			element, err := reference(p.Items.Ref, names, objects)
			return "[]" + element, err
		case p.Items.Ref == "" && scalar(p.Items.Type):
			return "[]" + scalarElement(p.Items.Type, p.Items.Format), nil
		}
	case p.Ref == "" && p.Type == "object" && p.Items == nil:
		return "{}", nil
	case p.Ref == "" && scalar(p.Type) && p.Items == nil:
		return scalarElement(p.Type, p.Format), nil
	}
	return "", errors.New("not a scalar, an object, or a list of scalars or of objects of a schema")
}

func scalarElement(kind string, format json.RawMessage) string {
	switch {
	case kind == "integer" && string(format) == `"int64"`:
		return int64Element
	case kind == "string":
		return stringElement
	}
	return ""
}

func scalar(t string) bool {
	return t == "string" || t == "integer" || t == "number" || t == "boolean"
}

func reference(ref string, names map[string]string, objects map[string]schemaObject) (string, error) {
	target, err := resolve(ref, objects)
	return names[target], err
}

func resolve(ref string, objects map[string]schemaObject) (string, error) {
	name, found := strings.CutPrefix(ref, refPrefix)
	if _, exists := objects[name]; !found || !exists {
		return "", fmt.Errorf("%q is not a schema of the specification", ref)
	}
	return name, nil
}

func decodeStrictly(raw json.RawMessage, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(into)
}

func emit(catalogue map[string]schema) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("// Code generated by scripts/catalogue.go from api/openapi.json. DO NOT EDIT.\n\n")
	b.WriteString("package youtrack\n\nfunc catalogue() map[string]schema {\n\treturn map[string]schema{\n")
	for _, name := range slices.Sorted(maps.Keys(catalogue)) {
		s := catalogue[name]
		var fields []string
		if s.parent != "" {
			fields = append(fields, fmt.Sprintf("parent: %q", s.parent))
		}
		if s.properties != nil {
			var properties strings.Builder
			for _, property := range slices.Sorted(maps.Keys(s.properties)) {
				fmt.Fprintf(&properties, "%q: %q,\n", property, s.properties[property])
			}
			fields = append(fields, "properties: map[string]string{\n"+properties.String()+"}")
		}
		fmt.Fprintf(&b, "%q: {%s},\n", name, strings.Join(fields, ", "))
	}
	b.WriteString("}\n}\n")
	return format.Source(b.Bytes())
}
