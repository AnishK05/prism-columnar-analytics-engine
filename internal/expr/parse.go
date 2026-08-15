package expr

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokKind uint8

const (
	tokEOF tokKind = iota
	tokIdent
	tokNumber
	tokString
	tokTrue
	tokFalse
	tokNull
	tokAnd
	tokOr
	tokNot
	tokIs
	tokIn
	tokBetween
	tokLParen
	tokRParen
	tokComma
	tokEq
	tokNe
	tokLt
	tokLe
	tokGt
	tokGe
)

type token struct {
	kind tokKind
	pos  int
	raw  string
}

type lexer struct {
	s   string
	i   int
	err error
}

func (l *lexer) peek() rune {
	if l.i >= len(l.s) {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(l.s[l.i:])
	return r
}

func (l *lexer) next() rune {
	if l.i >= len(l.s) {
		return 0
	}
	r, w := utf8.DecodeRuneInString(l.s[l.i:])
	l.i += w
	return r
}

func (l *lexer) skipSpace() {
	for l.i < len(l.s) {
		r, w := utf8.DecodeRuneInString(l.s[l.i:])
		if !unicode.IsSpace(r) {
			return
		}
		l.i += w
	}
}

func (l *lexer) lex() token {
	l.skipSpace()
	if l.i >= len(l.s) {
		return token{kind: tokEOF, pos: l.i}
	}
	pos := l.i
	r := l.peek()
	switch r {
	case '(':
		l.next()
		return token{kind: tokLParen, pos: pos, raw: "("}
	case ')':
		l.next()
		return token{kind: tokRParen, pos: pos, raw: ")"}
	case ',':
		l.next()
		return token{kind: tokComma, pos: pos, raw: ","}
	case '=':
		l.next()
		return token{kind: tokEq, pos: pos, raw: "="}
	case '<':
		l.next()
		if l.peek() == '=' {
			l.next()
			return token{kind: tokLe, pos: pos, raw: "<="}
		}
		if l.peek() == '>' {
			l.next()
			return token{kind: tokNe, pos: pos, raw: "<>"}
		}
		return token{kind: tokLt, pos: pos, raw: "<"}
	case '>':
		l.next()
		if l.peek() == '=' {
			l.next()
			return token{kind: tokGe, pos: pos, raw: ">="}
		}
		return token{kind: tokGt, pos: pos, raw: ">"}
	case '!':
		l.next()
		if l.peek() == '=' {
			l.next()
			return token{kind: tokNe, pos: pos, raw: "!="}
		}
		l.err = fmt.Errorf("parse error at column %d: unexpected '!'", pos+1)
		return token{kind: tokEOF, pos: pos}
	case '\'':
		return l.lexString(pos)
	}
	if r == '-' || (r >= '0' && r <= '9') {
		return l.lexNumber(pos)
	}
	if isIdentStart(r) {
		return l.lexIdent(pos)
	}
	l.err = fmt.Errorf("parse error at column %d: unexpected %q", pos+1, string(r))
	return token{kind: tokEOF, pos: pos}
}

func (l *lexer) lexString(pos int) token {
	l.next() // opening '
	var b strings.Builder
	for {
		r := l.next()
		if r == 0 {
			l.err = fmt.Errorf("parse error at column %d: unterminated string", pos+1)
			return token{kind: tokEOF, pos: pos}
		}
		if r == '\'' {
			if l.peek() == '\'' {
				l.next()
				b.WriteByte('\'')
				continue
			}
			return token{kind: tokString, pos: pos, raw: b.String()}
		}
		b.WriteRune(r)
	}
}

func (l *lexer) lexNumber(pos int) token {
	start := l.i
	if l.peek() == '-' {
		l.next()
	}
	for r := l.peek(); r >= '0' && r <= '9'; r = l.peek() {
		l.next()
	}
	if l.peek() == '.' {
		l.next()
		for r := l.peek(); r >= '0' && r <= '9'; r = l.peek() {
			l.next()
		}
	}
	return token{kind: tokNumber, pos: pos, raw: l.s[start:l.i]}
}

func (l *lexer) lexIdent(pos int) token {
	start := l.i
	for r := l.peek(); isIdentPart(r); r = l.peek() {
		l.next()
	}
	raw := l.s[start:l.i]
	up := strings.ToUpper(raw)
	kind := tokIdent
	switch up {
	case "AND":
		kind = tokAnd
	case "OR":
		kind = tokOr
	case "NOT":
		kind = tokNot
	case "IS":
		kind = tokIs
	case "IN":
		kind = tokIn
	case "BETWEEN":
		kind = tokBetween
	case "TRUE":
		kind = tokTrue
	case "FALSE":
		kind = tokFalse
	case "NULL":
		kind = tokNull
	}
	return token{kind: kind, pos: pos, raw: raw}
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

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

func (p *parser) expect(k tokKind, what string) error {
	if p.lx.err != nil {
		return p.lx.err
	}
	if p.tok.kind != k {
		return fmt.Errorf("parse error at column %d: expected %s, got %q", p.tok.pos+1, what, p.tok.raw)
	}
	p.advance()
	return nil
}

// ParseWhere parses a Phase-3 predicate (not full SQL).
func ParseWhere(src string) (Expr, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil, fmt.Errorf("empty predicate")
	}
	p := parser{lx: lexer{s: src}}
	p.advance()
	if p.lx.err != nil {
		return nil, p.lx.err
	}
	e, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.lx.err != nil {
		return nil, p.lx.err
	}
	if p.tok.kind != tokEOF {
		return nil, fmt.Errorf("parse error at column %d: unexpected %q", p.tok.pos+1, p.tok.raw)
	}
	return e, nil
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
		left = &Binary{Op: OpOr, Left: left, Right: right}
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
		left = &Binary{Op: OpAnd, Left: left, Right: right}
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
		return &Unary{Op: OpNot, X: x}, nil
	}
	return p.parsePred()
}

func (p *parser) parsePred() (Expr, error) {
	if p.tok.kind == tokLParen {
		p.advance()
		e, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokRParen, ")"); err != nil {
			return nil, err
		}
		return e, nil
	}
	left, err := p.parseAtom()
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
			vals, err := p.parseList()
			if err != nil {
				return nil, err
			}
			return &InList{X: left, Vals: vals, Not: true}, nil
		}
		if p.tok.kind == tokBetween {
			p.advance()
			low, high, err := p.parseBetweenBounds()
			if err != nil {
				return nil, err
			}
			return &Between{X: left, Low: low, High: high, Not: true}, nil
		}
		return nil, fmt.Errorf("parse error at column %d: expected IN or BETWEEN after NOT", p.tok.pos+1)
	case tokIn:
		p.advance()
		vals, err := p.parseList()
		if err != nil {
			return nil, err
		}
		return &InList{X: left, Vals: vals}, nil
	case tokBetween:
		p.advance()
		low, high, err := p.parseBetweenBounds()
		if err != nil {
			return nil, err
		}
		return &Between{X: left, Low: low, High: high}, nil
	case tokEq, tokNe, tokLt, tokLe, tokGt, tokGe:
		op := cmpOp(p.tok.kind)
		p.advance()
		right, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		return &Binary{Op: op, Left: left, Right: right}, nil
	default:
		return nil, fmt.Errorf("parse error at column %d: expected comparison, got %q", p.tok.pos+1, p.tok.raw)
	}
}

func (p *parser) parseAtom() (Expr, error) {
	if p.lx.err != nil {
		return nil, p.lx.err
	}
	switch p.tok.kind {
	case tokIdent:
		c := &Col{Name: p.tok.raw}
		p.advance()
		return c, nil
	case tokNumber, tokString, tokTrue, tokFalse, tokNull:
		lit, err := p.parseLit()
		if err != nil {
			return nil, err
		}
		return lit, nil
	default:
		return nil, fmt.Errorf("parse error at column %d: expected column or literal, got %q", p.tok.pos+1, p.tok.raw)
	}
}

func (p *parser) parseLit() (*Lit, error) {
	tok := p.tok
	p.advance()
	switch tok.kind {
	case tokNull:
		return &Lit{Kind: LitNull}, nil
	case tokTrue:
		return &Lit{Kind: LitBool, Bool: true}, nil
	case tokFalse:
		return &Lit{Kind: LitBool, Bool: false}, nil
	case tokString:
		return &Lit{Kind: LitString, Str: tok.raw}, nil
	case tokNumber:
		if strings.ContainsAny(tok.raw, ".eE") {
			f, err := parseFloat(tok.raw)
			if err != nil {
				return nil, fmt.Errorf("parse error at column %d: %w", tok.pos+1, err)
			}
			return &Lit{Kind: LitFloat, F64: f}, nil
		}
		n, err := parseInt(tok.raw)
		if err != nil {
			return nil, fmt.Errorf("parse error at column %d: %w", tok.pos+1, err)
		}
		return &Lit{Kind: LitInt, I64: n}, nil
	default:
		return nil, fmt.Errorf("parse error at column %d: expected literal", tok.pos+1)
	}
}

func (p *parser) parseList() ([]*Lit, error) {
	if err := p.expect(tokLParen, "("); err != nil {
		return nil, err
	}
	var vals []*Lit
	for {
		if p.tok.kind == tokRParen && len(vals) == 0 {
			return nil, fmt.Errorf("parse error at column %d: empty IN list", p.tok.pos+1)
		}
		lit, err := p.parseLit()
		if err != nil {
			return nil, err
		}
		vals = append(vals, lit)
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

func (p *parser) parseBetweenBounds() (*Lit, *Lit, error) {
	low, err := p.parseLit()
	if err != nil {
		return nil, nil, err
	}
	if err := p.expect(tokAnd, "AND"); err != nil {
		return nil, nil, err
	}
	high, err := p.parseLit()
	if err != nil {
		return nil, nil, err
	}
	return low, high, nil
}

func cmpOp(k tokKind) Op {
	switch k {
	case tokEq:
		return OpEq
	case tokNe:
		return OpNe
	case tokLt:
		return OpLt
	case tokLe:
		return OpLe
	case tokGt:
		return OpGt
	case tokGe:
		return OpGe
	default:
		return OpInvalid
	}
}

func parseInt(s string) (int64, error) {
	var n int64
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	if s == "" {
		return 0, fmt.Errorf("invalid integer")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid integer %q", s)
		}
		n = n*10 + int64(c-'0')
	}
	if neg {
		n = -n
	}
	return n, nil
}

func parseFloat(s string) (float64, error) {
	var ip, fp float64
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	i := strings.IndexByte(s, '.')
	if i < 0 {
		n, err := parseInt(s)
		return float64(n), err
	}
	if i > 0 {
		n, err := parseInt(s[:i])
		if err != nil {
			return 0, err
		}
		ip = float64(n)
	}
	frac := s[i+1:]
	div := 1.0
	for _, c := range frac {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid float %q", s)
		}
		div *= 10
		fp += float64(c-'0') / div
	}
	v := ip + fp
	if neg {
		v = -v
	}
	return v, nil
}
