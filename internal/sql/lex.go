package sql

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
	tokStar
	tokLParen
	tokRParen
	tokComma
	tokDot
	tokPlus
	tokMinus
	tokSlash
	tokEq
	tokNe
	tokLt
	tokLe
	tokGt
	tokGe
	tokSemi
	tokSelect
	tokAll
	tokFrom
	tokWhere
	tokGroup
	tokBy
	tokOrder
	tokLimit
	tokAs
	tokAsc
	tokDesc
	tokAnd
	tokOr
	tokNot
	tokIs
	tokIn
	tokBetween
	tokNull
	tokTrue
	tokFalse
	tokCount
	tokSum
	tokAvg
	tokMin
	tokMax
	tokTimestamp
	tokUnsupported
)

type token struct {
	kind tokKind
	pos  int
	raw  string
}

var keywords = map[string]tokKind{
	"SELECT":    tokSelect,
	"ALL":       tokAll,
	"FROM":      tokFrom,
	"WHERE":     tokWhere,
	"GROUP":     tokGroup,
	"BY":        tokBy,
	"ORDER":     tokOrder,
	"LIMIT":     tokLimit,
	"AS":        tokAs,
	"ASC":       tokAsc,
	"DESC":      tokDesc,
	"AND":       tokAnd,
	"OR":        tokOr,
	"NOT":       tokNot,
	"IS":        tokIs,
	"IN":        tokIn,
	"BETWEEN":   tokBetween,
	"NULL":      tokNull,
	"TRUE":      tokTrue,
	"FALSE":     tokFalse,
	"COUNT":     tokCount,
	"SUM":       tokSum,
	"AVG":       tokAvg,
	"MIN":       tokMin,
	"MAX":       tokMax,
	"TIMESTAMP": tokTimestamp,
	"JOIN":      tokUnsupported,
	"INNER":     tokUnsupported,
	"LEFT":      tokUnsupported,
	"RIGHT":     tokUnsupported,
	"FULL":      tokUnsupported,
	"CROSS":     tokUnsupported,
	"ON":        tokUnsupported,
	"HAVING":    tokUnsupported,
	"DISTINCT":  tokUnsupported,
	"CASE":      tokUnsupported,
	"WHEN":      tokUnsupported,
	"THEN":      tokUnsupported,
	"ELSE":      tokUnsupported,
	"END":       tokUnsupported,
	"UNION":     tokUnsupported,
	"INTERSECT": tokUnsupported,
	"EXCEPT":    tokUnsupported,
	"INSERT":    tokUnsupported,
	"UPDATE":    tokUnsupported,
	"DELETE":    tokUnsupported,
	"CREATE":    tokUnsupported,
	"WITH":      tokUnsupported,
	"WINDOW":    tokUnsupported,
	"OVER":      tokUnsupported,
	"OFFSET":    tokUnsupported,
	"FETCH":     tokUnsupported,
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

func (l *lexer) skip() {
	for l.i < len(l.s) {
		r, w := utf8.DecodeRuneInString(l.s[l.i:])
		if unicode.IsSpace(r) {
			l.i += w
			continue
		}
		if r == '-' && l.i+1 < len(l.s) && l.s[l.i+1] == '-' {
			l.i += 2
			for l.i < len(l.s) {
				r, w = utf8.DecodeRuneInString(l.s[l.i:])
				l.i += w
				if r == '\n' {
					break
				}
			}
			continue
		}
		return
	}
}

func (l *lexer) lex() token {
	l.skip()
	if l.err != nil {
		return token{kind: tokEOF, pos: l.i}
	}
	if l.i >= len(l.s) {
		return token{kind: tokEOF, pos: l.i}
	}
	pos := l.i
	r := l.peek()
	switch r {
	case '*':
		l.next()
		return token{kind: tokStar, pos: pos, raw: "*"}
	case '(':
		l.next()
		return token{kind: tokLParen, pos: pos, raw: "("}
	case ')':
		l.next()
		return token{kind: tokRParen, pos: pos, raw: ")"}
	case ',':
		l.next()
		return token{kind: tokComma, pos: pos, raw: ","}
	case '.':
		l.next()
		return token{kind: tokDot, pos: pos, raw: "."}
	case '+':
		l.next()
		return token{kind: tokPlus, pos: pos, raw: "+"}
	case '-':
		l.next()
		return token{kind: tokMinus, pos: pos, raw: "-"}
	case '/':
		l.next()
		return token{kind: tokSlash, pos: pos, raw: "/"}
	case ';':
		l.next()
		return token{kind: tokSemi, pos: pos, raw: ";"}
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
	case '"':
		return l.lexQuotedIdent(pos)
	}
	if r >= '0' && r <= '9' {
		return l.lexNumber(pos)
	}
	if isIdentStart(r) {
		return l.lexIdent(pos)
	}
	l.err = fmt.Errorf("parse error at column %d: unexpected %q", pos+1, string(r))
	return token{kind: tokEOF, pos: pos}
}

func (l *lexer) lexString(pos int) token {
	l.next()
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

func (l *lexer) lexQuotedIdent(pos int) token {
	l.next()
	var b strings.Builder
	for {
		r := l.next()
		if r == 0 {
			l.err = fmt.Errorf("parse error at column %d: unterminated quoted identifier", pos+1)
			return token{kind: tokEOF, pos: pos}
		}
		if r == '"' {
			if l.peek() == '"' {
				l.next()
				b.WriteByte('"')
				continue
			}
			return token{kind: tokIdent, pos: pos, raw: b.String()}
		}
		b.WriteRune(r)
	}
}

func (l *lexer) lexNumber(pos int) token {
	start := l.i
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
	if k, ok := keywords[up]; ok {
		return token{kind: k, pos: pos, raw: raw}
	}
	return token{kind: tokIdent, pos: pos, raw: raw}
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// Tokenize returns all tokens (excluding EOF) for lexer tests.
func Tokenize(src string) ([]string, error) {
	l := lexer{s: src}
	var out []string
	for {
		t := l.lex()
		if l.err != nil {
			return nil, l.err
		}
		if t.kind == tokEOF {
			return out, nil
		}
		label := t.raw
		if t.kind == tokUnsupported {
			label = "UNSUPPORTED:" + strings.ToUpper(t.raw)
		}
		out = append(out, label)
	}
}
