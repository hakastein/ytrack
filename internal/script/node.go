package script

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/hakastein/cliche"
	"github.com/hakastein/go-youtrack"

	"github.com/hakastein/ytrack/internal/render"
)

// nodeMap and nodeList are a node of an answer as a script reads it; given back, they are the same node.
type nodeMap struct {
	node *youtrack.Node
}

func (m nodeMap) Keys() []string {
	pairs := m.node.Pairs()
	keys := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		keys = append(keys, pair.Key)
	}
	return keys
}

func (m nodeMap) Get(key string) (any, bool) {
	child, found := m.node.Lookup(key)
	if !found {
		return nil, false
	}
	return valueOf(child), true
}

type nodeList struct {
	node  *youtrack.Node
	items []*youtrack.Node
}

func (l nodeList) Len() int {
	return len(l.items)
}

func (l nodeList) Index(i int) any {
	return valueOf(l.items[i])
}

func valueOf(node *youtrack.Node) any {
	switch node.Kind() {
	case youtrack.MapNode:
		return nodeMap{node: node}
	case youtrack.ListNode:
		return nodeList{node: node, items: node.Items()}
	case youtrack.StringNode, youtrack.TextNode:
		return node.Value()
	case youtrack.BoolNode:
		return node.Value() == "true"
	case youtrack.NumberNode:
		return numberOf(node.Value())
	}
	return nil
}

func numberOf(text string) any {
	if whole, err := strconv.ParseInt(text, 10, 64); err == nil {
		return whole
	}
	if whole, isWhole := new(big.Int).SetString(text, 10); isWhole {
		return whole
	}
	number, _ := strconv.ParseFloat(text, 64)
	return number
}

// toNode turns a value a script built into a node; a node of an answer prints as itself wherever the script puts it.
func toNode(value any) (*youtrack.Node, error) {
	switch v := value.(type) {
	case nodeMap:
		return v.node, nil
	case nodeList:
		return v.node, nil
	case nil:
		return youtrack.NewNull(), nil
	case string:
		if strings.Contains(v, "\n") {
			return youtrack.NewText(v), nil
		}
		return youtrack.NewString(v), nil
	case bool:
		return youtrack.NewBool(v), nil
	case int64:
		return youtrack.NewNumber(json.Number(strconv.FormatInt(v, 10))), nil
	case float64:
		return numberNode(v)
	case *big.Int:
		return youtrack.NewNumber(json.Number(v.String())), nil
	case []byte:
		return nil, errors.New("is bytes, which a YAML document does not hold")
	case []any:
		items := make([]*youtrack.Node, 0, len(v))
		for i, item := range v {
			node, err := toNode(item)
			if err != nil {
				return nil, fmt.Errorf("at [%d] %w", i, err)
			}
			items = append(items, node)
		}
		return youtrack.NewList(items...), nil
	case *cliche.Object:
		var pairs []youtrack.Pair
		for _, key := range v.Keys() {
			held, _ := v.Get(key)
			node, err := toNode(held)
			if err != nil {
				return nil, fmt.Errorf("under %s %w", render.Quote(key), err)
			}
			pairs = append(pairs, pairOf(key, node))
		}
		return youtrack.NewMap(pairs...), nil
	case cliche.Func:
		return nil, errors.New("is a function")
	}
	return nil, fmt.Errorf("is a %T", value)
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
