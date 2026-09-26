package script

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
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

type param struct {
	name   string
	kind   FlagType
	refuse func(value any) string
}

type function struct {
	args   []string
	params []param
	// Refused as bad_usage before the login is looked up.
	check func(opts options) *diag.Fault
	call  func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error)
}

type options map[string]any

func (o options) string(name string) string {
	text, _ := o[name].(string)
	return text
}

func (o options) int(name string) int {
	number, _ := o[name].(int)
	return number
}

func v1(e *engine) *goja.Object {
	api := e.vm.NewObject()
	e.define(api, "project", e.entity("project", map[string]function{
		"show": {
			args:   []string{"code"},
			params: []param{fieldsParam()},
			call: func(ctx context.Context, c *youtrack.Client, args []string, opts options) (*youtrack.Node, error) {
				return c.Projects.Show(ctx, args[0], &youtrack.ShowProjectOptions{Fields: opts.string("fields")})
			},
		},
		"list": {
			params: []param{fieldsParam(), {name: "limit", kind: IntFlag}, {name: "skip", kind: IntFlag}},
			check:  checkPage,
			call: func(ctx context.Context, c *youtrack.Client, _ []string, opts options) (*youtrack.Node, error) {
				page := youtrack.Page{Limit: opts.int("limit"), Skip: opts.int("skip")}
				return c.Projects.List(ctx, &youtrack.ListProjectsOptions{Fields: opts.string("fields"), Page: page})
			},
		},
	}))
	e.define(api, "fail", e.vm.ToValue(e.fail))
	e.define(api, "warn", e.vm.ToValue(e.warn))
	if err := api.DefineAccessorProperty("address", e.vm.ToValue(e.address), nil, goja.FLAG_FALSE, goja.FLAG_TRUE); err != nil {
		panic(err)
	}
	return api
}

// The answer of a function must not change when ytrack changes the default fields of a command.
func fieldsParam() param {
	return param{name: "fields", kind: StringFlag, refuse: func(value any) string {
		expression := strings.TrimSpace(value.(string))
		if expression == "" || strings.HasPrefix(expression, "+") {
			return "names the default fields, and a function has no default: it takes the whole expression"
		}
		return ""
	}}
}

// The module reads a limit of 0 as its own default page.
func checkPage(opts options) *diag.Fault {
	if limit := opts.int("limit"); limit < 1 {
		message := fmt.Sprintf("--limit %d: a page holds at least one record", limit)
		return &diag.Fault{Code: youtrack.CodeBadUsage, Message: message}
	}
	return nil
}

func (e *engine) entity(name string, functions map[string]function) *goja.Object {
	entity := e.vm.NewObject()
	names := make([]string, 0, len(functions))
	for verb := range functions {
		names = append(names, verb)
	}
	slices.Sort(names)
	for _, verb := range names {
		e.define(entity, verb, e.vm.ToValue(e.commandFunction(name+"."+verb, functions[verb])))
	}
	return entity
}

func (e *engine) commandFunction(name string, f function) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		args, opts := e.arguments(name, f, call.Arguments)
		if f.check != nil {
			if fault := f.check(opts); fault != nil {
				panic(e.throw(fault))
			}
		}
		node, fault := e.host.Call(e.ctx, func(ctx context.Context, c *youtrack.Client) (*youtrack.Node, error) {
			return f.call(ctx, c, args, opts)
		})
		if fault != nil {
			if fault.MayHaveWritten() {
				e.wrote = true
			}
			panic(e.throw(fault))
		}
		return e.value(node)
	}
}

func (e *engine) arguments(name string, f function, given []goja.Value) ([]string, options) {
	takes := len(f.args)
	if len(f.params) > 0 {
		takes++
	}
	if len(given) != takes {
		panic(e.throw(e.callerFault(fmt.Sprintf("%s takes %s, and it was given %d arguments", name, signature(f), len(given)))))
	}
	args := make([]string, 0, len(f.args))
	for i, arg := range f.args {
		if !goja.IsString(given[i]) {
			panic(e.throw(e.callerFault(fmt.Sprintf("%s takes <%s> as a string", name, arg))))
		}
		args = append(args, given[i].String())
	}
	opts := options{}
	if len(f.params) > 0 {
		opts = e.options(name, f, given[len(f.args)])
	}
	for _, p := range f.params {
		if _, isGiven := opts[p.name]; !isGiven {
			panic(e.throw(e.callerFault(fmt.Sprintf("%s was not given %s, and a function has no default values",
				name, p.name))))
		}
	}
	return args, opts
}

func signature(f function) string {
	var parts []string
	for _, arg := range f.args {
		parts = append(parts, "<"+arg+">")
	}
	if len(f.params) > 0 {
		names := make([]string, 0, len(f.params))
		for _, p := range f.params {
			names = append(names, p.name)
		}
		parts = append(parts, "{"+strings.Join(names, ", ")+"}")
	}
	if len(parts) == 0 {
		return "no arguments"
	}
	return strings.Join(parts, ", ")
}

func (e *engine) options(name string, f function, value goja.Value) options {
	object, isObject := value.(*goja.Object)
	if !isObject || object.ClassName() != "Object" {
		panic(e.throw(e.callerFault(fmt.Sprintf("%s takes its flags as an object", name))))
	}
	opts := options{}
	for _, key := range object.Keys() {
		at := slices.IndexFunc(f.params, func(p param) bool { return p.name == key })
		if at < 0 {
			panic(e.throw(e.callerFault(fmt.Sprintf("%s takes no %s: it takes %s", name, render.Quote(key), signature(f)))))
		}
		given := object.Get(key)
		if goja.IsUndefined(given) {
			continue
		}
		p := f.params[at]
		read, reason := readParam(p.kind, given)
		if reason == "" && p.refuse != nil {
			reason = p.refuse(read)
		}
		if reason != "" {
			panic(e.throw(e.callerFault(fmt.Sprintf("%s: %s %s", name, key, reason))))
		}
		opts[key] = read
	}
	return opts
}

// An int is 32 bits, as the int flag of a declaration is, so whatever the command line gives a function takes.
func readParam(kind FlagType, value goja.Value) (any, string) {
	if kind == StringFlag {
		if !goja.IsString(value) {
			return nil, "is no string"
		}
		return value.String(), ""
	}
	if !goja.IsNumber(value) {
		return nil, "is no number"
	}
	number := value.ToFloat()
	if number != math.Trunc(number) || number < math.MinInt32 || number > math.MaxInt32 {
		return nil, "is no whole number of 32 bits"
	}
	return int(number), ""
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
