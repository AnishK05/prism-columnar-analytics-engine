// Package sql is the Phase-5 Prism SQL frontend: lexer, parser, AST, binder.
package sql

import (
	"strconv"
	"strings"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
)

// Query is a single SELECT statement.
type Query struct {
	All     bool
	Items   []SelectItem
	From    string
	Where   Expr
	GroupBy []Expr
	OrderBy []OrderItem
	Limit   *int64
}

func (q *Query) String() string {
	var b strings.Builder
	b.WriteString("SELECT ")
	if q.All {
		b.WriteString("ALL ")
	}
	for i, it := range q.Items {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(it.String())
	}
	b.WriteString("\nFROM ")
	b.WriteString(q.From)
	if q.Where != nil {
		b.WriteString("\nWHERE ")
		b.WriteString(q.Where.String())
	}
	if len(q.GroupBy) > 0 {
		b.WriteString("\nGROUP BY ")
		for i, e := range q.GroupBy {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(e.String())
		}
	}
	if len(q.OrderBy) > 0 {
		b.WriteString("\nORDER BY ")
		for i, o := range q.OrderBy {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(o.String())
		}
	}
	if q.Limit != nil {
		b.WriteString("\nLIMIT ")
		b.WriteString(strconv.FormatInt(*q.Limit, 10))
	}
	return b.String()
}

// SelectItem is one SELECT list entry.
type SelectItem struct {
	Star  bool
	Expr  Expr
	Alias string
}

func (s SelectItem) String() string {
	if s.Star {
		return "*"
	}
	out := s.Expr.String()
	if s.Alias != "" {
		out += " AS " + s.Alias
	}
	return out
}

// OrderItem is one ORDER BY entry.
type OrderItem struct {
	Expr Expr
	Desc bool
}

func (o OrderItem) String() string {
	if o.Desc {
		return o.Expr.String() + " DESC"
	}
	return o.Expr.String() + " ASC"
}

// Expr is a SQL expression node.
type Expr interface {
	sqlExpr()
	String() string
}

// Ident is a column or table identifier.
type Ident struct{ Name string }

func (*Ident) sqlExpr()         {}
func (i *Ident) String() string { return i.Name }

// Literal is a constant.
type Literal struct {
	Kind expr.LitKind
	I64  int64
	F64  float64
	Str  string
	Bool bool
	// Timestamp is true when the literal came from TIMESTAMP '...' (I64 is unix ms).
	Timestamp bool
}

func (*Literal) sqlExpr() {}
func (l *Literal) String() string {
	if l.Timestamp {
		return "TIMESTAMP '" + l.Str + "'"
	}
	switch l.Kind {
	case expr.LitNull:
		return "NULL"
	case expr.LitInt:
		return strconv.FormatInt(l.I64, 10)
	case expr.LitFloat:
		return strconv.FormatFloat(l.F64, 'g', -1, 64)
	case expr.LitString:
		return "'" + strings.ReplaceAll(l.Str, "'", "''") + "'"
	case expr.LitBool:
		if l.Bool {
			return "TRUE"
		}
		return "FALSE"
	default:
		return "?"
	}
}

// Star is *.
type Star struct{}

func (*Star) sqlExpr()       {}
func (*Star) String() string { return "*" }

// Binary is a binary operator.
type Binary struct {
	Op          string
	Left, Right Expr
}

func (*Binary) sqlExpr() {}
func (b *Binary) String() string {
	return "(" + b.Left.String() + " " + b.Op + " " + b.Right.String() + ")"
}

// Unary is NOT or unary minus.
type Unary struct {
	Op string
	X  Expr
}

func (*Unary) sqlExpr()         {}
func (u *Unary) String() string { return u.Op + " " + u.X.String() }

// IsNull is X IS [NOT] NULL.
type IsNull struct {
	X   Expr
	Not bool
}

func (*IsNull) sqlExpr() {}
func (n *IsNull) String() string {
	if n.Not {
		return n.X.String() + " IS NOT NULL"
	}
	return n.X.String() + " IS NULL"
}

// InList is X [NOT] IN (...).
type InList struct {
	X    Expr
	Vals []Expr
	Not  bool
}

func (*InList) sqlExpr() {}
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
	Low, High Expr
	Not       bool
}

func (*Between) sqlExpr() {}
func (b *Between) String() string {
	kw := " BETWEEN "
	if b.Not {
		kw = " NOT BETWEEN "
	}
	return b.X.String() + kw + b.Low.String() + " AND " + b.High.String()
}

// Call is COUNT/SUM/AVG/MIN/MAX(...).
type Call struct {
	Name string
	Star bool
	Args []Expr
}

func (*Call) sqlExpr() {}
func (c *Call) String() string {
	if c.Star {
		return strings.ToUpper(c.Name) + "(*)"
	}
	parts := make([]string, len(c.Args))
	for i, a := range c.Args {
		parts[i] = a.String()
	}
	return strings.ToUpper(c.Name) + "(" + strings.Join(parts, ", ") + ")"
}
