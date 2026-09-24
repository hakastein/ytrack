package render

import (
	"encoding/json"
	"slices"
	"strconv"
)

// Kind is what a renderer may not break: a Scalar prints its text quoted, or bare when the text
// is a literal of the format such as a number or a boolean; Prose prints as a literal block.
type Kind int

const (
	// The zero Kind is no kind, so a Node that no constructor built is an error to render
	// rather than a null.
	noKind Kind = iota
	Null
	Scalar
	Prose
	List
	Map
)

// Only this package sets a Node's content, so it cannot disagree with its kind.
type Node struct {
	kind Kind
	text string
	// The text of a bare Scalar is a literal of the format, such as true, and is not quoted.
	bare  bool
	pairs []Pair
	items []*Node
}

type Pair struct {
	Key   string
	Value *Node
	// A key of ytrack's own is held to the grammar of the names it prints; one that came from the data, such
	// as the name of a custom field, is held to no grammar and is quoted whatever it reads as (ADR-0003).
	FromData bool
}

// FromData is a pair whose key is a name the server sent rather than one of ytrack's own.
func FromData(key string, value *Node) Pair {
	return Pair{Key: key, Value: value, FromData: true}
}

func NewNull() *Node {
	return &Node{kind: Null}
}

func NewString(s string) *Node {
	return &Node{kind: Scalar, text: s}
}

// NewProse is text meant to be read as the lines it is, such as the description of an issue.
func NewProse(s string) *Node {
	return &Node{kind: Prose, text: s}
}

func NewBool(b bool) *Node {
	return &Node{kind: Scalar, text: strconv.FormatBool(b), bare: true}
}

// n is printed bare and unchecked, so it has to be a valid JSON number.
func NewNumber(n json.Number) *Node {
	return &Node{kind: Scalar, text: string(n), bare: true}
}

// NewMap keeps the pairs in the order given, which is the order they print in.
func NewMap(pairs ...Pair) *Node {
	return &Node{kind: Map, pairs: slices.Clone(pairs)}
}

func NewList(items ...*Node) *Node {
	return &Node{kind: List, items: slices.Clone(items)}
}

func (n *Node) Kind() Kind {
	return n.kind
}
