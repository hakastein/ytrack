package render

import (
	"encoding/json"
	"slices"
	"strconv"
)

type Kind int

const (
	unsetKind Kind = iota
	Null
	Scalar
	Text
	List
	Map
)

type Node struct {
	kind  Kind
	text  string
	bare  bool
	pairs []Pair
	items []*Node
}

type Pair struct {
	Key      string
	Value    *Node
	FromData bool
}

func FromData(key string, value *Node) Pair {
	return Pair{Key: key, Value: value, FromData: true}
}

func NewNull() *Node {
	return &Node{kind: Null}
}

func NewString(s string) *Node {
	return &Node{kind: Scalar, text: s}
}

func NewText(s string) *Node {
	return &Node{kind: Text, text: s}
}

func NewBool(b bool) *Node {
	return &Node{kind: Scalar, text: strconv.FormatBool(b), bare: true}
}

func NewNumber(n json.Number) *Node {
	return &Node{kind: Scalar, text: string(n), bare: true}
}

func NewMap(pairs ...Pair) *Node {
	return &Node{kind: Map, pairs: slices.Clone(pairs)}
}

func NewList(items ...*Node) *Node {
	return &Node{kind: List, items: slices.Clone(items)}
}

func (n *Node) Kind() Kind {
	return n.kind
}
