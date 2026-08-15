package kernel

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Compact gathers rows where mask is valid and true (UNKNOWN/false dropped).
func Compact(rec arrow.Record, mask *array.Boolean) (arrow.Record, error) {
	if rec.NumRows() != int64(mask.Len()) {
		return nil, fmt.Errorf("filter length %d != batch rows %d", mask.Len(), rec.NumRows())
	}
	idx := make([]int, 0, mask.Len())
	for i := 0; i < mask.Len(); i++ {
		if mask.IsValid(i) && mask.Value(i) {
			idx = append(idx, i)
		}
	}
	cols := make([]arrow.Array, rec.NumCols())
	for c := 0; c < int(rec.NumCols()); c++ {
		taken, err := takeArray(rec.Column(c), idx)
		if err != nil {
			for _, col := range cols {
				if col != nil {
					col.Release()
				}
			}
			return nil, err
		}
		cols[c] = taken
	}
	return array.NewRecord(rec.Schema(), cols, int64(len(idx))), nil
}

func takeArray(arr arrow.Array, idx []int) (arrow.Array, error) {
	mem := memory.DefaultAllocator
	switch a := arr.(type) {
	case *array.Int64:
		b := array.NewInt64Builder(mem)
		defer b.Release()
		b.Reserve(len(idx))
		for _, i := range idx {
			if a.IsNull(i) {
				b.AppendNull()
			} else {
				b.Append(a.Value(i))
			}
		}
		return b.NewArray(), nil
	case *array.Int32:
		b := array.NewInt32Builder(mem)
		defer b.Release()
		b.Reserve(len(idx))
		for _, i := range idx {
			if a.IsNull(i) {
				b.AppendNull()
			} else {
				b.Append(a.Value(i))
			}
		}
		return b.NewArray(), nil
	case *array.Float64:
		b := array.NewFloat64Builder(mem)
		defer b.Release()
		b.Reserve(len(idx))
		for _, i := range idx {
			if a.IsNull(i) {
				b.AppendNull()
			} else {
				b.Append(a.Value(i))
			}
		}
		return b.NewArray(), nil
	case *array.Boolean:
		b := array.NewBooleanBuilder(mem)
		defer b.Release()
		b.Reserve(len(idx))
		for _, i := range idx {
			if a.IsNull(i) {
				b.AppendNull()
			} else {
				b.Append(a.Value(i))
			}
		}
		return b.NewArray(), nil
	case *array.String:
		b := array.NewStringBuilder(mem)
		defer b.Release()
		b.Reserve(len(idx))
		for _, i := range idx {
			if a.IsNull(i) {
				b.AppendNull()
			} else {
				b.Append(a.Value(i))
			}
		}
		return b.NewArray(), nil
	case *array.Timestamp:
		b := array.NewTimestampBuilder(mem, a.DataType().(*arrow.TimestampType))
		defer b.Release()
		b.Reserve(len(idx))
		for _, i := range idx {
			if a.IsNull(i) {
				b.AppendNull()
			} else {
				b.Append(a.Value(i))
			}
		}
		return b.NewArray(), nil
	case *array.Dictionary:
		decoded, cloned := unwrapDict(a)
		if cloned {
			defer decoded.Release()
		}
		return takeArray(decoded, idx)
	default:
		return nil, fmt.Errorf("compact: unsupported type %s", arr.DataType())
	}
}

// Project keeps named columns in order. Empty names returns rec unchanged (retained).
func Project(rec arrow.Record, names []string) (arrow.Record, error) {
	if len(names) == 0 {
		rec.Retain()
		return rec, nil
	}
	fields := make([]arrow.Field, len(names))
	cols := make([]arrow.Array, len(names))
	for i, name := range names {
		idx := rec.Schema().FieldIndices(name)
		if len(idx) == 0 {
			return nil, fmt.Errorf("unknown column %q", name)
		}
		fields[i] = rec.Schema().Field(idx[0])
		col := rec.Column(idx[0])
		col.Retain()
		cols[i] = col
	}
	md := rec.Schema().Metadata()
	schema := arrow.NewSchema(fields, &md)
	return array.NewRecord(schema, cols, rec.NumRows()), nil
}

// CountTrue counts valid-true slots in a mask.
func CountTrue(mask *array.Boolean) int64 {
	var n int64
	for i := 0; i < mask.Len(); i++ {
		if mask.IsValid(i) && mask.Value(i) {
			n++
		}
	}
	return n
}
