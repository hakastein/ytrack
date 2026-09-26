package script

import (
	"bytes"
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
	file    string
	host    Host
	modules map[string]*goja.Object
	apis    map[int]*goja.Object
	// A map or a list of an answer prints as its node wherever the script puts it.
	answers map[*goja.Object]*youtrack.Node
	thrown  map[*goja.Object]*diag.Fault
	wrote   bool
	// Taken before any script runs, since a script may replace the globals Object and Error.
	objectPrototype *goja.Object
	errorPrototype  *goja.Object
}

// goja sets no limit of its own, and a runaway recursion would take the memory of the process.
const maxCallStack = 10000

// wrote holds once the instance may have changed, even when a fault came after.
func Run(ctx context.Context, command *Command, input Input, host Host) (answer *youtrack.Node, wrote bool, fault *diag.Fault) {
	vm := goja.New()
	vm.SetMaxCallStackSize(maxCallStack)
	e := &engine{ctx: ctx, vm: vm, root: command.root, file: command.root.display(command.file), host: host,
		modules: map[string]*goja.Object{}, apis: map[int]*goja.Object{}, answers: map[*goja.Object]*youtrack.Node{},
		thrown: map[*goja.Object]*diag.Fault{}, objectPrototype: prototypeOf(vm, "Object"),
		errorPrototype: prototypeOf(vm, "Error")}
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-stopped:
		}
	}()
	answer, fault = e.run(command, input)
	if fault != nil && fault.Code == diag.CodeScriptFailed && command.root.Builtin() {
		defect(fault)
	}
	return answer, e.wrote, fault
}

func prototypeOf(vm *goja.Runtime, class string) *goja.Object {
	return vm.Get(class).ToObject(vm).Get("prototype").ToObject(vm)
}

func (e *engine) run(command *Command, input Input) (*youtrack.Node, *diag.Fault) {
	var answer *youtrack.Node
	var refused *diag.Fault
	if fault := e.catch(func() { answer, refused = e.answer(command, input) }); fault != nil {
		return nil, fault
	}
	return answer, refused
}

func (e *engine) answer(command *Command, input Input) (*youtrack.Node, *diag.Fault) {
	exports, _ := e.load(command.file).(*goja.Object)
	var run goja.Callable
	if exports != nil {
		run, _ = goja.AssertFunction(exports.Get("run"))
	}
	if run == nil {
		return nil, fileFault("exports.run is no function, and a command script is run by calling it", e.file)
	}
	returned, err := run(goja.Undefined(), e.call(command, input)...)
	if err != nil {
		panic(err)
	}
	document, err := e.node(returned, 0)
	if err == nil && document.Kind() != youtrack.MapNode {
		err = errors.New("is no object, and a command prints one YAML mapping")
	}
	if err != nil {
		return nil, fileFault("the value exports.run returned "+err.Error(), e.file)
	}
	return document, nil
}

// run takes the arguments of the call in order and its flags last, as a function of the API does.
func (e *engine) call(command *Command, input Input) []goja.Value {
	values := make([]goja.Value, 0, len(input.Args)+1)
	for _, arg := range input.Args {
		values = append(values, e.vm.ToValue(arg))
	}
	flags := e.vm.NewObject()
	for _, flag := range command.Flags {
		value, given := input.Flags[flag.Name]
		switch {
		case flag.Type == FieldsFlag:
			text, _ := value.(string)
			value, given = flag.fields(text), true
		case !given && flag.Default != nil:
			value, given = flag.Default, true
		}
		if given {
			e.define(flags, flag.Name, e.vm.ToValue(value))
		}
	}
	return append(values, flags)
}

func (e *engine) define(object *goja.Object, key string, value goja.Value) {
	if err := object.DefineDataProperty(key, value, goja.FLAG_FALSE, goja.FLAG_FALSE, goja.FLAG_TRUE); err != nil {
		panic(err)
	}
}

// Try turns what the script threw into an exception; a stopped script and an overflowed stack pass it as panics.
func (e *engine) catch(f func()) (fault *diag.Fault) {
	defer func() {
		switch thrown := recover().(type) {
		case nil:
		case error:
			fault = e.faultOf(thrown)
		default:
			panic(thrown)
		}
	}()
	if exception := e.vm.Try(f); exception != nil {
		return e.faultOf(exception)
	}
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
		return scriptFault("RangeError: Maximum call stack size exceeded", e.placeIn(overflow.Stack()))
	case errors.As(err, &exception):
		if object, isObject := exception.Value().(*goja.Object); isObject {
			if fault := e.thrown[object]; fault != nil {
				return fault
			}
		}
		return scriptFault(e.describe(exception.Value()), e.placeIn(exception.Stack()))
	}
	panic(err)
}

// String runs the toString of a thrown object, which may throw in turn.
func (e *engine) describe(value goja.Value) string {
	text := "a value with no string form was thrown"
	e.vm.Try(func() { text = value.String() })
	return text
}

// The first frame with a source is the line of the script; native frames of the API come before it. A fault
// raised by goja while ytrack reads the returned value has no frame of the script at all.
func (e *engine) placeIn(stack []goja.StackFrame) file.Position {
	for _, frame := range stack {
		if at := frame.Position(); at.Line > 0 {
			return unwrapped(at)
		}
	}
	return file.Position{Filename: e.file}
}

func unwrapped(at file.Position) file.Position {
	if at.Line == 1 {
		at.Column -= len(modulePrefix)
	}
	return at
}

func scriptFault(message string, at file.Position) *diag.Fault {
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

func fileFault(message, display string) *diag.Fault {
	return scriptFault(message, file.Position{Filename: display})
}

// A builtin script is code of ytrack, so its defect is a bug of ytrack and panics as one in Go would.
func defect(fault *diag.Fault) {
	place := make([]string, 0, len(fault.Details))
	for _, pair := range fault.Details {
		place = append(place, pair.Value.Value())
	}
	panic("a builtin script failed at " + strings.Join(place, ":") + ": " + fault.Message)
}

// The wrapper keeps the lines of the module where they are; only the first line moves right by its length.
const modulePrefix = "(function (exports, require, module) {"

// A syntax error was reported with its place when the declaration was read; what fails only here is strict mode and
// a top-level declaration of exports, require or module, which the wrapper declares.
func (e *engine) load(name string) goja.Value {
	if module, loaded := e.modules[name]; loaded {
		return module.Get("exports")
	}
	display := e.root.display(name)
	source, err := fs.ReadFile(e.root.fsys, name)
	if err != nil {
		panic(e.throw(fileFault(fmt.Sprintf("module %s cannot be read: %v", render.Quote(display), unwrapPath(err)), display)))
	}
	// The parser takes #! only at the start of the source, which the wrapper moves.
	if bytes.HasPrefix(source, []byte("#!")) {
		copy(source, "//")
	}
	program, err := parser.ParseFile(nil, display, modulePrefix+string(source)+"\n})", 0)
	if err != nil {
		panic(e.throw(fileFault(err.Error(), display)))
	}
	compiled, err := goja.CompileAST(program, true)
	var syntax *goja.CompilerSyntaxError
	if errors.As(err, &syntax) && syntax.File != nil {
		panic(e.throw(scriptFault("SyntaxError: "+syntax.Message, unwrapped(syntax.File.Position(syntax.Offset)))))
	}
	if err != nil {
		panic(e.throw(fileFault(err.Error(), display)))
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
		delete(e.modules, name)
		panic(err)
	}
	return module.Get("exports")
}

const apiPrefix = "ytrack/v"

func (e *engine) require(from string) func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if !goja.IsString(call.Argument(0)) {
			panic(e.throw(e.callerFault("require takes a string")))
		}
		wanted := call.Argument(0).String()
		switch {
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
	declared, err := declaration(e.root.display(name), string(source))
	var failed *unreadable
	if errors.As(err, &failed) {
		panic(e.throw(scriptFault(failed.message, failed.at)))
	}
	if declared != nil {
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
	version, _ := strconv.Atoi(strings.TrimPrefix(wanted, apiPrefix))
	build, supported := versions[version]
	if !supported || strconv.Itoa(version) != strings.TrimPrefix(wanted, apiPrefix) {
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
	return scriptFault(message, e.placeIn(e.vm.CaptureCallStack(0, nil)))
}
