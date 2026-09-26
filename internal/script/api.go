package script

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/dop251/goja"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

const currentVersion = 1

var versions = map[int]func(e *engine) *goja.Object{
	1: v1,
}

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

func (o options) optional(name string) *string {
	text, given := o[name].(string)
	if !given {
		return nil
	}
	return &text
}

func (o options) int(name string) int {
	number, _ := o[name].(int)
	return number
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

func (e *engine) service(name string, functions map[string]function) *goja.Object {
	service := e.vm.NewObject()
	for _, method := range slices.Sorted(maps.Keys(functions)) {
		e.define(service, method, e.vm.ToValue(e.function(name+"."+method, functions[method])))
	}
	return service
}

func (e *engine) function(name string, f function) func(goja.FunctionCall) goja.Value {
	return func(given goja.FunctionCall) goja.Value {
		bound, fault := f.bind(e.options(name, f, given.Arguments))
		if fault != nil {
			panic(e.throw(fault))
		}
		node, fault := e.host.Call(e.ctx, bound)
		if fault != nil {
			if fault.MayHaveWritten() {
				e.wrote = true
			}
			panic(e.throw(fault))
		}
		if f.writes {
			e.wrote = true
		}
		return e.value(node)
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

func (e *engine) options(name string, f function, given []goja.Value) options {
	takes := min(len(f.params), 1)
	if len(given) != takes {
		panic(e.throw(e.callerFault(fmt.Sprintf("%s takes %s, and it was given %d arguments", name, signature(f),
			len(given)))))
	}
	opts := options{}
	if takes == 0 {
		return opts
	}
	object, isObject := given[0].(*goja.Object)
	if !isObject || object.ClassName() != "Object" {
		panic(e.throw(e.callerFault(fmt.Sprintf("%s takes an object: %s", name, signature(f)))))
	}
	for _, key := range object.Keys() {
		at := slices.IndexFunc(f.params, func(p param) bool { return p.name == key })
		if at < 0 {
			panic(e.throw(e.callerFault(fmt.Sprintf("%s takes no %s: it takes %s", name, render.Quote(key),
				signature(f)))))
		}
		value := object.Get(key)
		if goja.IsUndefined(value) {
			continue
		}
		p := f.params[at]
		read, reason := readParam(p.kind, value)
		if reason == "" && p.refuse != nil {
			reason = p.refuse(read)
		}
		if reason != "" {
			panic(e.throw(e.callerFault(fmt.Sprintf("%s: %s %s", name, key, reason))))
		}
		opts[key] = read
	}
	for _, p := range f.params {
		if p.required && !opts.given(p.name) {
			panic(e.throw(e.callerFault(fmt.Sprintf("%s was not given %s, which it takes: %s", name, p.name,
				signature(f)))))
		}
	}
	return opts
}

func readParam(kind paramKind, value goja.Value) (any, string) {
	switch kind {
	case clearableParam:
		if goja.IsNull(value) {
			return cleared{}, ""
		}
		return readText(value, "is neither a string nor null")
	case wholeParam:
		return readWhole(value)
	case textsParam:
		return readTexts(value)
	case fieldValuesParam:
		return readFieldValues(value)
	case attributesParam:
		return readAttributes(value)
	case commentsParam:
		if goja.IsString(value) && value.String() == everyComment {
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

func readText(value goja.Value, reason string) (any, string) {
	if !goja.IsString(value) {
		return nil, reason
	}
	return value.String(), ""
}

// An int is 32 bits, as the int flag of a declaration is, so whatever the command line gives a function takes.
func readWhole(value goja.Value) (any, string) {
	if !goja.IsNumber(value) {
		return nil, "is no number"
	}
	number := value.ToFloat()
	if number != math.Trunc(number) || number < math.MinInt32 || number > math.MaxInt32 {
		return nil, "is no whole number of 32 bits"
	}
	return int(number), ""
}

func readTexts(value goja.Value) (any, string) {
	items, isArray := arrayItems(value)
	if !isArray {
		return nil, "is no array of strings"
	}
	texts := make([]string, 0, len(items))
	for _, item := range items {
		if !goja.IsString(item) {
			return nil, "is no array of strings"
		}
		texts = append(texts, item.String())
	}
	return texts, ""
}

func arrayItems(value goja.Value) ([]goja.Value, bool) {
	list, isObject := value.(*goja.Object)
	if !isObject || list.ClassName() != "Array" {
		return nil, false
	}
	items := []goja.Value{}
	for index := range list.Get("length").ToInteger() {
		items = append(items, list.Get(strconv.FormatInt(index, 10)))
	}
	return items, true
}

func plainEntries(value goja.Value) ([]string, *goja.Object, bool) {
	object, isObject := value.(*goja.Object)
	if !isObject || object.ClassName() != "Object" {
		return nil, nil, false
	}
	return object.Keys(), object, true
}

func readFieldValues(value goja.Value) (any, string) {
	const reason = "is no object of custom fields, each a string, an array of strings or null"
	names, object, isObject := plainEntries(value)
	if !isObject {
		return nil, reason
	}
	writes := make([]youtrack.FieldWrite, 0, len(names))
	for _, name := range names {
		held := object.Get(name)
		switch {
		case goja.IsUndefined(held):
		case goja.IsNull(held):
			writes = append(writes, youtrack.FieldWrite{Name: name, Clear: true})
		case goja.IsString(held):
			writes = append(writes, youtrack.FieldWrite{Name: name, Values: []string{held.String()}})
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

func readAttributes(value goja.Value) (any, string) {
	const reason = "is no object of attributes, each a string or null"
	names, object, isObject := plainEntries(value)
	if !isObject {
		return nil, reason
	}
	writes := make([]youtrack.AttributeWrite, 0, len(names))
	for _, name := range names {
		held := object.Get(name)
		switch {
		case goja.IsUndefined(held):
		case goja.IsNull(held):
			writes = append(writes, youtrack.AttributeWrite{Name: name, Clear: true})
		case goja.IsString(held):
			writes = append(writes, youtrack.AttributeWrite{Name: name, Value: held.String()})
		default:
			return nil, reason
		}
	}
	return writes, ""
}

type faultValue struct {
	engine  *engine
	fault   *diag.Fault
	details goja.Value
}

func (f *faultValue) Get(key string) goja.Value {
	switch key {
	case "code":
		return f.engine.vm.ToValue(string(f.fault.Code))
	case "message":
		return f.engine.vm.ToValue(f.fault.Message)
	case "details":
		return f.details
	case "wrote":
		return f.engine.vm.ToValue(f.fault.MayHaveWritten())
	}
	return nil
}

func (f *faultValue) Has(key string) bool {
	return slices.Contains(f.Keys(), key)
}

func (f *faultValue) Set(string, goja.Value) bool {
	return false
}

func (f *faultValue) Delete(key string) bool {
	return !f.Has(key)
}

func (f *faultValue) Keys() []string {
	return []string{"code", "message", "details", "wrote"}
}

func (e *engine) throw(fault *diag.Fault) *goja.Object {
	thrown := e.vm.NewDynamicObject(&faultValue{engine: e, fault: fault, details: e.value(youtrack.NewMap(fault.Details...))})
	if err := thrown.SetPrototype(e.errorPrototype); err != nil {
		panic(err)
	}
	e.thrown[thrown] = fault
	return thrown
}

func (e *engine) fail(call goja.FunctionCall) goja.Value {
	panic(e.throw(e.faultArguments("fail", call)))
}

func (e *engine) warn(call goja.FunctionCall) goja.Value {
	e.host.Warn(e.faultArguments("warn", call).Warning())
	return goja.Undefined()
}

// script_failed is missing: only ytrack says a script failed, so its place in a script cannot be forged.
var scriptCodes = []youtrack.Code{youtrack.CodeBadUsage, youtrack.CodeUnknownName, youtrack.CodeMissingRequired,
	youtrack.CodeNotFound, youtrack.CodeDenied, youtrack.CodeRejected, youtrack.CodeUpstreamFailed,
	youtrack.CodeUpstreamInvalid, youtrack.CodeWriteUncertain}

func (e *engine) faultArguments(name string, call goja.FunctionCall) *diag.Fault {
	if len(call.Arguments) < 2 || len(call.Arguments) > 3 {
		panic(e.throw(e.callerFault(name + " takes a code, a message and, if the fault has them, its details")))
	}
	var code youtrack.Code
	if goja.IsString(call.Argument(0)) {
		code = youtrack.Code(call.Argument(0).String())
	}
	if !slices.Contains(scriptCodes, code) {
		codes := make([]string, 0, len(scriptCodes))
		for _, known := range scriptCodes {
			codes = append(codes, string(known))
		}
		panic(e.throw(e.callerFault(fmt.Sprintf("%s takes a string code of the dictionary: %s", name,
			strings.Join(codes, ", ")))))
	}
	if !goja.IsString(call.Argument(1)) {
		panic(e.throw(e.callerFault(name + " takes its message as a string")))
	}
	fault := &diag.Fault{Code: code, Message: call.Argument(1).String()}
	details := call.Argument(2)
	if goja.IsUndefined(details) {
		return fault
	}
	node, err := e.node(details, 0)
	if err == nil && node.Kind() != youtrack.MapNode {
		err = errors.New("is no object")
	}
	if err == nil && slices.ContainsFunc(node.Pairs(), func(pair youtrack.Pair) bool {
		return pair.Key == "code" || pair.Key == "message"
	}) {
		err = errors.New("hold code or message, which the fault has already")
	}
	if err != nil {
		panic(e.throw(e.callerFault(name + ": the details " + err.Error())))
	}
	fault.Details = node.Pairs()
	return fault
}

func (e *engine) address(goja.FunctionCall) goja.Value {
	address, fault := e.host.Address()
	if fault != nil {
		panic(e.throw(fault))
	}
	return e.vm.ToValue(address)
}
