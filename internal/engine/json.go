package engine

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// JSONResult is the HTTP/UI payload from §13.
type JSONResult struct {
	Columns   []string `json:"columns"`
	Types     []string `json:"types"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated"`
	Profile   Profile  `json:"profile"`
}

const defaultJSONCap int64 = 1000

// JSON encodes the result. Cap is applied if ReturnLimit was not already.
func (r *Result) JSON() ([]byte, error) {
	return r.JSONCap(defaultJSONCap)
}

// JSONCap encodes at most cap rows (0 = all remaining).
func (r *Result) JSONCap(capRows int64) ([]byte, error) {
	js := JSONResult{Truncated: r.Truncated, Profile: r.Profile}
	if r.Record != nil {
		ncols := int(r.Record.NumCols())
		js.Columns = make([]string, ncols)
		js.Types = make([]string, ncols)
		for i := 0; i < ncols; i++ {
			js.Columns[i] = r.Record.ColumnName(i)
			js.Types[i] = r.Record.Schema().Field(i).Type.String()
		}
		rows := r.Record.NumRows()
		if capRows > 0 && rows > capRows {
			rows = capRows
			js.Truncated = true
		}
		js.Rows = make([][]any, rows)
		for i := int64(0); i < rows; i++ {
			row := make([]any, ncols)
			for c := 0; c < ncols; c++ {
				row[c] = jsonCell(r.Record.Column(c), int(i))
			}
			js.Rows[i] = row
		}
	}
	return json.MarshalIndent(js, "", "  ")
}

func jsonCell(col arrow.Array, row int) any {
	if col.IsNull(row) {
		return nil
	}
	switch a := col.(type) {
	case *array.Int64:
		return a.Value(row)
	case *array.Int32:
		return int64(a.Value(row))
	case *array.Float64:
		return a.Value(row)
	case *array.Float32:
		return float64(a.Value(row))
	case *array.Boolean:
		return a.Value(row)
	case *array.String:
		return a.Value(row)
	case *array.Binary:
		return string(a.Value(row))
	case *array.Timestamp:
		return a.Value(row).ToTime(a.DataType().(*arrow.TimestampType).Unit).UTC().Format(time.RFC3339Nano)
	case *array.Date32:
		return a.Value(row).ToTime().UTC().Format("2006-01-02")
	case *array.Dictionary:
		return jsonCell(a.Dictionary(), a.GetValueIndex(row))
	default:
		return col.ValueStr(row)
	}
}

// Fingerprint is a stable row encoding for multiset comparison.
func Fingerprint(rec arrow.Record, ordered bool) []string {
	if rec == nil {
		return nil
	}
	n := rec.NumRows()
	out := make([]string, n)
	for i := int64(0); i < n; i++ {
		s := ""
		for c := 0; c < int(rec.NumCols()); c++ {
			if c > 0 {
				s += "\t"
			}
			s += rec.ColumnName(c) + "=" + rec.Column(c).ValueStr(int(i))
		}
		out[i] = s
	}
	if !ordered {
		sort.Strings(out)
	}
	return out
}
