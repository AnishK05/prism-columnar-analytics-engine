package kernel

import (
	"fmt"
	"math"

	"github.com/AnishK05/prism-columnar-analytics-engine/internal/expr"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

func evalCompare(n *expr.Binary, rec arrow.Record, mem memory.Allocator) (*array.Boolean, error) {
	leftCol, leftLit, err := side(n.Left, rec)
	if err != nil {
		return nil, err
	}
	rightCol, rightLit, err := side(n.Right, rec)
	if err != nil {
		return nil, err
	}
	switch {
	case leftCol != nil && rightLit != nil:
		return compareArrayLit(mem, leftCol, n.Op, rightLit)
	case rightCol != nil && leftLit != nil:
		return compareArrayLit(mem, rightCol, swapOp(n.Op), leftLit)
	case leftCol != nil && rightCol != nil:
		return compareArrayArray(mem, leftCol, n.Op, rightCol)
	default:
		// lit vs lit: broadcast
		if leftLit != nil && rightLit != nil {
			return compareLitLit(mem, rec.NumRows(), leftLit, n.Op, rightLit)
		}
		return nil, fmt.Errorf("invalid comparison")
	}
}

func side(e expr.Expr, rec arrow.Record) (arrow.Array, *expr.Lit, error) {
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

func swapOp(op expr.Op) expr.Op {
	switch op {
	case expr.OpLt:
		return expr.OpGt
	case expr.OpLe:
		return expr.OpGe
	case expr.OpGt:
		return expr.OpLt
	case expr.OpGe:
		return expr.OpLe
	default:
		return op
	}
}

func compareArrayLit(mem memory.Allocator, arr arrow.Array, op expr.Op, lit *expr.Lit) (*array.Boolean, error) {
	if lit.Kind == expr.LitNull {
		return allNullBool(mem, arr.Len()), nil
	}
	cloned := false
	arr, cloned = unwrapDict(arr)
	if cloned {
		defer arr.Release()
	}
	switch a := arr.(type) {
	case *array.Int64:
		v, err := litAsInt(lit)
		if err != nil {
			return nil, err
		}
		return cmpInt64(mem, a, op, v)
	case *array.Int32:
		v, err := litAsInt(lit)
		if err != nil {
			return nil, err
		}
		return cmpInt32(mem, a, op, int32(v))
	case *array.Timestamp:
		v, err := litAsInt(lit)
		if err != nil {
			return nil, err
		}
		return cmpTimestamp(mem, a, op, v)
	case *array.Float64:
		v, err := litAsFloat(lit)
		if err != nil {
			return nil, err
		}
		return cmpFloat64(mem, a, op, v)
	case *array.Float32:
		v, err := litAsFloat(lit)
		if err != nil {
			return nil, err
		}
		return cmpFloat32(mem, a, op, float32(v))
	case *array.String:
		if lit.Kind != expr.LitString {
			return nil, fmt.Errorf("cannot compare utf8 column to %s", lit.String())
		}
		return cmpString(mem, a, op, lit.Str)
	case *array.Boolean:
		if lit.Kind != expr.LitBool {
			return nil, fmt.Errorf("cannot compare bool column to %s", lit.String())
		}
		return cmpBool(mem, a, op, lit.Bool)
	default:
		return nil, fmt.Errorf("unsupported column type %s for comparison", arr.DataType())
	}
}

func unwrapDict(arr arrow.Array) (arrow.Array, bool) {
	d, ok := arr.(*array.Dictionary)
	if !ok {
		return arr, false
	}
	dict := d.Dictionary()
	switch dict.(type) {
	case *array.String:
		b := array.NewStringBuilder(memory.DefaultAllocator)
		defer b.Release()
		b.Reserve(d.Len())
		for i := 0; i < d.Len(); i++ {
			if d.IsNull(i) {
				b.AppendNull()
				continue
			}
			b.Append(dict.(*array.String).Value(d.GetValueIndex(i)))
		}
		return b.NewArray(), true
	default:
		return arr, false
	}
}

func cmpInt64(mem memory.Allocator, a *array.Int64, op expr.Op, v int64) (*array.Boolean, error) {
	out := array.NewBooleanBuilder(mem)
	defer out.Release()
	n := a.Len()
	out.Reserve(n)
	for i := 0; i < n; i++ {
		if a.IsNull(i) {
			out.AppendNull()
			continue
		}
		ok, unk := cmpOrdered(int64(a.Value(i)), v, op)
		if unk {
			out.AppendNull()
			continue
		}
		out.Append(ok)
	}
	return out.NewBooleanArray(), nil
}

func cmpInt32(mem memory.Allocator, a *array.Int32, op expr.Op, v int32) (*array.Boolean, error) {
	out := array.NewBooleanBuilder(mem)
	defer out.Release()
	n := a.Len()
	out.Reserve(n)
	for i := 0; i < n; i++ {
		if a.IsNull(i) {
			out.AppendNull()
			continue
		}
		ok, unk := cmpOrdered(int64(a.Value(i)), int64(v), op)
		if unk {
			out.AppendNull()
			continue
		}
		out.Append(ok)
	}
	return out.NewBooleanArray(), nil
}

func cmpTimestamp(mem memory.Allocator, a *array.Timestamp, op expr.Op, v int64) (*array.Boolean, error) {
	out := array.NewBooleanBuilder(mem)
	defer out.Release()
	n := a.Len()
	out.Reserve(n)
	for i := 0; i < n; i++ {
		if a.IsNull(i) {
			out.AppendNull()
			continue
		}
		ok, unk := cmpOrdered(int64(a.Value(i)), v, op)
		if unk {
			out.AppendNull()
			continue
		}
		out.Append(ok)
	}
	return out.NewBooleanArray(), nil
}

func cmpFloat64(mem memory.Allocator, a *array.Float64, op expr.Op, v float64) (*array.Boolean, error) {
	out := array.NewBooleanBuilder(mem)
	defer out.Release()
	n := a.Len()
	out.Reserve(n)
	for i := 0; i < n; i++ {
		if a.IsNull(i) {
			out.AppendNull()
			continue
		}
		x := a.Value(i)
		if math.IsNaN(x) || math.IsNaN(v) {
			out.AppendNull()
			continue
		}
		ok, unk := cmpOrdered(x, v, op)
		if unk {
			out.AppendNull()
			continue
		}
		out.Append(ok)
	}
	return out.NewBooleanArray(), nil
}

func cmpFloat32(mem memory.Allocator, a *array.Float32, op expr.Op, v float32) (*array.Boolean, error) {
	out := array.NewBooleanBuilder(mem)
	defer out.Release()
	n := a.Len()
	out.Reserve(n)
	for i := 0; i < n; i++ {
		if a.IsNull(i) {
			out.AppendNull()
			continue
		}
		ok, unk := cmpOrdered(float64(a.Value(i)), float64(v), op)
		if unk {
			out.AppendNull()
			continue
		}
		out.Append(ok)
	}
	return out.NewBooleanArray(), nil
}

func cmpString(mem memory.Allocator, a *array.String, op expr.Op, v string) (*array.Boolean, error) {
	out := array.NewBooleanBuilder(mem)
	defer out.Release()
	n := a.Len()
	out.Reserve(n)
	for i := 0; i < n; i++ {
		if a.IsNull(i) {
			out.AppendNull()
			continue
		}
		ok, unk := cmpOrdered(a.Value(i), v, op)
		if unk {
			out.AppendNull()
			continue
		}
		out.Append(ok)
	}
	return out.NewBooleanArray(), nil
}

func cmpBool(mem memory.Allocator, a *array.Boolean, op expr.Op, v bool) (*array.Boolean, error) {
	if op != expr.OpEq && op != expr.OpNe {
		return nil, fmt.Errorf("bool columns only support = and <>")
	}
	out := array.NewBooleanBuilder(mem)
	defer out.Release()
	n := a.Len()
	out.Reserve(n)
	for i := 0; i < n; i++ {
		if a.IsNull(i) {
			out.AppendNull()
			continue
		}
		eq := a.Value(i) == v
		if op == expr.OpNe {
			eq = !eq
		}
		out.Append(eq)
	}
	return out.NewBooleanArray(), nil
}

func cmpOrdered[T interface{ ~int64 | ~float64 | ~string }](a, b T, op expr.Op) (val bool, unknown bool) {
	switch op {
	case expr.OpEq:
		return a == b, false
	case expr.OpNe:
		return a != b, false
	case expr.OpLt:
		return a < b, false
	case expr.OpLe:
		return a <= b, false
	case expr.OpGt:
		return a > b, false
	case expr.OpGe:
		return a >= b, false
	default:
		return false, true
	}
}

func compareArrayArray(mem memory.Allocator, left arrow.Array, op expr.Op, right arrow.Array) (*array.Boolean, error) {
	if left.Len() != right.Len() {
		return nil, fmt.Errorf("column length mismatch")
	}
	// Keep Phase 3 focused: same physical kinds only.
	l64, lok := left.(*array.Int64)
	r64, rok := right.(*array.Int64)
	if lok && rok {
		out := array.NewBooleanBuilder(mem)
		defer out.Release()
		out.Reserve(l64.Len())
		for i := 0; i < l64.Len(); i++ {
			if l64.IsNull(i) || r64.IsNull(i) {
				out.AppendNull()
				continue
			}
			ok, _ := cmpOrdered(l64.Value(i), r64.Value(i), op)
			out.Append(ok)
		}
		return out.NewBooleanArray(), nil
	}
	return nil, fmt.Errorf("column-column comparison only supports int64 in this phase")
}

func compareLitLit(mem memory.Allocator, n int64, left *expr.Lit, op expr.Op, right *expr.Lit) (*array.Boolean, error) {
	if left.Kind == expr.LitNull || right.Kind == expr.LitNull {
		return allNullBool(mem, int(n)), nil
	}
	var val bool
	switch left.Kind {
	case expr.LitInt:
		r, err := litAsInt(right)
		if err != nil {
			return nil, err
		}
		val, _ = cmpOrdered(left.I64, r, op)
	case expr.LitFloat:
		r, err := litAsFloat(right)
		if err != nil {
			return nil, err
		}
		val, _ = cmpOrdered(left.F64, r, op)
	case expr.LitString:
		if right.Kind != expr.LitString {
			return nil, fmt.Errorf("type mismatch in literal comparison")
		}
		val, _ = cmpOrdered(left.Str, right.Str, op)
	case expr.LitBool:
		if right.Kind != expr.LitBool || (op != expr.OpEq && op != expr.OpNe) {
			return nil, fmt.Errorf("invalid bool literal comparison")
		}
		val = left.Bool == right.Bool
		if op == expr.OpNe {
			val = !val
		}
	default:
		return nil, fmt.Errorf("unsupported literal comparison")
	}
	out := array.NewBooleanBuilder(mem)
	defer out.Release()
	out.Reserve(int(n))
	for i := int64(0); i < n; i++ {
		out.Append(val)
	}
	return out.NewBooleanArray(), nil
}

func allNullBool(mem memory.Allocator, n int) *array.Boolean {
	out := array.NewBooleanBuilder(mem)
	defer out.Release()
	out.Reserve(n)
	for i := 0; i < n; i++ {
		out.AppendNull()
	}
	return out.NewBooleanArray()
}

func litAsInt(l *expr.Lit) (int64, error) {
	switch l.Kind {
	case expr.LitInt:
		return l.I64, nil
	case expr.LitFloat:
		return int64(l.F64), nil
	default:
		return 0, fmt.Errorf("expected numeric literal, got %s", l.String())
	}
}

func litAsFloat(l *expr.Lit) (float64, error) {
	switch l.Kind {
	case expr.LitFloat:
		return l.F64, nil
	case expr.LitInt:
		return float64(l.I64), nil
	default:
		return 0, fmt.Errorf("expected numeric literal, got %s", l.String())
	}
}
