package filter

// parser is a recursive-descent parser for the grammar in docs/SPEC.md:
//
//	expr       = or ;
//	or         = and { "||" and } ;
//	and        = unary { "&&" unary } ;
//	unary      = "!" unary | primary ;
//	primary    = "(" expr ")" | comparison ;
//	comparison = field op value ;
//
// Comparisons are type-checked as they are parsed (see compare in filter.go),
// so a successful parse yields a tree that is ready to evaluate.
type parser struct {
	toks []token
	pos  int
}

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) next() token {
	t := p.toks[p.pos]
	if t.Kind != tokEOF {
		p.pos++
	}
	return t
}

// parse parses a whole expression and requires that all input is consumed.
func (p *parser) parse() (node, error) {
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.Kind != tokEOF {
		return nil, errorf(t.Col, "unexpected %s", t.Kind)
	}
	return n, nil
}

func (p *parser) parseOr() (node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().Kind == tokOr {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = orNode{left, right}
	}
	return left, nil
}

func (p *parser) parseAnd() (node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().Kind == tokAnd {
		p.next()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = andNode{left, right}
	}
	return left, nil
}

func (p *parser) parseUnary() (node, error) {
	if p.peek().Kind == tokNot {
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return notNode{x}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (node, error) {
	if p.peek().Kind == tokLParen {
		p.next()
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if t := p.next(); t.Kind != tokRParen {
			return nil, errorf(t.Col, `expected ")", got %s`, t.Kind)
		}
		return n, nil
	}
	return p.parseComparison()
}

func (p *parser) parseComparison() (node, error) {
	field := p.next()
	if field.Kind != tokIdent {
		return nil, errorf(field.Col, "expected field, got %s", field.Kind)
	}

	op := p.next()
	if !isComparisonOp(op.Kind) {
		return nil, errorf(op.Col, "expected comparison operator, got %s", op.Kind)
	}

	value := p.next()
	switch value.Kind {
	case tokInt, tokString, tokClass:
	default:
		return nil, errorf(value.Col, "expected value, got %s", value.Kind)
	}

	return compare(field, op, value)
}

func isComparisonOp(k tokenKind) bool {
	switch k {
	case tokEq, tokNe, tokLt, tokLe, tokGt, tokGe, tokMatch, tokNotMatch:
		return true
	}
	return false
}
