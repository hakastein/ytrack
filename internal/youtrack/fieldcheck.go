package youtrack

import (
	"cmp"
	"net/http"
	"slices"
	"strings"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

type fieldNode struct {
	parent   *fieldNode
	field    requestedField
	children []*fieldNode
	// Each $type the server named on the objects that stood here, once.
	named []string
}

type missingField struct {
	at   *fieldNode
	name string
	// The $type named on the object; typed is false where the object named none or a scalar stood there.
	named  string
	typed  bool
	scalar bool
	// ytrack asked for the name itself, so no caller can be told to fix it.
	internal bool
}

type schemaSet struct {
	schemas []string
	names   []string
	untyped bool
}

type schemaResolver struct {
	schemas    *schemas
	schemaSets map[*fieldNode]schemaSet
}

// checkMissingFields refuses the names absent from the answer, unless the $type the server named shows a name to
// belong to another schema of its place.
func checkMissingFields(spec *schemas, response *http.Response, responseSchema string, requested []requestedField, tree any) *diag.Fault {
	root, absences := findMissingFields(requested, tree)
	if len(absences) == 0 {
		return nil
	}
	j := newSchemaResolver(spec, responseSchema, root)
	var missing, unknown []*render.Node
	listed := map[string]bool{}
	for _, a := range absences {
		code := j.classify(a)
		if code == "" {
			continue
		}
		field := fieldPath(a.at.path(), a.name)
		if listed[string(code)+" "+field] {
			continue
		}
		listed[string(code)+" "+field] = true
		if code == diag.UpstreamInvalid {
			missing = append(missing, missingEntry(field, a))
		} else {
			unknown = append(unknown, unknownEntry(field, nearestNames(a.name, j.schemaSets[a.at].names)))
		}
	}
	details := []render.Pair{
		requestDetail(response.Request.Method, response.Request.URL.Redacted()),
		{Key: "fields", Value: render.NewString(formatFields(requested))},
	}
	// A field the server did not send stays unsent whatever name is fixed.
	if len(missing) > 0 {
		message := "the fields under missing were asked for and did not arrive: the caller's rights may hide them"
		details = append(details, render.Pair{Key: "missing", Value: render.NewList(missing...)})
		return &diag.Fault{Code: diag.UpstreamInvalid, Message: message, Details: details}
	}
	if len(unknown) > 0 {
		message := "the names under unknown are not declared where they were asked for"
		details = append(details, render.Pair{Key: "unknown", Value: render.NewList(unknown...)})
		return &diag.Fault{Code: diag.UnknownName, Message: message, Details: details}
	}
	return nil
}

func findMissingFields(requested []requestedField, tree any) (*fieldNode, []missingField) {
	s := missingFieldCollector{found: map[missingField]bool{}}
	root := newFieldNode(nil, requestedField{children: requested})
	s.visit(root, tree)
	return root, s.absences
}

type missingFieldCollector struct {
	absences []missingField
	found    map[missingField]bool
}

func newFieldNode(parent *fieldNode, field requestedField) *fieldNode {
	p := &fieldNode{parent: parent, field: field}
	for _, child := range field.children {
		p.children = append(p.children, newFieldNode(p, child))
	}
	return p
}

func (p *fieldNode) path() []string {
	if p.parent == nil {
		return nil
	}
	return append(p.parent.path(), p.field.name)
}

// visit walks the fields asked of p over the value standing there and stops where it is null or [].
func (s *missingFieldCollector) visit(p *fieldNode, value any) {
	switch value := value.(type) {
	case nil:
	case []any:
		for _, item := range value {
			s.visit(p, item)
		}
	case map[string]any:
		named, typed := value["$type"].(string)
		if typed && !slices.Contains(p.named, named) {
			p.named = append(p.named, named)
		}
		for i, field := range p.field.children {
			child, ok := value[field.name]
			switch {
			case !ok:
				s.add(missingField{at: p, name: field.name, named: named, typed: typed, internal: fromDefault(field)})
			case field.children != nil && !field.normalized:
				s.visit(p.children[i], child)
			}
		}
	default:
		for _, field := range p.field.children {
			s.add(missingField{at: p, name: field.name, scalar: true, internal: fromDefault(field)})
		}
	}
}

func (s *missingFieldCollector) add(a missingField) {
	if !s.found[a] {
		s.found[a] = true
		s.absences = append(s.absences, a)
	}
}

func newSchemaResolver(schemas *schemas, responseSchema string, root *fieldNode) schemaResolver {
	j := schemaResolver{schemas: schemas, schemaSets: map[*fieldNode]schemaSet{}}
	top := schemas.subtree(responseSchema)
	j.assignSchemas(root, schemaSet{schemas: top, names: schemas.names(top)})
	return j
}

func (j schemaResolver) assignSchemas(p *fieldNode, f schemaSet) {
	j.schemaSets[p] = f
	for _, child := range p.children {
		j.assignSchemas(child, j.childSchemas(f, child))
	}
}

func (j schemaResolver) childSchemas(parentSet schemaSet, p *fieldNode) schemaSet {
	var schemas []string
	untyped := false
	for _, owner := range parentSet.schemas {
		decl, declared := j.schemas.declaration(owner, p.field.name)
		switch {
		case decl.schema != "":
			schemas = append(schemas, j.schemas.subtree(decl.schema)...)
		case declared:
			untyped = true
		}
	}
	if untyped || schemas == nil {
		schemas = append(schemas, j.schemas.hierarchies(p.named)...)
	}
	for _, schema := range p.field.extraSchemas {
		schemas = append(schemas, j.schemas.subtree(schema)...)
	}
	return schemaSet{schemas: schemas, names: j.schemas.names(schemas), untyped: untyped}
}

func (j schemaResolver) classify(a missingField) diag.Code {
	f := j.schemaSets[a.at]
	member := slices.Contains(f.schemas, a.named)
	switch {
	case member && j.declares(a.named, a.name):
		return diag.UpstreamInvalid
	case !slices.Contains(f.names, a.name):
		// unknown_name hands the caller a name of theirs to fix, and a name of ytrack's own is none: the
		// specification declares neither styleRanges nor the members of a range, and the request asks for
		// them all the same.
		if a.internal {
			return diag.UpstreamInvalid
		}
		return diag.UnknownName
	case member, a.scalar && f.untyped:
		return ""
	}
	return diag.UpstreamInvalid
}

func (j schemaResolver) declares(schema, name string) bool {
	_, ok := j.schemas.declaration(schema, name)
	return ok
}

// A nested name is written the way fields= nests it: leader(login).
func fieldPath(parents []string, name string) string {
	return strings.Join(append(slices.Clip(parents), name), "(") + strings.Repeat(")", len(parents))
}

func missingEntry(field string, a missingField) *render.Node {
	schema := render.NewNull()
	if a.typed {
		schema = render.NewString(a.named)
	}
	return render.NewMap(render.Pair{Key: "field", Value: render.NewString(field)}, render.Pair{Key: "type", Value: schema})
}

func unknownEntry(field string, nearest []string) *render.Node {
	return nearestEntry("field", field, nearest)
}

// nearestEntry is a name that resolved to nothing and the names to write instead, standing under the key the
// name was written by: a field of a fields= expression, a category of --category.
func nearestEntry(key, written string, nearest []string) *render.Node {
	names := make([]*render.Node, 0, len(nearest))
	for _, name := range nearest {
		names = append(names, render.NewString(name))
	}
	return render.NewMap(render.Pair{Key: key, Value: render.NewString(written)}, render.Pair{Key: "nearest", Value: render.NewList(names...)})
}

// A name the caller may have meant, and the other forms it answers to: a custom field answers to what the
// project calls it as well. Whichever form is nearest, the name is what the caller is handed.
type suggestion struct {
	name string
	also []string
}

func nearest(asked string, among []suggestion, fallback []string) []string {
	type candidate struct {
		name     string
		distance int
	}
	lowered := []rune(strings.ToLower(asked))
	var near []candidate
	for _, s := range among {
		d := distance(lowered, []rune(strings.ToLower(s.name)))
		for _, form := range s.also {
			d = min(d, distance(lowered, []rune(strings.ToLower(form))))
		}
		if d <= 2 {
			near = append(near, candidate{name: s.name, distance: d})
		}
	}
	if len(near) == 0 {
		return fallback
	}
	slices.SortFunc(near, func(a, b candidate) int {
		return cmp.Or(cmp.Compare(a.distance, b.distance), strings.Compare(a.name, b.name))
	})
	names := make([]string, 0, min(len(near), 5))
	for _, c := range near[:min(len(near), 5)] {
		names = append(names, c.name)
	}
	return names
}

// A name declared by a schema goes by itself alone, and a caller near none of them is shown every name the
// place declares.
func nearestNames(asked string, names []string) []string {
	among := make([]suggestion, 0, len(names))
	for _, name := range names {
		among = append(among, suggestion{name: name})
	}
	return nearest(asked, among, names)
}

func distance(a, b []rune) int {
	previous, current := make([]int, len(b)+1), make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			substitution := previous[j-1]
			if a[i-1] != b[j-1] {
				substitution++
			}
			current[j] = min(previous[j]+1, current[j-1]+1, substitution)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}
