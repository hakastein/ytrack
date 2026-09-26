package script

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/dop251/goja"

	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/render"
)

type nodeMap struct {
	engine *engine
	node   *youtrack.Node
	pairs  []youtrack.Pair
	read   map[string]goja.Value
}

func (m *nodeMap) Get(key string) goja.Value {
	if value, held := m.read[key]; held {
		return value
	}
	node, found := m.node.Lookup(key)
	if !found {
		return nil
	}
	value := m.engine.value(node)
	m.read[key] = value
	return value
}

func (m *nodeMap) Has(key string) bool {
	_, found := m.node.Lookup(key)
	return found
}

func (m *nodeMap) Set(string, goja.Value) bool {
	return false
}

func (m *nodeMap) Delete(key string) bool {
	return !m.Has(key)
}

func (m *nodeMap) Keys() []string {
	keys := make([]string, 0, len(m.pairs))
	for _, pair := range m.pairs {
		keys = append(keys, pair.Key)
	}
	return keys
}

type nodeList struct {
	engine *engine
	items  []*youtrack.Node
	read   map[int]goja.Value
}

func (l *nodeList) Len() int {
	return len(l.items)
}

func (l *nodeList) Get(index int) goja.Value {
	if index < 0 || index >= len(l.items) {
		return nil
	}
	if value, held := l.read[index]; held {
		return value
	}
	value := l.engine.value(l.items[index])
	l.read[index] = value
	return value
}

func (l *nodeList) Set(int, goja.Value) bool {
	return false
}

func (l *nodeList) SetLen(int) bool {
	return false
}

func (e *engine) value(node *youtrack.Node) goja.Value {
	switch node.Kind() {
	case youtrack.MapNode:
		object := e.vm.NewDynamicObject(&nodeMap{engine: e, node: node, pairs: node.Pairs(), read: map[string]goja.Value{}})
		e.answers[object] = node
		return object
	case youtrack.ListNode:
		list := e.vm.NewDynamicArray(&nodeList{engine: e, items: node.Items(), read: map[int]goja.Value{}})
		e.answers[list] = node
		return list
	case youtrack.StringNode, youtrack.TextNode:
		return e.vm.ToValue(node.Value())
	case youtrack.BoolNode:
		return e.vm.ToValue(node.Value() == "true")
	case youtrack.NumberNode:
		return e.vm.ToValue(numberOf(node.Value()))
	}
	return goja.Null()
}

const exactInDouble = 1 << 53

func numberOf(text string) any {
	if whole, err := strconv.ParseInt(text, 10, 64); err == nil {
		if whole > exactInDouble || whole < -exactInDouble {
			return big.NewInt(whole)
		}
		return whole
	}
	if whole, isWhole := new(big.Int).SetString(text, 10); isWhole {
		return whole
	}
	number, _ := strconv.ParseFloat(text, 64)
	return number
}

const deepest = 100

func (e *engine) node(value goja.Value, depth int) (*youtrack.Node, error) {
	if depth > deepest {
		return nil, fmt.Errorf("nests deeper than %d levels, as a value that holds itself does", deepest)
	}
	switch {
	case value == nil || goja.IsUndefined(value):
		return nil, errors.New("is undefined")
	case goja.IsNull(value):
		return youtrack.NewNull(), nil
	case goja.IsString(value):
		return stringNode(value.String()), nil
	case goja.IsBigInt(value):
		return youtrack.NewNumber(json.Number(value.String())), nil
	case goja.IsNumber(value):
		return numberNode(value.ToFloat())
	}
	if _, isSymbol := value.(*goja.Symbol); isSymbol {
		return nil, errors.New("is a symbol")
	}
	object, isObject := value.(*goja.Object)
	if !isObject {
		if flag, isBool := value.Export().(bool); isBool {
			return youtrack.NewBool(flag), nil
		}
		return nil, fmt.Errorf("is a %s", value.ExportType())
	}
	if node, isAnswer := e.answers[object]; isAnswer {
		return node, nil
	}
	switch class := object.ClassName(); class {
	case "Array":
		return e.listNode(object, depth)
	case "Object":
		if e.isPlain(object) {
			return e.mapNode(object, depth)
		}
		return nil, errors.New("is an object of a class of its own")
	case "Function":
		return nil, errors.New("is a function")
	case "Date":
		return nil, errors.New("is a Date: a moment is written as a string, such as 2026-01-01T00:00:00Z")
	case "ArrayBuffer", "Uint8Array", "Int8Array", "Uint8ClampedArray", "Uint16Array", "Int16Array", "Uint32Array",
		"Int32Array", "Float32Array", "Float64Array", "BigInt64Array", "BigUint64Array", "DataView":
		return nil, errors.New("is bytes, which a YAML document does not hold")
	default:
		return nil, fmt.Errorf("is a %s", class)
	}
}

func (e *engine) isPlain(object *goja.Object) bool {
	prototype := object.Prototype()
	return prototype == nil || prototype.SameAs(e.objectPrototype)
}

func (e *engine) mapNode(object *goja.Object, depth int) (*youtrack.Node, error) {
	var pairs []youtrack.Pair
	for _, key := range object.Keys() {
		value := object.Get(key)
		if goja.IsUndefined(value) {
			continue
		}
		node, err := e.node(value, depth+1)
		if err != nil {
			return nil, fmt.Errorf("under %s %w", render.Quote(key), err)
		}
		pairs = append(pairs, pairOf(key, node))
	}
	return youtrack.NewMap(pairs...), nil
}

func (e *engine) listNode(object *goja.Object, depth int) (*youtrack.Node, error) {
	var items []*youtrack.Node
	for index := range object.Get("length").ToInteger() {
		node, err := e.node(object.Get(strconv.FormatInt(index, 10)), depth+1)
		if err != nil {
			return nil, fmt.Errorf("at [%d] %w", index, err)
		}
		items = append(items, node)
	}
	return youtrack.NewList(items...), nil
}

func stringNode(text string) *youtrack.Node {
	if strings.Contains(text, "\n") {
		return youtrack.NewText(text)
	}
	return youtrack.NewString(text)
}

// Above this a double prints with an exponent in JavaScript.
const plainDigits = 1e21

func numberNode(number float64) (*youtrack.Node, error) {
	switch {
	case math.IsNaN(number):
		return nil, errors.New("is NaN")
	case math.IsInf(number, 0):
		return nil, errors.New("is Infinity")
	case number == math.Trunc(number) && math.Abs(number) < plainDigits:
		return youtrack.NewNumber(json.Number(strconv.FormatFloat(number, 'f', -1, 64))), nil
	}
	return youtrack.NewNumber(json.Number(strconv.FormatFloat(number, 'g', -1, 64))), nil
}

// A key outside the grammar of the module's own names prints quoted, as a name from the data does.
func pairOf(key string, value *youtrack.Node) youtrack.Pair {
	if youtrack.CheckKey(key) == nil {
		return youtrack.Pair{Key: key, Value: value}
	}
	return youtrack.DataPair(key, value)
}
