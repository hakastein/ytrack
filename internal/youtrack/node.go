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

type layout int

const (
	blockLayout layout = iota
	inlineLayout
)

// converter writes the objects of an answer as the document prints them. How a scalar is written is settled by the
// schema its property is declared on, so one field of one command always prints the same way whatever arrived.
type converter struct {
	response decodedResponse
	layout   layout
	// The names the objects being written stand under, so a refusal about a value names its place the way the
	// caller wrote it in fields=.
	at []string
	// Заполняется только автором журнала.
	row activityCategory
	// Правила строки действуют только на самой записи: field, added и removed объявлены и в других схемах.
	activityRoot bool
	// Запись связи называет её конец переводом, а не id; непереведённая фраза хранится только в типе.
	phrases linkPhrases
}

func newConverter(a decodedResponse, l layout) converter {
	return converter{response: a, layout: l}
}

func (n converter) child(name string) converter {
	n.activityRoot = false
	n.at = append(slices.Clip(n.at), name)
	return n
}

// The objects are handed in rather than taken from the answer, since a command may print them in an order of
// its own or print only some of them. schema is the one the specification declares where they stand, which is
// the one the call answered with only at the root of the answer.
func (n converter) objectsAt(schema string, requested []requestedField, objects []map[string]any) ([]*render.Node, *diag.Fault) {
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
func objectNode(a decodedResponse, requested []requestedField, object map[string]any, own []render.Pair) (*render.Node, *diag.Fault) {
	n := newConverter(a, blockLayout)
	pairs, fault := n.pairs(a.schema, requested, object)
	if fault != nil {
		return nil, fault
	}
	return render.NewMap(append(pairs, own...)...), nil
}

func (n converter) object(schema string, requested []requestedField, object map[string]any) (*render.Node, *diag.Fault) {
	pairs, fault := n.pairs(schema, requested, object)
	if fault != nil {
		return nil, fault
	}
	return render.NewMap(pairs...), nil
}

func (n converter) pairs(schema string, requested []requestedField, object map[string]any) ([]render.Pair, *diag.Fault) {
	if named, typed := object["$type"].(string); typed {
		schema = named
	}
	pairs := make([]render.Pair, 0, len(requested))
	for _, field := range requested {
		value, ok := object[field.name]
		if !ok {
			continue
		}
		node, fault := n.property(schema, field, value)
		if fault != nil {
			return nil, fault
		}
		pairs = append(pairs, render.Pair{Key: field.name, Value: node})
	}
	return pairs, nil
}

// property is one name of an object. Which names hold an address of the instance is read off the schema the
// object stands at — the $type the server named it by, or, where it named none, the schema the specification
// declares for the place — so a value is never made an address on the strength of its name alone.
func (n converter) property(schema string, field requestedField, value any) (*render.Node, *diag.Fault) {
	if n.response.schemas.isInstanceURL(schema, field.name) {
		return n.address(field, value)
	}
	decl, _ := n.response.schemas.declaration(schema, field.name)
	return n.value(decl, field, value)
}

func (n converter) value(decl typeRef, field requestedField, value any) (*render.Node, *diag.Fault) {
	// The custom fields of an issue are a block of their own, taken whole and printed as the names the project
	// gave them, so the list is read here rather than walked item by item.
	if n.hasIssueBlocks() && decl.schema == customFieldSchema {
		return n.customFields(field, value)
	}
	if n.hasIssueBlocks() && decl.schema == linkSchema {
		return n.links(field, value)
	}
	// The attributes of a work item are one more: each printed as the value it holds under its name.
	if n.hasIssueBlocks() && decl.schema == workItemAttributeSchema {
		return n.attributes(value)
	}
	// Тот же идентификатор, что принимает --category.
	if n.activityRoot && decl.schema == categorySchema {
		return n.category(), nil
	}
	// Запись называет поле переводом или меткой категории; строка печатает вместо этого имя поля задачи.
	if n.activityRoot && field.name == fieldKey {
		return n.changedField(value)
	}
	// Ищется по имени: сервер шлёт здесь null, одно значение или список, а печатается всегда список.
	if n.activityRoot && (field.name == addedKey || field.name == removedKey) {
		return n.values(decl, field, value)
	}
	switch value := value.(type) {
	case []any:
		items := make([]*render.Node, 0, len(value))
		for _, item := range value {
			node, fault := n.value(decl, field, item)
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
	if decl.schema == durationSchema {
		return n.durationNode(value)
	}
	if decl.kind == timeKind {
		return n.instant(field.name, value)
	}
	switch value := value.(type) {
	case string:
		if decl.kind == textKind {
			return n.textNode(value), nil
		}
		return render.NewString(value), nil
	case bool:
		return render.NewBool(value), nil
	case json.Number:
		return render.NewNumber(value), nil
	case map[string]any:
		return n.child(field.name).object(decl.schema, field.children, value)
	}
	// Besides those, a decoded answer holds only null.
	return render.NewNull(), nil
}

// hasIssueBlocks is whether the blocks of an issue in this answer are the tool's own, which is so where the command
// built its tree with issueBlocks. Anywhere else the caller reached an issue through a tree of their own —
// project show --fields 'issues(customFields(value(login)))' is one — and is answered what they asked for.
func (n converter) hasIssueBlocks() bool {
	return hasIssueBlocks(n.response.schema)
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

// isInstanceURL is whether property of an object standing at schema holds an address of the instance. Where
// neither the server nor the specification names a schema for the place, schema is "", nothing descends from
// it, and the value is printed as it arrived: what stands at a place nobody named is unknown, and resolving
// it would be a guess.
func (c *schemas) isInstanceURL(schema, property string) bool {
	switch property {
	case urlKey, thumbnailURLKey:
		return c.isSubtypeOf(schema, issueAttachmentSchema) || c.isSubtypeOf(schema, articleAttachmentSchema)
	case avatarURLKey:
		return c.isSubtypeOf(schema, userSchema)
	case iconURLKey:
		return c.isSubtypeOf(schema, projectSchema)
	}
	return false
}

// address is what such a place prints. YouTrack writes an address of its own as a path —
// /api/files/12-2?sign=…&updated= — which is no use to a caller not holding the address ytrack was pointed at,
// so it is resolved against that address by RFC 3986, query and all: a signature is never rewritten.
//
// A reference carrying a scheme, an authority or an opaque part resolves elsewhere, so it is refused rather
// than printed under a name that means this instance (ADR-0005).
func (n converter) address(field requestedField, value any) (*render.Node, *diag.Fault) {
	if value == nil {
		return render.NewNull(), nil
	}
	text, isString := value.(string)
	if !isString {
		return nil, n.invalidURL(field, value)
	}
	reference, err := url.Parse(text)
	if err != nil || !isAbsolutePath(reference) {
		return nil, n.invalidURL(field, value)
	}
	return render.NewString(n.response.address.ResolveReference(reference).String()), nil
}

// isAbsolutePath is a reference that names no authority of its own and stands at the root of one, which is the
// one shape an address of the instance arrives in.
func isAbsolutePath(reference *url.URL) bool {
	return reference.Scheme == "" && reference.Opaque == "" && reference.User == nil &&
		reference.Host == "" && strings.HasPrefix(reference.Path, "/")
}

func (n converter) invalidURL(field requestedField, value any) *diag.Fault {
	at := fieldPath(n.at, field.name)
	message := fmt.Sprintf("%s is an address of the instance, and what arrived for it is no absolute path", at)
	details := []render.Pair{
		requestDetail(n.response.httpResponse.Request.Method, n.response.httpResponse.Request.URL.Redacted()),
		{Key: "field", Value: render.NewString(at)},
		{Key: "upstream_value", Value: rawValueNode(value)},
	}
	return &diag.Fault{Code: diag.UpstreamInvalid, Message: message, Details: details}
}

// rawValueNode is the value quoted back word for word: the string the server sent, the number, the boolean or the
// null it sent, or the JSON of whatever else — a tree decoded from JSON always writes back. Nothing is dropped
// on the way: a value written back as a null of ytrack's own would read as nothing having arrived at all,
// where ADR-0003 asks for what arrived word for word — under upstream_value, and under the arrived of a
// mismatch, which is the same quoting of the same thing.
func rawValueNode(value any) *render.Node {
	switch received := value.(type) {
	case string:
		return render.NewString(received)
	case json.Number:
		return render.NewNumber(received)
	case bool:
		return render.NewBool(received)
	case nil:
		return render.NewNull()
	}
	written, _ := json.Marshal(value)
	return render.NewString(string(written))
}

// textNode is text as the place it stands in lays it out: a literal block, which reads as the lines it is, or, in a
// record a list prints on one line, a double-quoted string of the same text. Neither loses a byte of it, and
// which of the two is written is a matter of the command and the place rather than of the text.
func (n converter) textNode(text string) *render.Node {
	if n.layout == inlineLayout {
		return render.NewString(text)
	}
	return render.NewText(text)
}

const (
	durationSchema = "DurationValue"
	minutesKey     = "minutes"
)

func (n converter) durationNode(value any) (*render.Node, *diag.Fault) {
	held, isObject := value.(map[string]any)
	if !isObject {
		return nil, n.malformed("a duration arrived as something other than a JSON object")
	}
	minutes, ok := held[minutesKey]
	if !ok {
		return nil, n.malformed("a duration arrived without the minutes it holds, which is what says how long it is")
	}
	count, isWhole := parseInt64(minutes)
	if !isWhole {
		return nil, n.malformed("the minutes of a duration are no whole number of them")
	}
	return render.NewString(duration(count)), nil
}

// A duration stands for the one thing it is — how long — so no name stands under it, and the two the server
// writes beside the minutes are refused here rather than printed: whichever of them the caller asked for, the
// document would read differently on another instance.
func rejectDurationParts(spec *schemas, at, expression string, requested []requestedField) *diag.Fault {
	var fault *diag.Fault
	fieldsOfType(spec, at, durationSchema, requested, nil, func(parents []string, field *requestedField) {
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
	fieldsOfType(spec, at, durationSchema, asked, nil, func(_ []string, field *requestedField) {
		field.children = []requestedField{{name: minutesKey}}
	})
}

// An instant is printed in UTC, so that one issue reads the same wherever it is read, and with a fraction only
// where the milliseconds are not zero.
func (n converter) instant(name string, value any) (*render.Node, *diag.Fault) {
	count, isInstant := parseInt64(value)
	if !isInstant {
		return nil, shapeFailure(n.response.httpResponse, n.response.body, notAnInstant(name))
	}
	return render.NewString(time.UnixMilli(count).UTC().Format(time.RFC3339Nano)), nil
}

// malformed is what the server sent held against what the specification says it is: the answer is in hand, so the
// refusal carries the body rather than a request that would bring the same body back.
func (n converter) malformed(message string) *diag.Fault {
	return shapeFailure(n.response.httpResponse, n.response.body, message)
}

// parseInt64 is the count a whole number arrives as: the milliseconds of an instant, the minutes of a
// duration. A fraction is neither.
func parseInt64(value any) (int64, bool) {
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
