package youtrack

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// The members more than one command of the tool asks for and reads a value by: an entity of YouTrack goes by
// its id, a field of a project and a value of a bundle by the name the project gave them, a user by a login,
// and the two under a field say what one value of it looks like. A member one command alone reads is written
// as the text it is.
const (
	idKey        = "id"
	nameKey      = "name"
	loginKey     = "login"
	fieldTypeKey = "fieldType"
	valueTypeKey = "valueType"
)

// A name of a fields= expression with the names asked of its value, in the order given.
type requestedField struct {
	name     string
	children []requestedField
	// The name stood in double quotes, which is how a name of the data is written: the custom fields of an
	// issue are named that way and the specification's own names never are.
	quoted bool
	// The name was written last with no list after it, so everything under it was asked for. Where a merge
	// puts two spellings of one name together, this is the one the later of them had.
	whole bool
	// The caller wrote the name in an expression of theirs. A name ytrack asked for on its own behalf is none
	// of theirs to fix, so an answer that lacks it is upstream_lied rather than unknown_name. The reader
	// is the one place the mark is put on, so a tree built in Go is ytrack's whole.
	theirs bool
	// ytrack reads what stands at this name by a rule of its own and prints one value for it, so the names it
	// asked below are held to that rule rather than to the catalogue.
	normalized bool
	// Schemas that may stand at this name beside the ones the specification declares for it, each with its
	// descendants. The family of the place is then the same whatever arrived, so a name is judged alike on a
	// journal of links alone and on one holding everything (ADR-0011).
	standing []string
}

// parseFields is the expression of a command whose names are all the specification's own, where a double
// quote is a character the grammar has no place for.
func parseFields(expression, defaults string) ([]requestedField, *diag.Fault) {
	return readFields(expression, defaults, false)
}

// theDefault is the default of a command read as an expression of its own, which is what a caller who wrote no
// --fields is answered: nothing in it reached the tree through them.
func theDefault(defaults string, named bool) ([]requestedField, *diag.Fault) {
	return (&fieldsReader{text: defaults, named: named}).expression(nil)
}

// theExpression is where a command takes its names from: the expression the caller wrote, or the default of the
// command where they wrote none, and the text of the one that stood, which is what a refusal quotes back. named
// is whether a name the project gave a custom field may be written in it.
func theExpression(expression *string, defaults string, named bool) (string, []requestedField, *diag.Fault) {
	if expression == nil {
		requested, fault := theDefault(defaults, named)
		return defaults, requested, fault
	}
	requested, fault := readFields(*expression, defaults, named)
	return *expression, requested, fault
}

func readFields(expression, defaults string, named bool) ([]requestedField, *diag.Fault) {
	requested, fault := readExpression(expression, defaults, named)
	if fault != nil {
		return nil, fault
	}
	if fault := refuseTheContentOfAFile(expression, requested); fault != nil {
		return nil, fault
	}
	return requested, nil
}

func readExpression(expression, defaults string, named bool) ([]requestedField, *diag.Fault) {
	given := &fieldsReader{text: expression, named: named, theirs: true}
	if !given.take('+') {
		return given.expression(nil)
	}
	// Read first into the same tree, so the default keeps its names in their places and new names follow them.
	// They are ytrack's own until the caller writes one of them again: a name that reached the expression
	// through the default alone is not theirs to answer for.
	tree, fault := theDefault(defaults, named)
	if fault != nil {
		return nil, fault
	}
	return given.expression(tree)
}

// The name the file itself arrives under: the server answers it with the bytes of the attachment as a data URL.
const fileContentKey = "base64Content"

// Why the content of a file is asked for nowhere: ytrack downloads nothing, and this name is a download.
const theContentOfAFile = "is the file itself, which ytrack does not download; the url printed with an " +
	"attachment is a signed link, and whoever holds it fetches the file with any client"

// refuseTheContentOfAFile holds an expression to naming no file content, at any depth and under any name above
// it. The catalogue is asked nothing, unlike the refusal of comments: the specification declares the name on
// the two schemas of an attachment and on nothing else, so it names the same thing wherever it is written, and
// the places the caller may write it at are not worth a walk of the schemas.
func refuseTheContentOfAFile(expression string, requested []requestedField) *diag.Fault {
	path, written := writtenAt(fileContentKey, requested, nil)
	if !written {
		return nil
	}
	message := fmt.Sprintf("fields %s: %s %s", render.Quote(expression), path, theContentOfAFile)
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

// writtenAt is the first place the caller wrote name at, in the syntax of fields=. A name of the data stands in
// double quotes and is a name a project gave a field of its own, so it is another name than this one.
func writtenAt(name string, requested []requestedField, parents []string) (string, bool) {
	for _, field := range requested {
		if field.name == name && !field.quoted {
			return fieldPath(parents, field.name), true
		}
		below := append(slices.Clip(parents), field.name)
		if path, found := writtenAt(name, field.children, below); found {
			return path, true
		}
	}
	return "", false
}

// A caller who wrote a leading + leans on the default of the command, and field show settles its own default
// only once it knows what the field holds.
func addsToTheDefault(expression string) bool {
	return (&fieldsReader{text: expression}).take('+')
}

// walk writes the tree the way the expression is written: as the fields= that goes out, and, for a tree still
// holding names of the data, as what the caller typed.
func walk(fields []requestedField) string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.children == nil {
			names = append(names, writtenName(field))
		} else {
			names = append(names, writtenName(field)+"("+walk(field.children)+")")
		}
	}
	return strings.Join(names, ",")
}

// writtenName is the name as the grammar carries it: bare, or in double quotes with the two characters the
// quotes cannot hold raw put back the way they were written.
func writtenName(field requestedField) string {
	if !field.quoted {
		return field.name
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(field.name) + `"`
}

// The server answers a name given twice by its last form alone, dropping what the first asked of it,
// so a repeat is merged into the first.
func merge(fields []requestedField, field requestedField) []requestedField {
	for i := range fields {
		if fields[i].name == field.name {
			for _, child := range field.children {
				fields[i].children = merge(fields[i].children, child)
			}
			// The last spelling says whether the whole of the field was asked for: written bare after names,
			// it takes the block back whole. Whose name it is follows the same hand: a name of the default
			// the caller wrote as well reached the expression through them too.
			fields[i].whole = field.whole
			fields[i].theirs = fields[i].theirs || field.theirs
			fields[i].normalized = fields[i].normalized || field.normalized
			for _, schema := range field.standing {
				if !slices.Contains(fields[i].standing, schema) {
					fields[i].standing = append(fields[i].standing, schema)
				}
			}
			return fields
		}
	}
	return append(fields, field)
}

// asking is what a command sends: the caller's expression with the names ytrack needs of its own merged into
// it. merge changes the tree it merges into and the caller's is still needed as they wrote it — it says what is
// printed — so the merging happens over a copy, down to the children of a name they both ask for.
func asking(requested []requestedField, own ...requestedField) []requestedField {
	asked := cloneFields(requested)
	for _, field := range own {
		asked = merge(asked, field)
	}
	return asked
}

func cloneFields(fields []requestedField) []requestedField {
	if fields == nil {
		return nil
	}
	copied := make([]requestedField, len(fields))
	for i, field := range fields {
		field.children = cloneFields(field.children)
		copied[i] = field
	}
	return copied
}

// eachPlace hands visit every place the caller wrote: the schema it stands on, what the specification declares
// for it there, the names it stands under, and the field itself, which visit may fill in — a place filled in is
// then walked as visit left it. The tree stands at the schema of at, and a place is walked below as well, since
// the same name may stand under it again: the issues at the other end of a link carry links of their own.
func eachPlace(c *schemas, at string, requested []requestedField, parents []string, visit func(standing string, held element, above []string, field *requestedField)) {
	for i := range requested {
		field := &requested[i]
		held, _ := c.declaration(at, field.name)
		visit(at, held, parents, field)
		if held.schema == "" {
			continue
		}
		below := append(slices.Clip(parents), field.name)
		eachPlace(c, held.schema, field.children, below, visit)
	}
}

// namesAt hands visit each place the caller wrote name at where the specification declares the place of schema.
func namesAt(c *schemas, at, schema, name string, requested []requestedField, parents []string, visit func(parents []string, field *requestedField)) {
	eachPlace(c, at, requested, parents, func(standing string, _ element, above []string, field *requestedField) {
		if standing == schema && field.name == name {
			visit(above, field)
		}
	})
}

// positionsOf hands visit every place the caller wrote where the specification declares a value of schema,
// wherever it stands below the schema of at. A place is found by the type declared for it rather than by the
// name it goes by, so one rule reaches every name the same type stands under: the duration of a work item and
// the two a change of one is written as are the same place three times over.
func positionsOf(c *schemas, at, schema string, requested []requestedField, parents []string, visit func(parents []string, field *requestedField)) {
	eachPlace(c, at, requested, parents, func(_ string, held element, above []string, field *requestedField) {
		if held.schema == schema {
			visit(above, field)
		}
	})
}

// nameAt is the first place namesAt finds, and false where the caller wrote the name at no such place.
func nameAt(c *schemas, at, schema, name string, requested []requestedField, parents []string) (string, bool) {
	first, found := "", false
	namesAt(c, at, schema, name, requested, parents, func(above []string, field *requestedField) {
		if !found {
			first, found = fieldPath(above, field.name), true
		}
	})
	return first, found
}

type fieldsReader struct {
	text string
	at   int
	// Names of the data are read here: without this a double quote is a character the grammar has no place for.
	named bool
	// Whose text this is. The reader is the one place a name of the caller's can enter a tree, so it is the one
	// place the mark is put on.
	theirs bool
}

func (r *fieldsReader) expression(tree []requestedField) ([]requestedField, *diag.Fault) {
	tree, fault := r.list(tree)
	if fault != nil {
		return nil, fault
	}
	if r.at < len(r.text) {
		return nil, r.unexpected()
	}
	return tree, nil
}

func (r *fieldsReader) list(fields []requestedField) ([]requestedField, *diag.Fault) {
	for {
		field, fault := r.item()
		if fault != nil {
			return nil, fault
		}
		fields = merge(fields, field)
		if !r.take(',') {
			return fields, nil
		}
	}
}

func (r *fieldsReader) item() (requestedField, *diag.Fault) {
	field, fault := r.itemName()
	if fault != nil {
		return requestedField{}, fault
	}
	if !r.take('(') {
		field.whole = true
		return field, nil
	}
	children, fault := r.list(nil)
	if fault != nil {
		return requestedField{}, fault
	}
	if !r.take(')') {
		return requestedField{}, r.unexpected()
	}
	field.children = children
	return field, nil
}

func (r *fieldsReader) itemName() (requestedField, *diag.Fault) {
	r.skipSpace()
	if r.named && r.at < len(r.text) && r.text[r.at] == '"' {
		return r.quotedName()
	}
	start := r.at
	for r.at < len(r.text) && isNameByte(r.text[r.at]) {
		r.at++
	}
	if r.at == start {
		return requestedField{}, r.unexpected()
	}
	field := requestedField{name: r.text[start:r.at], theirs: r.theirs}
	// A name asked for is printed as a key, so a name the renderer would refuse is refused before any request.
	if err := render.CheckKey(field.name); err != nil {
		message := fmt.Sprintf("fields %s: the name at column %d cannot be printed: %v", render.Quote(r.text), r.column(start), err)
		return requestedField{}, &diag.Fault{Code: diag.BadUsage, Message: message}
	}
	return field, nil
}

// A name a project gave a custom field is prose: it holds spaces, brackets and letters outside ASCII, so it is
// written in double quotes. A quote and a backslash of the name itself go behind a backslash, every other rune
// stands as it is, and a name of nothing at all names no field.
func (r *fieldsReader) quotedName() (requestedField, *diag.Fault) {
	r.at++
	var name strings.Builder
	for r.at < len(r.text) {
		switch c := r.text[r.at]; c {
		case '"':
			if name.Len() == 0 {
				return requestedField{}, r.unexpected()
			}
			r.at++
			return requestedField{name: name.String(), quoted: true, theirs: r.theirs}, nil
		case '\\':
			r.at++
			if r.at >= len(r.text) || (r.text[r.at] != '"' && r.text[r.at] != '\\') {
				return requestedField{}, r.unexpected()
			}
			name.WriteByte(r.text[r.at])
		default:
			name.WriteByte(c)
		}
		r.at++
	}
	return requestedField{}, r.unexpected()
}

// take moves past the spaces ahead and past c, if c follows them.
func (r *fieldsReader) take(c byte) bool {
	r.skipSpace()
	if r.at < len(r.text) && r.text[r.at] == c {
		r.at++
		return true
	}
	return false
}

// The server reads a space as a part of the name, so spaces and tabs are skipped and the walk writes none.
func (r *fieldsReader) skipSpace() {
	for r.at < len(r.text) && (r.text[r.at] == ' ' || r.text[r.at] == '\t') {
		r.at++
	}
}

func (r *fieldsReader) unexpected() *diag.Fault {
	found := "end"
	if r.at < len(r.text) {
		c, _ := utf8.DecodeRuneInString(r.text[r.at:])
		found = render.Quote(string(c))
	}
	message := fmt.Sprintf("fields %s: unexpected %s at column %d", render.Quote(r.text), found, r.column(r.at))
	return &diag.Fault{Code: diag.BadUsage, Message: message}
}

func (r *fieldsReader) column(at int) int {
	return utf8.RuneCountInString(r.text[:at]) + 1
}

// The characters of the specification's property names.
func isNameByte(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' || c == '_' || c == '$'
}
