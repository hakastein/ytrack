package script

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/file"
	"github.com/dop251/goja/parser"
	"github.com/dop251/goja/token"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/render"
)

// Read from the declaration alone, without running the module.
type Command struct {
	Path    []string
	Short   string
	Long    string
	Args    []Arg
	Flags   []Flag
	Example *youtrack.Node

	root *Root
	file string
}

func (c *Command) Root() *Root {
	return c.root
}

type Arg struct {
	Name string
	Path bool
}

type FlagType string

const (
	StringFlag  FlagType = "string"
	IntFlag     FlagType = "int"
	BoolFlag    FlagType = "bool"
	StringsFlag FlagType = "strings"
)

type Flag struct {
	Name    string
	Type    FlagType
	Usage   string
	Choices []string
}

type unreadable struct {
	message string
	at      file.Position
}

func (u *unreadable) Error() string {
	return u.message
}

// declaration reads exports.command of a module; a module without one is a library module, and nil, nil stands for it.
func declaration(name, source string) (*Command, error) {
	program, err := parser.ParseFile(nil, name, source, 0)
	if err != nil {
		var list parser.ErrorList
		if errors.As(err, &list) && len(list) > 0 {
			return nil, &unreadable{message: "SyntaxError: " + list[0].Message, at: list[0].Position}
		}
		return nil, &unreadable{message: err.Error()}
	}
	literal := commandLiteral(program)
	if literal == nil {
		return nil, nil
	}
	r := &literalReader{file: program.File}
	value := r.value(literal)
	if r.fault != nil {
		return nil, r.fault
	}
	command := r.command(value, literal)
	if r.fault != nil {
		return nil, r.fault
	}
	return command, nil
}

func commandLiteral(program *ast.Program) ast.Expression {
	for _, statement := range program.Body {
		expression, isExpression := statement.(*ast.ExpressionStatement)
		if !isExpression {
			continue
		}
		assignment, isAssignment := expression.Expression.(*ast.AssignExpression)
		if !isAssignment || assignment.Operator != token.ASSIGN {
			continue
		}
		target, isDot := assignment.Left.(*ast.DotExpression)
		if !isDot || target.Identifier.Name != "command" {
			continue
		}
		if exports, isName := target.Left.(*ast.Identifier); isName && exports.Name == "exports" {
			return assignment.Right
		}
	}
	return nil
}

// literal is a value of the declaration: string, float64, bool, nil, []literal or *object.
type literal any

type object struct {
	keys   []string
	values map[string]literal
	nodes  map[string]ast.Expression
}

type literalReader struct {
	file  *file.File
	fault *unreadable
}

func (r *literalReader) fail(at ast.Node, format string, args ...any) {
	if r.fault != nil {
		return
	}
	r.fault = &unreadable{
		message: "exports.command " + fmt.Sprintf(format, args...),
		at:      r.file.Position(int(at.Idx0()) - r.file.Base()),
	}
}

func (r *literalReader) value(expression ast.Expression) literal {
	switch e := expression.(type) {
	case *ast.StringLiteral:
		return e.Value.String()
	case *ast.TemplateLiteral:
		if e.Tag == nil && len(e.Expressions) == 0 && len(e.Elements) == 1 {
			return e.Elements[0].Parsed.String()
		}
	case *ast.NumberLiteral:
		if number, isFloat := e.Value.(float64); isFloat {
			return number
		}
		if number, isInt := e.Value.(int64); isInt {
			return float64(number)
		}
	case *ast.BooleanLiteral:
		return e.Value
	case *ast.NullLiteral:
		return nil
	case *ast.ArrayLiteral:
		items := make([]literal, 0, len(e.Value))
		for _, item := range e.Value {
			if item == nil {
				r.fail(e, "holds an array with a hole")
				return nil
			}
			items = append(items, r.value(item))
		}
		return items
	case *ast.ObjectLiteral:
		return r.object(e)
	}
	r.fail(expression, "is no pure literal: it may hold strings, numbers, true, false, null, arrays and objects, "+
		"but no names, calls or computations")
	return nil
}

func (r *literalReader) object(e *ast.ObjectLiteral) *object {
	read := &object{values: map[string]literal{}, nodes: map[string]ast.Expression{}}
	for _, property := range e.Value {
		keyed, isKeyed := property.(*ast.PropertyKeyed)
		if !isKeyed || keyed.Computed || keyed.Kind != ast.PropertyKindValue {
			r.fail(property, "holds a property that is no plain key and value")
			return read
		}
		var key string
		switch name := keyed.Key.(type) {
		case *ast.StringLiteral:
			key = name.Value.String()
		case *ast.Identifier:
			key = name.Name.String()
		default:
			r.fail(keyed.Key, "holds a key that is neither a name nor a string")
			return read
		}
		if _, repeated := read.values[key]; repeated {
			r.fail(keyed.Key, "holds the key %s twice", render.Quote(key))
			return read
		}
		read.keys = append(read.keys, key)
		read.values[key] = r.value(keyed.Value)
		read.nodes[key] = keyed.Value
	}
	return read
}

// A word cannot start with a dash, or it would read as a flag.
var wordGrammar = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

const maxShort = 60

func (r *literalReader) command(value literal, at ast.Expression) *Command {
	declared, isObject := value.(*object)
	if !isObject {
		r.fail(at, "is no object")
		return nil
	}
	r.onlyKeys(declared, at, "short", "long", "args", "flags", "example")
	command := &Command{}
	command.Short = r.text(declared, "short", at, true)
	if short := command.Short; r.fault == nil && (short == "" || strings.ContainsAny(short, "\r\n") ||
		len([]rune(short)) > maxShort) {
		r.fail(declared.nodes["short"], "short is not one line of 1 to %d characters", maxShort)
	}
	command.Long = r.text(declared, "long", at, false)
	command.Args = r.args(declared)
	command.Flags = r.flags(declared)
	if example, given := declared.values["example"]; given {
		node, err := exampleNode(example)
		if err == nil {
			err = (render.YAML{}).Render(io.Discard, node)
		}
		if err != nil {
			r.fail(declared.nodes["example"], "example %v", err)
		}
		command.Example = node
	}
	names := map[string]bool{"help": true}
	for _, arg := range command.Args {
		names[arg.Name] = true
	}
	for _, flag := range command.Flags {
		if names[flag.Name] {
			r.fail(declared.nodes["flags"], "names %s twice among its arguments, its flags and --help", render.Quote(flag.Name))
		}
		names[flag.Name] = true
	}
	return command
}

func (r *literalReader) onlyKeys(declared *object, at ast.Node, allowed ...string) {
	for _, key := range declared.keys {
		if !slices.Contains(allowed, key) {
			r.fail(at, "holds %s, which is none of %s", render.Quote(key), strings.Join(allowed, ", "))
		}
	}
}

func (r *literalReader) text(declared *object, key string, at ast.Node, required bool) string {
	value, given := declared.values[key]
	if !given {
		if required {
			r.fail(at, "has no %s", key)
		}
		return ""
	}
	text, isString := value.(string)
	if !isString {
		r.fail(declared.nodes[key], "%s is no string", key)
	}
	return text
}

func (r *literalReader) args(declared *object) []Arg {
	value, given := declared.values["args"]
	if !given {
		return nil
	}
	items, isArray := value.([]literal)
	if !isArray {
		r.fail(declared.nodes["args"], "args is no array")
		return nil
	}
	at := declared.nodes["args"].(*ast.ArrayLiteral)
	args := make([]Arg, 0, len(items))
	for i, item := range items {
		arg, isObject := item.(*object)
		if !isObject {
			r.fail(at.Value[i], "holds an argument that is no object")
			return nil
		}
		r.onlyKeys(arg, at.Value[i], "name", "type")
		name := r.name(arg, at.Value[i])
		kind := r.text(arg, "type", at.Value[i], true)
		if kind != "string" && kind != "path" && r.fault == nil {
			r.fail(arg.nodes["type"], "gives argument %s the type %s, which is neither string nor path",
				render.Quote(name), render.Quote(kind))
		}
		args = append(args, Arg{Name: name, Path: kind == "path"})
	}
	return args
}

func (r *literalReader) name(declared *object, at ast.Node) string {
	name := r.text(declared, "name", at, true)
	if r.fault == nil && !wordGrammar.MatchString(name) {
		r.fail(declared.nodes["name"], "names %s, which is not lowercase letters, digits, - and _",
			render.Quote(name))
	}
	return name
}

func (r *literalReader) flags(declared *object) []Flag {
	value, given := declared.values["flags"]
	if !given {
		return nil
	}
	flags, isObject := value.(*object)
	if !isObject {
		r.fail(declared.nodes["flags"], "flags is no object")
		return nil
	}
	read := make([]Flag, 0, len(flags.keys))
	for _, name := range flags.keys {
		at := flags.nodes[name]
		if !wordGrammar.MatchString(name) {
			r.fail(at, "names the flag %s, which is not lowercase letters, digits, - and _", render.Quote(name))
			return nil
		}
		flag, isObject := flags.values[name].(*object)
		if !isObject {
			r.fail(at, "declares --%s with no object", name)
			return nil
		}
		r.onlyKeys(flag, at, "type", "usage", "choices")
		kind := FlagType(r.text(flag, "type", at, true))
		if !slices.Contains([]FlagType{StringFlag, IntFlag, BoolFlag, StringsFlag}, kind) && r.fault == nil {
			r.fail(flag.nodes["type"], "gives --%s the type %s, which is none of string, int, bool and strings",
				name, render.Quote(string(kind)))
		}
		read = append(read, Flag{Name: name, Type: kind, Usage: r.text(flag, "usage", at, true),
			Choices: r.choices(flag, name, kind)})
	}
	return read
}

func (r *literalReader) choices(flag *object, name string, kind FlagType) []string {
	value, given := flag.values["choices"]
	if !given {
		return nil
	}
	at := flag.nodes["choices"]
	if kind != StringFlag && kind != StringsFlag {
		r.fail(at, "gives choices to --%s, which takes no strings", name)
		return nil
	}
	items, isArray := value.([]literal)
	if !isArray || len(items) == 0 {
		r.fail(at, "gives --%s choices that are no array of strings", name)
		return nil
	}
	choices := make([]string, 0, len(items))
	for _, item := range items {
		choice, isString := item.(string)
		if !isString {
			r.fail(at, "gives --%s choices that are no array of strings", name)
			return nil
		}
		choices = append(choices, choice)
	}
	return choices
}

func exampleNode(value literal) (*youtrack.Node, error) {
	declared, isObject := value.(*object)
	if !isObject {
		return nil, errors.New("is no object")
	}
	return literalNode(declared)
}

func literalNode(value literal) (*youtrack.Node, error) {
	switch v := value.(type) {
	case nil:
		return youtrack.NewNull(), nil
	case bool:
		return youtrack.NewBool(v), nil
	case float64:
		return numberNode(v)
	case string:
		return stringNode(v), nil
	case []literal:
		items := make([]*youtrack.Node, 0, len(v))
		for _, item := range v {
			node, err := literalNode(item)
			if err != nil {
				return nil, err
			}
			items = append(items, node)
		}
		return youtrack.NewList(items...), nil
	case *object:
		pairs := make([]youtrack.Pair, 0, len(v.keys))
		for _, key := range v.keys {
			node, err := literalNode(v.values[key])
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, pairOf(key, node))
		}
		return youtrack.NewMap(pairs...), nil
	}
	return nil, fmt.Errorf("holds %T", value)
}
