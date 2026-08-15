package kernel

import (
	"fmt"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Eval returns a boolean mask for expr over rec.
//
// SQL three-valued logic: NULL comparisons are unknown (null in the mask),
// not true. Compact keeps only true slots.
func Eval(e expr.Expr, rec arrow.Record) (*array.Boolean, error) {
	if e == nil {
		return nil, fmt.Errorf("nil expression")
	}
	if rec == nil {
		return nil, fmt.Errorf("nil record")
	}
	return eval(e, rec, memory.DefaultAllocator)
}

func eval(e expr.Expr, rec arrow.Record, mem memory.Allocator) (*array.Boolean, error) {
	switch n := e.(type) {
	case *expr.Binary:
		switch n.Op {
		case expr.OpAnd, expr.OpOr:
			left, err := eval(n.Left, rec, mem)
			if err != nil {
				return nil, err
			}
			defer left.Release()
			right, err := eval(n.Right, rec, mem)
			if err != nil {
				return nil, err
			}
			defer right.Release()
			if n.Op == expr.OpAnd {
				return combineBool(mem, left, right, true), nil
			}
			return combineBool(mem, left, right, false), nil
		case expr.OpEq, expr.OpNe, expr.OpLt, expr.OpLe, expr.OpGt, expr.OpGe:
			return evalCompare(n, rec, mem)
		default:
			return nil, fmt.Errorf("unsupported op %s", n.Op)
		}
	case *expr.Unary:
		if n.Op != expr.OpNot {
			return nil, fmt.Errorf("unsupported unary %s", n.Op)
		}
		x, err := eval(n.X, rec, mem)
		if err != nil {
			return nil, err
		}
		defer x.Release()
		return notBool(mem, x), nil
	case *expr.IsNull:
		return evalIsNull(n, rec, mem)
	case *expr.InList:
		return evalIn(n, rec, mem)
	case *expr.Between:
		return evalBetween(n, rec, mem)
	default:
		return nil, fmt.Errorf("expression %T is not a predicate", e)
	}
}

func combineBool(mem memory.Allocator, a, b *array.Boolean, and bool) *array.Boolean {
	n := a.Len()
	out := array.NewBooleanBuilder(mem)
	defer out.Release()
	out.Reserve(n)
	for i := 0; i < n; i++ {
		aNull, bNull := a.IsNull(i), b.IsNull(i)
		aVal, bVal := false, false
		if !aNull {
			aVal = a.Value(i)
		}
		if !bNull {
			bVal = b.Value(i)
		}
		if and {
			// FALSE wins; TRUE only if both TRUE; else UNKNOWN
			if (!aNull && !aVal) || (!bNull && !bVal) {
				out.Append(false)
				continue
			}
			if aNull || bNull {
				out.AppendNull()
				continue
			}
			out.Append(true)
			continue
		}
		// OR: TRUE wins; FALSE only if both FALSE; else UNKNOWN
		if (!aNull && aVal) || (!bNull && bVal) {
			out.Append(true)
			continue
		}
		if aNull || bNull {
			out.AppendNull()
			continue
		}
		out.Append(false)
	}
	return out.NewBooleanArray()
}

func notBool(mem memory.Allocator, a *array.Boolean) *array.Boolean {
	out := array.NewBooleanBuilder(mem)
	defer out.Release()
	n := a.Len()
	out.Reserve(n)
	for i := 0; i < n; i++ {
		if a.IsNull(i) {
			out.AppendNull()
			continue
		}
		out.Append(!a.Value(i))
	}
	return out.NewBooleanArray()
}

func colArray(rec arrow.Record, name string) (arrow.Array, error) {
	idx := rec.Schema().FieldIndices(name)
	if len(idx) == 0 {
		return nil, fmt.Errorf("unknown column %q", name)
	}
	return rec.Column(idx[0]), nil
}

func evalIsNull(n *expr.IsNull, rec arrow.Record, mem memory.Allocator) (*array.Boolean, error) {
	col, ok := n.X.(*expr.Col)
	if !ok {
		return nil, fmt.Errorf("IS NULL requires a column")
	}
	arr, err := colArray(rec, col.Name)
	if err != nil {
		return nil, err
	}
	out := array.NewBooleanBuilder(mem)
	defer out.Release()
	out.Reserve(arr.Len())
	for i := 0; i < arr.Len(); i++ {
		isNull := arr.IsNull(i)
		if n.Not {
			out.Append(!isNull)
		} else {
			out.Append(isNull)
		}
	}
	return out.NewBooleanArray(), nil
}

func evalIn(n *expr.InList, rec arrow.Record, mem memory.Allocator) (*array.Boolean, error) {
	var acc *array.Boolean
	for i, v := range n.Vals {
		eq := &expr.Binary{Op: expr.OpEq, Left: n.X, Right: v}
		part, err := evalCompare(eq, rec, mem)
		if err != nil {
			if acc != nil {
				acc.Release()
			}
			return nil, err
		}
		if i == 0 {
			acc = part
			continue
		}
		merged := combineBool(mem, acc, part, false)
		acc.Release()
		part.Release()
		acc = merged
	}
	if n.Not {
		neg := notBool(mem, acc)
		acc.Release()
		return neg, nil
	}
	return acc, nil
}

func evalBetween(n *expr.Between, rec arrow.Record, mem memory.Allocator) (*array.Boolean, error) {
	lo := &expr.Binary{Op: expr.OpGe, Left: n.X, Right: n.Low}
	hi := &expr.Binary{Op: expr.OpLe, Left: n.X, Right: n.High}
	a, err := evalCompare(lo, rec, mem)
	if err != nil {
		return nil, err
	}
	defer a.Release()
	b, err := evalCompare(hi, rec, mem)
	if err != nil {
		return nil, err
	}
	defer b.Release()
	and := combineBool(mem, a, b, true)
	if n.Not {
		neg := notBool(mem, and)
		and.Release()
		return neg, nil
	}
	return and, nil
}
