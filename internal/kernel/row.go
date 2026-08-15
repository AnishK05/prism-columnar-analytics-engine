package kernel

import (
	"fmt"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
	"github.com/apache/arrow-go/v18/arrow"
)

// tri is SQL three-valued logic: false, true, unknown.
type tri uint8

const (
	triFalse tri = iota
	triTrue
	triUnk
)

// KeepRow is the row-at-a-time filter: true only when the predicate is SQL TRUE.
func KeepRow(e expr.Expr, rec arrow.Record, row int) (bool, error) {
	if e == nil {
		return true, nil
	}
	t, err := evalRow(e, rec, row)
	if err != nil {
		return false, err
	}
	return t == triTrue, nil
}

func evalRow(e expr.Expr, rec arrow.Record, row int) (tri, error) {
	switch n := e.(type) {
	case *expr.Binary:
		switch n.Op {
		case expr.OpAnd:
			l, err := evalRow(n.Left, rec, row)
			if err != nil {
				return 0, err
			}
			if l == triFalse {
				return triFalse, nil
			}
			r, err := evalRow(n.Right, rec, row)
			if err != nil {
				return 0, err
			}
			if r == triFalse || l == triFalse {
				return triFalse, nil
			}
			if l == triUnk || r == triUnk {
				return triUnk, nil
			}
			return triTrue, nil
		case expr.OpOr:
			l, err := evalRow(n.Left, rec, row)
			if err != nil {
				return 0, err
			}
			if l == triTrue {
				return triTrue, nil
			}
			r, err := evalRow(n.Right, rec, row)
			if err != nil {
				return 0, err
			}
			if l == triTrue || r == triTrue {
				return triTrue, nil
			}
			if l == triUnk || r == triUnk {
				return triUnk, nil
			}
			return triFalse, nil
		case expr.OpEq, expr.OpNe, expr.OpLt, expr.OpLe, expr.OpGt, expr.OpGe:
			return evalRowCompare(n, rec, row)
		default:
			return 0, fmt.Errorf("unsupported op %s", n.Op)
		}
	case *expr.Unary:
		if n.Op != expr.OpNot {
			return 0, fmt.Errorf("unsupported unary %s", n.Op)
		}
		x, err := evalRow(n.X, rec, row)
		if err != nil {
			return 0, err
		}
		if x == triUnk {
			return triUnk, nil
		}
		if x == triTrue {
			return triFalse, nil
		}
		return triTrue, nil
	case *expr.IsNull:
		col, ok := n.X.(*expr.Col)
		if !ok {
			return 0, fmt.Errorf("IS NULL requires a column")
		}
		arr, err := colArray(rec, col.Name)
		if err != nil {
			return 0, err
		}
		isNull := arr.IsNull(row)
		if n.Not {
			isNull = !isNull
		}
		if isNull {
			return triTrue, nil
		}
		return triFalse, nil
	case *expr.InList:
		var anyTrue, anyUnk bool
		for _, v := range n.Vals {
			eq := &expr.Binary{Op: expr.OpEq, Left: n.X, Right: v}
			t, err := evalRowCompare(eq, rec, row)
			if err != nil {
				return 0, err
			}
			if t == triTrue {
				anyTrue = true
			}
			if t == triUnk {
				anyUnk = true
			}
		}
		out := triFalse
		if anyTrue {
			out = triTrue
		} else if anyUnk {
			out = triUnk
		}
		if n.Not {
			if out == triTrue {
				return triFalse, nil
			}
			if out == triFalse {
				return triTrue, nil
			}
			return triUnk, nil
		}
		return out, nil
	case *expr.Between:
		lo := &expr.Binary{Op: expr.OpGe, Left: n.X, Right: n.Low}
		hi := &expr.Binary{Op: expr.OpLe, Left: n.X, Right: n.High}
		a, err := evalRowCompare(lo, rec, row)
		if err != nil {
			return 0, err
		}
		b, err := evalRowCompare(hi, rec, row)
		if err != nil {
			return 0, err
		}
		if a == triFalse || b == triFalse {
			if n.Not {
				return triTrue, nil
			}
			return triFalse, nil
		}
		if a == triUnk || b == triUnk {
			if n.Not {
				return triUnk, nil
			}
			return triUnk, nil
		}
		if n.Not {
			return triFalse, nil
		}
		return triTrue, nil
	case *expr.Lit:
		if n.Kind == expr.LitNull {
			return triUnk, nil
		}
		if n.Kind == expr.LitBool {
			if n.Bool {
				return triTrue, nil
			}
			return triFalse, nil
		}
		return 0, fmt.Errorf("literal %s is not a predicate", n.String())
	default:
		return 0, fmt.Errorf("expression %T is not a predicate", e)
	}
}

func evalRowCompare(n *expr.Binary, rec arrow.Record, row int) (tri, error) {
	lc, ll, err := rowSide(n.Left, rec, row)
	if err != nil {
		return 0, err
	}
	rc, rl, err := rowSide(n.Right, rec, row)
	if err != nil {
		return 0, err
	}
	var left, right cell
	switch {
	case lc != nil && rl != nil:
		left, err = cellFromArray(lc, row)
		if err != nil {
			return 0, err
		}
		right, err = cellFromLit(rl, left.kind)
		if err != nil {
			return 0, err
		}
	case rc != nil && ll != nil:
		right, err = cellFromArray(rc, row)
		if err != nil {
			return 0, err
		}
		left, err = cellFromLit(ll, right.kind)
		if err != nil {
			return 0, err
		}
	case lc != nil && rc != nil:
		left, err = cellFromArray(lc, row)
		if err != nil {
			return 0, err
		}
		right, err = cellFromArray(rc, row)
		if err != nil {
			return 0, err
		}
	case ll != nil && rl != nil:
		left, err = cellFromLit(ll, cellNone)
		if err != nil {
			return 0, err
		}
		right, err = cellFromLit(rl, left.kind)
		if err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("invalid comparison")
	}
	if left.kind == cellNull || right.kind == cellNull {
		return triUnk, nil
	}
	cmp := cmpCellTyped(left, right)
	ok := false
	switch n.Op {
	case expr.OpEq:
		ok = cmp == 0
	case expr.OpNe:
		ok = cmp != 0
	case expr.OpLt:
		ok = cmp < 0
	case expr.OpLe:
		ok = cmp <= 0
	case expr.OpGt:
		ok = cmp > 0
	case expr.OpGe:
		ok = cmp >= 0
	}
	if ok {
		return triTrue, nil
	}
	return triFalse, nil
}

func rowSide(e expr.Expr, rec arrow.Record, row int) (arrow.Array, *expr.Lit, error) {
	_ = row
	switch n := e.(type) {
	case *expr.Col:
		arr, err := colArray(rec, n.Name)
		return arr, nil, err
	case *expr.Lit:
		return nil, n, nil
	default:
		return nil, nil, fmt.Errorf("comparison operand must be a column or literal, got %T", e)
	}
}

const cellNone cellKind = 255

func cellFromLit(l *expr.Lit, hint cellKind) (cell, error) {
	if l == nil {
		return cell{kind: cellNull}, nil
	}
	switch l.Kind {
	case expr.LitNull:
		return cell{kind: cellNull}, nil
	case expr.LitInt:
		if hint == cellF64 {
			return cell{kind: cellF64, f64: float64(l.I64)}, nil
		}
		return cell{kind: cellI64, i64: l.I64}, nil
	case expr.LitFloat:
		return cell{kind: cellF64, f64: l.F64}, nil
	case expr.LitString:
		return cell{kind: cellStr, str: l.Str}, nil
	case expr.LitBool:
		return cell{kind: cellBool, b: l.Bool}, nil
	default:
		return cell{}, fmt.Errorf("unsupported literal")
	}
}

func cmpCellTyped(a, b cell) int {
	if a.kind == cellI64 && b.kind == cellF64 {
		a = cell{kind: cellF64, f64: float64(a.i64)}
	}
	if a.kind == cellF64 && b.kind == cellI64 {
		b = cell{kind: cellF64, f64: float64(b.i64)}
	}
	return cmpCell(a, b)
}
