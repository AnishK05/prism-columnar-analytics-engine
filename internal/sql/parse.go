package sql

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
)

type parser struct {
	lx  lexer
	tok token
}

func (p *parser) advance() {
	if p.lx.err != nil {
		return
	}
	p.tok = p.lx.lex()
}

func (p *parser) errAt(tok token, msg string) error {
	if p.lx.err != nil {
		return p.lx.err
	}
	raw := tok.raw
	if raw == "" {
		raw = "end of input"
	}
	return fmt.Errorf("parse error at column %d: %s, got %q", tok.pos+1, msg, raw)
}

func (p *parser) expect(k tokKind, what string) error {
	if p.lx.err != nil {
		return p.lx.err
	}
	if p.tok.kind == tokUnsupported {
		return fmt.Errorf("%s not supported in v1", strings.ToUpper(p.tok.raw))
	}
	if p.tok.kind != k {
		return p.errAt(p.tok, "expected "+what)
	}
	p.advance()
	return nil
}

func (p *parser) rejectUnsupported() error {
	if p.tok.kind == tokUnsupported {
		return fmt.Errorf("%s not supported in v1", strings.ToUpper(p.tok.raw))
	}
	return nil
}

// Parse parses one Prism SQL SELECT statement.
func Parse(src string) (*Query, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, fmt.Errorf("parse error at column 1: expected SELECT, got %q", "end of input")
	}
	p := parser{lx: lexer{s: src}}
	p.advance()
	if p.lx.err != nil {
		return nil, p.lx.err
	}
	q, err := p.parseQuery()
	if err != nil {
		return nil, err
	}
	if p.lx.err != nil {
		return nil, p.lx.err
	}
	if p.tok.kind == tokSemi {
		p.advance()
	}
	if p.tok.kind != tokEOF {
		if p.tok.kind == tokUnsupported {
			return nil, fmt.Errorf("%s not supported in v1", strings.ToUpper(p.tok.raw))
		}
		return nil, p.errAt(p.tok, "expected end of statement")
	}
	return q, nil
}

func (p *parser) parseQuery() (*Query, error) {
	if err := p.rejectUnsupported(); err != nil {
		return nil, err
	}
	if p.tok.kind != tokSelect {
		return nil, p.errAt(p.tok, "expected SELECT")
	}
	p.advance()
	q := &Query{}
	if p.tok.kind == tokAll {
		q.All = true
		p.advance()
	}
	if p.tok.kind == tokUnsupported {
		return nil, fmt.Errorf("%s not supported in v1", strings.ToUpper(p.tok.raw))
	}
	items, err := p.parseSelectList()
	if err != nil {
		return nil, err
	}
	q.Items = items
	if err := p.expect(tokFrom, "FROM"); err != nil {
		return nil, err
	}
	if p.tok.kind != tokIdent {
		return nil, p.errAt(p.tok, "expected table name")
	}
	q.From = p.tok.raw
	p.advance()
	if p.tok.kind == tokWhere {
		p.advance()
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		q.Where = e
	}
	if p.tok.kind == tokGroup {
		p.advance()
		if err := p.expect(tokBy, "BY"); err != nil {
			return nil, err
		}
		gb, err := p.parseExprList()
		if err != nil {
			return nil, err
		}
		q.GroupBy = gb
	}
	if p.tok.kind == tokOrder {
		p.advance()
		if err := p.expect(tokBy, "BY"); err != nil {
			return nil, err
		}
		ob, err := p.parseOrderList()
		if err != nil {
			return nil, err
		}
		q.OrderBy = ob
	}
	if p.tok.kind == tokLimit {
		p.advance()
		if p.tok.kind != tokNumber {
			return nil, p.errAt(p.tok, "expected integer LIMIT")
		}
		n, err := strconv.ParseInt(p.tok.raw, 10, 64)
		if err != nil || strings.ContainsAny(p.tok.raw, ".eE") {
			return nil, fmt.Errorf("parse error at column %d: LIMIT must be an integer", p.tok.pos+1)
		}
		q.Limit = &n
		p.advance()
	}
	if err := p.rejectUnsupported(); err != nil {
		return nil, err
	}
	return q, nil
}

func (p *parser) parseSelectList() ([]SelectItem, error) {
	var items []SelectItem
	for {
		it, err := p.parseSelectItem()
		if err != nil {
			return nil, err
		}
		items = append(items, it)
		if p.tok.kind == tokComma {
			p.advance()
			continue
		}
		break
	}
	if len(items) == 0 {
		return nil, p.errAt(p.tok, "expected select item")
	}
	return items, nil
}

func (p *parser) parseSelectItem() (SelectItem, error) {
	if err := p.rejectUnsupported(); err != nil {
		return SelectItem{}, err
	}
	if p.tok.kind == tokStar {
		p.advance()
		return SelectItem{Star: true}, nil
	}
	e, err := p.parseOr()
	if err != nil {
		return SelectItem{}, err
	}
	it := SelectItem{Expr: e}
	if p.tok.kind == tokAs {
		p.advance()
		if p.tok.kind != tokIdent {
			return SelectItem{}, p.errAt(p.tok, "expected alias")
		}
		it.Alias = p.tok.raw
		p.advance()
	} else if p.tok.kind == tokIdent {
		// optional alias without AS
		it.Alias = p.tok.raw
		p.advance()
	}
	return it, nil
}

func (p *parser) parseExprList() ([]Expr, error) {
	var out []Expr
	for {
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		out = append(out, e)
		if p.tok.kind == tokComma {
			p.advance()
			continue
		}
		break
	}
	return out, nil
}

func (p *parser) parseOrderList() ([]OrderItem, error) {
	var out []OrderItem
	for {
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		it := OrderItem{Expr: e}
		switch p.tok.kind {
		case tokAsc:
			p.advance()
		case tokDesc:
			it.Desc = true
			p.advance()
		}
		out = append(out, it)
		if p.tok.kind == tokComma {
			p.advance()
			continue
		}
		break
	}
	return out, nil
}

func (p *parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.tok.kind == tokOr {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: "OR", Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Expr, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.tok.kind == tokAnd {
		p.advance()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: "AND", Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseNot() (Expr, error) {
	if p.tok.kind == tokNot {
		p.advance()
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &Unary{Op: "NOT", X: x}, nil
	}
	return p.parseCmp()
}

func (p *parser) parseCmp() (Expr, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	switch p.tok.kind {
	case tokIs:
		p.advance()
		not := false
		if p.tok.kind == tokNot {
			not = true
			p.advance()
		}
		if err := p.expect(tokNull, "NULL"); err != nil {
			return nil, err
		}
		return &IsNull{X: left, Not: not}, nil
	case tokNot:
		p.advance()
		if p.tok.kind == tokIn {
			p.advance()
			vals, err := p.parseInList()
			if err != nil {
				return nil, err
			}
			return &InList{X: left, Vals: vals, Not: true}, nil
		}
		if p.tok.kind == tokBetween {
			p.advance()
			lo, hi, err := p.parseBetween()
			if err != nil {
				return nil, err
			}
			return &Between{X: left, Low: lo, High: hi, Not: true}, nil
		}
		return nil, p.errAt(p.tok, "expected IN or BETWEEN after NOT")
	case tokIn:
		p.advance()
		vals, err := p.parseInList()
		if err != nil {
			return nil, err
		}
		return &InList{X: left, Vals: vals}, nil
	case tokBetween:
		p.advance()
		lo, hi, err := p.parseBetween()
		if err != nil {
			return nil, err
		}
		return &Between{X: left, Low: lo, High: hi}, nil
	case tokEq, tokNe, tokLt, tokLe, tokGt, tokGe:
		op := p.tok.raw
		if p.tok.kind == tokNe {
			op = "<>"
		}
		p.advance()
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		return &Binary{Op: op, Left: left, Right: right}, nil
	default:
		return left, nil
	}
}

func (p *parser) parseAdd() (Expr, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.tok.kind == tokPlus || p.tok.kind == tokMinus {
		op := p.tok.raw
		p.advance()
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseMul() (Expr, error) {
	left, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	for p.tok.kind == tokStar || p.tok.kind == tokSlash {
		op := p.tok.raw
		p.advance()
		right, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseAtom() (Expr, error) {
	if err := p.rejectUnsupported(); err != nil {
		return nil, err
	}
	if p.lx.err != nil {
		return nil, p.lx.err
	}
	switch p.tok.kind {
	case tokMinus:
		p.advance()
		x, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		return &Unary{Op: "-", X: x}, nil
	case tokLParen:
		p.advance()
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokRParen, ")"); err != nil {
			return nil, err
		}
		return e, nil
	case tokStar:
		p.advance()
		return &Star{}, nil
	case tokIdent:
		id := &Ident{Name: p.tok.raw}
		p.advance()
		return id, nil
	case tokCount, tokSum, tokAvg, tokMin, tokMax:
		return p.parseCall()
	case tokTimestamp:
		return p.parseTimestamp()
	case tokNumber, tokString, tokTrue, tokFalse, tokNull:
		return p.parseLiteral()
	default:
		return nil, p.errAt(p.tok, "expected expression")
	}
}

func (p *parser) parseCall() (Expr, error) {
	name := strings.ToUpper(p.tok.raw)
	p.advance()
	if err := p.expect(tokLParen, "("); err != nil {
		return nil, err
	}
	c := &Call{Name: name}
	if p.tok.kind == tokStar {
		if name != "COUNT" {
			return nil, fmt.Errorf("%s(*) is not supported", name)
		}
		c.Star = true
		p.advance()
		if err := p.expect(tokRParen, ")"); err != nil {
			return nil, err
		}
		return c, nil
	}
	arg, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	c.Args = []Expr{arg}
	if p.tok.kind == tokComma {
		return nil, fmt.Errorf("%s does not accept multiple arguments in v1", name)
	}
	if err := p.expect(tokRParen, ")"); err != nil {
		return nil, err
	}
	return c, nil
}

func (p *parser) parseTimestamp() (Expr, error) {
	pos := p.tok.pos
	p.advance()
	if p.tok.kind != tokString {
		return nil, p.errAt(p.tok, "expected timestamp string")
	}
	raw := p.tok.raw
	ms, err := parseTimestampLiteral(raw)
	if err != nil {
		return nil, fmt.Errorf("parse error at column %d: %w", pos+1, err)
	}
	p.advance()
	return &Literal{Kind: expr.LitInt, I64: ms, Str: raw, Timestamp: true}, nil
}

func parseTimestampLiteral(s string) (int64, error) {
	layouts := []string{
		"2006-01-02",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.000",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.UnixMilli(), nil
		}
	}
	return 0, fmt.Errorf("invalid TIMESTAMP %q", s)
}

func (p *parser) parseLiteral() (Expr, error) {
	tok := p.tok
	p.advance()
	switch tok.kind {
	case tokNull:
		return &Literal{Kind: expr.LitNull}, nil
	case tokTrue:
		return &Literal{Kind: expr.LitBool, Bool: true}, nil
	case tokFalse:
		return &Literal{Kind: expr.LitBool, Bool: false}, nil
	case tokString:
		return &Literal{Kind: expr.LitString, Str: tok.raw}, nil
	case tokNumber:
		if strings.ContainsAny(tok.raw, ".") {
			f, err := strconv.ParseFloat(tok.raw, 64)
			if err != nil {
				return nil, fmt.Errorf("parse error at column %d: invalid float", tok.pos+1)
			}
			return &Literal{Kind: expr.LitFloat, F64: f}, nil
		}
		n, err := strconv.ParseInt(tok.raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse error at column %d: invalid integer", tok.pos+1)
		}
		return &Literal{Kind: expr.LitInt, I64: n}, nil
	default:
		return nil, p.errAt(tok, "expected literal")
	}
}

func (p *parser) parseInList() ([]Expr, error) {
	if err := p.expect(tokLParen, "("); err != nil {
		return nil, err
	}
	var vals []Expr
	for {
		if p.tok.kind == tokRParen && len(vals) == 0 {
			return nil, p.errAt(p.tok, "empty IN list")
		}
		e, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		vals = append(vals, e)
		if p.tok.kind == tokComma {
			p.advance()
			continue
		}
		break
	}
	if err := p.expect(tokRParen, ")"); err != nil {
		return nil, err
	}
	return vals, nil
}

func (p *parser) parseBetween() (Expr, Expr, error) {
	lo, err := p.parseAtom()
	if err != nil {
		return nil, nil, err
	}
	if err := p.expect(tokAnd, "AND"); err != nil {
		return nil, nil, err
	}
	hi, err := p.parseAtom()
	if err != nil {
		return nil, nil, err
	}
	return lo, hi, nil
}
