package script

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"github.com/dop251/goja"
	"github.com/dop251/goja/file"
	"github.com/dop251/goja/parser"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/diag"
	"github.com/hakastein/ytrack/internal/render"
)

// The login is looked up only when a script first needs it, so a script that fails before that reports its own fault.
type Host struct {
	Call    func(ctx context.Context, call func(context.Context, *youtrack.Client) (*youtrack.Node, error)) (*youtrack.Node, *diag.Fault)
	Address func() (string, *diag.Fault)
	Warn    func(*youtrack.Warning)
}

// A flag left out of the call has no key in Flags.
type Input struct {
	Args  []string
	Flags map[string]any
}

type engine struct {
	ctx     context.Context
	vm      *goja.Runtime
	root    *Root
	host    Host
	modules map[string]*goja.Object
	apis    map[int]*goja.Object
	wrote   bool
}

// Deep enough for any honest recursion, shallow enough to stop a runaway one long before the memory runs out.
const maxCallStack = 10000

// wrote holds once the instance may have changed, even when a fault came after.
func Run(ctx context.Context, command *Command, input Input, host Host) (answer *youtrack.Node, wrote bool, fault *diag.Fault) {
	e := &engine{ctx: ctx, vm: goja.New(), root: command.root, host: host, modules: map[string]*goja.Object{},
		apis: map[int]*goja.Object{}}
	e.vm.SetMaxCallStackSize(maxCallStack)
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			e.vm.Interrupt(ctx.Err())
		case <-stopped:
		}
	}()
	answer, fault = e.run(command, input)
	if fault != nil && fault.Code == diag.CodeScriptFailed && command.root.Builtin() {
		defect(fault)
	}
	if fault != nil && e.wrote {
		fault.AfterWrite = true
	}
	return answer, e.wrote, fault
}

func (e *engine) run(command *Command, input Input) (*youtrack.Node, *diag.Fault) {
	var run goja.Callable
	var isFunction bool
	if fault := e.catch(func() {
		exports := e.load(command.file)
		if object, isObject := exports.(*goja.Object); isObject {
			run, isFunction = goja.AssertFunction(object.Get("run"))
		}
	}); fault != nil {
		return nil, fault
	}
	if !isFunction {
		return nil, e.fileFault(command.file, "exports.run is no function, and a command script is run by calling it")
	}
	var returned goja.Value
	fault := e.catch(func() {
		var err error
		returned, err = run(goja.Undefined(), e.input(command, input))
		if err != nil {
			panic(err)
		}
	})
	if fault != nil {
		return nil, fault
	}
	document, err := e.node(returned, 0)
	if err == nil && document.Kind() != youtrack.MapNode {
		err = errors.New("is no object, and a command prints one YAML mapping")
	}
	if err != nil {
		return nil, e.fileFault(command.file, "the value exports.run returned "+err.Error())
	}
	return document, nil
}

func (e *engine) input(command *Command, input Input) *goja.Object {
	given := e.vm.NewObject()
	for i, arg := range command.Args {
		e.define(given, arg.Name, e.vm.ToValue(input.Args[i]))
	}
	for _, flag := range command.Flags {
		if value, isGiven := input.Flags[flag.Name]; isGiven {
			e.define(given, flag.Name, e.vm.ToValue(value))
		}
	}
	return given
}

func (e *engine) define(object *goja.Object, key string, value goja.Value) {
	if err := object.DefineDataProperty(key, value, goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_TRUE); err != nil {
		panic(err)
	}
}

func (e *engine) catch(f func()) (fault *diag.Fault) {
	defer func() {
		thrown := recover()
		if thrown == nil {
			return
		}
		switch thrown := thrown.(type) {
		case goja.Value:
			fault = e.root.fault(thrown.String(), file.Position{})
			if object, isObject := thrown.(*goja.Object); isObject {
				if held, isFault := object.Export().(*faultValue); isFault {
					copied := *held.fault
					fault = &copied
				}
			}
		case error:
			fault = e.faultOf(thrown)
		default:
			panic(thrown)
		}
	}()
	f()
	return nil
}

func (e *engine) faultOf(err error) *diag.Fault {
	var interrupted *goja.InterruptedError
	var overflow *goja.StackOverflowError
	var exception *goja.Exception
	switch {
	case errors.As(err, &interrupted):
		return &diag.Fault{Code: youtrack.CodeUpstreamFailed, Message: "the script was stopped: " + interrupted.Error()}
	case errors.As(err, &overflow):
		return e.stackFault("RangeError: Maximum call stack size exceeded", overflow.Stack())
	case errors.As(err, &exception):
		if object, isObject := exception.Value().(*goja.Object); isObject {
			if thrown, isFault := object.Export().(*faultValue); isFault {
				copied := *thrown.fault
				return &copied
			}
		}
		return e.stackFault(exception.Value().String(), exception.Stack())
	}
	var syntax *unreadable
	if errors.As(err, &syntax) {
		return e.root.fault(syntax.message, syntax.at)
	}
	return &diag.Fault{Code: diag.CodeScriptFailed, Message: err.Error()}
}

func (e *engine) stackFault(message string, stack []goja.StackFrame) *diag.Fault {
	return e.root.fault(message, placeIn(stack))
}

// The first frame with a source is the line of the script; native frames of the API come before it.
func placeIn(stack []goja.StackFrame) file.Position {
	for _, frame := range stack {
		at := frame.Position()
		if at.Line > 0 {
			if at.Line == 1 {
				at.Column -= len(modulePrefix)
			}
			return at
		}
	}
	return file.Position{}
}

func (r *Root) fault(message string, at file.Position) *diag.Fault {
	fault := &diag.Fault{Code: diag.CodeScriptFailed, Message: message}
	if at.Filename != "" {
		fault.Details = append(fault.Details, youtrack.Pair{Key: "file", Value: youtrack.NewString(at.Filename)})
	}
	if at.Line > 0 {
		fault.Details = append(fault.Details,
			youtrack.Pair{Key: "line", Value: youtrack.NewNumber(json.Number(strconv.Itoa(at.Line)))},
			youtrack.Pair{Key: "column", Value: youtrack.NewNumber(json.Number(strconv.Itoa(at.Column)))})
	}
	return fault
}

// A builtin script is code of ytrack, so its defect is a bug of ytrack and panics as one in Go would.
func defect(fault *diag.Fault) {
	place := make([]string, 0, len(fault.Details))
	for _, pair := range fault.Details {
		place = append(place, pair.Value.Value())
	}
	panic("a builtin script failed at " + strings.Join(place, ":") + ": " + fault.Message)
}

func (e *engine) fileFault(name, message string) *diag.Fault {
	return e.root.fault(message, file.Position{Filename: e.root.display(name)})
}

// The wrapper keeps the lines of the module where they are; only the first line moves right by its length.
const modulePrefix = "(function (exports, require, module) {"

func (e *engine) load(name string) goja.Value {
	if module, loaded := e.modules[name]; loaded {
		return module.Get("exports")
	}
	source, err := fs.ReadFile(e.root.fsys, name)
	if err != nil {
		panic(e.throw(e.callerFault(fmt.Sprintf("module %s cannot be read: %v", render.Quote(name), err))))
	}
	display := e.root.display(name)
	program, err := parser.ParseFile(nil, display, modulePrefix+string(source)+"\n})", 0)
	if err != nil {
		var list parser.ErrorList
		if errors.As(err, &list) && len(list) > 0 {
			at := list[0].Position
			if at.Line == 1 {
				at.Column -= len(modulePrefix)
			}
			panic(e.throw(e.root.fault("SyntaxError: "+list[0].Message, at)))
		}
		panic(e.throw(e.root.fault(err.Error(), file.Position{Filename: display})))
	}
	compiled, err := goja.CompileAST(program, true)
	if err != nil {
		panic(e.throw(e.root.fault(err.Error(), file.Position{Filename: display})))
	}
	wrapper, err := e.vm.RunProgram(compiled)
	if err != nil {
		panic(err)
	}
	module, exports := e.vm.NewObject(), e.vm.NewObject()
	if err := module.Set("exports", exports); err != nil {
		panic(err)
	}
	e.modules[name] = module
	call, _ := goja.AssertFunction(wrapper)
	if _, err := call(goja.Undefined(), exports, e.vm.ToValue(e.require(name)), module); err != nil {
		panic(err)
	}
	return module.Get("exports")
}

const apiPrefix = "ytrack/v"

func (e *engine) require(from string) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		wanted, isString := call.Argument(0).Export().(string)
		switch {
		case !isString:
			panic(e.throw(e.callerFault("require takes a string")))
		case strings.HasPrefix(wanted, apiPrefix):
			return e.api(wanted)
		case strings.HasPrefix(wanted, "./"), strings.HasPrefix(wanted, "../"):
			return e.load(e.library(from, wanted))
		}
		panic(e.throw(e.callerFault(fmt.Sprintf("require %s names neither %s<N> nor a module by a path starting "+
			"with ./ or ../", render.Quote(wanted), apiPrefix))))
	}
}

func (e *engine) library(from, wanted string) string {
	name := path.Join(path.Dir(from), wanted)
	if !strings.HasSuffix(name, ".js") {
		name += ".js"
	}
	if name == ".." || strings.HasPrefix(name, "../") {
		panic(e.throw(e.callerFault(fmt.Sprintf("require %s leaves the root of the script, and a script takes "+
			"modules only from its own root", render.Quote(wanted)))))
	}
	source, err := fs.ReadFile(e.root.fsys, name)
	if err != nil {
		panic(e.throw(e.callerFault(fmt.Sprintf("require %s: %v", render.Quote(wanted), unwrapPath(err)))))
	}
	if declared, _ := declaration(name, string(source)); declared != nil {
		panic(e.throw(e.callerFault(fmt.Sprintf("require %s names a command script, and a script calls no other "+
			"command", render.Quote(wanted)))))
	}
	return name
}

func unwrapPath(err error) error {
	var failed *fs.PathError
	if errors.As(err, &failed) {
		return failed.Err
	}
	return err
}

func (e *engine) api(wanted string) goja.Value {
	version, err := strconv.Atoi(strings.TrimPrefix(wanted, apiPrefix))
	if err != nil || version < 1 || strconv.Itoa(version) != strings.TrimPrefix(wanted, apiPrefix) {
		version = 0
	}
	build, supported := versions[version]
	if !supported {
		panic(e.throw(e.callerFault(fmt.Sprintf("require %s names no version of the API: ytrack has %s%d",
			render.Quote(wanted), apiPrefix, currentVersion))))
	}
	if api, built := e.apis[version]; built {
		return api
	}
	api := build(e)
	e.apis[version] = api
	return api
}

func (e *engine) callerFault(message string) *diag.Fault {
	return e.root.fault(message, placeIn(e.vm.CaptureCallStack(0, nil)))
}
