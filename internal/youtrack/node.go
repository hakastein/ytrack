package youtrack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// How much of the document a value may take: a record of a list is one line, and everything under it is written
// on that line, while anywhere else prose stands as the lines it is.
type layout int

const (
	onLinesOfItsOwn layout = iota
	onOneLine
)

// nodes writes the objects of an answer as the document prints them. How a scalar is written is settled by the
// schema its property is declared on, so one field of one command always prints the same way whatever arrived.
type nodes struct {
	answer answer
	layout layout
	// The names the objects being written stand under, so a refusal about a value names its place the way the
	// caller wrote it in fields=.
	at []string
	// Заполняется только автором журнала.
	row activityCategory
	// Правила строки действуют только на самой записи: field, added и removed объявлены и в других схемах.
	ofTheRecord bool
	// Запись связи называет её конец переводом, а не id; непереведённая фраза хранится только в типе.
	phrases linkPhrases
}

func printing(a answer, l layout) nodes {
	return nodes{answer: a, layout: l}
}

func (n nodes) below(name string) nodes {
	n.ofTheRecord = false
	n.at = append(slices.Clip(n.at), name)
	return n
}

// The objects are handed in rather than taken from the answer, since a command may print them in an order of
// its own or print only some of them. schema is the one the specification declares where they stand, which is
// the one the call answered with only at the root of the answer.
func (n nodes) objectsAt(schema string, requested []requestedField, objects []map[string]any) ([]*render.Node, *diag.Fault) {
	printed := make([]*render.Node, 0, len(objects))
	for _, object := range objects {
		node, fault := n.object(schema, requested, object)
		if fault != nil {
			return nil, fault
		}
		printed = append(printed, node)
	}
	return printed, nil
}

// objectNode is one object's document, with own the pairs a command fills itself after the fields asked of it:
// the comments of an issue are such a pair, since no expression names them.
func objectNode(a answer, requested []requestedField, object map[string]any, own []render.Pair) (*render.Node, *diag.Fault) {
	n := printing(a, onLinesOfItsOwn)
	pairs, fault := n.pairs(a.schema, requested, object)
	if fault != nil {
		return nil, fault
	}
	return render.NewMap(append(pairs, own...)...), nil
}

func (n nodes) object(schema string, requested []requestedField, object map[string]any) (*render.Node, *diag.Fault) {
	pairs, fault := n.pairs(schema, requested, object)
	if fault != nil {
		return nil, fault
	}
	return render.NewMap(pairs...), nil
}

// Only what was asked is taken, in the order it was asked: $type, which the server adds on its own, is
// left out unless it was asked for, and so is a name the judgment found to belong to another schema.
func (n nodes) pairs(schema string, requested []requestedField, object map[string]any) ([]render.Pair, *diag.Fault) {
	// An object of a subtype declares more than the schema of its place does, and the server names the subtype
	// in $type. This is the schema the judgment of names reads a place by (ADR-0007), and everything below
	// reads the object by the same one.
	if named, typed := object["$type"].(string); typed {
		schema = named
	}
	pairs := make([]render.Pair, 0, len(requested))
	for _, field := range requested {
		value, arrived := object[field.name]
		if !arrived {
			continue
		}
		node, fault := n.member(schema, field, value)
		if fault != nil {
			return nil, fault
		}
		pairs = append(pairs, render.Pair{Key: field.name, Value: node})
	}
	return pairs, nil
}

// member is one name of an object. Which names hold an address of the instance is read off the schema the
// object stands at — the $type the server named it by, or, where it named none, the schema the specification
// declares for the place — so a value is never made an address on the strength of its name alone.
func (n nodes) member(schema string, field requestedField, value any) (*render.Node, *diag.Fault) {
	if n.answer.schemas.holdsAnAddress(schema, field.name) {
		return n.address(field, value)
	}
	held, _ := n.answer.schemas.declaration(schema, field.name)
	return n.value(held, field, value)
}

func (n nodes) value(held element, field requestedField, value any) (*render.Node, *diag.Fault) {
	// The custom fields of an issue are a block of their own, taken whole and printed as the names the project
	// gave them, so the list is read here rather than walked item by item.
	if n.composed() && held.schema == customFieldSchema {
		return n.customFields(field, value)
	}
	// A link slot is a block of the same kind: the issue's links read as the phrases they go by, whether the
	// place holds the whole array or the one slot parent and subtasks stand for.
	if n.composed() && held.schema == linkSchema {
		return n.links(field, value)
	}
	// The attributes of a work item are one more: each printed as the value it holds under its name.
	if n.composed() && held.schema == workItemAttributeSchema {
		return n.attributes(value)
	}
	// Тот же идентификатор, что принимает --category.
	if n.ofTheRecord && held.schema == categorySchema {
		return n.category(), nil
	}
	// Запись называет поле переводом или меткой категории; строка печатает вместо этого имя поля задачи.
	if n.ofTheRecord && field.name == fieldKey {
		return n.changedField(value)
	}
	// Ищется по имени: сервер шлёт здесь null, одно значение или список, а печатается всегда список.
	if n.ofTheRecord && (field.name == addedKey || field.name == removedKey) {
		return n.values(held, field, value)
	}
	switch value := value.(type) {
	case []any:
		items := make([]*render.Node, 0, len(value))
		for _, item := range value {
			node, fault := n.value(held, field, item)
			if fault != nil {
				return nil, fault
			}
			items = append(items, node)
		}
		return render.NewList(items...), nil
	case nil:
		// A field that was asked for and is empty is printed empty, whatever its kind.
		return render.NewNull(), nil
	}
	// A duration is read by ytrack rather than printed as it arrived, wherever one stands.
	if held.schema == durationSchema {
		return n.period(value)
	}
	if held.kind == timeKind {
		return n.instant(field.name, value)
	}
	switch value := value.(type) {
	case string:
		// A wrong class of prose costs the layout of a value and never the value, so a property of the class
		// that arrived as something other than a string needs no refusal of its own.
		if held.kind == proseKind {
			return n.prose(value), nil
		}
		return render.NewString(value), nil
	case bool:
		return render.NewBool(value), nil
	case json.Number:
		return render.NewNumber(value), nil
	case map[string]any:
		return n.below(field.name).object(held.schema, field.children, value)
	}
	// Besides those, a decoded answer holds only null.
	return render.NewNull(), nil
}

// composed is whether the blocks of an issue in this answer are the tool's own, which is so where the command
// built its tree with issueBlocks. Anywhere else the caller reached an issue through a tree of their own —
// project show --fields 'issues(customFields(value(login)))' is one — and is answered what they asked for.
func (n nodes) composed() bool {
	return composesBlocks(n.answer.schema)
}

// The schemas whose objects carry an address of the instance, and the names they carry one under. The table is
// by schema rather than by name alone: url is declared on sixteen schemas of the specification and on several
// of them — ExternalIssue among them — it arrives absolute and belongs to another host altogether.
const (
	issueAttachmentSchema   = "IssueAttachment"
	articleAttachmentSchema = "ArticleAttachment"
	userSchema              = "User"
	urlKey                  = "url"
	thumbnailURLKey         = "thumbnailURL"
	avatarURLKey            = "avatarUrl"
	iconURLKey              = "iconUrl"
)

// holdsAnAddress is whether property of an object standing at schema holds an address of the instance. Where
// neither the server nor the specification names a schema for the place, schema is "", nothing descends from
// it, and the value is printed as it arrived: what stands at a place nobody named is unknown, and resolving
// it would be a guess.
func (c *schemas) holdsAnAddress(schema, property string) bool {
	switch property {
	case urlKey, thumbnailURLKey:
		return c.descends(schema, issueAttachmentSchema) || c.descends(schema, articleAttachmentSchema)
	case avatarURLKey:
		return c.descends(schema, userSchema)
	case iconURLKey:
		return c.descends(schema, projectSchema)
	}
	return false
}

// address is what such a place prints. YouTrack writes an address of its own as a path —
// /api/files/12-2?sign=…&updated= — which is no use to a caller not holding the address ytrack was pointed at,
// so it is resolved against that address by RFC 3986, query and all: a signature is never rewritten.
//
// A reference carrying a scheme, an authority or an opaque part resolves elsewhere, so it is refused rather
// than printed under a name that means this instance (ADR-0005).
func (n nodes) address(field requestedField, value any) (*render.Node, *diag.Fault) {
	if value == nil {
		return render.NewNull(), nil
	}
	text, isString := value.(string)
	if !isString {
		return nil, n.notAnAddress(field, value)
	}
	reference, err := url.Parse(text)
	if err != nil || !anAbsolutePath(reference) {
		return nil, n.notAnAddress(field, value)
	}
	return render.NewString(n.answer.address.ResolveReference(reference).String()), nil
}

// anAbsolutePath is a reference that names no authority of its own and stands at the root of one, which is the
// one shape an address of the instance arrives in.
func anAbsolutePath(reference *url.URL) bool {
	return reference.Scheme == "" && reference.Opaque == "" && reference.User == nil &&
		reference.Host == "" && strings.HasPrefix(reference.Path, "/")
}

func (n nodes) notAnAddress(field requestedField, value any) *diag.Fault {
	at := fieldPath(n.at, field.name)
	message := fmt.Sprintf("%s is an address of the instance, and what arrived for it is no absolute path", at)
	details := []render.Pair{
		requestDetail(n.answer.response.Request.Method, n.answer.response.Request.URL.Redacted()),
		{Key: "field", Value: render.NewString(at)},
		{Key: "upstream_value", Value: asArrived(value)},
	}
	return &diag.Fault{Code: diag.UpstreamLied, Message: message, Details: details}
}

// asArrived is the value quoted back word for word: the string the server sent, the number, the boolean or the
// null it sent, or the JSON of whatever else — a tree decoded from JSON always writes back. Nothing is dropped
// on the way: a value written back as a null of ytrack's own would read as nothing having arrived at all,
// where ADR-0003 asks for what arrived word for word — under upstream_value, and under the arrived of a
// mismatch, which is the same quoting of the same thing.
func asArrived(value any) *render.Node {
	switch arrived := value.(type) {
	case string:
		return render.NewString(arrived)
	case json.Number:
		return render.NewNumber(arrived)
	case bool:
		return render.NewBool(arrived)
	case nil:
		return render.NewNull()
	}
	written, _ := json.Marshal(value)
	return render.NewString(string(written))
}

// prose is text as the place it stands in lays it out: a literal block, which reads as the lines it is, or, in a
// record a list prints on one line, a double-quoted string of the same text. Neither loses a byte of it, and
// which of the two is written is a matter of the command and the place rather than of the text.
func (n nodes) prose(text string) *render.Node {
	if n.layout == onOneLine {
		return render.NewString(text)
	}
	return render.NewProse(text)
}

const (
	durationSchema = "DurationValue"
	minutesKey     = "minutes"
)

// A duration is printed out of the minutes it holds and out of nothing else it arrives with: presentation reads
// a day as the working day of the instance and is written in the language of the server, and the id is the
// minutes as text. So the one identity a duration has is the ISO period the minutes make.
func (n nodes) period(value any) (*render.Node, *diag.Fault) {
	held, isObject := value.(map[string]any)
	if !isObject {
		return nil, n.lied("a duration arrived as something other than a JSON object")
	}
	minutes, arrived := held[minutesKey]
	if !arrived {
		return nil, n.lied("a duration arrived without the minutes it holds, which is what says how long it is")
	}
	count, isWhole := wholeNumber(minutes)
	if !isWhole {
		return nil, n.lied("the minutes of a duration are no whole number of them")
	}
	return render.NewString(duration(count)), nil
}

// A duration stands for the one thing it is — how long — so no name stands under it, and the two the server
// writes beside the minutes are refused here rather than printed: whichever of them the caller asked for, the
// document would read differently on another instance.
func refuseDurationParts(spec *schemas, at, expression string, requested []requestedField) *diag.Fault {
	var fault *diag.Fault
	positionsOf(spec, at, durationSchema, requested, nil, func(parents []string, field *requestedField) {
		if field.children == nil || fault != nil {
			return
		}
		message := fmt.Sprintf("fields %s: %s is printed as the ISO 8601 period of the minutes it holds, as in "+
			"PT1H30M, so no name stands under it", render.Quote(expression), fieldPath(parents, field.name))
		fault = &diag.Fault{Code: diag.BadUsage, Message: message}
	})
	return fault
}

// fillInDurations is what a request carries for every duration the caller asked for: the minutes, which a bare
// duration does not bring — the server answers that with the type of the value and nothing else.
func fillInDurations(spec *schemas, at string, asked []requestedField) {
	positionsOf(spec, at, durationSchema, asked, nil, func(_ []string, field *requestedField) {
		field.children = []requestedField{{name: minutesKey}}
	})
}

// An instant is printed in UTC, so that one issue reads the same wherever it is read, and with a fraction only
// where the milliseconds are not zero.
func (n nodes) instant(name string, value any) (*render.Node, *diag.Fault) {
	count, isInstant := wholeNumber(value)
	if !isInstant {
		return nil, shapeFailure(n.answer.response, n.answer.body, notAnInstant(name))
	}
	return render.NewString(time.UnixMilli(count).UTC().Format(time.RFC3339Nano)), nil
}

// lied is what the server sent held against what the specification says it is: the answer is in hand, so the
// refusal carries the body rather than a request that would bring the same body back.
func (n nodes) lied(message string) *diag.Fault {
	return shapeFailure(n.answer.response, n.answer.body, message)
}

// wholeNumber is the count a whole number arrives as: the milliseconds of an instant, the minutes of a
// duration. A fraction is neither.
func wholeNumber(value any) (int64, bool) {
	number, isNumber := value.(json.Number)
	if !isNumber {
		return 0, false
	}
	count, err := number.Int64()
	if err != nil {
		return 0, false
	}
	return count, true
}

func notAnInstant(name string) string {
	return fmt.Sprintf("%s is a time, and what arrived for it is no whole number of milliseconds since the epoch", name)
}

func intNode(n int) *render.Node {
	return render.NewNumber(json.Number(strconv.Itoa(n)))
}
