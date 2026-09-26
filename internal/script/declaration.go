package script

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/file"
	"github.com/dop251/goja/parser"
	"github.com/dop251/goja/token"

	"github.com/hakastein/ytrack/internal/render"
)

// Read from the declaration alone, without running the module.
type Command struct {
	Path  []string
	Short string
	Long  string
	Args  []Arg
	Flags []Flag

	root *Root
	file string
}

func (c *Command) Root() *Root {
	return c.root
}

type Arg struct {
	Name  string
	Path  bool
	Usage string
}

type FlagType string

const (
	StringFlag  FlagType = "string"
	IntFlag     FlagType = "int"
	BoolFlag    FlagType = "bool"
	StringsFlag FlagType = "strings"
	FieldsFlag  FlagType = "fields"
)

// Default is nil for a flag without one, and otherwise a string, an int, a bool or a []string by the type.
type Flag struct {
	Name    string
	Type    FlagType
	Usage   string
	Choices []string
	Default any
}

const fieldsUsage = "YouTrack fields `expression`; +expr adds to the default"

func (f Flag) fields(given string) string {
	return Fields(f.Default.(string), given)
}

// The SDK's fields= has no defaults, so a command that has them adds them itself.
func Fields(defaults, given string) string {
	given = strings.TrimSpace(given)
	added, adds := strings.CutPrefix(given, "+")
	switch {
	case given == "" || adds && strings.TrimSpace(added) == "":
		return defaults
	case adds:
		return defaults + "," + strings.TrimSpace(added)
	}
	return given
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

const wordRule = "lowercase letters, digits, - and _, starting with a letter or a digit"

const maxShort = 60

func (r *literalReader) command(value literal, at ast.Expression) *Command {
	declared, isObject := value.(*object)
	if !isObject {
		r.fail(at, "is no object")
		return nil
	}
	r.onlyKeys(declared, at, "short", "long", "args", "flags")
	command := &Command{}
	command.Short = r.text(declared, "short", at)
	if short := command.Short; r.fault == nil && (short == "" || strings.ContainsAny(short, "\r\n") ||
		len([]rune(short)) > maxShort) {
		r.fail(declared.nodes["short"], "short is not one line of 1 to %d characters", maxShort)
	}
	command.Long = r.text(declared, "long", at)
	command.Args = r.args(declared)
	command.Flags = r.flags(declared)
	argNames := []string{}
	for _, arg := range command.Args {
		argNames = append(argNames, arg.Name)
	}
	flagNames := []string{"help"}
	for _, flag := range command.Flags {
		flagNames = append(flagNames, flag.Name)
	}
	for _, names := range [][]string{argNames, flagNames} {
		for i, name := range names {
			if slices.Contains(names[:i], name) {
				r.fail(at, "names %s twice", render.Quote(name))
			}
		}
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

func (r *literalReader) text(declared *object, key string, at ast.Node) string {
	value, given := declared.values[key]
	if !given {
		r.fail(at, "has no %s", key)
		return ""
	}
	text, isString := value.(string)
	if !isString {
		r.fail(declared.nodes[key], "%s is no string", key)
	}
	return text
}

func (r *literalReader) items(declared *object, key string, allowed ...string) ([]*object, []ast.Expression) {
	value, given := declared.values[key]
	if !given {
		return nil, nil
	}
	listed, isArray := value.([]literal)
	if !isArray {
		r.fail(declared.nodes[key], "%s is no array", key)
		return nil, nil
	}
	at := declared.nodes[key].(*ast.ArrayLiteral).Value
	read := make([]*object, 0, len(listed))
	for i, item := range listed {
		held, isObject := item.(*object)
		if !isObject {
			r.fail(at[i], "%s holds an item that is no object", key)
			return nil, nil
		}
		r.onlyKeys(held, at[i], allowed...)
		read = append(read, held)
	}
	return read, at
}

func (r *literalReader) args(declared *object) []Arg {
	items, at := r.items(declared, "args", "name", "type", "usage")
	args := make([]Arg, 0, len(items))
	for i, arg := range items {
		name := r.name(arg, at[i])
		kind := r.text(arg, "type", at[i])
		if kind != "string" && kind != "path" && r.fault == nil {
			r.fail(arg.nodes["type"], "gives argument %s the type %s, which is neither string nor path",
				render.Quote(name), render.Quote(kind))
		}
		args = append(args, Arg{Name: name, Path: kind == "path", Usage: r.text(arg, "usage", at[i])})
	}
	return args
}

func (r *literalReader) name(declared *object, at ast.Node) string {
	name := r.text(declared, "name", at)
	if r.fault == nil && !wordGrammar.MatchString(name) {
		r.fail(declared.nodes["name"], "names %s, which is not %s", render.Quote(name), wordRule)
	}
	return name
}

var flagTypes = []FlagType{StringFlag, IntFlag, BoolFlag, StringsFlag, FieldsFlag}

func (r *literalReader) flags(declared *object) []Flag {
	items, at := r.items(declared, "flags", "name", "type", "usage", "choices", "default")
	flags := make([]Flag, 0, len(items))
	for i, held := range items {
		flag := Flag{Name: r.name(held, at[i]), Type: FlagType(r.text(held, "type", at[i]))}
		if !slices.Contains(flagTypes, flag.Type) && r.fault == nil {
			r.fail(held.nodes["type"], "gives --%s the type %s, which is none of %s", flag.Name,
				render.Quote(string(flag.Type)), joined(flagTypes))
		}
		if _, given := held.values["usage"]; flag.Type == FieldsFlag && given {
			r.fail(held.nodes["usage"], "gives --%s a usage, and the usage of a fields flag is the rule of +", flag.Name)
		}
		flag.Usage = fieldsUsage
		if flag.Type != FieldsFlag {
			flag.Usage = r.text(held, "usage", at[i])
		}
		flag.Choices = r.choices(held, flag)
		flag.Default = r.flagDefault(held, flag, at[i])
		flags = append(flags, flag)
	}
	return flags
}

func joined(types []FlagType) string {
	names := make([]string, 0, len(types))
	for _, kind := range types {
		names = append(names, string(kind))
	}
	return strings.Join(names, ", ")
}

func (r *literalReader) choices(held *object, flag Flag) []string {
	value, given := held.values["choices"]
	if !given {
		return nil
	}
	at := held.nodes["choices"]
	if flag.Type != StringFlag && flag.Type != StringsFlag {
		r.fail(at, "gives choices to --%s, which is neither string nor strings", flag.Name)
		return nil
	}
	choices, isStrings := stringsOf(value)
	if !isStrings || len(choices) == 0 {
		r.fail(at, "gives --%s choices that are no array of strings", flag.Name)
	}
	return choices
}

func stringsOf(value literal) ([]string, bool) {
	items, isArray := value.([]literal)
	texts := make([]string, 0, len(items))
	for _, item := range items {
		text, isString := item.(string)
		if !isString {
			return nil, false
		}
		texts = append(texts, text)
	}
	return texts, isArray
}

func (r *literalReader) flagDefault(held *object, flag Flag, at ast.Node) any {
	value, given := held.values["default"]
	if !given {
		if flag.Type == FieldsFlag {
			r.fail(at, "gives --%s no default, and + adds to the default", flag.Name)
		}
		return nil
	}
	read, fits := defaultOf(flag.Type, value)
	switch {
	case !fits:
		r.fail(held.nodes["default"], "gives --%s a default that is no %s", flag.Name, flag.Type)
	case !chosen(flag.Choices, read):
		r.fail(held.nodes["default"], "gives --%s a default outside its choices", flag.Name)
	}
	return read
}

func defaultOf(kind FlagType, value literal) (any, bool) {
	switch kind {
	case StringFlag, FieldsFlag:
		text, isString := value.(string)
		return text, isString && (kind != FieldsFlag || strings.TrimSpace(text) != "")
	case IntFlag:
		number, isNumber := value.(float64)
		return int(number), isNumber && number == math.Trunc(number) && number >= math.MinInt32 && number <= math.MaxInt32
	case BoolFlag:
		set, isBool := value.(bool)
		return set, isBool
	}
	return stringsOf(value)
}

func chosen(choices []string, value any) bool {
	if len(choices) == 0 {
		return true
	}
	texts, isList := value.([]string)
	if !isList {
		texts = []string{value.(string)}
	}
	for _, text := range texts {
		if !slices.Contains(choices, text) {
			return false
		}
	}
	return true
}
