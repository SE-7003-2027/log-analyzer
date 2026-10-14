// Package filter implements the filter expression language described in
// docs/SPEC.md: a lexer, a recursive-descent parser with type checking, and
// an evaluator over log entries.
package filter

import (
	"regexp"
	"strconv"
	"time"

	"log-analyzer/internal/logparse"
)

// Filter is a compiled filter expression.
type Filter struct {
	root node
}

// Compile parses and type-checks expr. Errors are of type *Error.
func Compile(expr string) (*Filter, error) {
	root, err := compile(expr)
	if err != nil {
		err.Expr = expr
		return nil, err
	}
	return &Filter{root: root}, nil
}

func compile(expr string) (node, error) {
	toks, err := lex(expr)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	return p.parse()
}

// Match reports whether e satisfies the filter.
func (f *Filter) Match(e logparse.Entry) bool {
	return f.root.eval(&e)
}

type fieldType int

const (
	typeString fieldType = iota
	typeInt
	typeTime
)

func (t fieldType) String() string {
	return [...]string{"string", "int", "time"}[t]
}

type field struct {
	typ fieldType
	str func(*logparse.Entry) string // set for typeString
	num func(*logparse.Entry) int    // set for typeInt
}

var fields = map[string]field{
	"ip":       {typ: typeString, str: func(e *logparse.Entry) string { return e.IP }},
	"user":     {typ: typeString, str: func(e *logparse.Entry) string { return e.User }},
	"time":     {typ: typeTime},
	"method":   {typ: typeString, str: func(e *logparse.Entry) string { return e.Method }},
	"path":     {typ: typeString, str: func(e *logparse.Entry) string { return e.Path }},
	"protocol": {typ: typeString, str: func(e *logparse.Entry) string { return e.Protocol }},
	"status":   {typ: typeInt, num: func(e *logparse.Entry) int { return e.Status }},
	"bytes":    {typ: typeInt, num: func(e *logparse.Entry) int { return e.Bytes }},
	"referer":  {typ: typeString, str: func(e *logparse.Entry) string { return e.Referer }},
	"agent":    {typ: typeString, str: func(e *logparse.Entry) string { return e.Agent }},
}

// compare type-checks a single comparison and builds its evaluation node.
func compare(fieldTok, op, value token) (node, error) {
	f, ok := fields[fieldTok.Text]
	if !ok {
		return nil, errorf(fieldTok.Col, "unknown field %q", fieldTok.Text)
	}

	if value.Kind == tokClass {
		if fieldTok.Text != "status" {
			return nil, errorf(value.Col, "status class can only be compared with status")
		}
		if op.Kind != tokEq && op.Kind != tokNe {
			return nil, errorf(op.Col, "operator %s is not supported for status classes", op.Kind)
		}
		class := int(value.Text[0] - '0')
		return classNode{class: class, negate: op.Kind == tokNe}, nil
	}

	isRegex := op.Kind == tokMatch || op.Kind == tokNotMatch
	isOrdering := op.Kind == tokLt || op.Kind == tokLe || op.Kind == tokGt || op.Kind == tokGe

	switch f.typ {
	case typeString:
		if isOrdering {
			return nil, errorf(op.Col, "operator %s is not supported for %s field %q", op.Kind, f.typ, fieldTok.Text)
		}
		if value.Kind != tokString {
			return nil, mismatch(fieldTok, f, value)
		}
		if isRegex {
			re, err := regexp.Compile(value.Text)
			if err != nil {
				return nil, errorf(value.Col, "invalid regex: %v", err)
			}
			return regexNode{get: f.str, re: re, negate: op.Kind == tokNotMatch}, nil
		}
		return stringNode{get: f.str, op: op.Kind, val: value.Text}, nil

	case typeInt:
		if isRegex {
			return nil, errorf(op.Col, "operator %s is not supported for %s field %q", op.Kind, f.typ, fieldTok.Text)
		}
		if value.Kind != tokInt {
			return nil, mismatch(fieldTok, f, value)
		}
		n, err := strconv.Atoi(value.Text)
		if err != nil {
			return nil, errorf(value.Col, "invalid integer %q", value.Text)
		}
		return intNode{get: f.num, op: op.Kind, val: n}, nil

	default: // typeTime
		if isRegex {
			return nil, errorf(op.Col, "operator %s is not supported for %s field %q", op.Kind, f.typ, fieldTok.Text)
		}
		if value.Kind != tokString {
			return nil, mismatch(fieldTok, f, value)
		}
		t, err := time.Parse(time.RFC3339, value.Text)
		if err != nil {
			return nil, errorf(value.Col, "invalid time %q, want RFC 3339 such as \"2026-10-01T18:00:00-06:00\"", value.Text)
		}
		return timeNode{op: op.Kind, val: t}, nil
	}
}

func mismatch(fieldTok token, f field, value token) error {
	return errorf(value.Col, "cannot compare %s field %q with %s", f.typ, fieldTok.Text, value.Kind)
}

// node is an evaluable expression tree node.
type node interface {
	eval(e *logparse.Entry) bool
}

type orNode struct{ left, right node }

func (n orNode) eval(e *logparse.Entry) bool { return n.left.eval(e) || n.right.eval(e) }

type andNode struct{ left, right node }

func (n andNode) eval(e *logparse.Entry) bool { return n.left.eval(e) && n.right.eval(e) }

type notNode struct{ x node }

func (n notNode) eval(e *logparse.Entry) bool { return !n.x.eval(e) }

type stringNode struct {
	get func(*logparse.Entry) string
	op  tokenKind // tokEq or tokNe
	val string
}

func (n stringNode) eval(e *logparse.Entry) bool {
	return (n.get(e) == n.val) == (n.op == tokEq)
}

type regexNode struct {
	get    func(*logparse.Entry) string
	re     *regexp.Regexp
	negate bool
}

func (n regexNode) eval(e *logparse.Entry) bool {
	return n.re.MatchString(n.get(e)) != n.negate
}

type intNode struct {
	get func(*logparse.Entry) int
	op  tokenKind
	val int
}

func (n intNode) eval(e *logparse.Entry) bool {
	v := n.get(e)
	switch n.op {
	case tokEq:
		return v == n.val
	case tokNe:
		return v != n.val
	case tokLt:
		return v < n.val
	case tokLe:
		return v <= n.val
	case tokGt:
		return v > n.val
	default: // tokGe
		return v >= n.val
	}
}

type timeNode struct {
	op  tokenKind
	val time.Time
}

func (n timeNode) eval(e *logparse.Entry) bool {
	v := e.Time
	switch n.op {
	case tokEq:
		return v.Equal(n.val)
	case tokNe:
		return !v.Equal(n.val)
	case tokLt:
		return v.Before(n.val)
	case tokLe:
		return !v.After(n.val)
	case tokGt:
		return v.After(n.val)
	default: // tokGe
		return !v.Before(n.val)
	}
}

type classNode struct {
	class  int // 1-5
	negate bool
}

func (n classNode) eval(e *logparse.Entry) bool {
	return (e.Status/100 == n.class) != n.negate
}
