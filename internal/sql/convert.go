package sql

import (
	"fmt"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
)

func toLit(n *Literal) *expr.Lit {
	return &expr.Lit{Kind: n.Kind, I64: n.I64, F64: n.F64, Str: n.Str, Bool: n.Bool}
}

func asLiteral(e Expr) *Literal {
	if l, ok := e.(*Literal); ok {
		return l
	}
	u, ok := e.(*Unary)
	if !ok || u.Op != "-" {
		return nil
	}
	inner, ok := u.X.(*Literal)
	if !ok {
		return nil
	}
	neg := *inner
	neg.I64 = -neg.I64
	neg.F64 = -neg.F64
	return &neg
}

// ToPred lowers a WHERE expression to the Phase-3 predicate AST.
func ToPred(e Expr) (expr.Expr, error) {
	if e == nil {
		return nil, nil
	}
	return toPred(e)
}

func toPred(e Expr) (expr.Expr, error) {
	switch n := e.(type) {
	case *Ident:
		return &expr.Col{Name: n.Name}, nil
	case *Literal:
		return toLit(n), nil
	case *Unary:
		if n.Op == "-" {
			if lit := asLiteral(n); lit != nil {
				return toLit(lit), nil
			}
			return nil, fmt.Errorf("arithmetic in WHERE not supported in v1")
		}
		if n.Op != "NOT" {
			return nil, fmt.Errorf("arithmetic in WHERE not supported in v1")
		}
		x, err := toPred(n.X)
		if err != nil {
			return nil, err
		}
		return &expr.Unary{Op: expr.OpNot, X: x}, nil
	case *Binary:
		switch n.Op {
		case "AND", "OR", "=", "<>", "<", "<=", ">", ">=":
			left, err := toPred(n.Left)
			if err != nil {
				return nil, err
			}
			right, err := toPred(n.Right)
			if err != nil {
				return nil, err
			}
			return &expr.Binary{Op: predOp(n.Op), Left: left, Right: right}, nil
		default:
			return nil, fmt.Errorf("operator %s in WHERE not supported in v1", n.Op)
		}
	case *IsNull:
		x, err := toPred(n.X)
		if err != nil {
			return nil, err
		}
		return &expr.IsNull{X: x, Not: n.Not}, nil
	case *InList:
		x, err := toPred(n.X)
		if err != nil {
			return nil, err
		}
		var vals []*expr.Lit
		for _, v := range n.Vals {
			lit := asLiteral(v)
			if lit == nil {
				return nil, fmt.Errorf("IN list must contain literals")
			}
			vals = append(vals, toLit(lit))
		}
		return &expr.InList{X: x, Vals: vals, Not: n.Not}, nil
	case *Between:
		x, err := toPred(n.X)
		if err != nil {
			return nil, err
		}
		lo, hi := asLiteral(n.Low), asLiteral(n.High)
		if lo == nil || hi == nil {
			return nil, fmt.Errorf("BETWEEN bounds must be literals")
		}
		return &expr.Between{X: x, Low: toLit(lo), High: toLit(hi), Not: n.Not}, nil
	case *Call, *Star:
		return nil, fmt.Errorf("aggregates are not allowed in WHERE")
	default:
		return nil, fmt.Errorf("unsupported WHERE expression %T", e)
	}
}

func predOp(op string) expr.Op {
	switch op {
	case "AND":
		return expr.OpAnd
	case "OR":
		return expr.OpOr
	case "=":
		return expr.OpEq
	case "<>":
		return expr.OpNe
	case "<":
		return expr.OpLt
	case "<=":
		return expr.OpLe
	case ">":
		return expr.OpGt
	case ">=":
		return expr.OpGe
	default:
		return expr.OpInvalid
	}
}

func isArith(e Expr) bool {
	switch n := e.(type) {
	case *Binary:
		switch n.Op {
		case "+", "-", "*", "/":
			return true
		}
		return isArith(n.Left) || isArith(n.Right)
	case *Unary:
		if n.Op == "-" {
			if _, ok := n.X.(*Literal); ok {
				return false
			}
			return true
		}
		return isArith(n.X)
	default:
		return false
	}
}
