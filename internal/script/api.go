package script

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"slices"
	"strings"

	"github.com/hakastein/cliche"
	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

type paramKind int

const (
	textParam paramKind = iota
	// null empties the part; undefined leaves it as it stands.
	clearableParam
	wholeParam
	textsParam
	// An object of custom fields by name, each a value, an array of values, or null to empty it.
	fieldValuesParam
	// An object of work item attributes by name, each a value or null to empty it.
	attributesParam
	// "all" or the count of the latest comments.
	commentsParam
	// A string, written in UTF-8, or the bytes of a Buffer, a Uint8Array or an ArrayBuffer.
	bytesParam
	boolParam
)

type param struct {
	name     string
	kind     paramKind
	required bool
	refuse   func(value any) string
}

type call func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error)

type function struct {
	params []param
	// A script that called a function which writes exits 2 on any fault after it.
	writes bool
	refuse func(opts options) string
	// bind refuses as bad_usage what the SDK would take for another call, before the login is looked up.
	bind binder
}

type binder func(opts options) (call, *diag.Fault)

type options map[string]any

type cleared struct{}

func (o options) given(name string) bool {
	_, given := o[name]
	return given
}

func (o options) cleared(name string) bool {
	_, isCleared := o[name].(cleared)
	return isCleared
}

func (o options) string(name string) string {
	text, _ := o[name].(string)
	return text
}

func (o options) optionalBool(name string) *bool {
	set, given := o[name].(bool)
	if !given {
		return nil
	}
	return &set
}

func (o options) optional(name string) *string {
	text, given := o[name].(string)
	if !given {
		return nil
	}
	return &text
}

func (o options) issue(name string) youtrack.IssueID {
	return youtrack.IssueID(o.string(name))
}

func (o options) article(name string) youtrack.ArticleID {
	return youtrack.ArticleID(o.string(name))
}

func (o options) optionalArticle(name string) *youtrack.ArticleID {
	text := o.optional(name)
	if text == nil {
		return nil
	}
	return new(youtrack.ArticleID(*text))
}

// withOwner reads the issue or the article under name by the form of its id; an id of neither form is the refusal of
// the SDK.
func withOwner(opts options, name string, do func(owner youtrack.Owner) (*youtrack.Node, error)) (*youtrack.Node, error) {
	owner, err := youtrack.OwnerOf(opts.string(name))
	if err != nil {
		return nil, err
	}
	return do(owner)
}

func (o options) int(name string) int {
	number, _ := o[name].(int)
	return number
}

func (o options) bytes(name string) []byte {
	content, _ := o[name].([]byte)
	return content
}

func (o options) strings(name string) []string {
	texts, _ := o[name].([]string)
	return texts
}

func (o options) fieldWrites(name string) []youtrack.FieldWrite {
	writes, _ := o[name].([]youtrack.FieldWrite)
	return writes
}

func (o options) attributeWrites(name string) []youtrack.AttributeWrite {
	writes, _ := o[name].([]youtrack.AttributeWrite)
	return writes
}

func (o options) comments(name string) youtrack.Comments {
	comments, _ := o[name].(youtrack.Comments)
	return comments
}

func (r *run) service(name string, functions map[string]function) *cliche.Object {
	methods := slices.Sorted(maps.Keys(functions))
	values := make([]any, 0, len(methods))
	for _, method := range methods {
		values = append(values, r.function(name+"."+method, functions[method]))
	}
	return cliche.NewObject(methods, values)
}

func (r *run) function(name string, f function) cliche.Func {
	return func(ctx context.Context, args ...any) (any, error) {
		opts, err := readOptions(ctx, name, f, args)
		if err != nil {
			return nil, err
		}
		bound, fault := f.bind(opts)
		if fault != nil {
			return nil, thrown{fault: fault}
		}
		node, fault := r.host.Call(ctx, bound)
		if fault != nil {
			if fault.MayHaveWritten() {
				r.wrote = true
			}
			return nil, thrown{fault: fault}
		}
		if f.writes {
			r.wrote = true
		}
		return valueOf(node), nil
	}
}

func signature(f function) string {
	if len(f.params) == 0 {
		return "no arguments"
	}
	names := make([]string, 0, len(f.params))
	for _, p := range f.params {
		if p.required {
			names = append(names, p.name)
		} else {
			names = append(names, p.name+"?")
		}
	}
	return "{ " + strings.Join(names, ", ") + " }"
}

func readOptions(ctx context.Context, name string, f function, given []any) (options, error) {
	takes := min(len(f.params), 1)
	if len(given) != takes {
		return nil, callerFault(ctx, fmt.Sprintf("%s takes %s, and it was given %d arguments", name, signature(f),
			len(given)))
	}
	opts := options{}
	if takes == 0 {
		return opts, nil
	}
	object, isObject := given[0].(cliche.Map)
	if !isObject {
		return nil, callerFault(ctx, fmt.Sprintf("%s takes an object: %s", name, signature(f)))
	}
	for _, key := range object.Keys() {
		at := slices.IndexFunc(f.params, func(p param) bool { return p.name == key })
		if at < 0 {
			return nil, callerFault(ctx, fmt.Sprintf("%s takes no %s: it takes %s", name, render.Quote(key),
				signature(f)))
		}
		value, _ := object.Get(key)
		p := f.params[at]
		read, reason := readParam(p.kind, value)
		if reason == "" && p.refuse != nil {
			reason = p.refuse(read)
		}
		if reason != "" {
			return nil, callerFault(ctx, fmt.Sprintf("%s: %s %s", name, key, reason))
		}
		opts[key] = read
	}
	for _, p := range f.params {
		if p.required && !opts.given(p.name) {
			return nil, callerFault(ctx, fmt.Sprintf("%s was not given %s, which it takes: %s", name, p.name,
				signature(f)))
		}
	}
	if f.refuse != nil {
		if reason := f.refuse(opts); reason != "" {
			return nil, callerFault(ctx, fmt.Sprintf("%s: %s", name, reason))
		}
	}
	return opts, nil
}

func readParam(kind paramKind, value any) (any, string) {
	switch kind {
	case clearableParam:
		if value == nil {
			return cleared{}, ""
		}
		return readText(value, "is neither a string nor null")
	case wholeParam:
		return readWhole(value)
	case boolParam:
		if set, isBool := value.(bool); isBool {
			return set, ""
		}
		return nil, "is no boolean"
	case textsParam:
		return readTexts(value)
	case fieldValuesParam:
		return readFieldValues(value)
	case attributesParam:
		return readAttributes(value)
	case bytesParam:
		switch v := value.(type) {
		case string:
			return []byte(v), ""
		case []byte:
			return v, ""
		}
		return nil, "is neither a string nor a Buffer, a Uint8Array or an ArrayBuffer"
	case commentsParam:
		if value == everyComment {
			return youtrack.AllComments(), ""
		}
		last, reason := readWhole(value)
		if reason != "" {
			return nil, "is neither " + everyComment + " nor a whole number of comments"
		}
		return youtrack.LastComments(last.(int)), ""
	}
	return readText(value, "is no string")
}

const everyComment = "all"

func readText(value any, reason string) (any, string) {
	text, isText := value.(string)
	if !isText {
		return nil, reason
	}
	return text, ""
}

// An int is 32 bits, as the int flag of a declaration is, so whatever the command line gives a function takes.
func readWhole(value any) (any, string) {
	const notWhole = "is no whole number of 32 bits"
	switch v := value.(type) {
	case int64:
		if v < math.MinInt32 || v > math.MaxInt32 {
			return nil, notWhole
		}
		return int(v), ""
	case float64:
		return nil, notWhole
	case *big.Int:
		return nil, "is no number"
	}
	return nil, "is no number"
}

// items reads an array a script built or a list of an answer.
func items(value any) ([]any, bool) {
	switch v := value.(type) {
	case []any:
		return v, true
	case cliche.List:
		held := make([]any, 0, v.Len())
		for i := range v.Len() {
			held = append(held, v.Index(i))
		}
		return held, true
	}
	return nil, false
}

func readTexts(value any) (any, string) {
	listed, isArray := items(value)
	if !isArray {
		return nil, "is no array of strings"
	}
	texts := make([]string, 0, len(listed))
	for _, item := range listed {
		text, isText := item.(string)
		if !isText {
			return nil, "is no array of strings"
		}
		texts = append(texts, text)
	}
	return texts, ""
}

func readFieldValues(value any) (any, string) {
	const reason = "is no object of custom fields, each a string, an array of strings or null"
	object, isObject := value.(cliche.Map)
	if !isObject {
		return nil, reason
	}
	writes := []youtrack.FieldWrite{}
	for _, name := range object.Keys() {
		held, _ := object.Get(name)
		switch v := held.(type) {
		case nil:
			writes = append(writes, youtrack.FieldWrite{Name: name, Clear: true})
		case string:
			writes = append(writes, youtrack.FieldWrite{Name: name, Values: []string{v}})
		default:
			texts, failed := readTexts(held)
			if failed != "" {
				return nil, reason
			}
			writes = append(writes, youtrack.FieldWrite{Name: name, Values: texts.([]string)})
		}
	}
	return writes, ""
}

func readAttributes(value any) (any, string) {
	const reason = "is no object of attributes, each a string or null"
	object, isObject := value.(cliche.Map)
	if !isObject {
		return nil, reason
	}
	writes := []youtrack.AttributeWrite{}
	for _, name := range object.Keys() {
		held, _ := object.Get(name)
		switch v := held.(type) {
		case nil:
			writes = append(writes, youtrack.AttributeWrite{Name: name, Clear: true})
		case string:
			writes = append(writes, youtrack.AttributeWrite{Name: name, Value: v})
		default:
			return nil, reason
		}
	}
	return writes, ""
}

func (r *run) fail(ctx context.Context, args ...any) (any, error) {
	fault, err := faultArguments(ctx, "fail", args)
	if err != nil {
		return nil, err
	}
	return nil, thrown{fault: fault}
}

func (r *run) warn(ctx context.Context, args ...any) (any, error) {
	fault, err := faultArguments(ctx, "warn", args)
	if err != nil {
		return nil, err
	}
	r.host.Warn(fault.Warning())
	return cliche.Undefined{}, nil
}

// script_failed is missing: only ytrack says a script failed, so its place in a script cannot be forged.
var scriptCodes = []youtrack.Code{youtrack.CodeBadUsage, youtrack.CodeUnknownName, youtrack.CodeMissingRequired,
	youtrack.CodeNotFound, youtrack.CodeDenied, youtrack.CodeRejected, youtrack.CodeUpstreamFailed,
	youtrack.CodeUpstreamInvalid, youtrack.CodeWriteUncertain}

func faultArguments(ctx context.Context, name string, args []any) (*diag.Fault, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, callerFault(ctx, name+" takes a code, a message and, if the fault has them, its details")
	}
	text, _ := args[0].(string)
	code := youtrack.Code(text)
	if !slices.Contains(scriptCodes, code) {
		codes := make([]string, 0, len(scriptCodes))
		for _, known := range scriptCodes {
			codes = append(codes, string(known))
		}
		return nil, callerFault(ctx, fmt.Sprintf("%s takes a string code of the dictionary: %s", name,
			strings.Join(codes, ", ")))
	}
	message, isText := args[1].(string)
	if !isText {
		return nil, callerFault(ctx, name+" takes its message as a string")
	}
	fault := &diag.Fault{Code: code, Message: message}
	if len(args) < 3 {
		return fault, nil
	}
	if _, undefined := args[2].(cliche.Undefined); undefined {
		return fault, nil
	}
	node, err := toNode(args[2])
	if err == nil && node.Kind() != youtrack.MapNode {
		err = errors.New("is no object")
	}
	if err == nil && slices.ContainsFunc(node.Pairs(), func(pair youtrack.Pair) bool {
		return pair.Key == "code" || pair.Key == "message"
	}) {
		err = errors.New("hold code or message, which the fault has already")
	}
	if err != nil {
		return nil, callerFault(ctx, name+": the details "+err.Error())
	}
	fault.Details = node.Pairs()
	return fault, nil
}

func (r *run) address(context.Context) (any, error) {
	address, fault := r.host.Address()
	if fault != nil {
		return nil, thrown{fault: fault}
	}
	return address, nil
}
