package kernel

import (
	"fmt"
	"math"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

type cellKind uint8

const (
	cellNull cellKind = iota
	cellI64
	cellF64
	cellStr
	cellBool
)

// cell is a detached scalar used for hash keys and sort comparisons.
type cell struct {
	kind cellKind
	i64  int64
	f64  float64
	str  string
	b    bool
}

func cellFromArray(arr arrow.Array, row int) (cell, error) {
	if arr.IsNull(row) {
		return cell{kind: cellNull}, nil
	}
	cloned := false
	arr, cloned = unwrapDict(arr)
	if cloned {
		defer arr.Release()
	}
	switch a := arr.(type) {
	case *array.Int64:
		return cell{kind: cellI64, i64: a.Value(row)}, nil
	case *array.Int32:
		return cell{kind: cellI64, i64: int64(a.Value(row))}, nil
	case *array.Timestamp:
		return cell{kind: cellI64, i64: int64(a.Value(row))}, nil
	case *array.Float64:
		return cell{kind: cellF64, f64: a.Value(row)}, nil
	case *array.Float32:
		return cell{kind: cellF64, f64: float64(a.Value(row))}, nil
	case *array.String:
		return cell{kind: cellStr, str: a.Value(row)}, nil
	case *array.Boolean:
		return cell{kind: cellBool, b: a.Value(row)}, nil
	default:
		return cell{}, fmt.Errorf("unsupported type %s", arr.DataType())
	}
}

func cmpCell(a, b cell) int {
	if a.kind == cellNull && b.kind == cellNull {
		return 0
	}
	// Postgres: NULL sorts as larger than any non-null (ASC → nulls last).
	if a.kind == cellNull {
		return 1
	}
	if b.kind == cellNull {
		return -1
	}
	if a.kind != b.kind {
		if a.kind < b.kind {
			return -1
		}
		return 1
	}
	switch a.kind {
	case cellI64:
		switch {
		case a.i64 < b.i64:
			return -1
		case a.i64 > b.i64:
			return 1
		}
	case cellF64:
		switch {
		case a.f64 < b.f64:
			return -1
		case a.f64 > b.f64:
			return 1
		}
	case cellStr:
		if a.str < b.str {
			return -1
		}
		if a.str > b.str {
			return 1
		}
	case cellBool:
		ai, bi := 0, 0
		if a.b {
			ai = 1
		}
		if b.b {
			bi = 1
		}
		return ai - bi
	}
	return 0
}

func appendCellKey(buf []byte, c cell) []byte {
	buf = append(buf, byte(c.kind))
	switch c.kind {
	case cellI64:
		return appendU64(buf, uint64(c.i64))
	case cellF64:
		return appendU64(buf, math.Float64bits(c.f64))
	case cellBool:
		if c.b {
			return append(buf, 1)
		}
		return append(buf, 0)
	case cellStr:
		buf = appendU32(buf, uint32(len(c.str)))
		return append(buf, c.str...)
	}
	return buf
}

func appendU64(buf []byte, v uint64) []byte {
	return append(buf,
		byte(v>>56), byte(v>>48), byte(v>>40), byte(v>>32),
		byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func appendU32(buf []byte, v uint32) []byte {
	return append(buf, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

func colByName(rec arrow.Record, name string) (arrow.Array, error) {
	idx := rec.Schema().FieldIndices(name)
	if len(idx) == 0 {
		return nil, fmt.Errorf("unknown column %q", name)
	}
	return rec.Column(idx[0]), nil
}
