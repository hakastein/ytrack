package youtrack

import (
	"cmp"
	"net/http"
	"slices"
	"strings"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// A place is where values stand in the answer: its root, or a field asked of the values at the place
// above. Every item of a list stands at the place of the list.
type place struct {
	above *place
	field requestedField
	below []*place
	// Each $type the server named on the objects that stood here, once.
	named []string
}

// An absence is a name asked of a value that lacks it.
type absence struct {
	at   *place
	name string
	// The $type named on the object; typed is false where the object named none or a scalar stood there.
	named  string
	typed  bool
	scalar bool
	// ytrack asked for the name itself, so no caller can be told to fix it.
	mine bool
}

// A family is the schemas an object at a place may be of, with every name they declare.
type family struct {
	schemas []string
	names   []string
	// A schema of the family above declares the field a scalar or an object of no schema, where the server
	// sends scalars: a custom field's value is one.
	untyped bool
}

type judgment struct {
	schemas  *schemas
	families map[*place]family
}

// judge refuses the names absent from the answer, unless the $type the server named shows a name to
// belong to another schema of its place.
func judge(spec *schemas, response *http.Response, answerSchema string, requested []requestedField, tree any) *diag.Fault {
	root, absences := survey(requested, tree)
	if len(absences) == 0 {
		return nil
	}
	j := newJudgment(spec, answerSchema, root)
	var missing, unknown []*render.Node
	listed := map[string]bool{}
	for _, a := range absences {
		code := j.verdict(a)
		if code == "" {
			continue
		}
		field := fieldPath(a.at.path(), a.name)
		if listed[string(code)+" "+field] {
			continue
		}
		listed[string(code)+" "+field] = true
		if code == diag.UpstreamLied {
			missing = append(missing, missingEntry(field, a))
		} else {
			unknown = append(unknown, unknownEntry(field, nearestNames(a.name, j.families[a.at].names)))
		}
	}
	details := []render.Pair{
		requestDetail(response.Request.Method, response.Request.URL.Redacted()),
		{Key: "fields", Value: render.NewString(walk(requested))},
	}
	// A field the server did not send stays unsent whatever name is fixed.
	if len(missing) > 0 {
		message := "the fields under missing were asked for and did not arrive: the caller's rights may hide them"
		details = append(details, render.Pair{Key: "missing", Value: render.NewList(missing...)})
		return &diag.Fault{Code: diag.UpstreamLied, Message: message, Details: details}
	}
	if len(unknown) > 0 {
		message := "the names under unknown are not declared where they were asked for"
		details = append(details, render.Pair{Key: "unknown", Value: render.NewList(unknown...)})
		return &diag.Fault{Code: diag.UnknownName, Message: message, Details: details}
	}
	return nil
}

// survey walks the whole answer before any name is judged, since the family of a place is read off every $type
// named there, and returns the root of the places with each absence once, in the order found.
func survey(requested []requestedField, tree any) (*place, []absence) {
	s := surveyor{found: map[absence]bool{}}
	root := newPlace(nil, requestedField{children: requested})
	s.visit(root, tree)
	return root, s.absences
}

type surveyor struct {
	absences []absence
	found    map[absence]bool
}

func newPlace(above *place, field requestedField) *place {
	p := &place{above: above, field: field}
	for _, child := range field.children {
		p.below = append(p.below, newPlace(p, child))
	}
	return p
}

func (p *place) path() []string {
	if p.above == nil {
		return nil
	}
	return append(p.above.path(), p.field.name)
}

// visit walks the fields asked of p over the value standing there and stops where it is null or [].
func (s *surveyor) visit(p *place, value any) {
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
			child, arrived := value[field.name]
			switch {
			case !arrived:
				s.absent(absence{at: p, name: field.name, named: named, typed: typed, mine: ytrackOwn(field)})
			// Below a name ytrack normalizes, the server sends subtypes the specification declares nowhere —
			// LinkTypeFilterField, WorkItemFilterField — and the catalogue would read a member they lack as a
			// member withheld. What stands there is judged by the rule that prints it, and no name of the
			// caller's stands there at all: one written under such a name is refused before any request.
			case field.children != nil && !field.normalized:
				s.visit(p.below[i], child)
			}
		}
	default:
		for _, field := range p.field.children {
			s.absent(absence{at: p, name: field.name, scalar: true, mine: ytrackOwn(field)})
		}
	}
}

// Objects that lack a name alike get one verdict, so the absence is kept once however many of them lack it.
func (s *surveyor) absent(a absence) {
	if !s.found[a] {
		s.found[a] = true
		s.absences = append(s.absences, a)
	}
}

// newJudgment gives each place of a finished survey its family, once, from the root down.
func newJudgment(schemas *schemas, answerSchema string, root *place) judgment {
	j := judgment{schemas: schemas, families: map[*place]family{}}
	top := schemas.subtree(answerSchema)
	j.settle(root, family{schemas: top, names: schemas.names(top)})
	return j
}

func (j judgment) settle(p *place, f family) {
	j.families[p] = f
	for _, below := range p.below {
		j.settle(below, j.familyBelow(f, below))
	}
}

// familyBelow is what the family above declares the field of p to hold and, where a schema of it declares no
// schema for the field or none declares the field, the hierarchies of what the server named at p.
func (j judgment) familyBelow(above family, p *place) family {
	var schemas []string
	untyped := false
	for _, owner := range above.schemas {
		held, declared := j.schemas.declaration(owner, p.field.name)
		switch {
		case held.schema != "":
			schemas = append(schemas, j.schemas.subtree(held.schema)...)
		case declared:
			untyped = true
		}
	}
	if untyped || schemas == nil {
		schemas = append(schemas, j.schemas.hierarchies(p.named)...)
	}
	for _, standing := range p.field.standing {
		schemas = append(schemas, j.schemas.subtree(standing)...)
	}
	return family{schemas: schemas, names: j.schemas.names(schemas), untyped: untyped}
}

// verdict is the code an absence is refused with, or "" when the name belongs to another schema that may stand
// at its place and not to what stands there.
func (j judgment) verdict(a absence) diag.Code {
	f := j.families[a.at]
	member := slices.Contains(f.schemas, a.named)
	switch {
	case member && j.declares(a.named, a.name):
		return diag.UpstreamLied
	case !slices.Contains(f.names, a.name):
		// unknown_name hands the caller a name of theirs to fix, and a name of ytrack's own is none: the
		// specification declares neither styleRanges nor the members of a range, and the request asks for
		// them all the same.
		if a.mine {
			return diag.UpstreamLied
		}
		return diag.UnknownName
	// Nothing but a schema of the family that lacks the name, or a scalar where scalars may stand, shows the
	// name not to apply: a schema that may not stand at the place vouches for nothing.
	case member, a.scalar && f.untyped:
		return ""
	}
	return diag.UpstreamLied
}

func (j judgment) declares(schema, name string) bool {
	_, ok := j.schemas.declaration(schema, name)
	return ok
}

// A nested name is written the way fields= nests it: leader(login).
func fieldPath(parents []string, name string) string {
	return strings.Join(append(slices.Clip(parents), name), "(") + strings.Repeat(")", len(parents))
}

func missingEntry(field string, a absence) *render.Node {
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

// nearest is up to five of among within two edits of the name asked for, letter case aside, nearest first and
// ties by name; where none is that near, fallback, since a caller nowhere near a name needs to see what there
// is. The names of fields= are ASCII and the name a project gives a field is prose, so the edits are runes.
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

// distance is the Levenshtein distance in runes: the names of fields= are ASCII, the names a project gives a
// custom field are prose.
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
