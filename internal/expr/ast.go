// Package expr is a tiny predicate AST used by the vectorized filter
// (Phase 3). It is not a SQL parser — just WHERE-clause-shaped expressions.
package expr

import (
	"strconv"
	"strings"
)

type Op uint8

const (
	OpInvalid Op = iota
	OpAnd
	OpOr
	OpNot
	OpEq
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
)

func (o Op) String() string {
	switch o {
	case OpAnd:
		return "AND"
	case OpOr:
		return "OR"
	case OpNot:
		return "NOT"
	case OpEq:
		return "="
	case OpNe:
		return "<>"
	case OpLt:
		return "<"
	case OpLe:
		return "<="
	case OpGt:
		return ">"
	case OpGe:
		return ">="
	default:
		return "?"
	}
}

// Expr is a filter expression.
type Expr interface {
	isExpr()
	Columns() []string
	String() string
}

// Col is a column reference.
type Col struct{ Name string }

func (c *Col) isExpr()           {}
func (c *Col) Columns() []string { return []string{c.Name} }
func (c *Col) String() string    { return c.Name }

type LitKind uint8

const (
	LitNull LitKind = iota
	LitInt
	LitFloat
	LitString
	LitBool
)

// Lit is a constant.
type Lit struct {
	Kind LitKind
	I64  int64
	F64  float64
	Str  string
	Bool bool
}

func (l *Lit) isExpr()           {}
func (l *Lit) Columns() []string { return nil }
func (l *Lit) String() string {
	switch l.Kind {
	case LitNull:
		return "NULL"
	case LitInt:
		return strconv.FormatInt(l.I64, 10)
	case LitFloat:
		return strconv.FormatFloat(l.F64, 'g', -1, 64)
	case LitString:
		return "'" + strings.ReplaceAll(l.Str, "'", "''") + "'"
	case LitBool:
		if l.Bool {
			return "TRUE"
		}
		return "FALSE"
	default:
		return "?"
	}
}

// Binary is AND/OR or a comparison.
type Binary struct {
	Op          Op
	Left, Right Expr
}

func (b *Binary) isExpr() {}
func (b *Binary) Columns() []string {
	return uniq(append(b.Left.Columns(), b.Right.Columns()...))
}
func (b *Binary) String() string {
	return "(" + b.Left.String() + " " + b.Op.String() + " " + b.Right.String() + ")"
}

// Unary is currently only NOT.
type Unary struct {
	Op Op
	X  Expr
}

func (u *Unary) isExpr()           {}
func (u *Unary) Columns() []string { return u.X.Columns() }
func (u *Unary) String() string    { return "NOT " + u.X.String() }

// IsNull is X IS [NOT] NULL.
type IsNull struct {
	X   Expr
	Not bool
}

func (n *IsNull) isExpr()           {}
func (n *IsNull) Columns() []string { return n.X.Columns() }
func (n *IsNull) String() string {
	if n.Not {
		return n.X.String() + " IS NOT NULL"
	}
	return n.X.String() + " IS NULL"
}

// InList is X [NOT] IN (literals).
type InList struct {
	X    Expr
	Vals []*Lit
	Not  bool
}

func (in *InList) isExpr()           {}
func (in *InList) Columns() []string { return in.X.Columns() }
func (in *InList) String() string {
	parts := make([]string, len(in.Vals))
	for i, v := range in.Vals {
		parts[i] = v.String()
	}
	kw := " IN ("
	if in.Not {
		kw = " NOT IN ("
	}
	return in.X.String() + kw + strings.Join(parts, ", ") + ")"
}

// Between is X [NOT] BETWEEN low AND high.
type Between struct {
	X         Expr
	Low, High *Lit
	Not       bool
}

func (b *Between) isExpr()           {}
func (b *Between) Columns() []string { return b.X.Columns() }
func (b *Between) String() string {
	kw := " BETWEEN "
	if b.Not {
		kw = " NOT BETWEEN "
	}
	return b.X.String() + kw + b.Low.String() + " AND " + b.High.String()
}

func uniq(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func Columns(e Expr) []string {
	if e == nil {
		return nil
	}
	return e.Columns()
}
